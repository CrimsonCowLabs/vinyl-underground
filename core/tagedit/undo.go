package tagedit

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

var (
	// ErrNotUndoable is an entry that can't be undone: it wasn't written (it failed, or is still
	// pending), or it is of a kind with no undo yet.
	ErrNotUndoable = errors.New("this write can't be undone")
	// ErrChangedLater is an entry a later write to the same File changed some of the same fields
	// of: undoing it would clobber the later values, so that write is undone first.
	ErrChangedLater = errors.New("a later write changed the same fields of this File; undo that one first")
	// ErrAlreadyUndone is an entry whose undo stands: undoing the undo redoes it.
	ErrAlreadyUndone = errors.New("this write was undone already; undo the undo to redo it")
	// ErrUnfinished is an entry with a later write to the same File still pending: an interrupted
	// write the app settles at its next start.
	ErrUnfinished = errors.New("a later write to this File is unfinished; restart the app to settle it")
	// ErrTagsChanged is an entry whose File, though unchanged since the last scan, no longer holds
	// what the entry wrote: its tags were changed outside the app (and rescanned) since.
	ErrTagsChanged = errors.New("the File's tags were changed outside the app since this write")
)

// undoer undoes one written entry of its kind, through the write path (Editor.apply for tags),
// and journals the undo as a new entry with UndoOf set to the entry it undoes. It is called with
// writeMu held.
type undoer func(e *Editor, ctx context.Context, jobID string, entry *model.EditEntry) (*Result, error)

// undoers are the undo of each kind of Edit journal entry. Every kind that writes to a File
// brings its own, and its entries keep what it needs:
//
//   - tags (here): OriginalTags hold every tag before the write, and Changes the tags written.
//     Undo writes OriginalTags' values of Changes' keys back, after checking the File still holds
//     what Changes wrote.
//   - a move (Organize), later: Path is where the File was and NewPath where the write left it.
//     Its undo moves the File from NewPath back to Path (refused when NewPath no longer holds the
//     File, or Path is taken), and re-keys the Track by path, as Promote does. Changes stays
//     empty.
//   - a Quarantine move, later: the same as a move, with NewPath inside Quarantine.
//   - a cover write (#164, cover.go): NewPath is the cover File (the folder's cover.jpg), Image
//     the image written (none for a removal) and OriginalImage the cover it replaced (none when
//     there was none); their bytes are kept by checksum in the journal's images (journalImages),
//     since tags can't hold them. Its undo puts OriginalImage back, or removes the cover, while
//     the File at NewPath still holds Image (ErrCoverChanged otherwise). Any later write of the
//     cover conflicts.
//   - an embed (#164, embed.go): Path is the File, Image the picture it put first in the File and
//     OriginalImage the one it replaced there (none when it had none). Its undo writes
//     OriginalImage back there, or removes the picture, through the write path, while the File's
//     first picture is still Image. A later embed into the File conflicts; a later tag edit
//     doesn't.
//
// A later write conflicts with an entry when it touched the same File (see
// model.EditJournalRepository.After) and the same tags, or moved the File; each undoer refuses
// such an entry with ErrChangedLater. The job queue (#162) undoes a whole job by undoing its
// entries, newest first, with Undo.
var undoers = map[model.EditKind]undoer{
	model.EditTags:  (*Editor).undoTags,
	model.EditCover: (*Editor).undoCover,
	model.EditEmbed: (*Editor).undoEmbed,
}

// checkLater refuses an entry because of the later writes to its File: one still pending
// (ErrUnfinished), the entry's own undo, standing (ErrAlreadyUndone), or another standing write
// that conflict says conflicts with it (its error).
func (e *Editor) checkLater(ctx context.Context, entry *model.EditEntry, conflict func(later model.EditEntry) error) error {
	later, err := e.ds.EditJournal(ctx).After(entry)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(later, func(l model.EditEntry) bool { return l.State == model.EditPending }) {
		return ErrUnfinished
	}
	for _, l := range standing(later) {
		if l.UndoOf == entry.ID {
			return ErrAlreadyUndone
		}
		if err := conflict(l); err != nil {
			return err
		}
	}
	return nil
}

// Undo undoes the written Edit journal entry entryID: it writes the File's values from before
// the entry back, through the same write path as Edit (guard, journal, temp copy, verify,
// replace, re-key), so the Track keeps its ID. The undo is journaled as a new entry with UndoOf
// set, so it can be undone too, which redoes the entry.
//
// Undo refuses, without touching the File, an entry that a later write to the File overlaps
// (ErrChangedLater), an entry already undone (ErrAlreadyUndone), a File with an unfinished later
// write (ErrUnfinished), and a File that changed since: its modification time or size differ from
// the last scan's (ErrChangedOnDisk), or its tags no longer hold what the entry wrote
// (ErrTagsChanged).
func (e *Editor) Undo(ctx context.Context, entryID string) (*Result, error) {
	return e.UndoInJob(ctx, id.NewRandom(), entryID)
}

// UndoInJob is Undo, as one step of the edit job jobID: the undo's Edit journal entry has the
// job's ID.
func (e *Editor) UndoInJob(ctx context.Context, jobID, entryID string) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	entry, err := e.ds.EditJournal(ctx).Get(entryID)
	if err != nil {
		return nil, err
	}
	if entry.State != model.EditWritten {
		return nil, fmt.Errorf("%w: it was never written (%s)", ErrNotUndoable, entry.State)
	}
	undo, ok := undoers[entry.Kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s writes have no undo yet", ErrNotUndoable, entry.Kind)
	}
	return undo(e, ctx, jobID, entry)
}

// undoTags undoes a tag edit (see undoers).
func (e *Editor) undoTags(ctx context.Context, jobID string, entry *model.EditEntry) (*Result, error) {
	// An unfinished write may have changed the File; it is settled at the next start.
	err := e.checkLater(ctx, entry, func(l model.EditEntry) error {
		if l.NewPath != l.Path {
			return fmt.Errorf("%w: the File was moved since", ErrChangedLater)
		}
		if same := overlap(l.Changes, entry.Changes); len(same) > 0 {
			return fmt.Errorf("%w: %s", ErrChangedLater, strings.Join(same, ", "))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	f, err := e.file(ctx, entry.MediaFileID)
	if err != nil {
		return nil, err
	}
	if f.mf.LibraryID != entry.LibraryID || f.mf.Path != entry.NewPath {
		return nil, fmt.Errorf("%w: the File isn't where the write left it", ErrChangedOnDisk)
	}
	if err := e.guard(f); err != nil {
		return nil, err
	}
	cur, err := e.tagger.Read(f.abs)
	if err != nil {
		return nil, err
	}
	set := map[string][]string{}
	for _, k := range slices.Sorted(maps.Keys(entry.Changes)) {
		if !sameValues(cur[k], entry.Changes[k]) {
			return nil, fmt.Errorf("%w: %s isn't what the write left", ErrTagsChanged, k)
		}
		set[k] = slices.Clone(entry.OriginalTags[k])
	}
	return e.apply(ctx, jobID, f, cur, set, entry.ID)
}

// standing drops, from the later writes to a File (oldest first), each write that a later one
// undid, and that undo: together they left the File as it was. A redo (an undo of such an undo)
// stands, as it writes the values again.
func standing(later []model.EditEntry) []model.EditEntry {
	var res []model.EditEntry
	for _, l := range later {
		if i := slices.IndexFunc(res, func(r model.EditEntry) bool { return r.ID == l.UndoOf }); i >= 0 {
			res = slices.Delete(res, i, i+1)
			continue
		}
		res = append(res, l)
	}
	return res
}

// overlap returns the tags both a and b hold, sorted.
func overlap(a, b model.EditTagValues) []string {
	var res []string
	for k := range a {
		if _, ok := b[k]; ok {
			res = append(res, k)
		}
	}
	slices.Sort(res)
	return res
}

// sameValues compares two tags' values, where no values and an empty list are the same.
func sameValues(a, b []string) bool {
	return slices.Equal(a, b) || (len(a) == 0 && len(b) == 0)
}
