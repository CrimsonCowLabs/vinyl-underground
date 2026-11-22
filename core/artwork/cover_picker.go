package artwork

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/ffmpeg"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/cache"
	"github.com/zeebo/xxh3"
)

// CoverSource is one place the cover picker looks for an album's cover. Each is asked on its own,
// so a slow or failing one never holds the others up.
type CoverSource string

const (
	// CoverSourceLocal is the album folder's cover.*, folder.* and front.* images, and the embedded art.
	CoverSourceLocal CoverSource = "local"
	// CoverSourceExternal is the image agents' album images (Last.fm).
	CoverSourceExternal CoverSource = "external"
	// CoverSourceCoverArtArchive is the album's exact release, then other releases of its release group.
	CoverSourceCoverArtArchive CoverSource = "coverartarchive"
)

// coverPickerPatterns are the album folder images offered, as the default CoverArtPriority names them.
var coverPickerPatterns = []string{"cover.*", "folder.*", "front.*"}

// maxOtherReleases is how many other releases of the release group the archive source offers.
const maxOtherReleases = 8

// CoverCandidate is one image an admin can choose as an album's cover.
type CoverCandidate struct {
	ID string `json:"id"`
	// Source is "folder", "embedded", an image agent's name ("lastfm") or "coverartarchive".
	Source string `json:"source"`
	// Label names the image within its source: a file name, an agent or a release title.
	Label string `json:"label"`
	// Detail tells releases apart: "GB, 2×CD, 1999".
	Detail string `json:"detail,omitempty"`
	// ExactRelease marks the archive's cover of the album's own release.
	ExactRelease bool `json:"exactRelease,omitempty"`
}

// Description is how the Corrections page names a chosen candidate:
// "Cover Art Archive: Record (GB, CD, 1999)".
func (c CoverCandidate) Description() string {
	name := sourceName(c.Source)
	var s string
	switch {
	case c.Label == "" || c.Label == name:
		s = name
	default:
		s = name + ": " + c.Label
	}
	if c.Detail != "" {
		s += " (" + c.Detail + ")"
	}
	return s
}

func sourceName(source string) string {
	switch source {
	case "folder":
		return "Folder image"
	case "embedded":
		return "Embedded art"
	case coverArtArchiveCandidate:
		return "Cover Art Archive"
	case "lastfm":
		return "Last.fm"
	}
	return source
}

// CoverCandidates is what one source offers. Failed means it could not be fully asked (an error,
// or it outlasted its time); Candidates holds whatever it found before that.
type CoverCandidates struct {
	Candidates []CoverCandidate `json:"candidates"`
	Failed     bool             `json:"failed"`
}

// CoverPicker lists the images an admin can choose as an album's cover, from every source, and
// fetches the chosen one. A candidate is only known to the picker that listed it, for about an
// hour, so a choice can only be one the picker offered.
type CoverPicker interface {
	Candidates(ctx context.Context, albumID string, source CoverSource) (*CoverCandidates, error)
	// Preview opens a small version of the candidate's image, where the source has one.
	Preview(ctx context.Context, albumID, candidateID string) (io.ReadCloser, error)
	// Store fetches the candidate's image and keeps it in the ChosenCoverStore.
	Store(ctx context.Context, albumID, candidateID string) (StoredCover, CoverCandidate, error)
}

// releaseCovers and releaseBrowser are the parts of the archive and MusicBrainz clients the picker uses.
type releaseCovers interface {
	ReleaseCovers(ctx context.Context, releaseID string) ([]musicbrainz.Cover, error)
	ReleaseFront(releaseID string) musicbrainz.Cover
}

type releaseBrowser interface {
	ReleaseGroupReleases(ctx context.Context, releaseGroupID string) ([]musicbrainz.Release, error)
}

// pickerClients are shared by every picker, so their caches and rate limits hold across requests.
var pickerClients = sync.OnceValues(func() (releaseCovers, releaseBrowser) {
	return musicbrainz.NewCoverArt(musicbrainz.CoverArtOptions{}), musicbrainz.New(musicbrainz.Options{})
})

// coverSourceTimeout is how long one source may take; the archive source returns what it has by then.
const coverSourceTimeout = 20 * time.Second

func NewCoverPicker(ds model.DataStore, ag *agents.Agents, ffm ffmpeg.FFmpeg) CoverPicker {
	archive, mbz := pickerClients()
	return newCoverPicker(ds, ag, ffm, archive, mbz, coverSourceTimeout)
}

func newCoverPicker(ds model.DataStore, ag *agents.Agents, ffm ffmpeg.FFmpeg, archive releaseCovers, mbz releaseBrowser,
	timeout time.Duration) CoverPicker {
	return &coverPicker{ds: ds, agents: ag, ffmpeg: ffm, archive: archive, mbz: mbz, timeout: timeout,
		listed: cache.NewSimpleCache[string, listedCandidate](cache.Options{SizeLimit: 2000, DefaultTTL: time.Hour})}
}

type coverPicker struct {
	ds      model.DataStore
	agents  *agents.Agents
	ffmpeg  ffmpeg.FFmpeg
	archive releaseCovers
	mbz     releaseBrowser
	timeout time.Duration
	listed  cache.SimpleCache[string, listedCandidate]
}

// listedCandidate is what the picker remembers of a candidate it listed, to preview and store it.
type listedCandidate struct {
	albumID string
	CoverCandidate
	// key tells two candidates of one source apart: a file path, an image URL, a release ID.
	key string
	// open opens the full image, or the preview when thumb is set.
	open func(ctx context.Context, thumb bool) (io.ReadCloser, error)
}

func (p *coverPicker) Candidates(ctx context.Context, albumID string, source CoverSource) (*CoverCandidates, error) {
	al, err := p.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	var listed []listedCandidate
	var failed bool
	switch source {
	case CoverSourceLocal:
		listed, err = p.local(ctx, *al)
	case CoverSourceExternal:
		listed, failed = p.external(ctx, *al)
	case CoverSourceCoverArtArchive:
		listed, failed = p.coverArtArchive(ctx, *al)
	default:
		return nil, fmt.Errorf("%w: unknown cover source %q", model.ErrValidation, source)
	}
	if err != nil {
		return nil, err
	}
	res := &CoverCandidates{Candidates: []CoverCandidate{}, Failed: failed}
	for _, c := range listed {
		c.albumID = al.ID
		c.ID = candidateID(al.ID, c.Source, c.key)
		_ = p.listed.Add(c.ID, c)
		res.Candidates = append(res.Candidates, c.CoverCandidate)
	}
	return res, nil
}

func candidateID(albumID, source, key string) string {
	return fmt.Sprintf("%016x", xxh3.HashString(albumID+"\x00"+source+"\x00"+key))
}

func (p *coverPicker) lookup(albumID, candidateID string) (listedCandidate, error) {
	c, err := p.listed.Get(candidateID)
	if err != nil || c.albumID != albumID {
		return listedCandidate{}, fmt.Errorf("%w: no such cover candidate; list them again", model.ErrNotFound)
	}
	return c, nil
}

func (p *coverPicker) Preview(ctx context.Context, albumID, candidateID string) (io.ReadCloser, error) {
	c, err := p.lookup(albumID, candidateID)
	if err != nil {
		return nil, err
	}
	return c.open(ctx, true)
}

func (p *coverPicker) Store(ctx context.Context, albumID, candidateID string) (StoredCover, CoverCandidate, error) {
	c, err := p.lookup(albumID, candidateID)
	if err != nil {
		return StoredCover{}, CoverCandidate{}, err
	}
	r, err := c.open(ctx, false)
	if err != nil {
		return StoredCover{}, c.CoverCandidate, err
	}
	defer r.Close()
	data, err := readCapped(r)
	if err != nil {
		return StoredCover{}, c.CoverCandidate, fmt.Errorf("%w: %w", model.ErrValidation, err)
	}
	stored, err := StoreChosenCover(data)
	return stored, c.CoverCandidate, err
}

// local lists the album folder's images matching coverPickerPatterns, then the embedded art.
func (p *coverPicker) local(ctx context.Context, al model.Album) ([]listedCandidate, error) {
	_, imgFiles, _, err := loadAlbumFoldersPaths(ctx, p.ds, al)
	if err != nil {
		return nil, err
	}
	lib, err := loadLibraryView(ctx, p.ds, al.LibraryID)
	if err != nil {
		return nil, err
	}
	var res []listedCandidate
	for _, pattern := range coverPickerPatterns {
		for _, file := range imgFiles {
			if ok, _ := path.Match(pattern, strings.ToLower(path.Base(file))); !ok || !model.IsImageFile(file) {
				continue
			}
			res = append(res, listedCandidate{
				CoverCandidate: CoverCandidate{Source: "folder", Label: path.Base(file)},
				key:            file,
				open: func(context.Context, bool) (io.ReadCloser, error) {
					return lib.FS.Open(file)
				},
			})
		}
	}
	if al.EmbedArtPath != "" {
		res = append(res, listedCandidate{
			CoverCandidate: CoverCandidate{Source: "embedded", Label: path.Base(al.EmbedArtPath)},
			key:            al.EmbedArtPath,
			open: func(ctx context.Context, _ bool) (io.ReadCloser, error) {
				res, ok := resolveEmbedded(ctx, lib, p.ffmpeg, al.EmbedArtPath)
				if !ok {
					return nil, fmt.Errorf("%w: the embedded art can't be read", model.ErrNotFound)
				}
				return res.reader, nil
			},
		})
	}
	return res, nil
}

// external asks each image agent for the album's image, offering the largest of each.
func (p *coverPicker) external(ctx context.Context, al model.Album) ([]listedCandidate, bool) {
	if !conf.Server.EnableExternalServices || p.agents == nil {
		return nil, false
	}
	var res []listedCandidate
	var failed bool
	for _, a := range p.agents.AlbumImageAgents() {
		imgs, err := a.Retriever.GetAlbumImages(ctx, externalName(al.Name), externalName(al.AlbumArtist), al.MbzAlbumID)
		if err != nil {
			if !errors.Is(err, agents.ErrNotFound) {
				log.Warn(ctx, "Cover picker: an image agent failed", "agent", a.Name, "album", al.Name, err)
				failed = true
			}
			continue
		}
		if u := bestImageURL(imgs); u != nil {
			res = append(res, remoteCandidate(CoverCandidate{Source: a.Name, Label: sourceName(a.Name)}, u.String(), u, u, ""))
		}
	}
	return res, failed
}

// coverArtArchive offers the exact release's front cover, then the front covers of up to
// maxOtherReleases other releases of the release group: official ones first, then the oldest.
func (p *coverPicker) coverArtArchive(ctx context.Context, al model.Album) ([]listedCandidate, bool) {
	if !conf.Server.EnableExternalServices || (al.MbzAlbumID == "" && al.MbzReleaseGroupID == "") {
		return nil, false
	}
	var failed bool
	var exact *musicbrainz.Cover
	if al.MbzAlbumID != "" {
		covers, err := p.archive.ReleaseCovers(ctx, al.MbzAlbumID)
		switch {
		case err == nil && covers[0].Front:
			exact = &covers[0]
		case err != nil && !errors.Is(err, musicbrainz.ErrNoCover):
			log.Warn(ctx, "Cover picker: the Cover Art Archive failed", "album", al.Name, "release", al.MbzAlbumID, err)
			failed = true
		}
	}
	var releases []musicbrainz.Release
	if al.MbzReleaseGroupID != "" {
		var err error
		if releases, err = p.mbz.ReleaseGroupReleases(ctx, al.MbzReleaseGroupID); err != nil {
			log.Warn(ctx, "Cover picker: MusicBrainz failed listing the releases", "album", al.Name,
				"releaseGroup", al.MbzReleaseGroupID, err)
			failed = true
		}
	}

	var res []listedCandidate
	if exact != nil {
		c := CoverCandidate{Source: coverArtArchiveCandidate, Label: al.Name, ExactRelease: true}
		if i := slices.IndexFunc(releases, func(r musicbrainz.Release) bool { return r.ID == al.MbzAlbumID }); i >= 0 {
			c.Label, c.Detail = releaseLabel(releases[i])
		}
		res = append(res, archiveCandidate(c, al.MbzAlbumID, *exact))
	}
	others := slices.DeleteFunc(slices.Clone(releases), func(r musicbrainz.Release) bool {
		return !r.CoverArtArchive.Front || r.ID == al.MbzAlbumID
	})
	slices.SortStableFunc(others, func(a, b musicbrainz.Release) int {
		return cmp.Or(
			cmp.Compare(officialRank(a), officialRank(b)),
			cmp.Compare(dateRank(a), dateRank(b)),
			strings.Compare(a.Date, b.Date),
		)
	})
	for _, r := range others[:min(len(others), maxOtherReleases)] {
		c := CoverCandidate{Source: coverArtArchiveCandidate}
		c.Label, c.Detail = releaseLabel(r)
		res = append(res, archiveCandidate(c, r.ID, p.archive.ReleaseFront(r.ID)))
	}
	return res, failed
}

func officialRank(r musicbrainz.Release) int {
	if r.Status == "Official" {
		return 0
	}
	return 1
}

// dateRank puts releases of unknown date after the dated ones.
func dateRank(r musicbrainz.Release) int {
	if r.Date == "" {
		return 1
	}
	return 0
}

// releaseLabel is a release's title, and its country, formats and year: "GB, 2×CD, 1999".
func releaseLabel(r musicbrainz.Release) (string, string) {
	var detail []string
	for _, s := range []string{r.Country, r.Formats(), r.Date[:min(len(r.Date), 4)]} {
		if s != "" {
			detail = append(detail, s)
		}
	}
	return r.Title, strings.Join(detail, ", ")
}

func archiveCandidate(c CoverCandidate, releaseID string, cover musicbrainz.Cover) listedCandidate {
	full, errFull := url.Parse(cover.URL)
	thumb, errThumb := url.Parse(cmp.Or(cover.ThumbnailURL, cover.URL))
	if errFull != nil || errThumb != nil {
		return listedCandidate{CoverCandidate: c, key: releaseID, open: func(context.Context, bool) (io.ReadCloser, error) {
			return nil, fmt.Errorf("%w: the archive named no usable image", model.ErrNotFound)
		}}
	}
	return remoteCandidate(c, releaseID, full, thumb, musicbrainz.CoverArtUserAgent())
}

// remoteCandidate fetches the image through the guarded client that refuses private addresses.
func remoteCandidate(c CoverCandidate, key string, full, thumb *url.URL, userAgent string) listedCandidate {
	return listedCandidate{CoverCandidate: c, key: key, open: func(ctx context.Context, preview bool) (io.ReadCloser, error) {
		u := full
		if preview {
			u = thumb
		}
		r, _, err := fromURLAs(ctx, u, userAgent)
		return r, err
	}}
}
