package nativeapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/log"
)

// releaseSearch is the part of the MusicBrainz client Find on MusicBrainz uses.
type releaseSearch interface {
	SearchReleases(ctx context.Context, artist, title string) ([]musicbrainz.Release, error)
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
// routes): GET /musicbrainz/release?artist=&title= lists the matching releases. Picking one is
// part of the album Correction (POST /correction/album).
func (api *Router) addMusicBrainzRoute(r chi.Router) {
	r.Get("/musicbrainz/release", api.searchReleases)
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

func writeMusicBrainzError(w http.ResponseWriter, status int, code string) {
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code})
}
