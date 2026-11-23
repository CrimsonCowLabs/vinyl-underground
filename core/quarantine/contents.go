package quarantine

import (
	"context"
	"errors"
	"slices"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Item is a File in Quarantine, as the Quarantine page lists it.
type Item struct {
	LibraryID int    `json:"libraryId"`
	Library   string `json:"library"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	// Writable is whether the app can write the File's library: a restore and Empty Quarantine
	// need it.
	Writable bool `json:"writable"`
	// Original is where the File came from: the path the app's move took it from, or, for a File
	// the library-cleanup recipes moved, its path beneath Quarantine.
	Original string `json:"original"`
	// EntryID is the app's move that put the File there (its undo restores it), if any.
	EntryID string `json:"entryId,omitempty"`
	// TrackID, Title, Artist and Album are the File's Track, kept while it is in Quarantine.
	TrackID string `json:"trackId,omitempty"`
	Title   string `json:"title,omitempty"`
	Artist  string `json:"artist,omitempty"`
	Album   string `json:"album,omitempty"`
}

// Summary is what is in Quarantine: every File of every library's Quarantine, in library and
// path order, and their count and total size; and the count and size of those Empty Quarantine
// deletes, the Files in libraries the app can write.
type Summary struct {
	Count      int    `json:"count"`
	Size       int64  `json:"size"`
	EmptyCount int    `json:"emptyCount"`
	EmptySize  int64  `json:"emptySize"`
	Files      []Item `json:"files"`
}

// Move is the app's move that put the File at rel in Quarantine (in the library libID), while it
// still holds: no later write to the File, or to its Track, was made since (an undo, a restore,
// a delete by Empty Quarantine, another move). Nil when there is none: the File came there some
// other way (the library-cleanup recipes), or that move no longer stands.
func Move(ctx context.Context, ds model.DataStore, libID int, rel string) (*model.EditEntry, error) {
	journal := ds.EditJournal(ctx)
	last, err := journal.LastWritten(libID, rel)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if last.Kind != model.EditQuarantine {
		return nil, nil
	}
	later, err := journal.After(last)
	if err != nil {
		return nil, err
	}
	if len(later) > 0 {
		return nil, nil
	}
	return last, nil
}

// Writable reports whether the app can write the library libID (checked at startup).
func Writable(libID int) bool {
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

// Contents lists what is in each library's Quarantine, each File with the app's move that put it
// there (see Move). A library whose folder can't be read is left out (logged).
func Contents(ctx context.Context, ds model.DataStore, fsys safefile.FS) (*Summary, error) {
	libs, err := ds.Library(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := &Summary{Files: []Item{}}
	var trackIDs []string
	for _, lib := range libs {
		files, err := Files(fsys, lib.Path)
		if err != nil {
			log.Warn(ctx, "Could not list a library's Quarantine", "library", lib.Name, err)
			continue
		}
		for _, f := range files {
			it := Item{LibraryID: lib.ID, Library: lib.Name, Path: f.Path, Size: f.Size,
				Writable: Writable(lib.ID), Original: Original(f.Path)}
			m, err := Move(ctx, ds, lib.ID, f.Path)
			if err != nil {
				return nil, err
			}
			if m != nil {
				it.Original, it.EntryID, it.TrackID = m.Path, m.ID, m.MediaFileID
				if m.MediaFileID != "" {
					trackIDs = append(trackIDs, m.MediaFileID)
				}
			}
			res.Files = append(res.Files, it)
			res.Count++
			res.Size += f.Size
			if it.Writable {
				res.EmptyCount++
				res.EmptySize += f.Size
			}
		}
	}

	tracks := map[string]model.MediaFile{}
	for chunk := range slices.Chunk(trackIDs, 500) {
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.id": chunk}})
		if err != nil {
			return nil, err
		}
		for _, mf := range mfs {
			tracks[mf.ID] = mf
		}
	}
	for i := range res.Files {
		it := &res.Files[i]
		if mf, ok := tracks[it.TrackID]; ok {
			it.Title, it.Artist, it.Album = mf.Title, mf.Artist, mf.Album
		} else {
			it.TrackID = ""
		}
	}
	return res, nil
}
