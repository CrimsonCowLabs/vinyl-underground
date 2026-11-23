// Package tagedit edits one Track's tags in its File, safely (ADR-0011): the write every later
// editing ticket reuses. See Editor.Edit for the write path.
package tagedit

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/audiodata"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/scanner"
)

var (
	// ErrEditingOff is an edit of a Track whose library the app can't write (or with editing off).
	ErrEditingOff = errors.New("editing this Track's File is off: its library is read-only")
	// ErrChangedOnDisk is a File whose modification time or size differs from the last scan's.
	ErrChangedOnDisk = errors.New("the File changed on disk, rescan first")
	// ErrUnsupportedFormat is a File whose audio data can't be checked, so it isn't written.
	ErrUnsupportedFormat = errors.New("this kind of File can't be edited yet (only mp3, m4a, aiff and wav)")
	// ErrNotVerified is a written copy that didn't check out; the File was left as it was.
	ErrNotVerified = errors.New("the edited copy didn't check out, so the File was left as it was")
)

// editable are the formats whose audio data audiodata can find, so an edit can be checked.
var editable = []string{"mp3", "m4a", "aiff", "aif", "wav"}

// writeMu makes the app write one File at a time (ADR-0011).
var writeMu sync.Mutex

// Scanner rescans the folder of an edited File.
type Scanner interface {
	ScanFolders(ctx context.Context, fullScan bool, targets []model.ScanTarget) ([]string, error)
}

// Editor reads and writes the tags of a Track's File.
type Editor struct {
	ds      model.DataStore
	scanner Scanner
	fsys    safefile.FS
	tagger  Tagger
	// scanAttempts and scanRetryDelay bound how long an edit waits for a busy scanner.
	scanAttempts   int
	scanRetryDelay time.Duration
}

// New returns an Editor. The scanner, if any, re-reads an edited File into its Track.
func New(ds model.DataStore, s Scanner) *Editor {
	return &Editor{ds: ds, scanner: s, fsys: safefile.OS{}, tagger: taglibTagger{},
		scanAttempts: 20, scanRetryDelay: 500 * time.Millisecond}
}

// TrackTags is what the Edit dialog shows: the editable fields, and their values in the File.
type TrackTags struct {
	ID     string  `json:"id"`
	Path   string  `json:"path"`
	Fields []Field `json:"fields"`
	Values Values  `json:"values"`
}

// Result is what an edit did. Track is the Track as the app now has it.
type Result struct {
	Changed bool             `json:"changed"`
	Track   *model.MediaFile `json:"track,omitempty"`
	Entry   string           `json:"entryId,omitempty"`
	Warning string           `json:"warning,omitempty"`
}

// file is a Track's File, checked as editable.
type file struct {
	mf  *model.MediaFile
	lib *model.Library
	abs string
}

func (e *Editor) file(ctx context.Context, trackID string) (*file, error) {
	mf, err := e.ds.MediaFile(ctx).Get(trackID)
	if err != nil {
		return nil, err
	}
	if !writable(mf.LibraryID) {
		return nil, ErrEditingOff
	}
	lib, err := e.ds.Library(ctx).Get(mf.LibraryID)
	if err != nil {
		return nil, err
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(mf.Path)), ".")
	if !slices.Contains(editable, ext) {
		return nil, ErrUnsupportedFormat
	}
	return &file{mf: mf, lib: lib, abs: filepath.Join(lib.Path, filepath.FromSlash(mf.Path))}, nil
}

// writable reports whether editing is on for the library libID (checked at startup).
func writable(libID int) bool {
	editing := core.CurrentLibraryEditing()
	if !editing.Enabled {
		return false
	}
	for _, l := range editing.Libraries {
		if l.ID == libID {
			return l.Writable
		}
	}
	return false
}

// Read returns the Track's editable fields, as they are in its File.
func (e *Editor) Read(ctx context.Context, trackID string) (*TrackTags, error) {
	f, err := e.file(ctx, trackID)
	if err != nil {
		return nil, err
	}
	props, err := e.tagger.Read(f.abs)
	if err != nil {
		return nil, err
	}
	return &TrackTags{ID: f.mf.ID, Path: f.mf.Path, Fields: Fields, Values: valuesOf(props)}, nil
}

// Edit writes the edited fields (by Field.Key; the others are left alone) into the Track's
// File, and keeps the Track's ID. It is the write path every write to a File takes:
//
//  1. Guard: a File whose modification time or size differs from the last scan's is refused
//     (ErrChangedOnDisk); it is never overwritten blindly.
//  2. Journal: the File's original tags, and the changes, are recorded in the Edit journal
//     before anything is written.
//  3. Write to a temp copy: the File is copied to a hidden temp beside it, and only the changed
//     tags are written into the copy.
//  4. Verify: the copy is read back. The changed tags must read as written, every other tag as
//     it was, an mp3 must be ID3v2.4 with an ID3v1 tag only if it had one, and the audio data
//     must be identical to the File's (ErrNotVerified).
//  5. Replace: the copy takes the File's place (safefile.Replace). If any step fails, the File is
//     untouched and the temp removed.
//  6. Re-key: the File's folder is rescanned, which re-reads the File into the same Track (a
//     Track's row is found by its File's path, never by the tags its persistent ID comes from),
//     so its favorites, Playlist entries, plays, bookmarks and Downloads stay attached.
func (e *Editor) Edit(ctx context.Context, trackID string, edit Values) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()

	f, err := e.file(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if err := e.guard(f); err != nil {
		return nil, err
	}
	orig, err := e.tagger.Read(f.abs)
	if err != nil {
		return nil, err
	}
	set, err := changes(orig, edit)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return &Result{Changed: false, Track: f.mf}, nil
	}

	entry := &model.EditEntry{
		JobID: id.NewRandom(), Kind: model.EditTags, State: model.EditPending,
		LibraryID: f.mf.LibraryID, MediaFileID: f.mf.ID, Path: f.mf.Path, NewPath: f.mf.Path,
		OriginalTags: model.EditTagValues(orig), Changes: model.EditTagValues(set),
	}
	if u, ok := request.UserFrom(ctx); ok {
		entry.UserID = u.ID
	}
	journal := e.ds.EditJournal(ctx)
	if err := journal.Put(entry); err != nil {
		return nil, fmt.Errorf("recording the edit in the Edit journal: %w", err)
	}

	if err := e.write(f.abs, orig, set, entry); err != nil {
		if errors.Is(err, safefile.ErrInterrupted) {
			// The File is moved aside (and the copy is checked): try to put it back now, or a
			// restart does.
			if written, serr := safefile.Settle(e.fsys, f.abs, sumOf(entry)); serr == nil {
				e.close(ctx, entry, written, err)
			} else {
				entry.Error = err.Error()
				_ = journal.Put(entry)
				log.Error(ctx, "An edit left a File moved aside; it is put back at the next start", "path", f.abs, err, "settle", serr)
			}
		} else {
			e.close(ctx, entry, false, err)
		}
		log.Warn(ctx, "Could not edit a Track's tags", "id", trackID, "path", f.abs, err)
		return nil, err
	}
	e.close(ctx, entry, true, nil)
	log.Info(ctx, "Edited a Track's tags", "id", trackID, "path", f.abs, "tags", slices.Sorted(maps.Keys(set)))

	res := &Result{Changed: true, Entry: entry.ID, Track: f.mf}
	if err := e.rekey(ctx, f); err != nil {
		log.Warn(ctx, "Scan after an edit failed; the Track shows its new tags after the next scan", "path", f.abs, err)
		res.Warning = "The File was written; the app shows its new tags after the next scan."
	}
	if mf, err := e.ds.MediaFile(ctx).Get(trackID); err == nil {
		res.Track = mf
	}
	return res, nil
}

// guard refuses a File that changed on disk since the last scan.
func (e *Editor) guard(f *file) error {
	info, err := e.fsys.Stat(f.abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: the File isn't there", ErrChangedOnDisk)
		}
		return err
	}
	// Seconds only: a share may keep no finer modification times.
	if info.Size() != f.mf.Size || info.ModTime().Unix() != f.mf.UpdatedAt.Unix() {
		return ErrChangedOnDisk
	}
	return nil
}

// write makes the checked, edited copy of the File at abs and puts it in the File's place
// (steps 3 to 5 of Edit). On failure the temp copy is gone, and the File untouched, unless the
// error is safefile.ErrInterrupted.
func (e *Editor) write(abs string, orig, set map[string][]string, entry *model.EditEntry) (err error) {
	temp := safefile.TempName(abs)
	if err := e.fsys.Remove(temp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing an earlier edit's copy: %w", err)
	}
	defer func() {
		if err != nil && !errors.Is(err, safefile.ErrInterrupted) {
			_ = e.fsys.Remove(temp)
		}
	}()
	sum, size, _, err := safefile.CopyToTemp(e.fsys, abs, temp)
	if err != nil {
		return err
	}
	if err := safefile.CheckCopy(e.fsys, temp, sum, size); err != nil {
		return err
	}
	if err := e.tagger.Write(temp, set); err != nil {
		return err
	}
	if err := e.verify(abs, temp, orig, set); err != nil {
		return err
	}
	copySum, _, err := safefile.Hash(e.fsys, temp)
	if err != nil {
		return err
	}
	entry.CopySum = hex.EncodeToString(copySum)
	if err := e.ds.EditJournal(context.Background()).Put(entry); err != nil {
		return fmt.Errorf("recording the edit in the Edit journal: %w", err)
	}
	err = safefile.Replace(e.fsys, abs, temp)
	if errors.Is(err, safefile.ErrBackupLeft) {
		log.Warn("An edited File's old copy could not be removed; it is removed at the next start", "path", abs, err)
		return nil
	}
	return err
}

// verify checks the edited copy at temp against the File at abs (step 4 of Edit).
func (e *Editor) verify(abs, temp string, orig, set map[string][]string) error {
	got, err := e.tagger.Read(temp)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	for _, k := range slices.Sorted(maps.Keys(union(orig, got))) {
		want, changed := set[k]
		if !changed {
			want = orig[k]
		}
		if !slices.Equal(got[k], want) && (len(got[k]) > 0 || len(want) > 0) {
			if changed {
				return fmt.Errorf("%w: %s reads back as %q, not %q", ErrNotVerified, k, got[k], want)
			}
			return fmt.Errorf("%w: %s changed too, to %q", ErrNotVerified, k, got[k])
		}
	}
	if strings.EqualFold(filepath.Ext(abs), ".mp3") {
		if err := checkMP3(abs, temp); err != nil {
			return fmt.Errorf("%w: %v", ErrNotVerified, err)
		}
	}
	same, err := audiodata.Same(abs, temp)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	if !same {
		return fmt.Errorf("%w: its audio data differs from the File's", ErrNotVerified)
	}
	return nil
}

// checkMP3 checks an edited mp3 is ID3v2.4, with an ID3v1 tag only if the File has one.
func checkMP3(abs, temp string) error {
	head, err := readHead(temp, 4)
	if err != nil {
		return err
	}
	if !bytes.Equal(head[:3], []byte("ID3")) || head[3] != 4 {
		return errors.New("it isn't ID3v2.4")
	}
	had, err := hasID3v1(abs)
	if err != nil {
		return err
	}
	has, err := hasID3v1(temp)
	if err != nil {
		return err
	}
	if had != has {
		return errors.New("its ID3v1 tag was added or removed")
	}
	return nil
}

func union(a, b map[string][]string) map[string][]string {
	res := maps.Clone(a)
	for k, v := range b {
		res[k] = v
	}
	return res
}

func sumOf(entry *model.EditEntry) []byte {
	b, _ := hex.DecodeString(entry.CopySum)
	return b
}

// close records how an edit ended in its journal entry.
func (e *Editor) close(ctx context.Context, entry *model.EditEntry, written bool, cause error) {
	entry.State, entry.Error = model.EditWritten, ""
	if !written {
		entry.State = model.EditFailed
		if cause != nil {
			entry.Error = cause.Error()
		}
	}
	if err := e.ds.EditJournal(ctx).Put(entry); err != nil {
		log.Error(ctx, "Could not record how an edit ended in the Edit journal", "entry", entry.ID, "state", entry.State, err)
	}
}

// rekey rescans the edited File's folder, so its Track (the same row, found by the File's path)
// gets its new tags, and its album and artists are brought up to date (step 6 of Edit). A busy
// scanner is waited for, a little.
func (e *Editor) rekey(ctx context.Context, f *file) error {
	if e.scanner == nil {
		return nil
	}
	dir := path.Dir(f.mf.Path)
	if dir == "." {
		dir = ""
	}
	targets := []model.ScanTarget{{LibraryID: f.mf.LibraryID, FolderPath: dir}}
	ctx = context.WithoutCancel(ctx)
	var err error
	for range max(e.scanAttempts, 1) {
		if _, err = e.scanner.ScanFolders(ctx, false, targets); !errors.Is(err, scanner.ErrAlreadyScanning) {
			return err
		}
		time.Sleep(e.scanRetryDelay)
	}
	return err
}

// Settle finishes or undoes every edit the app stopped in the middle of, from what is on disk
// (safefile.Settle), and closes its journal entry. It runs at startup, before any new edit.
func (e *Editor) Settle(ctx context.Context) {
	writeMu.Lock()
	defer writeMu.Unlock()
	pending, err := e.ds.EditJournal(ctx).Pending()
	if err != nil {
		log.Error(ctx, "Could not look for interrupted edits", err)
		return
	}
	for i := range pending {
		entry := &pending[i]
		lib, err := e.ds.Library(ctx).Get(entry.LibraryID)
		if err != nil {
			log.Error(ctx, "Could not settle an interrupted edit: its library is gone", "entry", entry.ID, err)
			continue
		}
		abs := filepath.Join(lib.Path, filepath.FromSlash(entry.Path))
		interrupted := errors.New("interrupted by a restart")
		if entry.CopySum == "" {
			// It stopped before the checked copy was about to replace the File.
			if !safefile.Exists(e.fsys, abs) {
				log.Error(ctx, "Could not settle an interrupted edit: its File isn't there", "entry", entry.ID, "path", abs)
				continue
			}
			if err := e.fsys.Remove(safefile.TempName(abs)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Warn(ctx, "Could not remove an interrupted edit's copy", "path", safefile.TempName(abs), err)
			}
			e.close(ctx, entry, false, interrupted)
			continue
		}
		written, err := safefile.Settle(e.fsys, abs, sumOf(entry))
		if err != nil {
			log.Error(ctx, "Could not settle an interrupted edit; it is tried again at the next start", "entry", entry.ID, "path", abs, err)
			continue
		}
		log.Info(ctx, "Settled an interrupted edit", "path", abs, "written", written)
		e.close(ctx, entry, written, interrupted)
		if written {
			mf := &model.MediaFile{LibraryID: entry.LibraryID, Path: entry.Path}
			if err := e.rekey(ctx, &file{mf: mf, lib: lib, abs: abs}); err != nil {
				log.Warn(ctx, "Scan after settling an edit failed", "path", abs, err)
			}
		}
	}
}
