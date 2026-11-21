package rip

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/rip/propose"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"go.senan.xyz/taglib"
)

// MaxUploadSize is the biggest File the admin can upload: 500 MB.
const MaxUploadSize int64 = 500 << 20

// uploadDirName is the hidden Inbox folder an uploaded File is streamed into, so the scanner
// never sees it half-written (it skips hidden folders), and the finished File is renamed into
// place on the same disk.
const uploadDirName = ".uploads"

// UploadFormats are the extensions of the Files the admin can upload.
var UploadFormats = []string{".mp3", ".m4a", ".opus", ".ogg", ".flac", ".aiff", ".aif", ".wav"}

// UploadRefusedError is a File refused by Upload: not one of UploadFormats, not really audio of
// its type, or too big. Nothing of it is kept.
type UploadRefusedError struct {
	Name   string
	Reason string
	// TooBig is true for a File over the size limit, which was read only up to the limit.
	TooBig bool
}

func (e *UploadRefusedError) Error() string { return e.Name + ": " + e.Reason }

// ErrNotAnUpload is returned when a File is to be added to a job that isn't an upload.
var ErrNotAnUpload = errors.New("only an upload's card can take more uploaded Files")

const formatsText = "mp3, m4a, opus, ogg, flac, aiff or wav"

// Upload streams one uploaded File into the Inbox and adds it to an upload job (#35): the job
// jobID, or with "" a new one, in its own Inbox subfolder, created once its first File is
// accepted. The File must have one of UploadFormats' extensions, must read as audio of that type
// (its first bytes, then TagLib), and may be at most Options.MaxUploadSize (500 MB): it is
// refused with an *UploadRefusedError otherwise, and nothing of it is kept.
//
// The File is streamed to a hidden temp File inside the Inbox, never held in memory, and the
// size limit is checked as it streams. Only then is it moved into place, never over an existing
// File (" (2)" is added instead). Its tags are left exactly as they are: no SOURCE is written.
// Its item is ready at once, and its "already have" matches and tag proposal are worked out as
// for a Rip's.
func (r *Ripper) Upload(ctx context.Context, jobID, name string, body io.Reader) (*model.RipJob, *model.RipItem, error) {
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err == nil && inbox == nil {
		err = ErrNoInbox
	}
	if err != nil {
		return nil, nil, err
	}
	var job *model.RipJob
	if jobID != "" {
		if job, err = r.ds.RipJob(ctx).Get(jobID); err != nil {
			return nil, nil, err
		}
		if job.Kind != model.RipKindUpload {
			return nil, nil, ErrNotAnUpload
		}
	}

	name, ext := uploadName(name)
	if !slices.Contains(UploadFormats, ext) {
		return nil, nil, &UploadRefusedError{Name: name, Reason: "only " + formatsText + " Files can be uploaded"}
	}
	tmp, err := r.receive(inbox.Path, name, ext, body)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.Remove(tmp) }() // a no-op once it is moved into place
	in, err := fileInput(tmp, name)
	if err != nil {
		return nil, nil, err
	}

	job, item, err := r.placeUpload(ctx, inbox, job, tmp, name, in)
	if err != nil {
		return nil, nil, err
	}
	log.Info(ctx, "Uploaded a File into the Inbox", "id", job.ID, "path", item.Path)
	batch := model.RipItems{*item}
	r.landed(ctx, inbox, batch)
	r.scanInboxSoon(ctx, inbox.ID)
	return job, &batch[0], nil
}

// placeUpload moves the received File at tmp into the job's folder (a new upload job's, for a
// nil job) and saves its item. It holds the landing lock, so the drop reconciler never takes the
// File for a dropped one, and the review lock, so a Discard or Promote of the same job can't
// slip in between.
func (r *Ripper) placeUpload(ctx context.Context, inbox *model.Library, job *model.RipJob, tmp, name string, in propose.Input) (*model.RipJob, *model.RipItem, error) {
	r.landing.Lock()
	defer r.landing.Unlock()
	r.reviewMu.Lock()
	defer r.reviewMu.Unlock()
	var err error
	created := job == nil
	if created {
		job, err = r.newUploadJob(ctx, inbox.Path)
	} else {
		job, err = r.ds.RipJob(ctx).Get(job.ID) // still there?
	}
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*model.RipJob, *model.RipItem, error) {
		if created {
			// No card is left without its first File.
			if derr := r.ds.RipJob(ctx).Delete(job.ID); derr != nil {
				log.Warn(ctx, "Could not remove an empty upload", "id", job.ID, derr)
			}
			_ = os.Remove(filepath.Join(inbox.Path, job.Folder))
		}
		return nil, nil, err
	}
	items, err := r.ds.RipItem(ctx).ForJobs(job.ID)
	if err != nil {
		return fail(err)
	}
	dir, err := inboxDir(inbox.Path, job.Folder)
	if err != nil {
		return fail(err)
	}
	dest, err := moveIntoInbox(tmp, dir, name)
	if err != nil {
		return fail(err)
	}
	rel := filepath.ToSlash(filepath.Join(job.Folder, filepath.Base(dest)))
	item := newFileItem(job.ID, nextPosition(items), rel, in, time.Now())
	if err := r.ds.RipItem(ctx).Put(&item); err != nil {
		_ = os.Remove(dest)
		return fail(err)
	}
	return job, &item, nil
}

// receive streams body into a new hidden temp File inside the Inbox, checking its first bytes
// are the File type its extension says and its size stays within the limit, and then that
// TagLib reads it as audio. It returns the temp File's path; the caller removes it.
func (r *Ripper) receive(inboxPath, name, ext string, body io.Reader) (string, error) {
	limit := r.maxUploadSize()
	br := bufio.NewReaderSize(body, sniffLen)
	head, err := br.Peek(sniffLen)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	if !sniffAudio(ext, head) {
		return "", &UploadRefusedError{Name: name, Reason: "its content isn't " + kindOf(ext) + " audio; only " + formatsText + " Files can be uploaded"}
	}

	dir := filepath.Join(inboxPath, uploadDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "upload-*"+ext)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(br, limit+1))
	err = errors.Join(err, f.Sync(), f.Close())
	switch {
	case err != nil:
		err = fmt.Errorf("receiving %s: %w", name, err)
	case n > limit:
		err = &UploadRefusedError{Name: name, TooBig: true, Reason: fmt.Sprintf("it is bigger than %d MB, the most one File can be", limit>>20)}
	default:
		props, perr := taglib.ReadProperties(f.Name())
		if perr != nil || (props.SampleRate == 0 && props.Length == 0) {
			err = &UploadRefusedError{Name: name, Reason: "it doesn't read as " + kindOf(ext) + " audio"}
		}
	}
	if err != nil {
		// The hidden .uploads folder stays: another upload may be about to stream into it.
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (r *Ripper) maxUploadSize() int64 {
	if r.opts.MaxUploadSize > 0 {
		return r.opts.MaxUploadSize
	}
	return MaxUploadSize
}

// newUploadJob saves a new upload job, with its own Inbox subfolder named after when it began.
func (r *Ripper) newUploadJob(ctx context.Context, inboxPath string) (*model.RipJob, error) {
	now := time.Now()
	folder, err := claimDir(inboxPath, "Upload "+now.Format("2006-01-02 15.04.05"))
	if err != nil {
		return nil, err
	}
	user, _ := request.UserFrom(ctx)
	job := &model.RipJob{Kind: model.RipKindUpload, UserID: user.ID, Status: model.RipReady,
		Title: folder, Folder: folder, CreatedAt: now, UpdatedAt: now}
	if err := r.ds.RipJob(ctx).Put(job); err != nil {
		_ = os.Remove(filepath.Join(inboxPath, folder))
		return nil, err
	}
	return job, nil
}

// claimDir makes a new, empty Inbox folder named base, or "base (2)", "base (3)"... when one with
// Files in it is there already, and returns its name. An empty folder is reused.
func claimDir(inboxPath, base string) (string, error) {
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		dir := filepath.Join(inboxPath, name)
		if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			return name, nil
		}
	}
	return "", fmt.Errorf("too many folders named %q in the Inbox", base)
}

// inboxDir is the job's Inbox folder (the Inbox itself for ""), made again if it is gone.
func inboxDir(root, folder string) (string, error) {
	if folder == "" {
		return root, nil
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(folder)), 0o755); err != nil {
		return "", err
	}
	return inboxPath(root, folder)
}

func nextPosition(items model.RipItems) int {
	pos := 0
	for _, it := range items {
		pos = max(pos, it.Position)
	}
	return pos + 1
}

// uploadName is the uploaded File's name, made safe for any disk, and its extension in lower case.
func uploadName(name string) (string, string) {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimLeft(truncate(sanitize(strings.TrimSuffix(name, filepath.Ext(name))), 160), ". -")
	if base == "" {
		base = "Upload"
	}
	return base + ext, ext
}

// kindOf names the File type of the extension ext: "MP3", "AIFF".
func kindOf(ext string) string {
	if ext == ".aif" {
		ext = ".aiff"
	}
	return strings.ToUpper(strings.TrimPrefix(ext, "."))
}

// sniffLen is how much of a File is looked at to tell its type.
const sniffLen = 64

// sniffAudio reports whether head, the first bytes of a File, are those of the audio type its
// extension ext says: an MPEG audio frame or an ID3 tag for mp3, an MP4 "ftyp" box for m4a, an
// Ogg page for ogg and opus (an Opus stream for opus), "fLaC" for flac, an AIFF FORM and a WAVE
// RIFF. TagLib then reads the whole File to confirm it.
func sniffAudio(ext string, head []byte) bool {
	at := func(off int, s string) bool {
		return len(head) >= off+len(s) && string(head[off:off+len(s)]) == s
	}
	id3 := at(0, "ID3")
	switch ext {
	case ".mp3":
		return id3 || (len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0)
	case ".m4a":
		return at(4, "ftyp")
	case ".ogg":
		return at(0, "OggS")
	case ".opus":
		return at(0, "OggS") && bytes.Contains(head, []byte("OpusHead"))
	case ".flac":
		return at(0, "fLaC")
	case ".aiff", ".aif":
		return at(0, "FORM") && (at(8, "AIFF") || at(8, "AIFC"))
	case ".wav":
		return (at(0, "RIFF") || at(0, "RF64")) && at(8, "WAVE")
	}
	return false
}
