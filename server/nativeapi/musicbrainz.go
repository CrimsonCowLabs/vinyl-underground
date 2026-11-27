package nativeapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/log"
)

// releaseSearch is the part of the MusicBrainz client Find on MusicBrainz uses: searching
// releases, and looking one up with its tracks, to pair it with the album's and write it (#170).
type releaseSearch interface {
	SearchReleases(ctx context.Context, artist, title string) ([]musicbrainz.Release, error)
	Release(ctx context.Context, id string) (*musicbrainz.Release, error)
}

// SetMusicBrainz makes Find on MusicBrainz ask c instead of MusicBrainz's web service (e.g. a
// Client on a test server).
func (api *Router) SetMusicBrainz(c *musicbrainz.Client) {
	api.releaseSearch = c
}

// coverArtNames names a release's Cover Art Archive thumbnail, with no request.
var coverArtNames = musicbrainz.NewCoverArt(musicbrainz.CoverArtOptions{})

// releaseMatch is one MusicBrainz release Find on MusicBrainz offers for an album.
type releaseMatch struct {
	ID             string `json:"id"`
	ReleaseGroupID string `json:"releaseGroupId"`
	Title          string `json:"title"`
	Artist         string `json:"artist"`
	Year           string `json:"year"`
	Country        string `json:"country"`
	Format         string `json:"format"`
	Status         string `json:"status"`
	Score          int    `json:"score"`
	// TrackCount is the release's number of tracks, which the dialog compares with the album's
	// to pick the releases worth fitting (see fitReleases).
	TrackCount int `json:"trackCount"`
	// Description names the release on the Corrections page once picked: "Record (GB, CD, 1999)".
	Description string `json:"description"`
	// ThumbnailURL is the archive's public 250px front cover, which may not exist. It is public,
	// so the browser loads it directly, with no login token.
	ThumbnailURL string `json:"thumbnailUrl"`
}

// addMusicBrainzRoute serves Find on MusicBrainz in the album Edit dialog (admin only, see
// routes): GET /musicbrainz/release?artist=&title= lists the matching releases, and
// GET /musicbrainz/release/{id}/match?albumId= pairs the album's Tracks with one, as the dialog
// shows before saving, and GET /musicbrainz/release/fit?albumId=&ids= scores the releases listed
// by how well they fit the album's Files, marking the best. Saving writes the pick into the
// album's Files (POST /correction/album).
func (api *Router) addMusicBrainzRoute(r chi.Router) {
	r.Get("/musicbrainz/release", api.searchReleases)
	r.Get("/musicbrainz/release/fit", api.fitReleases)
	r.Get("/musicbrainz/release/{id}/match", api.matchRelease)
}

// matchRelease pairs the album's Tracks with the release (see corrections.ReleaseMatch).
func (api *Router) matchRelease(w http.ResponseWriter, r *http.Request) {
	rel, ok := api.lookUpRelease(w, r, strings.ToLower(strings.TrimSpace(chi.URLParam(r, "id"))))
	if !ok {
		return
	}
	m, err := api.corrections.MatchRelease(r.Context(), r.URL.Query().Get("albumId"), rel)
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, m)
}

// maxFitReleases is how many releases fitReleases looks up at most: each is a request to
// MusicBrainz, about a second apart.
const maxFitReleases = 5

// fitReleases scores the releases ids (comma-separated, in search order; the first
// maxFitReleases) by how well they fit the album's Files (see corrections.ReleaseFit), and marks
// the best. Each is looked up as matchRelease does (and cached, for the pick that may follow);
// one that can't be is left out of the answer, which fails only when MusicBrainz is too busy for
// every one.
func (api *Router) fitReleases(w http.ResponseWriter, r *http.Request) {
	if !conf.Server.EnableExternalServices {
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "disabled")
		return
	}
	albumID := r.URL.Query().Get("albumId")
	var ids []string
	for id := range strings.SplitSeq(r.URL.Query().Get("ids"), ",") {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" || slices.Contains(ids, id) {
			continue
		}
		if !mbzIDPattern.MatchString(id) {
			writeMusicBrainzError(w, http.StatusBadRequest, "invalid")
			return
		}
		ids = append(ids, id)
	}
	if albumID == "" || len(ids) == 0 {
		writeMusicBrainzError(w, http.StatusBadRequest, "invalid")
		return
	}
	ids = ids[:min(len(ids), maxFitReleases)]

	// The lookups wait their turns at the shared Limiter all the same: asked together, a slow
	// answer doesn't hold up the others.
	ctx := r.Context()
	looked := make([]*musicbrainz.Release, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() { looked[i], errs[i] = api.releaseSearch.Release(ctx, id) })
	}
	wg.Wait()
	rels := make([]*musicbrainz.Release, 0, len(ids))
	busy := 0
	for i, rel := range looked {
		switch err := errs[i]; {
		case errors.Is(err, musicbrainz.ErrBusy):
			busy++
		case err != nil:
			log.Warn(ctx, "Find on MusicBrainz: could not look up a release to fit", "release", ids[i], err)
		case rel != nil && rel.ID != "":
			rels = append(rels, rel)
		}
	}
	if busy == len(ids) {
		log.Warn(ctx, "Find on MusicBrainz: MusicBrainz is busy", "releases", len(ids))
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "busy")
		return
	}
	fits, err := api.corrections.FitReleases(ctx, albumID, rels)
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, fits)
}

// lookUpRelease looks the release up with its tracks, or answers why it can't (disabled, busy,
// unavailable, notFound) and returns false.
func (api *Router) lookUpRelease(w http.ResponseWriter, r *http.Request, id string) (*musicbrainz.Release, bool) {
	if !conf.Server.EnableExternalServices {
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "disabled")
		return nil, false
	}
	if !mbzIDPattern.MatchString(id) {
		writeMusicBrainzError(w, http.StatusBadRequest, "invalid")
		return nil, false
	}
	rel, err := api.releaseSearch.Release(r.Context(), id)
	switch {
	case errors.Is(err, musicbrainz.ErrBusy):
		log.Warn(r.Context(), "Find on MusicBrainz: MusicBrainz is busy", err)
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "busy")
		return nil, false
	case errors.Is(err, musicbrainz.ErrNotFound):
		writeMusicBrainzError(w, http.StatusNotFound, "notFound")
		return nil, false
	case err != nil:
		log.Warn(r.Context(), "Find on MusicBrainz: could not look up a release", "release", id, err)
		writeMusicBrainzError(w, http.StatusBadGateway, "unavailable")
		return nil, false
	case rel.ID == "":
		writeMusicBrainzError(w, http.StatusNotFound, "notFound")
		return nil, false
	}
	return rel, true
}

func (api *Router) searchReleases(w http.ResponseWriter, r *http.Request) {
	artist, title := r.URL.Query().Get("artist"), strings.TrimSpace(r.URL.Query().Get("title"))
	switch {
	case !conf.Server.EnableExternalServices:
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "disabled")
		return
	case title == "":
		writeMusicBrainzError(w, http.StatusBadRequest, "invalid")
		return
	}
	releases, err := api.releaseSearch.SearchReleases(r.Context(), artist, title)
	switch {
	case errors.Is(err, musicbrainz.ErrBusy):
		log.Warn(r.Context(), "Find on MusicBrainz: MusicBrainz is busy", err)
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "busy")
		return
	case err != nil:
		log.Warn(r.Context(), "Find on MusicBrainz: could not search MusicBrainz", "artist", artist, "title", title, err)
		writeMusicBrainzError(w, http.StatusBadGateway, "unavailable")
		return
	}
	res := make([]releaseMatch, 0, len(releases))
	for _, rel := range releases {
		res = append(res, releaseMatch{ID: rel.ID, ReleaseGroupID: rel.ReleaseGroup.ID, Title: rel.Title,
			Artist: rel.ArtistCredit.String(), Year: rel.Year(), Country: rel.Country,
			Format: rel.Formats(), Status: rel.Status, Score: rel.Score, TrackCount: rel.TrackCount(), Description: rel.Description(),
			ThumbnailURL: coverArtNames.ReleaseFront(rel.ID).ThumbnailURL})
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// mbzIDPattern is a MusicBrainz ID (a UUID, lower case).
var mbzIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func writeMusicBrainzError(w http.ResponseWriter, status int, code string) {
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code})
}
