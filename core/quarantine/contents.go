package quarantine

import (
	"context"
	"slices"

	"github.com/Masterminds/squirrel"
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
// path order, and their count and total size (what Empty Quarantine would delete).
type Summary struct {
	Count int    `json:"count"`
	Size  int64  `json:"size"`
	Files []Item `json:"files"`
}

// Contents lists what is in each library's Quarantine. A library whose folder can't be read is
// left out (logged).
func Contents(ctx context.Context, ds model.DataStore, fsys safefile.FS) (*Summary, error) {
	libs, err := ds.Library(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	moves, err := ds.EditJournal(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Eq{"kind": model.EditQuarantine, "state": model.EditWritten}})
	if err != nil {
		return nil, err
	}
	type key struct {
		lib  int
		path string
	}
	// Newest first: the first move to leave a File at a path is the one that put it there.
	moved := map[key]model.EditEntry{}
	for _, m := range moves {
		k := key{m.LibraryID, m.NewPath}
		if _, ok := moved[k]; !ok && Within(m.NewPath) {
			moved[k] = m
		}
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
			it := Item{LibraryID: lib.ID, Library: lib.Name, Path: f.Path, Size: f.Size, Original: Original(f.Path)}
			if m, ok := moved[key{lib.ID, f.Path}]; ok {
				it.Original, it.EntryID, it.TrackID = m.Path, m.ID, m.MediaFileID
				if m.MediaFileID != "" {
					trackIDs = append(trackIDs, m.MediaFileID)
				}
			}
			res.Files = append(res.Files, it)
			res.Count++
			res.Size += f.Size
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
