package nativeapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

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
	// Description names the release on the Corrections page once picked: "Record (GB, CD, 1999)".
	Description string `json:"description"`
	// ThumbnailURL is the archive's public 250px front cover, which may not exist. It is public,
	// so the browser loads it directly, with no login token.
	ThumbnailURL string `json:"thumbnailUrl"`
}

// addMusicBrainzRoute serves Find on MusicBrainz in the album Edit dialog (admin only, see
// routes): GET /musicbrainz/release?artist=&title= lists the matching releases, and
// GET /musicbrainz/release/{id}/match?albumId= pairs the album's Tracks with one, as the dialog
// shows before saving. Saving writes it into the album's Files (POST /correction/album).
func (api *Router) addMusicBrainzRoute(r chi.Router) {
	r.Get("/musicbrainz/release", api.searchReleases)
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
			Format: rel.Formats(), Status: rel.Status, Score: rel.Score, Description: rel.Description(),
			ThumbnailURL: coverArtNames.ReleaseFront(rel.ID).ThumbnailURL})
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// mbzIDPattern is a MusicBrainz ID (a UUID, lower case).
var mbzIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func writeMusicBrainzError(w http.ResponseWriter, status int, code string) {
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code})
}
