package tagedit

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// frontCover is the picture type an embedded cover gets.
const frontCover = "Front Cover"

// FolderCover returns the album's folder cover (the cover File WriteCover writes), for Embed in
// Files: ErrNoFolderCover when its folder has none.
func (e *Editor) FolderCover(ctx context.Context, albumID string) ([]byte, error) {
	return e.coverOf(ctx, albumID)
}

// KeepImage keeps the image in the journal's images (see journalImages), where a queued cover
// write or embed reads it from when it runs, and returns its checksum.
func (e *Editor) KeepImage(data []byte) (string, error) { return e.images.put(data) }

// KeptImage returns the image KeepImage kept under sum, checked against it.
func (e *Editor) KeptImage(sum string) ([]byte, error) { return e.images.read(sum) }

// EmbedCoverInJob embeds the image into the Track's File, as its first picture (replacing the
// one it had there, or as its only one), through the same write path as Edit, as one step of the
// edit job jobID (Embed in Files): guard, journal (the picture it replaces is kept in the journal's
// images), temp copy, verify (the picture reads back, every tag and other picture is as it was, an
// mp3 is ID3v2.4, and the audio data is identical), replace, re-key. A File whose first picture is
// the image already isn't written.
func (e *Editor) EmbedCoverInJob(ctx context.Context, jobID, trackID string, image []byte) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	f, err := e.file(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if err := e.guard(f); err != nil {
		return nil, err
	}
	pics, err := e.tagger.Pictures(f.abs)
	if err != nil {
		return nil, err
	}
	if len(pics) > 0 && bytes.Equal(pics[0].Data, image) {
		return &Result{Changed: false, Track: f.mf}, nil
	}
	if _, err := e.images.put(image); err != nil {
		return nil, err
	}
	want := &Picture{Data: image, Type: frontCover, Mime: http.DetectContentType(image)}
	return e.embed(ctx, jobID, f, pics, want, "")
}

// embed journals and writes want (nil removes it) as the first picture of the File f, whose
// pictures are pics, and re-keys its Track. undoOf is the entry it undoes, if it is an undo.
func (e *Editor) embed(ctx context.Context, jobID string, f *file, pics []Picture, want *Picture, undoOf string) (*Result, error) {
	orig, err := e.tagger.Read(f.abs)
	if err != nil {
		return nil, err
	}
	entry := &model.EditEntry{
		JobID: jobID, Kind: model.EditEmbed, State: model.EditPending,
		LibraryID: f.mf.LibraryID, MediaFileID: f.mf.ID, Path: f.mf.Path, NewPath: f.mf.Path, UndoOf: undoOf,
	}
	if want != nil {
		entry.Image = model.EditImage{Sum: hexSum(want.Data), Mime: want.Mime, Type: want.Type, Description: want.Description}
	}
	if len(pics) > 0 {
		sum, err := e.images.put(pics[0].Data)
		if err != nil {
			return nil, err
		}
		entry.OriginalImage = model.EditImage{Sum: sum, Mime: pics[0].Mime, Type: pics[0].Type, Description: pics[0].Description}
	}
	if u, ok := request.UserFrom(ctx); ok {
		entry.UserID = u.ID
	}
	if err := e.ds.EditJournal(ctx).Put(entry); err != nil {
		return nil, fmt.Errorf("recording the embed in the Edit journal: %w", err)
	}

	// What the File's pictures must read back as.
	expected := pics
	switch {
	case want == nil && len(pics) > 0:
		expected = pics[1:]
	case want != nil && len(pics) > 0:
		expected = append([]Picture{*want}, pics[1:]...)
	case want != nil:
		expected = []Picture{*want}
	}
	write := func(temp string) error { return e.tagger.WritePicture(temp, want) }
	check := func(temp string) error {
		if err := e.verify(f.abs, temp, orig, nil); err != nil {
			return err
		}
		got, err := e.tagger.Pictures(temp)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNotVerified, err)
		}
		if len(got) != len(expected) {
			return fmt.Errorf("%w: it has %d pictures, not %d", ErrNotVerified, len(got), len(expected))
		}
		for i := range got {
			if !bytes.Equal(got[i].Data, expected[i].Data) {
				return fmt.Errorf("%w: picture %d doesn't read back as written", ErrNotVerified, i+1)
			}
		}
		return nil
	}
	if err := e.closeWrite(ctx, f.abs, entry, e.write(ctx, f.abs, entry, write, check)); err != nil {
		log.Warn(ctx, "Could not embed a cover into a File", "id", f.mf.ID, "path", f.abs, err)
		return nil, err
	}
	log.Info(ctx, "Embedded a cover into a File", "id", f.mf.ID, "path", f.abs, "removed", want == nil)
	res := &Result{Changed: true, Entry: entry.ID, Track: f.mf}
	if err := e.rekey(ctx, f); err != nil {
		log.Warn(ctx, "Scan after an embed failed; the Track shows its picture after the next scan", "path", f.abs, err)
		res.Warning = "The File was written; the app shows its new picture after the next scan."
	}
	if mf, err := e.ds.MediaFile(ctx).Get(f.mf.ID); err == nil {
		res.Track = mf
	}
	return res, nil
}

// undoEmbed undoes an embed (see undoers): it puts the File's first picture back as it was before,
// or removes it when the File had none, while that picture is still the one the embed wrote.
func (e *Editor) undoEmbed(ctx context.Context, jobID string, entry *model.EditEntry) (*Result, error) {
	err := e.checkLater(ctx, entry, func(l model.EditEntry) error {
		switch {
		case l.NewPath != l.Path:
			return fmt.Errorf("%w: the File was moved since", ErrChangedLater)
		case l.Kind == model.EditEmbed:
			return fmt.Errorf("%w: a cover was embedded into the File again since", ErrChangedLater)
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
	pics, err := e.tagger.Pictures(f.abs)
	if err != nil {
		return nil, err
	}
	if len(pics) == 0 || hexSum(pics[0].Data) != entry.Image.Sum {
		return nil, ErrCoverChanged
	}
	var want *Picture
	if o := entry.OriginalImage; o.Sum != "" {
		data, err := e.images.read(o.Sum)
		if err != nil {
			return nil, err
		}
		want = &Picture{Data: data, Type: o.Type, Description: o.Description, Mime: o.Mime}
	}
	return e.embed(ctx, jobID, f, pics, want, entry.ID)
}
