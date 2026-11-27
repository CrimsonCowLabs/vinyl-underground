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
// releases (by title, barcode or catalog number), and looking one up with its tracks, to pair it
// with the album's and write it (#170).
type releaseSearch interface {
	SearchReleases(ctx context.Context, artist, title string) ([]musicbrainz.Release, error)
	SearchReleasesByBarcode(ctx context.Context, barcode string) ([]musicbrainz.Release, error)
	SearchReleasesByCatalogNumber(ctx context.Context, catno string) ([]musicbrainz.Release, error)
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
	// DiscTrackCounts are the numbers of tracks of each of its discs, in order, when it has
	// several: an album that is one of them is worth fitting too.
	DiscTrackCounts []int `json:"discTrackCounts,omitempty"`
	// Description names the release on the Corrections page once picked: "Record (GB, CD, 1999)".
	Description string `json:"description"`
	// ThumbnailURL is the archive's public 250px front cover, which may not exist. It is public,
	// so the browser loads it directly, with no login token.
	ThumbnailURL string `json:"thumbnailUrl"`
}

// addMusicBrainzRoute serves Find on MusicBrainz in the album Edit dialog (admin only, see
// routes): GET /musicbrainz/release?q=&artist=&compilation= lists the releases matching what the
// admin typed (a title, barcode, catalog number or release link; see searchReleases), and
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

// searchReleases lists the releases matching what the admin typed in the dialog's search field,
// q (see musicbrainz.ParseQuery): a release link or MBID is looked up and listed alone; a
// barcode, a catalog number or a title is searched, then, if that finds nothing, searched
// otherwise (see findReleases). A title is searched with the album artist (artist), as Various
// Artists for a compilation (compilation=true) or an album by "Various", "VA"... (see
// musicbrainz.ReleaseSearchArtist); a barcode or catalog number by any artist. With no q, title
// is searched as a title, as before q was added.
func (api *Router) searchReleases(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	artist := musicbrainz.ReleaseSearchArtist(params.Get("artist"), params.Get("compilation") == "true")
	q := musicbrainz.ParseQuery(params.Get("q"))
	if q.Text == "" {
		title := strings.TrimSpace(params.Get("title"))
		q = musicbrainz.Query{Kind: musicbrainz.QueryTitle, Value: title, Text: title}
	}
	switch {
	case !conf.Server.EnableExternalServices:
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "disabled")
		return
	case q.Text == "":
		writeMusicBrainzError(w, http.StatusBadRequest, "invalid")
		return
	case q.Kind == musicbrainz.QueryOtherLink:
		writeMusicBrainzError(w, http.StatusBadRequest, "notRelease")
		return
	case q.Kind == musicbrainz.QueryRelease:
		rel, ok := api.lookUpRelease(w, r, q.Value)
		if !ok {
			return
		}
		m := releaseMatchOf(*rel)
		// Asked for by its ID, it is the release sought
		m.Score = 100
		_ = rest.RespondWithJSON(w, http.StatusOK, []releaseMatch{m})
		return
	}
	releases, err := api.findReleases(r.Context(), q, artist)
	switch {
	case errors.Is(err, musicbrainz.ErrBusy):
		log.Warn(r.Context(), "Find on MusicBrainz: MusicBrainz is busy", err)
		writeMusicBrainzError(w, http.StatusServiceUnavailable, "busy")
		return
	case err != nil:
		log.Warn(r.Context(), "Find on MusicBrainz: could not search MusicBrainz", "artist", artist, "query", q.Text, err)
		writeMusicBrainzError(w, http.StatusBadGateway, "unavailable")
		return
	}
	res := make([]releaseMatch, 0, len(releases))
	for _, rel := range releases {
		res = append(res, releaseMatchOf(rel))
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// findReleases searches the releases q names, in turn, until one search finds some: a barcode,
// then the text as a catalog number (many are digits and hyphens, like barcodes); something like
// a catalog number, then as a title; a title, then as a catalog number. A failed search answers
// at once: MusicBrainz busy for one is busy for the next.
func (api *Router) findReleases(ctx context.Context, q musicbrainz.Query, artist string) ([]musicbrainz.Release, error) {
	byTitle := func() ([]musicbrainz.Release, error) { return api.releaseSearch.SearchReleases(ctx, artist, q.Text) }
	byCatno := func() ([]musicbrainz.Release, error) {
		return api.releaseSearch.SearchReleasesByCatalogNumber(ctx, q.Text)
	}
	var searches []func() ([]musicbrainz.Release, error)
	switch q.Kind {
	case musicbrainz.QueryBarcode:
		byBarcode := func() ([]musicbrainz.Release, error) {
			return api.releaseSearch.SearchReleasesByBarcode(ctx, q.Value)
		}
		searches = append(searches, byBarcode, byCatno)
	case musicbrainz.QueryCatalogNumber:
		searches = append(searches, byCatno, byTitle)
	default:
		searches = append(searches, byTitle, byCatno)
	}
	for _, search := range searches {
		if rels, err := search(); err != nil || len(rels) > 0 {
			return rels, err
		}
	}
	return nil, nil
}

// releaseMatchOf is the release as Find on MusicBrainz lists it.
func releaseMatchOf(rel musicbrainz.Release) releaseMatch {
	var discs []int
	if len(rel.Media) > 1 && rel.TrackCount() > 0 {
		for _, m := range rel.Media {
			discs = append(discs, max(m.TrackCount, len(m.Tracks)))
		}
	}
	return releaseMatch{DiscTrackCounts: discs, ID: rel.ID, ReleaseGroupID: rel.ReleaseGroup.ID, Title: rel.Title,
		Artist: rel.ArtistCredit.String(), Year: rel.Year(), Country: rel.Country,
		Format: rel.Formats(), Status: rel.Status, Score: rel.Score, TrackCount: rel.TrackCount(), Description: rel.Description(),
		ThumbnailURL: coverArtNames.ReleaseFront(rel.ID).ThumbnailURL}
}

// mbzIDPattern is a MusicBrainz ID (a UUID, lower case).
var mbzIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func writeMusicBrainzError(w http.ResponseWriter, status int, code string) {
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code})
}
