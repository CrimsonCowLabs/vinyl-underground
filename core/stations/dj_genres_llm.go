package stations

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// With an LLM, a Listener's DJ's genres are the Library genres the LLM finds in its name and
// description (#107), which reads intent where word matching can't ("Pump the house music"
// is Electronica & Dance, Dance, Electronica, House, Club-House). The answer only keeps
// genres the Library has; when the LLM is down, slow or names none of them, the genres are
// word-matched instead (deriveGenres) and the LLM is asked again on the DJ's next use: the next
// time it plays, in the background so the Station never waits on it, or is opened in the form.
// Genres picked by hand are never mapped again, unless the Listener asks for a new suggestion.
//
// The LLM's host is slow, so each Listener has one mapping running at a time, a background one
// waits for their Station selection under way, and their answers are kept a while: saving the
// name and description the form just previewed reuses the preview's answer, and a DJ the LLM
// found nothing for isn't asked about at every play. Blurbs are written alongside, as they are
// alongside selections.

const (
	// genreMapTimeout bounds a mapping the Listener waits for (saving or opening a DJ, or the
	// form's preview); past it, the genres are word-matched and the LLM is asked again later.
	genreMapTimeout = 30 * time.Second
	// genreBackfillTimeout bounds a mapping in the background, as a backstop.
	genreBackfillTimeout = 3 * time.Minute
	// maxPromptGenres is how many of the Library's genres, the most common, the LLM chooses
	// from, so the prompt stays short for a small model.
	maxPromptGenres = 150
	// maxMappedGenres is the most genres the LLM gives a DJ.
	maxMappedGenres = 12
	// keptGenreAnswers is how many of the LLM's answers each Listener keeps, and genreAnswerAge
	// for how long, so a DJ the LLM found no genres for isn't asked about at every play.
	keptGenreAnswers = 20
	genreAnswerAge   = time.Hour
)

var genreSystem = fmt.Sprintf(`You match a music Station's DJ to genres from a listener's music library.
From the library's genre list, pick the genres whose music fits the DJ's name and description, broader ones included (a house DJ also fits "Dance" and "Electronica").
Use the list's exact names. Pick at most %d, best first. Answer an empty list if none fit.`, maxMappedGenres)

var genreSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"genres": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": maxMappedGenres},
	},
	"required":             []string{"genres"},
	"additionalProperties": false,
}

// Option configures a Service.
type Option func(*service)

// WithGenreLLM has the LLM map a Listener's DJs to the Library's genres. Without it, their
// genres are matched from the words of their name and description.
func WithGenreLLM(client llm.Client) Option {
	return func(s *service) {
		if client != nil {
			s.genreLLM = &genreMapper{client: client}
		}
	}
}

// genreMapper asks the LLM for a DJ's genres, one call at a time per Listener.
type genreMapper struct {
	client llm.Client
	mu     sync.Mutex
	// answers holds each Listener's latest answers, newest first.
	answers map[string][]genreAnswer
	// failedAt is when each Listener's last mapping failed, if it did and none succeeded since.
	// Until retryAfter has passed, their DJs are word-matched without asking.
	failedAt map[string]time.Time
	// backfilling holds the Listeners with a background mapping under way.
	backfilling map[string]bool
	// backoff is how long after a failure a Listener's mappings wait: retryAfter, but shorter
	// in tests.
	backoff     time.Duration
	turns       sync.Map       // Listener ID -> chan struct{}, the one LLM call they may have running
	wg          sync.WaitGroup // background mappings, for tests
}

// genreAnswer is the genres the LLM named for a DJ's name and description.
type genreAnswer struct {
	text   string
	genres []string
	at     time.Time
}

// initMaps makes the maps. m.mu must be held.
func (m *genreMapper) initMaps() {
	if m.answers == nil {
		m.answers = map[string][]genreAnswer{}
		m.failedAt = map[string]time.Time{}
		m.backfilling = map[string]bool{}
	}
}

// genreText is what the LLM maps: the DJ's name and description.
func genreText(name, description string) string {
	return name + "\n" + description
}

// cached returns the LLM's recent answer for the text, if it has one. m.mu must be held.
func (m *genreMapper) cached(userID, text string, now time.Time) ([]string, bool) {
	for _, a := range m.answers[userID] {
		if a.text == text && now.Sub(a.at) < genreAnswerAge {
			return a.genres, true
		}
	}
	return nil, false
}

// failedRecently reports whether the Listener's last mapping failed within retryAfter. m.mu
// must be held.
func (m *genreMapper) failedRecently(userID string, now time.Time) bool {
	at, ok := m.failedAt[userID]
	return ok && now.Sub(at) < cmp.Or(m.backoff, retryAfter)
}

// mapGenres returns the Library genres the LLM finds in the DJ's name and description, and
// whether it answered (now or lately). It doesn't ask when the Listener's last mapping failed
// within retryAfter, and waits for any mapping of theirs already under way.
func (m *genreMapper) mapGenres(ctx context.Context, userID, name, description string, prompt, library []string) (model.DJGenres, bool) {
	text := genreText(name, description)
	m.mu.Lock()
	answer, ok := m.cached(userID, text, time.Now())
	failed := m.failedRecently(userID, time.Now())
	m.mu.Unlock()
	if ok {
		return limitGenres(libraryOnly(answer, library)), true
	}
	if failed {
		return nil, false
	}

	turn, _ := m.turns.LoadOrStore(userID, make(chan struct{}, 1))
	select {
	case turn.(chan struct{}) <- struct{}{}:
		defer func() { <-turn.(chan struct{}) }()
	case <-ctx.Done():
		return nil, false
	}
	// The mapping waited for may have been for this text, or have failed.
	m.mu.Lock()
	answer, ok = m.cached(userID, text, time.Now())
	failed = m.failedRecently(userID, time.Now())
	m.mu.Unlock()
	if ok {
		return limitGenres(libraryOnly(answer, library)), true
	}
	if failed {
		return nil, false
	}

	user := fmt.Sprintf("DJ: %s\nDescription: %s\n\nGenres in the library: %s", name, description, strings.Join(prompt, ", "))
	var reply struct {
		Genres []string `json:"genres"`
	}
	err := m.client.Complete(ctx, llm.Request{System: genreSystem, User: user, SchemaName: "genres", Schema: genreSchema}, &reply)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.initMaps()
	if err != nil {
		// A Listener who left isn't an LLM that failed.
		if !errors.Is(ctx.Err(), context.Canceled) {
			m.failedAt[userID] = time.Now()
		}
		log.Warn(ctx, "Mapping a DJ to genres failed; matching words for now", "dj", name, err)
		return nil, false
	}
	delete(m.failedAt, userID)
	kept := append([]genreAnswer{{text: text, genres: reply.Genres, at: time.Now()}}, m.answers[userID]...)
	m.answers[userID] = kept[:min(len(kept), keptGenreAnswers)]
	genres := limitGenres(libraryOnly(reply.Genres, library))
	log.Debug(ctx, "Mapped a DJ to genres", "dj", name, "answer", reply.Genres, "genres", genres)
	return genres, true
}

// limitGenres keeps the first maxMappedGenres genres.
func limitGenres(genres model.DJGenres) model.DJGenres {
	return genres[:min(len(genres), maxMappedGenres)]
}

// commonGenres returns the names of the Library's most common genres, for the LLM to choose from.
func (s *service) commonGenres(ctx context.Context) ([]string, error) {
	genres, err := s.ds.Genre(ctx).GetAll(model.QueryOptions{Sort: "song_count", Order: "desc", Max: maxPromptGenres})
	if err != nil {
		return nil, fmt.Errorf("listing the Library's genres: %w", err)
	}
	res := make([]string, 0, len(genres))
	for _, g := range genres {
		if g.Name != "" {
			res = append(res, g.Name)
		}
	}
	return res, nil
}

// llmGenres returns the genres the LLM maps the DJ's name and description to, and whether it
// did: false without an LLM, when it fails or when it names none of the Library's genres.
func (s *service) llmGenres(ctx context.Context, name, description string, library []string) (model.DJGenres, bool) {
	usr, ok := request.UserFrom(ctx)
	if s.genreLLM == nil || !ok || len(library) == 0 {
		return nil, false
	}
	prompt, err := s.commonGenres(ctx)
	if err != nil {
		log.Warn(ctx, "Mapping a DJ to genres failed; matching words for now", "dj", name, err)
		return nil, false
	}
	genres, ok := s.genreLLM.mapGenres(ctx, usr.ID, name, description, prompt, library)
	return genres, ok && len(genres) > 0
}

// needsMapping reports whether the Listener's DJ's genres are still to be mapped by the LLM:
// they were neither picked by hand nor mapped by it.
func (s *service) needsMapping(dj DJ) bool {
	return s.genreLLM != nil && !dj.BuiltIn && dj.genreSource != model.DJGenresByListener && dj.genreSource != model.DJGenresFromLLM
}

// fillUnsetGenres gives a DJ saved before DJs had genres its word-matched ones, and stores them,
// so it plays them from now on. Mapping them with the LLM is left for later (mapGenresAhead).
func (s *service) fillUnsetGenres(ctx context.Context, dj DJ) (DJ, error) {
	if dj.BuiltIn || dj.genreSource != model.DJGenresUnset {
		return dj, nil
	}
	library, err := s.libraryGenres(ctx)
	if err != nil {
		return DJ{}, err
	}
	return s.storeGenres(ctx, dj, matchGenreWords(genreText(dj.Name, dj.Description), library), model.DJGenresFromWords)
}

// mapGenresAhead starts mapping the Listener's DJ's genres with the LLM in the background, when
// they still need it and no mapping of the Listener's is under way. It waits for the Listener's
// selection under way, if any, so the LLM host has one of their calls at a time. It returns
// at once.
func (s *service) mapGenresAhead(ctx context.Context, userID string, dj DJ) {
	if !s.needsMapping(dj) {
		return
	}
	m := s.genreLLM
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initMaps()
	if m.backfilling[userID] || m.failedRecently(userID, time.Now()) {
		return
	}
	m.backfilling[userID] = true
	m.wg.Add(1)
	// The mapping outlives the refill's request, keeping its Listener for the Library.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), genreBackfillTimeout)
	go func() {
		defer m.wg.Done()
		defer cancel()
		defer func() {
			m.mu.Lock()
			delete(m.backfilling, userID)
			m.mu.Unlock()
		}()
		if done := s.selectionUnderWay(userID); done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return
			}
		}
		library, err := s.libraryGenres(ctx)
		if err != nil {
			log.Warn(ctx, "Mapping a DJ to genres failed", "dj", dj.ID, err)
			return
		}
		if genres, ok := s.llmGenres(ctx, dj.Name, dj.Description, library); ok {
			if _, err := s.storeGenres(ctx, dj, genres, model.DJGenresFromLLM); err != nil {
				log.Warn(ctx, "Storing a DJ's genres failed", "dj", dj.ID, err)
			}
		}
	}()
}

// selectionUnderWay returns what closes when the Listener's selection under way ends, or nil
// when none is.
func (s *service) selectionUnderWay(userID string) <-chan struct{} {
	s.plans.mu.Lock()
	defer s.plans.mu.Unlock()
	if sel := s.plans.selecting[userID]; sel != nil {
		return sel.done
	}
	return nil
}

// storeGenres stores genres worked out for the Listener's DJ as it was, and returns the DJ as
// it is now: unchanged when it was edited meanwhile, or its genres picked by hand.
func (s *service) storeGenres(ctx context.Context, dj DJ, genres model.DJGenres, source model.DJGenreSource) (DJ, error) {
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return DJ{}, errors.New("storing a DJ's genres needs a logged-in Listener")
	}
	defer s.lock(usr.ID)()
	stored, err := s.ds.DJ(ctx).Get(dj.ID)
	if err != nil {
		return DJ{}, fmt.Errorf("loading the DJ: %w", err)
	}
	if stored.Name != dj.Name || stored.Description != dj.Description || stored.GenreSource == model.DJGenresByListener {
		return listenerDJ(*stored), nil
	}
	stored.Genres, stored.GenreSource = genres, source
	if err := s.ds.DJ(ctx).Put(stored); err != nil {
		return DJ{}, fmt.Errorf("saving the DJ: %w", err)
	}
	return listenerDJ(*stored), nil
}
