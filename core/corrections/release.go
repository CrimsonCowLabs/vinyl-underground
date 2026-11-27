package corrections

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/retag"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Find on MusicBrainz (#170): the release the admin picks for an album in its Edit dialog is
// written into the album's Files, as one edit job. Every File gets the release's album-level IDs
// (musicbrainz_albumid, musicbrainz_releasegroupid, and the album artist's), and each File the
// release pairs it with (retag.MatchRelease) its track's (musicbrainz_trackid, the recording, and
// musicbrainz_releasetrackid). The release's title, album artist, date and track titles are
// written only if the admin accepts them, and its Cover Art Archive cover, as the album's
// cover.jpg, and the album's MusicBrainz genres, only if asked.
//
// The album's ID comes from musicbrainz_albumid, so a job that writes it into only some of the
// album's Files splits the album. A Track ID stays (its row is matched by path), and the album's
// stars and ratings follow the album to its new ID File by File, as the scanner reassigns them.
// So should a File be skipped, the job puts back every File it wrote (JobRelease's rollback),
// and the album stays whole, as it was.

// JobRelease writes a picked MusicBrainz release into an album's Files.
const JobRelease model.EditKind = "musicbrainzRelease"

// Canonical says which of the release's own values the admin accepts for the album: its title,
// album artist, date (and its release group's first release date, as the original date), and
// the titles and the artists of the tracks the album's Tracks pair with.
type Canonical struct {
	Album       bool `json:"album"`
	AlbumArtist bool `json:"albumArtist"`
	Date        bool `json:"date"`
	Titles      bool `json:"titles"`
	Artists     bool `json:"artists"`
}

// ReleaseWrite is what saving the album Edit dialog with a picked release writes besides the
// release's IDs: the dialog's own values (Fix, nil for none) and Track numbers, the release's
// values the admin accepts, whether track IDs a File holds already that differ from the
// release's are replaced (else they are kept, and only empty ones are filled), whether the
// release's cover is written as the album's cover.jpg, and whether the album's MusicBrainz genres
// (ReleaseMatch.Genres) replace the Files' genres.
type ReleaseWrite struct {
	Fix             *AlbumFix
	Tracks          []TrackNumber
	Accept          Canonical
	ReplaceTrackIDs bool
	Cover           bool
	Genres          bool
}

// ReleaseMatch is how the album's Tracks pair with a release, as the dialog shows it before
// saving.
type ReleaseMatch struct {
	Release ReleaseSummary `json:"release"`
	// Holds is MatchRelease's verdict: the release has room for every Track, and pairs most of
	// them by title (or, untitled, by position and length). It holds too when every Track pairs,
	// some by their track numbers (ByPosition).
	Holds bool `json:"holds"`
	// ByPosition is how many Tracks pair by their disc and track numbers alone, which they do
	// when the album has as many Tracks as the release has tracks and their titles pair with
	// none: the admin knows the release is the album, however its titles differ. Or, when the
	// album is one disc of the release, by their track numbers on it (see Disc).
	ByPosition int `json:"byPosition"`
	// Disc is the release's disc (medium position) the album is, when it is one disc of a
	// release of several (Discs): every Track pairs with a track of it, by title or by track
	// number, and saving numbers the Files as that disc. 0 when the album is the whole release.
	Disc  int `json:"disc,omitempty"`
	Discs int `json:"discs,omitempty"`
	// Tracks are the album's Tracks, in album order, each with the release track it pairs with.
	Tracks []PairedTrack `json:"tracks"`
	// Unpaired are the release's tracks no Track pairs with.
	Unpaired []ReleaseTrack `json:"unpaired"`
	// AlbumArtistMatches says the album artist is the release's credit (as Artist names
	// compare): only then are the credit's MusicBrainz IDs written.
	AlbumArtistMatches bool `json:"albumArtistMatches"`
	// Canonical are the release's values that differ from the album's.
	Canonical CanonicalChanges `json:"canonical"`
	// AlbumIDs are the other MusicBrainz releases the album's Files name now, which the release
	// replaces.
	AlbumIDs []string `json:"albumIds"`
	// Genres are the album's genres on MusicBrainz (musicbrainz.Release.AlbumGenres), written
	// into every File in place of its own if the admin asks; empty when MusicBrainz has none.
	Genres []string `json:"genres"`
}

// ReleaseSummary is the release as the dialog names it.
type ReleaseSummary struct {
	ID             string   `json:"id"`
	ReleaseGroupID string   `json:"releaseGroupId"`
	Title          string   `json:"title"`
	AlbumArtist    string   `json:"albumArtist"`
	AlbumArtistIDs []string `json:"albumArtistIds"`
	Date           string   `json:"date"`
	Description    string   `json:"description"`
}

// ReleaseTrack is one track of the release.
type ReleaseTrack struct {
	ID          string `json:"id"`
	RecordingID string `json:"recordingId"`
	Title       string `json:"title"`
	Disc        int    `json:"disc"`
	Position    int    `json:"position"`
	// Length is in milliseconds, 0 when unknown.
	Length int `json:"length"`
	// Artist is the track's artist credit, as printed.
	Artist string `json:"artist"`
}

// PairedTrack is one of the album's Tracks, and the release track it pairs with (nil: none, and
// it gets no track IDs).
type PairedTrack struct {
	ID      string        `json:"id"`
	Path    string        `json:"path"`
	Title   string        `json:"title"`
	Disc    int           `json:"disc"`
	Track   int           `json:"track"`
	Paired  *ReleaseTrack `json:"paired,omitempty"`
	ByTitle bool          `json:"byTitle,omitempty"`
	// ByPosition says it pairs by its disc and track numbers alone (see ReleaseMatch.ByPosition).
	ByPosition bool `json:"byPosition,omitempty"`
	// MbzRecordingID and MbzReleaseTrackID are the track IDs the File holds now; Mismatch says
	// one of them isn't the paired track's.
	MbzRecordingID    string `json:"mbzRecordingId,omitempty"`
	MbzReleaseTrackID string `json:"mbzReleaseTrackId,omitempty"`
	Mismatch          bool   `json:"mismatch,omitempty"`
}

// CanonicalChange is an album value the release has otherwise: zero when it is the same.
type CanonicalChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// CanonicalChanges are the release's values that differ from the album's, and how many paired
// Tracks are titled otherwise.
type CanonicalChanges struct {
	Album       CanonicalChange `json:"album"`
	AlbumArtist CanonicalChange `json:"albumArtist"`
	Date        CanonicalChange `json:"date"`
	Titles      int             `json:"titles"`
	// Artists is how many paired Tracks have another artist than their release track's.
	Artists int `json:"artists"`
}

func change(from, to string) CanonicalChange {
	if from == to || to == "" {
		return CanonicalChange{}
	}
	return CanonicalChange{From: from, To: to}
}

func (c *corrections) MatchRelease(ctx context.Context, albumID string, rel *musicbrainz.Release) (*ReleaseMatch, error) {
	m, _, err := c.matchRelease(ctx, albumID, rel)
	return m, err
}

// matchRelease pairs the album's Tracks with the release, and returns them too.
func (c *corrections) matchRelease(ctx context.Context, albumID string, rel *musicbrainz.Release) (*ReleaseMatch, *albumEditing, error) {
	if !isMbzID(rel.ID) || !isMbzID(rel.ReleaseGroup.ID) {
		return nil, nil, fmt.Errorf("%w: a release and its release group need both their MusicBrainz IDs", model.ErrValidation)
	}
	e, err := c.albumEdit(ctx, albumID, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	files := make([]retag.File, len(e.tracks))
	for i, mf := range e.tracks {
		files[i] = retag.FileOf(mf)
	}
	placed, holds := retag.MatchRelease(files, rel)
	byPosition := pairByPosition(e.tracks, rel, placed)
	credit := rel.ArtistCredit.String()
	m := &ReleaseMatch{
		Release: ReleaseSummary{ID: rel.ID, ReleaseGroupID: rel.ReleaseGroup.ID, Title: rel.Title, AlbumArtist: credit,
			AlbumArtistIDs: creditIDs(rel.ArtistCredit), Date: rel.Date, Description: rel.Description()},
		Holds: holds, Tracks: []PairedTrack{}, Unpaired: []ReleaseTrack{}, AlbumIDs: []string{},
		AlbumArtistMatches: model.ArtistNameKey(e.al.AlbumArtist) == model.ArtistNameKey(credit),
		Genres:             rel.AlbumGenres(),
	}
	taken := map[string]bool{}
	for _, mf := range e.tracks {
		t := PairedTrack{ID: mf.ID, Path: mf.Path, Title: mf.Title, Disc: mf.DiscNumber, Track: mf.TrackNumber,
			MbzRecordingID: mf.MbzRecordingID, MbzReleaseTrackID: mf.MbzReleaseTrackID}
		if p, ok := placed[mf.ID]; ok {
			rt := releaseTrack(p.Disc, p.Track)
			t.Paired, t.ByTitle, t.ByPosition = &rt, p.ByTitle, byPosition[mf.ID]
			t.Mismatch = (mf.MbzRecordingID != "" && mf.MbzRecordingID != rt.RecordingID) ||
				(mf.MbzReleaseTrackID != "" && mf.MbzReleaseTrackID != rt.ID)
			taken[rt.ID] = true
			if rt.Title != "" && rt.Title != mf.Title {
				m.Canonical.Titles++
			}
			if rt.Artist != "" && rt.Artist != mf.Artist {
				m.Canonical.Artists++
			}
		}
		if mf.MbzAlbumID != "" && mf.MbzAlbumID != rel.ID && !slices.Contains(m.AlbumIDs, mf.MbzAlbumID) {
			m.AlbumIDs = append(m.AlbumIDs, mf.MbzAlbumID)
		}
		m.Tracks = append(m.Tracks, t)
	}
	for _, md := range rel.Media {
		for _, t := range md.Tracks {
			if !taken[t.ID] {
				m.Unpaired = append(m.Unpaired, releaseTrack(md.Position, t))
			}
		}
	}
	date := e.al.Date
	if date == "" && albumYear(*e.al) > 0 {
		date = strconv.Itoa(albumYear(*e.al))
	}
	m.ByPosition = len(byPosition)
	if m.ByPosition > 0 && len(placed) == len(e.tracks) {
		m.Holds = true
	}
	if d := pairedDisc(e.tracks, rel, placed); d > 0 {
		m.Disc, m.Discs = d, len(rel.Media)
	}
	m.Canonical.Album = change(e.al.Name, rel.Title)
	m.Canonical.AlbumArtist = change(e.al.AlbumArtist, credit)
	m.Canonical.Date = change(date, rel.Date)
	return m, e, nil
}

// pairByPosition places each Track the titles left unplaced on the release track at its track
// number: when the album has as many Tracks as the release has tracks, on its disc (none is disc
// 1); when it has as many as one disc of a release of several, on that disc (positionDisc). No
// Track takes a track another took. It returns the Tracks it placed.
func pairByPosition(tracks model.MediaFiles, rel *musicbrainz.Release, placed map[string]retag.Placed) map[string]bool {
	byPosition := map[string]bool{}
	if len(tracks) == 0 || placed == nil {
		return byPosition
	}
	discOf := func(mf model.MediaFile) int { return max(mf.DiscNumber, 1) }
	if len(tracks) != rel.TrackCount() {
		d := positionDisc(tracks, rel, placed)
		if d == 0 {
			return byPosition
		}
		discOf = func(model.MediaFile) int { return d }
	}
	taken := map[string]bool{}
	for _, p := range placed {
		taken[p.Track.ID] = true
	}
	for _, mf := range tracks {
		if _, ok := placed[mf.ID]; ok || mf.TrackNumber <= 0 {
			continue
		}
		disc := discOf(mf)
		for _, md := range rel.Media {
			if md.Position != disc {
				continue
			}
			for _, t := range md.Tracks {
				if t.Position == mf.TrackNumber && !taken[t.ID] {
					placed[mf.ID] = retag.Placed{Disc: md.Position, Track: t}
					taken[t.ID], byPosition[mf.ID] = true, true
				}
			}
		}
	}
	return byPosition
}

// positionDisc is the disc of a release of several the album is, going by track numbers and
// lengths: the disc with as many tracks as the album has Tracks, whose track at each Track's
// number lasts as long as it (most of the lengths known, and every known one fitting), and on
// which every Track the titles placed is. Asked of a release of discs with as many tracks (a
// double album of two halves), the Tracks' own disc number decides; failing that, none is.
// 0 when no disc is the album.
func positionDisc(tracks model.MediaFiles, rel *musicbrainz.Release, placed map[string]retag.Placed) int {
	if len(rel.Media) < 2 {
		return 0
	}
	var fitting []int
	for _, md := range rel.Media {
		if len(md.Tracks) != len(tracks) {
			continue
		}
		at := make(map[int]musicbrainz.Track, len(md.Tracks))
		for _, t := range md.Tracks {
			at[t.Position] = t
		}
		ok, known := true, 0
		for _, mf := range tracks {
			if p, done := placed[mf.ID]; done && p.Disc != md.Position {
				ok = false
				break
			}
			t, has := at[mf.TrackNumber]
			if !has {
				ok = false
				break
			}
			fits, k := retag.LengthFits(float64(mf.Duration), t.Length)
			if !fits {
				ok = false
				break
			}
			if k {
				known++
			}
		}
		if ok && known*2 > len(tracks) {
			fitting = append(fitting, md.Position)
		}
	}
	switch {
	case len(fitting) == 1:
		return fitting[0]
	case len(fitting) > 1:
		if d := tracks[0].DiscNumber; d > 0 && slices.Contains(fitting, d) {
			for _, mf := range tracks {
				if mf.DiscNumber != d {
					return 0
				}
			}
			return d
		}
	}
	return 0
}

// pairedDisc is the disc of a release of several every Track pairs with a track of, 0 when the
// Tracks pair across discs, or some with none.
func pairedDisc(tracks model.MediaFiles, rel *musicbrainz.Release, placed map[string]retag.Placed) int {
	if len(rel.Media) < 2 || len(tracks) == 0 || len(placed) != len(tracks) {
		return 0
	}
	disc := 0
	for _, mf := range tracks {
		p := placed[mf.ID]
		if disc != 0 && p.Disc != disc {
			return 0
		}
		disc = p.Disc
	}
	return disc
}

func releaseTrack(disc int, t musicbrainz.Track) ReleaseTrack {
	return ReleaseTrack{ID: t.ID, RecordingID: t.Recording.ID, Title: t.Title, Disc: disc, Position: t.Position, Length: t.Length,
		Artist: t.ArtistCredit.String()}
}

// creditIDs are the MusicBrainz IDs of the credit's artists, or none if any is unknown (they are
// written by position, one per album artist).
func creditIDs(credit musicbrainz.ArtistCredits) []string {
	ids := make([]string, 0, len(credit))
	for _, a := range credit {
		if !isMbzID(a.Artist.ID) {
			return []string{}
		}
		ids = append(ids, a.Artist.ID)
	}
	return ids
}

// PlanRelease works out the job that writes the release into the album's Files (see JobRelease).
func (c *corrections) PlanRelease(ctx context.Context, editor *tagedit.Editor, albumID string, rel *musicbrainz.Release, w ReleaseWrite) (*Plan, error) {
	m, base, err := c.matchRelease(ctx, albumID, rel)
	if err != nil {
		return nil, err
	}
	al := base.al
	credit := m.Release.AlbumArtist
	// The release's values the admin accepts take the place of the dialog's.
	var fix *AlbumFix
	if w.Fix != nil {
		f := *w.Fix
		fix = &f
	}
	if w.Accept.Album || w.Accept.AlbumArtist || w.Accept.Date {
		if fix == nil {
			fix = &AlbumFix{Name: al.Name, AlbumArtist: al.AlbumArtist}
		}
		if w.Accept.Album && rel.Title != "" {
			fix.Name = rel.Title
		}
		if w.Accept.AlbumArtist && credit != "" {
			fix.AlbumArtist = credit
		}
		if w.Accept.Date && rel.Date != "" {
			fix.Year = 0
		}
	}
	e, err := c.albumEdit(ctx, albumID, fix, w.Tracks)
	if err != nil {
		return nil, err
	}
	albumArtist := al.AlbumArtist
	if fix != nil {
		albumArtist = fix.AlbumArtist
	}
	artistIDs := m.Release.AlbumArtistIDs
	writeArtistIDs := len(artistIDs) > 0 && model.ArtistNameKey(albumArtist) == model.ArtistNameKey(credit)
	acceptDate := w.Accept.Date && rel.Date != ""
	creditNames := make([]string, len(rel.ArtistCredit))
	for i, a := range rel.ArtistCredit {
		creditNames[i] = a.Name
	}

	paired := map[string]*ReleaseTrack{}
	for _, t := range m.Tracks {
		paired[t.ID] = t.Paired
	}
	credits := map[string]musicbrainz.ArtistCredits{}
	for _, md := range rel.Media {
		for _, t := range md.Tracks {
			credits[t.ID] = t.ArtistCredit
		}
	}
	plan := &Plan{Kind: JobRelease, TrackID: e.tracks[0].ID}
	if w.Cover {
		step, pickCover, err := c.releaseCover(ctx, editor, al.ID, rel)
		if err != nil {
			return nil, err
		}
		plan.PickCover = pickCover
		if step != nil {
			plan.Steps = append(plan.Steps, *step)
		}
	}
	for _, mf := range e.tracks {
		r := e.rewrites[mf.ID]
		r.Set["musicbrainz_albumid"] = []string{rel.ID}
		r.Set["musicbrainz_releasegroupid"] = []string{rel.ReleaseGroup.ID}
		if writeArtistIDs {
			// The IDs go with the album artists by position: the credit's names go with them, as
			// the album artist is the credit already (or is made it).
			r.Set["musicbrainz_albumartistid"] = artistIDs
			r.Set["albumartists"] = creditNames
		}
		if w.Genres && len(m.Genres) > 0 {
			r.Set["genre"] = m.Genres
		}
		// The album is one disc of the release: its Files are numbered as that disc.
		if m.Disc > 0 && mf.DiscNumber != m.Disc {
			r.Set["discnumber"] = []string{strconv.Itoa(m.Disc)}
			r.Set["disctotal"] = []string{strconv.Itoa(m.Discs)}
		}
		if acceptDate {
			r.Year = 0
			r.Set["date"] = []string{rel.Date}
			if d := rel.ReleaseGroup.FirstReleaseDate; d != "" {
				r.Set["originaldate"] = []string{d}
			}
		}
		if t := paired[mf.ID]; t != nil {
			ids := tagedit.Values{}
			if isMbzID(t.RecordingID) {
				ids["musicbrainz_trackid"] = []string{t.RecordingID}
			}
			if isMbzID(t.ID) {
				ids["musicbrainz_releasetrackid"] = []string{t.ID}
			}
			if w.ReplaceTrackIDs {
				for k, v := range ids {
					r.Set[k] = v
				}
			} else if len(ids) > 0 {
				r.Fill = ids
			}
			if w.Accept.Titles && t.Title != "" {
				r.Set["title"] = []string{t.Title}
			}
			if credit := credits[t.ID]; w.Accept.Artists && len(credit) > 0 {
				names := make([]string, len(credit))
				for i, a := range credit {
					names[i] = a.Name
				}
				r.Set["artist"] = []string{credit.String()}
				r.Set["artists"] = names
				// The artist IDs go with the names by position; IDs naming the old artists go.
				if ids := creditIDs(credit); len(ids) > 0 {
					r.Set["musicbrainz_artistid"] = ids
				} else {
					if r.Drop == nil {
						r.Drop = map[string][]string{}
					}
					r.Drop["artist"] = append(r.Drop["artist"], "musicbrainz_artistid")
				}
			}
		}
		step, err := editjob.RewriteStep(mf.ID, r)
		if err != nil {
			return nil, err
		}
		plan.Steps = append(plan.Steps, step)
	}
	a := e.args
	a.Release, a.Year = true, a.Year || acceptDate
	plan.Args = a.String()
	return plan, nil
}

// releaseCover is the step writing the release's Cover Art Archive front cover as the album's
// cover.jpg, and what the archive answered: no step when it had none, or couldn't be asked.
func (c *corrections) releaseCover(ctx context.Context, editor *tagedit.Editor, albumID string, rel *musicbrainz.Release) (*model.EditJobStep, PickCover, error) {
	if c.opts.PickedCover == nil {
		return nil, PickCoverDisabled, nil
	}
	// A cover step for an album with no folder of its own would have the whole job refused when
	// queued; the release is written without it instead.
	if _, err := editor.CheckCover(ctx, albumID); errors.Is(err, tagedit.ErrNoAlbumFolder) || errors.Is(err, tagedit.ErrSharedFolder) {
		log.Info(ctx, "Corrections: the album has no folder of its own for the picked release's cover", "album", albumID, err)
		return nil, PickCoverNoFolder, nil
	} else if err != nil {
		return nil, "", err
	}
	cr := model.Correction{MbzAlbumID: rel.ID, MbzReleaseGroupID: rel.ReleaseGroup.ID}
	found := c.fetchPickedCover(ctx, &cr)
	if found != PickCoverFound {
		return nil, found, nil
	}
	image, err := readChosen(cr)
	if err != nil {
		log.Warn(ctx, "Corrections: could not read the picked release's cover", "release", rel.ID, err)
		return nil, PickCoverFailed, nil
	}
	step, err := editjob.CoverStep(editor, albumID, image)
	if err != nil {
		log.Warn(ctx, "Corrections: could not keep the picked release's cover to write", "release", rel.ID, err)
		return nil, PickCoverFailed, nil
	}
	return &step, found, nil
}

// queuedRollback returns the undo of the job writer already queued (or run), if any: the latest
// jobs are looked through.
func (k releaseKind) queuedRollback(ctx context.Context, writer string) (string, error) {
	jobs, err := k.ds.EditJob(ctx).GetAll(50)
	if err != nil {
		return "", err
	}
	for _, j := range jobs {
		if j.Kind == model.EditUndo && j.Args == editjob.UndoJobArgs(writer) {
			return j.ID, nil
		}
	}
	return "", nil
}

// ReleaseRollback is the job a JobRelease job queued to put back the Files it wrote, because it
// couldn't write them all; "" when it wrote them all.
func ReleaseRollback(job *model.EditJob) (string, error) {
	a, err := argsOf(job)
	return a.Rollback, err
}

// ReleaseJobKind is JobRelease's kind, for the edit job queue q: an album job's (it removes what
// pending album Corrections override of what it wrote, the picked release among it, and its undo
// puts them back), which, should a File of the album be skipped, queues the undo of every File
// it wrote instead, so the album isn't left split.
func ReleaseJobKind(ds model.DataStore, q *editjob.Queue) editjob.JobKind {
	return releaseKind{albumKind: albumKind{writeKind{ds: ds}}, q: q}
}

type releaseKind struct {
	albumKind
	q *editjob.Queue
}

func (k releaseKind) Done(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	a, err := argsOf(job)
	if err != nil || a.Rollback != "" {
		return err
	}
	tracks := slices.DeleteFunc(slices.Clone(steps), func(s model.EditJobStep) bool { return s.MediaFileID == "" })
	wrote := slices.ContainsFunc(steps, func(s model.EditJobStep) bool { return s.State == model.EditStepWritten })
	if !anySkipped(tracks) || !wrote {
		return k.albumKind.Done(ctx, job, tracks)
	}
	// The Files were written by the job, or by its redo (the job undone, and undone again), whose
	// writes are journaled with the redo's ID.
	writer := cmp.Or(editjob.RunningJob(ctx), job.ID)
	// Done runs again should the job's row not be saved after it: the rollback it queued then is
	// the one.
	if queued, err := k.queuedRollback(ctx, writer); err != nil || queued != "" {
		if queued != "" {
			a.Rollback = queued
			job.Args = a.String()
		}
		return err
	}
	entries, err := k.ds.EditJournal(ctx).ForJob(writer)
	if err != nil {
		return err
	}
	var undo []model.EditJobStep
	for _, e := range slices.Backward(entries) {
		if e.State != model.EditWritten {
			continue
		}
		s, err := editjob.UndoStep(e.ID)
		if err != nil {
			return err
		}
		undo = append(undo, s)
	}
	if len(undo) == 0 {
		return nil
	}
	u, err := k.q.EnqueueWith(ctx, model.EditUndo, editjob.UndoJobArgs(writer), undo)
	if err != nil {
		return err
	}
	log.Warn(ctx, "Corrections: a MusicBrainz release couldn't be written into every File of the album; putting back those written",
		"job", job.ID, "rollback", u.ID, "files", len(undo))
	a.Rollback = u.ID
	job.Args = a.String()
	return nil
}
