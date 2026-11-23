package retag

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// QuarantineFolder is the default folder of a library that holds Quarantine, named as the
// library-cleanup recipes name it. Files in Quarantine (quarantine.Folder, which the
// QuarantineFolder setting can move) are never retagged.
const QuarantineFolder = quarantine.DefaultFolder

const (
	// minReleaseScore is how well a release must match the search for its album to be looked up.
	minReleaseScore = 80
	// maxLookups is how many of a search's releases are looked up for one album.
	maxLookups = 4
)

// MusicBrainz is the part of musicbrainz.Client retag uses.
type MusicBrainz interface {
	SearchReleases(ctx context.Context, artist, title string) ([]musicbrainz.Release, error)
	Release(ctx context.Context, id string) (*musicbrainz.Release, error)
}

// Writer writes a File's tags: tagedit.Editor.
type Writer interface {
	EditWith(ctx context.Context, trackID string, edit tagedit.Values, opts tagedit.EditOptions) (*tagedit.Result, error)
	Rescan(ctx context.Context, libraryID int, dir string) error
}

// Library is one library to retag, with the Files in it that aren't missing.
type Library struct {
	ID    int
	Files func(ctx context.Context) (model.MediaFiles, error)
}

// Options set up a run. MusicBrainz is optional: without it only the local fixes are made.
type Options struct {
	Writer      Writer
	MusicBrainz MusicBrainz
	// Apply writes the changes; without it the run only reports them.
	Apply bool
	// JobID groups the run's writes in the Edit journal.
	JobID string
	// PathPrefix, if set, limits the run to the Files under it (relative to their library).
	PathPrefix string
	// Report gets a TSV line for every File the run changes, would change, or flags.
	Report io.Writer
	// Progress gets a line per folder.
	Progress io.Writer
}

// Summary counts what a run did.
type Summary struct {
	Files, Folders           int
	Changed, Unchanged       int
	Failed, Skipped, Flagged int
	FromMusicBrainz          int
	Fixes                    map[string]int
}

type runner struct {
	Options
	report *bufio.Writer
	sum    Summary
}

// "Song 1.mp3" beside "Song.mp3" is named like a Duplicate a music app left. Only identical
// audio makes it one (GLOSSARY.md), which retag doesn't check: it just leaves the File for the
// Quarantine tools to settle, rather than tag one that may soon be gone.
var duplicateNameRe = regexp.MustCompile(`^(.*) [12]$`)

// Run retags the libraries' Files, a folder at a time, and stops between Files when ctx ends.
func Run(ctx context.Context, libs []Library, opts Options) (Summary, error) {
	r := &runner{Options: opts, sum: Summary{Fixes: map[string]int{}}}
	if opts.Report != nil {
		r.report = bufio.NewWriter(opts.Report)
		r.row("action", "path", "changes", "source", "notes")
		defer r.report.Flush()
	}
	for _, lib := range libs {
		mfs, err := lib.Files(ctx)
		if err != nil {
			return r.sum, fmt.Errorf("loading library %d's Files: %w", lib.ID, err)
		}
		folders := r.folders(mfs)
		for i, dir := range slices.Sorted(maps.Keys(folders)) {
			if err := ctx.Err(); err != nil {
				return r.sum, err
			}
			changed := r.folder(ctx, folders[dir])
			r.sum.Folders++
			if changed > 0 && r.Apply {
				if err := r.Writer.Rescan(ctx, lib.ID, dir); err != nil {
					log.Warn(ctx, "Rescanning a retagged folder failed; the next scan shows its new tags", "dir", dir, err)
				}
			}
			if r.Progress != nil {
				_, _ = fmt.Fprintf(r.Progress, "[%d/%d] %s: %d of %d changed\n", i+1, len(folders), dir, changed, len(folders[dir]))
			}
			if r.report != nil {
				r.report.Flush()
			}
		}
	}
	return r.sum, nil
}

// folders groups the Files to retag by folder, leaving out those under Quarantine or outside
// PathPrefix, those named like a Duplicate, and formats that can't be edited.
func (r *runner) folders(mfs model.MediaFiles) map[string][]model.MediaFile {
	names := map[string]bool{}
	for _, mf := range mfs {
		names[strings.ToLower(mf.Path)] = true
	}
	prefix := strings.Trim(r.PathPrefix, "/")
	res := map[string][]model.MediaFile{}
	for _, mf := range mfs {
		p := mf.Path
		switch {
		case mf.Missing:
			continue
		case prefix != "" && p != prefix && !strings.HasPrefix(p, prefix+"/"):
			continue
		case quarantine.Within(p):
			continue
		case !tagedit.Editable(p):
			continue
		}
		r.sum.Files++
		stem := strings.TrimSuffix(path.Base(p), path.Ext(p))
		if m := duplicateNameRe.FindStringSubmatch(stem); m != nil && names[strings.ToLower(path.Join(path.Dir(p), m[1]+path.Ext(p)))] {
			r.sum.Skipped++
			r.row("skipped", p, "", "", "named like a Duplicate of the File beside it (not checked); left for the Quarantine tools")
			continue
		}
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		res[dir] = append(res[dir], mf)
	}
	return res
}

func localOf(mf model.MediaFile) Local {
	base := path.Base(mf.Path)
	return Local{
		Title: mf.Title, Artist: mf.Artist, Album: mf.Album, AlbumArtist: mf.AlbumArtist,
		Track: mf.TrackNumber, Disc: mf.DiscNumber, Year: mf.Year,
		Stem: strings.TrimSuffix(base, path.Ext(base)), Folder: path.Base(path.Dir(mf.Path)),
	}
}

// FileOf is the Track's File as MatchRelease pairs it: its values after the local fixes, and its
// length (Find on MusicBrainz pairs an album's Files with the release picked for it, #170).
func FileOf(mf model.MediaFile) File {
	return fileOf(mf, Clean(localOf(mf)))
}

func fileOf(mf model.MediaFile, c Cleaned) File {
	return File{ID: mf.ID, Path: mf.Path, Cleaned: c, Duration: float64(mf.Duration), MbzAlbumID: mf.MbzAlbumID, MbzRecordingID: mf.MbzRecordingID}
}

// work is one File as a run handles it.
type work struct {
	mf     model.MediaFile
	orig   Local
	file   File
	want   Want
	source string
	notes  []string
}

// folder retags one folder's Files, an album at a time, and returns how many it changed (or
// would).
func (r *runner) folder(ctx context.Context, mfs []model.MediaFile) int {
	albums := map[string][]*work{}
	for _, mf := range mfs {
		orig := localOf(mf)
		c := Clean(orig)
		w := &work{mf: mf, orig: orig,
			file: fileOf(mf, c),
			want: Want{Local: c.Local}, source: "local"}
		key := strings.ToLower(c.Album)
		albums[key] = append(albums[key], w)
	}
	changed := 0
	for _, key := range slices.Sorted(maps.Keys(albums)) {
		ws := albums[key]
		r.fromMusicBrainz(ctx, ws)
		for _, w := range ws {
			if ctx.Err() != nil {
				return changed
			}
			if r.write(ctx, w) {
				changed++
			}
		}
	}
	return changed
}

// needsLookup reports whether the album has anything a data source can repair: junk, or a
// track number or year the File had none of.
func needsLookup(ws []*work) bool {
	for _, w := range ws {
		f := w.file
		if f.Junk() || f.Orig.Track == 0 || f.Orig.Year == 0 {
			return true
		}
	}
	return false
}

func (r *runner) fromMusicBrainz(ctx context.Context, ws []*work) {
	if r.MusicBrainz == nil || !needsLookup(ws) {
		return
	}
	files := make([]File, len(ws))
	for i, w := range ws {
		files[i] = w.file
	}
	rel, placed, closest, err := r.findRelease(ctx, ws, files)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			for _, w := range ws {
				w.notes = append(w.notes, "MusicBrainz: "+err.Error())
			}
		}
		return
	}
	if rel == nil {
		note := "no MusicBrainz release matches the folder's album"
		if closest != nil {
			note += fmt.Sprintf(" (closest: %s by %s, release %s)", closest.Description(), closest.ArtistCredit, closest.ID)
		}
		for _, w := range ws {
			w.notes = append(w.notes, note)
		}
		return
	}
	for _, w := range ws {
		p, ok := placed[w.file.ID]
		if !ok {
			w.notes = append(w.notes, "not on MusicBrainz release "+rel.ID)
			continue
		}
		w.notes = append(w.notes, w.want.fromRelease(w.file, p, rel)...)
		w.source = "musicbrainz release " + rel.ID
	}
}

// findRelease finds the album's release: the one its Files are tagged with, or the best match
// among a search's, by album artist and title. An album with no name isn't searched for. With
// no match, closest is the search's best release, for the report.
func (r *runner) findRelease(ctx context.Context, ws []*work, files []File) (rel *musicbrainz.Release, placed map[string]Placed, closest *musicbrainz.Release, err error) {
	if id := ws[0].file.MbzAlbumID; id != "" && allSame(ws, func(w *work) string { return w.file.MbzAlbumID }) {
		rel, err = r.MusicBrainz.Release(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		if placed, ok := MatchRelease(files, rel); ok {
			return rel, placed, nil, nil
		}
		return nil, nil, rel, nil
	}
	if ws[0].file.Has(FlagNoAlbum) {
		return nil, nil, nil, nil
	}
	artist := ws[0].mf.AlbumArtist
	if unknownArtist(artist) {
		artist = ws[0].file.Artist
	}
	if unknownArtist(artist) {
		artist = ""
	}
	cands, err := r.MusicBrainz.SearchReleases(ctx, artist, ws[0].file.Album)
	if err != nil {
		return nil, nil, nil, err
	}
	// The best release places the most Files, then has the fewest tracks over the folder's. One
	// that places every File and has just as many tracks is taken straight away.
	if len(cands) > 0 {
		closest = &cands[0]
	}
	var best *musicbrainz.Release
	var bestPlaced map[string]Placed
	better := func(rel *musicbrainz.Release, placed map[string]Placed) bool {
		if best == nil || len(placed) != len(bestPlaced) {
			return best == nil || len(placed) > len(bestPlaced)
		}
		return rel.TrackCount() < best.TrackCount()
	}
	looked := 0
	for _, c := range cands {
		if c.Score < minReleaseScore || (c.TrackCount() > 0 && c.TrackCount() < len(files)) {
			continue
		}
		if looked++; looked > maxLookups {
			break
		}
		rel, err := r.MusicBrainz.Release(ctx, c.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		placed, ok := MatchRelease(files, rel)
		if !ok || !better(rel, placed) {
			continue
		}
		best, bestPlaced = rel, placed
		if len(placed) == len(files) && rel.TrackCount() == len(files) {
			break
		}
	}
	return best, bestPlaced, closest, nil
}

func allSame(ws []*work, f func(*work) string) bool {
	for _, w := range ws[1:] {
		if f(w) != f(ws[0]) {
			return false
		}
	}
	return true
}

// write writes (or, without Apply, reports) the File's changes, and reports whether it changed.
func (r *runner) write(ctx context.Context, w *work) bool {
	vals := w.want.values(w.orig)
	f := w.file
	var flags []string
	if w.source == "local" {
		for _, fl := range f.Flags {
			flags = append(flags, string(fl))
		}
	}
	notes := strings.Join(append(flags, w.notes...), "; ")
	if len(vals) == 0 {
		r.sum.Unchanged++
		switch {
		case len(flags) > 0:
			r.sum.Flagged++
			r.row("flagged", w.mf.Path, "", w.source, notes)
		case notes != "":
			// Kept as it is, where a data source differs.
			r.row("noted", w.mf.Path, "", w.source, notes)
		}
		return false
	}
	changes := describe(w.mf, vals)
	if !r.Apply {
		r.count(w)
		r.row("would-change", w.mf.Path, changes, w.source, notes)
		return true
	}
	res, err := r.Writer.EditWith(ctx, w.mf.ID, vals, tagedit.EditOptions{JobID: r.JobID, NoRescan: true})
	switch {
	case errors.Is(err, tagedit.ErrChangedOnDisk):
		r.sum.Skipped++
		r.row("skipped", w.mf.Path, changes, w.source, "changed on disk since the last scan; rescan and run again")
		return false
	case err != nil:
		r.sum.Failed++
		r.row("failed", w.mf.Path, changes, w.source, err.Error())
		return false
	case !res.Changed:
		r.sum.Unchanged++
		return false
	}
	r.count(w)
	r.row("changed", w.mf.Path, changes, w.source, notes)
	return true
}

func (r *runner) count(w *work) {
	r.sum.Changed++
	switch {
	case strings.HasPrefix(w.source, "musicbrainz"):
		r.sum.FromMusicBrainz++
	}
	for _, fx := range w.file.Fixes {
		r.sum.Fixes[fx]++
	}
	if len(w.file.Flags) > 0 && w.source == "local" {
		r.sum.Flagged++
	}
}

// describe prints a File's changes: `title: "10 - Bones" → "Bones"`.
func describe(mf model.MediaFile, vals tagedit.Values) string {
	was := map[string]string{
		"title": mf.Title, "artist": mf.Artist, "album": mf.Album, "albumartist": mf.AlbumArtist,
		"tracknumber": strconv.Itoa(mf.TrackNumber), "discnumber": strconv.Itoa(mf.DiscNumber), "date": strconv.Itoa(mf.Year),
	}
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(vals)) {
		if strings.HasPrefix(k, "musicbrainz_") {
			parts = append(parts, k+" set")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %q → %q", k, was[k], strings.Join(vals[k], "; ")))
	}
	return strings.Join(parts, "; ")
}

// tsvSafe keeps a value on its line and in its column.
var tsvSafe = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

// row writes a report line: tab-separated, unquoted, so it reads as it is.
func (r *runner) row(cols ...string) {
	if r.report == nil {
		return
	}
	for i, c := range cols {
		cols[i] = tsvSafe.Replace(c)
	}
	_, _ = r.report.WriteString(strings.Join(cols, "\t") + "\n")
}
