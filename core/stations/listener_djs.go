package stations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// A Listener can create their own DJs from a name and a personality description (#17). They
// are private to that Listener and kept in the dj table; the built-in DJs are read-only. Station
// history (station_track, station_skip) only holds a DJ's id, never a reference to its row, so
// deleting a DJ leaves play history as it is.

const (
	// MaxDJNameLength is the longest DJ name accepted, in characters.
	MaxDJNameLength = 60
	// MaxDJDescriptionLength is the longest DJ personality description accepted, in characters.
	MaxDJDescriptionLength = 1000
)

var (
	ErrBuiltInDJ            = errors.New("built-in DJs can't be edited or deleted")
	ErrInvalidDJName        = fmt.Errorf("a DJ needs a name of at most %d characters", MaxDJNameLength)
	ErrInvalidDJDescription = fmt.Errorf("a DJ needs a personality description of at most %d characters", MaxDJDescriptionLength)
)

// listenerGenreWeight is what a Listener's DJ's genres weigh, as much as a built-in DJ's.
const listenerGenreWeight = 4

// listenerTaste is how a Listener's own DJ chooses Tracks without an LLM. Its personality is
// free text the rules can't read, so it leans on its genres (see listenerDJ), if it has any,
// and on what the Listener loves and plays, with room for Tracks they haven't heard. With an
// LLM, its name and description choose instead.
var listenerTaste = Taste{
	FavoriteWeight: 2.5,
	HistoryWeight:  1.5,
	ArtistWeight:   2,
	UnplayedWeight: 1,
}

func (s *service) DJs(ctx context.Context) ([]DJ, error) {
	own, err := s.ds.DJ(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listing the Listener's DJs: %w", err)
	}
	res := DJs()
	for _, dj := range own {
		res = append(res, listenerDJ(dj))
	}
	return res, nil
}

func (s *service) DJ(ctx context.Context, id string) (DJ, error) {
	if dj, ok := FindDJ(id); ok {
		return dj, nil
	}
	if id == "" {
		return DJ{}, ErrUnknownDJ
	}
	dj, err := s.ds.DJ(ctx).Get(id)
	if errors.Is(err, model.ErrNotFound) {
		return DJ{}, ErrUnknownDJ
	}
	if err != nil {
		return DJ{}, fmt.Errorf("loading the DJ: %w", err)
	}
	return listenerDJ(*dj), nil
}

// DJEdit is what a Listener gives to create or edit one of their DJs.
type DJEdit struct {
	Name, Description string
	// Genres are the Library genres the Listener picked by hand, even none, or nil when they
	// didn't touch them. Picked genres stay until the Listener picks again; otherwise they are
	// worked out from the name and description.
	Genres []string
	// Resuggest works the genres out again from the name and description, replacing ones
	// picked by hand. It overrides Genres.
	Resuggest bool
}

// picked returns the genres the Listener picked by hand, or nil when they are to be worked out.
func (e DJEdit) picked() []string {
	if e.Resuggest {
		return nil
	}
	return e.Genres
}

// OpenDJ returns the logged-in Listener's own DJ for the DJ form, mapping its genres with the
// LLM first when they still need it, and storing them.
func (s *service) OpenDJ(ctx context.Context, id string) (DJ, error) {
	if err := editable(id); err != nil {
		return DJ{}, err
	}
	dj, err := s.DJ(ctx, id)
	if err != nil {
		return DJ{}, err
	}
	if dj, err = s.fillUnsetGenres(ctx, dj); err != nil || !s.needsMapping(dj) {
		return dj, err
	}
	genres, source, err := s.deriveGenres(ctx, dj.Name, dj.Description)
	if err != nil || source != model.DJGenresFromLLM {
		return dj, err
	}
	return s.storeGenres(ctx, dj, genres, source)
}

func (s *service) CreateDJ(ctx context.Context, edit DJEdit) (DJ, error) {
	dj, err := cleanDJ(edit.Name, edit.Description)
	if err != nil {
		return DJ{}, err
	}
	if err := s.setGenres(ctx, &dj, edit.picked()); err != nil {
		return DJ{}, err
	}
	if err := s.ds.DJ(ctx).Put(&dj); err != nil {
		return DJ{}, fmt.Errorf("saving the DJ: %w", err)
	}
	return listenerDJ(dj), nil
}

func (s *service) UpdateDJ(ctx context.Context, id string, edit DJEdit) (DJ, error) {
	if err := editable(id); err != nil {
		return DJ{}, err
	}
	dj, err := cleanDJ(edit.Name, edit.Description)
	if err != nil {
		return DJ{}, err
	}
	stored, err := s.ds.DJ(ctx).Get(id)
	if errors.Is(err, model.ErrNotFound) {
		return DJ{}, ErrUnknownDJ
	}
	if err != nil {
		return DJ{}, fmt.Errorf("loading the DJ: %w", err)
	}
	dj.ID = id
	// Genres picked by hand stay, and so do the LLM's for the same name and description.
	sameText := stored.Name == dj.Name && stored.Description == dj.Description
	keep := stored.GenreSource == model.DJGenresByListener || (sameText && stored.GenreSource == model.DJGenresFromLLM)
	if edit.picked() == nil && !edit.Resuggest && keep {
		dj.Genres, dj.GenreSource = stored.Genres, stored.GenreSource
	} else if err := s.setGenres(ctx, &dj, edit.picked()); err != nil {
		return DJ{}, err
	}
	err = s.putDJ(ctx, &dj)
	if errors.Is(err, model.ErrNotFound) {
		return DJ{}, ErrUnknownDJ
	}
	if err != nil {
		return DJ{}, fmt.Errorf("saving the DJ: %w", err)
	}
	return listenerDJ(dj), nil
}

// putDJ saves the Listener's edited DJ, holding their lock so a background mapping of the DJ's
// old description can't save over it (see storeGenres).
func (s *service) putDJ(ctx context.Context, dj *model.DJ) error {
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return errors.New("editing a DJ needs a logged-in Listener")
	}
	defer s.lock(usr.ID)()
	return s.ds.DJ(ctx).Put(dj)
}

func (s *service) DeleteDJ(ctx context.Context, id string) error {
	if err := editable(id); err != nil {
		return err
	}
	err := s.ds.DJ(ctx).Delete(id)
	if errors.Is(err, model.ErrNotFound) {
		return ErrUnknownDJ
	}
	if err != nil {
		return fmt.Errorf("deleting the DJ: %w", err)
	}
	return nil
}

// setGenres gives the DJ the genres the Listener picked, the ones the Library has, or when
// picked is nil the genres worked out from its name and description.
func (s *service) setGenres(ctx context.Context, dj *model.DJ, picked []string) error {
	var err error
	if picked == nil {
		dj.Genres, dj.GenreSource, err = s.deriveGenres(ctx, dj.Name, dj.Description)
		return err
	}
	library, err := s.libraryGenres(ctx)
	if err != nil {
		return err
	}
	dj.Genres, dj.GenreSource = libraryOnly(picked, library), model.DJGenresByListener
	return nil
}

// editable refuses ids no Listener DJ can have: a built-in DJ's, or none.
func editable(id string) error {
	if _, ok := FindDJ(id); ok {
		return ErrBuiltInDJ
	}
	if id == "" {
		return ErrUnknownDJ
	}
	return nil
}

// cleanDJ tidies a DJ's name (one line, single spaces) and description (trimmed), and checks
// both are there and not too long.
func cleanDJ(name, description string) (model.DJ, error) {
	name = strings.Join(strings.Fields(name), " ")
	description = strings.TrimSpace(description)
	if name == "" || utf8.RuneCountInString(name) > MaxDJNameLength {
		return model.DJ{}, ErrInvalidDJName
	}
	if description == "" || utf8.RuneCountInString(description) > MaxDJDescriptionLength {
		return model.DJ{}, ErrInvalidDJDescription
	}
	return model.DJ{Name: name, Description: description}, nil
}

// listenerDJ is the Listener's DJ as a Station plays it. Its genres weigh, and narrow, its
// Tracks the way a built-in DJ's do.
func listenerDJ(dj model.DJ) DJ {
	res := DJ{ID: dj.ID, Name: dj.Name, Description: dj.Description, Taste: listenerTaste,
		Genres: []string(dj.Genres), GenresEdited: dj.GenreSource == model.DJGenresByListener, genreSource: dj.GenreSource}
	if len(dj.Genres) > 0 {
		res.Taste.WholeGenres = true
		res.Taste.GenreWeight = listenerGenreWeight
		for _, g := range dj.Genres {
			res.Taste.Genres = append(res.Taste.Genres, strings.ToLower(g))
		}
	}
	return res
}
