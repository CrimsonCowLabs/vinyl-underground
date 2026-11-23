package tagedit

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/utils/natural"
)

var (
	// ErrInvalidImage is a cover that isn't an image the app can read.
	ErrInvalidImage = errors.New("the cover isn't an image the app can read")
	// ErrNoAlbumFolder is an album with no folder of its own to hold its cover: its Tracks are in
	// several folders with no common one, or in the library's root.
	ErrNoAlbumFolder = errors.New("the album has no folder of its own for its cover")
	// ErrSharedFolder is an album whose folder holds another album's Tracks too: its cover would
	// be theirs as well.
	ErrSharedFolder = errors.New("another album's Tracks are in this album's folder, so its cover would be theirs too")
	// ErrCoverChanged is a cover write whose cover File (or an embed whose File's picture) no
	// longer holds what the write left: it was changed outside the app since.
	ErrCoverChanged = errors.New("the cover was changed outside the app since this write")
	// ErrNoFolderCover is an Embed in Files for an album with no cover File in its folder.
	ErrNoFolderCover = errors.New("the album has no cover.jpg in its folder; choose or upload a cover first")
)

// coverName is the name of the cover File the app writes in an album's folder. coverNames are
// the names it reuses instead, in order, whatever their letter case: on a case-insensitive share
// (SMB1) a "cover.jpg" beside a "Cover.JPG" would be the same File, and a cover.jpeg would win
// over a new cover.jpg (it sorts first under CoverArtPriority's cover.*).
const coverName = "cover.jpg"

var coverNames = []string{"cover.jpg", "cover.jpeg"}

// maxCoverPixels caps the size of a cover decoded to be written as JPEG.
const maxCoverPixels = 64 << 20

// CoverJPEG returns the image as the JPEG a cover.jpg holds: a JPEG as it is, any other image the
// app can read (PNG, GIF, WebP) re-encoded. ErrInvalidImage for anything else.
func CoverJPEG(data []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	if format == "jpeg" {
		return data, nil
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxCoverPixels/cfg.Height {
		return nil, fmt.Errorf("%w: it is too large (%dx%d)", ErrInvalidImage, cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	return buf.Bytes(), nil
}

// albumFolder is where an album's cover File goes.
type albumFolder struct {
	album *model.Album
	lib   *model.Library
	// dir is the folder, relative to the library ("." for its root), and name the cover File's
	// name in it.
	dir, name string
	// folders are the album's folders, and the folder holding the cover if it is another (the one
	// above its disc folders).
	folders []model.Folder
}

func (a *albumFolder) rel() string { return path.Join(a.dir, a.name) }
func (a *albumFolder) abs() string {
	return filepath.Join(a.lib.Path, filepath.FromSlash(a.rel()))
}

// CheckCover returns where a cover write for the album would put its cover, relative to its
// library, or the error it would be refused with before anything is read: the album is gone
// (model.ErrNotFound), its library isn't writable (ErrEditingOff), or it has no folder of its own
// (ErrNoAlbumFolder, ErrSharedFolder).
func (e *Editor) CheckCover(ctx context.Context, albumID string) (string, error) {
	a, err := e.albumFolder(ctx, albumID)
	if err != nil {
		return "", err
	}
	return a.rel(), nil
}

// albumFolder finds the album's own folder: the one its Tracks are in, or the one above its disc
// folders (as artwork looks for an album's cover there), and the cover File's name in it.
func (e *Editor) albumFolder(ctx context.Context, albumID string) (*albumFolder, error) {
	al, err := e.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	if !writable(al.LibraryID) {
		return nil, ErrEditingOff
	}
	lib, err := e.ds.Library(ctx).Get(al.LibraryID)
	if err != nil {
		return nil, err
	}
	folders, err := e.ds.Folder(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"folder.id": al.FolderIDs, "missing": false}})
	if err != nil {
		return nil, err
	}
	if len(folders) == 0 {
		return nil, ErrNoAlbumFolder
	}
	others, err := e.ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.And{
		squirrel.Eq{"folder_id": al.FolderIDs, "missing": false}, squirrel.NotEq{"album_id": al.ID},
	}})
	if err != nil {
		return nil, err
	}
	if others > 0 {
		return nil, ErrSharedFolder
	}
	target := folders[0]
	if len(folders) > 1 {
		parentID := folders[0].ParentID
		for _, f := range folders[1:] {
			if f.ParentID != parentID {
				return nil, fmt.Errorf("%w: its Tracks are in folders with no common one", ErrNoAlbumFolder)
			}
		}
		if parentID == "" {
			return nil, ErrNoAlbumFolder
		}
		parent, err := e.ds.Folder(ctx).Get(parentID)
		if err != nil {
			return nil, err
		}
		if parent.ParentID == "" {
			return nil, fmt.Errorf("%w: its folders are in the library's root", ErrNoAlbumFolder)
		}
		if other, err := e.ds.Folder(ctx).HasAudioOutsideFolders(*parent, al.FolderIDs); err != nil {
			return nil, err
		} else if other {
			return nil, ErrSharedFolder
		}
		target = *parent
		folders = append(folders, *parent)
	} else if target.ParentID == "" {
		return nil, fmt.Errorf("%w: its Tracks are in the library's root", ErrNoAlbumFolder)
	}
	a := &albumFolder{album: al, lib: lib, dir: path.Join(target.Path, target.Name), folders: folders}
	if a.name, err = e.coverFileName(filepath.Join(lib.Path, filepath.FromSlash(a.dir))); err != nil {
		return nil, err
	}
	return a, nil
}

// coverFileName is the name of the cover File in the folder at dir: an existing one (see
// coverNames, in any letter case; the exact name first), or coverName.
func (e *Editor) coverFileName(dir string) (string, error) {
	entries, err := e.fsys.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, want := range coverNames {
		var found []string
		for _, en := range entries {
			if strings.EqualFold(en.Name(), want) && !en.IsDir() {
				found = append(found, en.Name())
			}
		}
		if len(found) > 0 {
			if slices.Contains(found, want) {
				return want, nil
			}
			slices.Sort(found)
			return found[0], nil
		}
	}
	return coverName, nil
}

// readCover returns the File at abs, or nil when there is none.
func (e *Editor) readCover(abs string) ([]byte, error) {
	f, err := e.fsys.Open(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// WriteCover writes the image (see CoverJPEG) as the album's folder cover, cover.jpg, through the
// write path (ADR-0011), so the album shows it, and keeps the album's Tracks' IDs (nothing is
// rescanned but the folder's images):
//
//  1. The cover goes in the album's own folder (see CheckCover), under the name of the cover
//     already there in any letter case (Cover.JPG, cover.jpeg), else cover.jpg.
//  2. Journal: the cover File it replaces, if any, is kept by its checksum in the journal's
//     images (see journalImages), and the write is recorded in the Edit journal.
//  3. The image is written to a hidden temp beside it, flushed, and checked; then it takes the
//     cover's place (safefile.Replace), or is renamed into place when there was none.
//  4. The album's chosen cover Correction, if it has one, is removed, so it doesn't hide the
//     File; an image marked wrong for the album stays marked. The folder is rescanned, and the
//     cover of every album in it resolved again.
//
// An image the folder's cover holds already writes nothing. Result.Warning says when another
// image still outranks the cover (see outranking).
func (e *Editor) WriteCover(ctx context.Context, albumID string, image []byte) (*Result, error) {
	return e.WriteCoverInJob(ctx, id.NewRandom(), albumID, image)
}

// WriteCoverInJob is WriteCover, as one step of the edit job jobID.
func (e *Editor) WriteCoverInJob(ctx context.Context, jobID, albumID string, image []byte) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	data, err := CoverJPEG(image)
	if err != nil {
		return nil, err
	}
	a, err := e.albumFolder(ctx, albumID)
	if err != nil {
		return nil, err
	}
	cur, err := e.readCover(a.abs())
	if err != nil {
		return nil, err
	}
	if cur != nil && bytes.Equal(cur, data) {
		return &Result{Changed: false}, nil
	}
	entry, err := e.putCover(ctx, jobID, a.lib, a.rel(), data, model.EditImage{Sum: hexSum(data), Mime: "image/jpeg"}, "")
	if err != nil {
		log.Warn(ctx, "Could not write an album's cover", "album", a.album.Name, "path", a.abs(), err)
		return nil, err
	}
	log.Info(ctx, "Wrote an album's cover", "album", a.album.Name, "path", a.abs(), "replaced", cur != nil)
	e.removeCoverChoice(ctx, a.album)
	res := &Result{Changed: true, Entry: entry.ID}
	res.Warning = e.coverDone(ctx, a.lib, a.dir)
	if w := e.outranking(ctx, a); w != "" {
		res.Warning = strings.TrimSpace(res.Warning + " " + w)
	}
	return res, nil
}

// removeCoverChoice removes the album's chosen (or uploaded) cover Correction: it outranks every
// source, so it would hide the cover File just written. A failure is only logged.
func (e *Editor) removeCoverChoice(ctx context.Context, al *model.Album) {
	repo := e.ds.Correction(ctx)
	choice, err := model.AlbumCorrection(repo, model.CorrectionAlbumCover, *al)
	if errors.Is(err, model.ErrNotFound) {
		return
	}
	if err == nil {
		err = repo.Delete(choice.ID)
	}
	if err != nil {
		log.Warn(ctx, "Could not remove the album's chosen cover; it still shows over the cover File", "album", al.Name, err)
		return
	}
	log.Info(ctx, "Removed the album's chosen cover, now written to its folder", "album", al.Name, "source", choice.To)
}

// putCover writes want (nil removes the cover) as the cover File rel of the library, journaled as
// a cover write: steps 2 and 3 of WriteCover. undoOf is the entry it undoes, if it is an undo.
func (e *Editor) putCover(ctx context.Context, jobID string, lib *model.Library, rel string, want []byte, wantImg model.EditImage, undoOf string) (*model.EditEntry, error) {
	abs := filepath.Join(lib.Path, filepath.FromSlash(rel))
	entry := &model.EditEntry{
		JobID: jobID, Kind: model.EditCover, State: model.EditPending, LibraryID: lib.ID,
		Path: rel, NewPath: rel, Image: wantImg, UndoOf: undoOf,
	}
	if u, ok := request.UserFrom(ctx); ok {
		entry.UserID = u.ID
	}
	cur, err := e.readCover(abs)
	if err != nil {
		return nil, err
	}
	if cur != nil {
		if err := safefile.CheckReplaceable(e.fsys, abs); err != nil {
			return nil, err
		}
		sum, err := e.images.put(cur)
		if err != nil {
			return nil, err
		}
		entry.OriginalImage = model.EditImage{Sum: sum, Mime: http.DetectContentType(cur)}
	}
	if err := e.ds.EditJournal(ctx).Put(entry); err != nil {
		return nil, fmt.Errorf("recording the cover write in the Edit journal: %w", err)
	}
	if err := e.closeWrite(ctx, abs, entry, e.writeCover(ctx, abs, cur != nil, want, entry)); err != nil {
		return nil, err
	}
	return entry, nil
}

// writeCover puts want in the place of the cover File at abs (exists says whether there is one),
// or removes it when want is nil (step 3 of WriteCover). On failure the temp is gone, and the
// cover as it was, unless the error is safefile.ErrInterrupted.
func (e *Editor) writeCover(ctx context.Context, abs string, exists bool, want []byte, entry *model.EditEntry) (err error) {
	if want == nil {
		// Its bytes are kept in the journal's images.
		return e.fsys.Remove(abs)
	}
	temp := safefile.TempName(abs)
	if err := e.clearOldCopy(ctx, abs, entry); err != nil {
		return err
	}
	if err := e.fsys.Remove(temp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing an earlier write's copy: %w", err)
	}
	defer func() {
		if err != nil && !errors.Is(err, safefile.ErrInterrupted) {
			_ = e.fsys.Remove(temp)
		}
	}()
	out, err := e.fsys.Create(temp)
	if err != nil {
		return fmt.Errorf("making the cover's copy: %w", err)
	}
	_, err = out.Write(want)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("writing the cover's copy: %w", err)
	}
	sum := sumBytes(want)
	if err := safefile.CheckCopy(e.fsys, temp, sum, int64(len(want))); err != nil {
		return err
	}
	if exists {
		if info, err := e.fsys.Stat(abs); err == nil {
			safefile.KeepAttrs(e.fsys, info, temp)
		}
	}
	entry.CopySum = hexSum(want)
	if err := e.ds.EditJournal(ctx).Put(entry); err != nil {
		return fmt.Errorf("recording the cover write in the Edit journal: %w", err)
	}
	if !exists {
		if safefile.Exists(e.fsys, abs) {
			return fmt.Errorf("a %s appeared in the folder meanwhile", filepath.Base(abs))
		}
		return e.fsys.Rename(temp, abs)
	}
	err = safefile.Replace(e.fsys, abs, temp)
	if errors.Is(err, safefile.ErrBackupLeft) {
		log.Warn(ctx, "A replaced cover's old copy could not be removed; the next write of the cover removes it", "path", abs, err)
		return nil
	}
	return err
}

func sumBytes(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// coverDone rescans the cover's folder dir, so the app sees its images as they are now, and
// resolves again the cover of every album with Tracks in it (and its Tracks' own art). It returns
// a warning for the admin when that failed: the album shows the cover after the next scan.
func (e *Editor) coverDone(ctx context.Context, lib *model.Library, dir string) string {
	scanDir := dir
	if scanDir == "." {
		scanDir = ""
	}
	warning := ""
	if err := e.rekey(ctx, &file{mf: &model.MediaFile{LibraryID: lib.ID, Path: path.Join(scanDir, coverName)}, lib: lib}); err != nil {
		log.Warn(ctx, "Scan after a cover write failed; the album shows its cover after the next scan", "folder", dir, err)
		warning = "The cover was written; the album shows it after the next scan."
	}
	filter := squirrel.And{squirrel.Eq{"media_file.library_id": lib.ID, "media_file.missing": false}}
	if scanDir != "" {
		filter = append(filter, squirrel.Like{"media_file.path": scanDir + "/%"})
	}
	tracks, err := e.ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: filter})
	if err != nil {
		log.Warn(ctx, "Could not find the albums of a written cover", "folder", dir, err)
		return warning
	}
	albums := map[string]bool{}
	for _, mf := range tracks {
		if mf.AlbumID == "" || albums[mf.AlbumID] {
			continue
		}
		albums[mf.AlbumID] = true
		if err := artwork.RefreshAlbumCover(ctx, e.ds, mf.AlbumID); err != nil {
			log.Warn(ctx, "Could not refresh an album's cover", "album", mf.AlbumID, err)
		}
	}
	return warning
}

// outranking names what the album still shows over its cover File, if anything, as a warning for
// the admin: the cover of the MusicBrainz release picked for it (Find on MusicBrainz), or a source
// CoverArtPriority tries first. The default order ("cover.*, folder.*, front.*, embedded, ...")
// puts cover.* first, so only another cover.<ext> sorting before it (cover.bmp, cover.gif) would,
// or a cover.* in a folder above it.
func (e *Editor) outranking(ctx context.Context, a *albumFolder) string {
	if covers, err := model.AlbumCoverCorrections(e.ds.Correction(ctx), *a.album); err == nil &&
		covers.Pick != nil && covers.Pick.ImageHash != "" {
		return "The album still shows the cover of the MusicBrainz release picked for it."
	}
	// The folders as the rescan left them.
	ids := make([]string, len(a.folders))
	for i, f := range a.folders {
		ids[i] = f.ID
	}
	folders, err := e.ds.Folder(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"folder.id": ids}})
	if err != nil {
		folders = a.folders
	}
	ours := strings.TrimPrefix(a.rel(), "./")
	images := []string{ours}
	for _, f := range folders {
		for _, img := range f.ImageFiles {
			if p := strings.TrimPrefix(path.Join(f.Path, f.Name, img), "./"); p != ours {
				images = append(images, p)
			}
		}
	}
	slices.SortFunc(images, compareImages)
	for pattern := range strings.SplitSeq(strings.ToLower(conf.Server.CoverArtPriority), ",") {
		switch pattern = strings.TrimSpace(pattern); pattern {
		case "":
		case "embedded":
			if a.album.EmbedArtPath != "" {
				return "The album still shows the art embedded in its Files: CoverArtPriority puts embedded first."
			}
		case "external", "coverartarchive":
			return fmt.Sprintf("The album may still show a cover from %s: CoverArtPriority puts it first.", pattern)
		default:
			for _, img := range images {
				if ok, _ := path.Match(pattern, strings.ToLower(path.Base(img))); !ok || !model.IsImageFile(img) {
					continue
				}
				if img == ours {
					return ""
				}
				return fmt.Sprintf("The album still shows %s: CoverArtPriority puts it before %s.", path.Base(img), a.name)
			}
		}
	}
	return fmt.Sprintf("CoverArtPriority has no pattern for %s, so the album doesn't show it.", a.name)
}

// compareImages orders image paths as artwork does: by name without extension (natural order),
// then shallower first, then by path.
func compareImages(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	baseA := strings.TrimSuffix(path.Base(a), path.Ext(a))
	baseB := strings.TrimSuffix(path.Base(b), path.Ext(b))
	return cmp.Or(natural.Compare(baseA, baseB), cmp.Compare(strings.Count(a, "/"), strings.Count(b, "/")), natural.Compare(a, b))
}

// undoCover undoes a cover write (see undoers): it puts back the cover the write replaced, or
// removes the one it created, while the cover File still holds what the write left.
func (e *Editor) undoCover(ctx context.Context, jobID string, entry *model.EditEntry) (*Result, error) {
	if err := e.checkLater(ctx, entry, func(model.EditEntry) error {
		return fmt.Errorf("%w: the cover was written again since", ErrChangedLater)
	}); err != nil {
		return nil, err
	}
	if !writable(entry.LibraryID) {
		return nil, ErrEditingOff
	}
	lib, err := e.ds.Library(ctx).Get(entry.LibraryID)
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(lib.Path, filepath.FromSlash(entry.NewPath))
	cur, err := e.readCover(abs)
	if err != nil {
		return nil, err
	}
	if (entry.Image.Sum == "") != (cur == nil) || (cur != nil && hexSum(cur) != entry.Image.Sum) {
		return nil, ErrCoverChanged
	}
	var want []byte
	if entry.OriginalImage.Sum != "" {
		if want, err = e.images.read(entry.OriginalImage.Sum); err != nil {
			return nil, err
		}
	}
	undo, err := e.putCover(ctx, jobID, lib, entry.NewPath, want, entry.OriginalImage, entry.ID)
	if err != nil {
		log.Warn(ctx, "Could not undo a cover write", "path", abs, err)
		return nil, err
	}
	log.Info(ctx, "Undid a cover write", "path", abs, "removed", want == nil)
	return &Result{Changed: true, Entry: undo.ID, Warning: e.coverDone(ctx, lib, path.Dir(entry.NewPath))}, nil
}

// settleCover finishes or undoes a cover write the app stopped in the middle of (see Settle).
func (e *Editor) settleCover(ctx context.Context, lib *model.Library, entry *model.EditEntry) {
	abs := filepath.Join(lib.Path, filepath.FromSlash(entry.NewPath))
	interrupted := errors.New("interrupted by a restart")
	var written bool
	switch {
	case entry.Image.Sum == "":
		// A removal (an undo of a new cover): done once the cover is gone.
		written = !safefile.Exists(e.fsys, abs)
	case entry.CopySum == "":
		// It stopped before its checked copy was about to take the cover's place.
		if err := e.fsys.Remove(safefile.TempName(abs)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Warn(ctx, "Could not remove an interrupted cover write's copy", "path", safefile.TempName(abs), err)
		}
	default:
		var err error
		if written, err = safefile.Settle(e.fsys, abs, sumOf(entry)); err != nil {
			if !safefile.Exists(e.fsys, safefile.BackupName(abs)) && !safefile.Exists(e.fsys, safefile.TempName(abs)) {
				// Nothing of it is left (the cover it replaced is in the journal's images).
				e.close(ctx, entry, false, fmt.Errorf("%w; the cover was gone after it", interrupted))
				return
			}
			log.Error(ctx, "Could not settle an interrupted cover write; it is tried again at the next start", "entry", entry.ID, "path", abs, err)
			return
		}
	}
	log.Info(ctx, "Settled an interrupted cover write", "path", abs, "written", written)
	e.close(ctx, entry, written, interrupted)
	if written {
		e.coverDone(ctx, lib, path.Dir(entry.NewPath))
	}
}

// coverOf is the cover File's bytes for an Embed in Files: the album's folder cover.
func (e *Editor) coverOf(ctx context.Context, albumID string) ([]byte, error) {
	a, err := e.albumFolder(ctx, albumID)
	if err != nil {
		return nil, err
	}
	cur, err := e.readCover(a.abs())
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, ErrNoFolderCover
	}
	return cur, nil
}
