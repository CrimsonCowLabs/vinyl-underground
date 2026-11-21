package ripapi

// Uploads into the Inbox (#35), admin only like the rest of the Rip API, and there whenever the
// Inbox is, with ripping on or off:
//
//	POST /api/rip/upload[?job={jobId}]   multipart/form-data with one or more "file" parts. Each
//	                                     File goes into the upload job jobId, or without it a new
//	                                     one (in its own Inbox subfolder), so a batch sent one File
//	                                     per request ends up on one card.
//	                                     201: {job, files: [{name, itemId, path}, {name, error}]}
//	                                          when at least one File was taken
//	                                     422: {error, job: null, files} when none was
//
// Accepted: mp3, m4a, opus, ogg, flac, aiff and wav, up to 500 MB each. It answers 404 for an
// unknown job, 409 for a job that isn't an upload or with no Inbox, and 413 for a request over
// the size limit.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Uploader is the part of *rip.Ripper that takes uploads.
type Uploader interface {
	Upload(ctx context.Context, jobID, name string, body io.Reader) (*model.RipJob, *model.RipItem, error)
}

const (
	// maxUploadFiles is the most Files one request can carry (the web app sends one at a time).
	maxUploadFiles = 20
	// uploadStall is how long an upload may send nothing before it is given up.
	uploadStall = 2 * time.Minute
)

// UploadedFile is one File of an upload request: where it went, or why it was refused.
type UploadedFile struct {
	Name   string `json:"name"`
	ItemID string `json:"itemId,omitempty"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

type uploadResponse struct {
	Error string         `json:"error,omitempty"`
	Job   *model.RipJob  `json:"job"`
	Files []UploadedFile `json:"files"`
}

func (api *Router) upload(w http.ResponseWriter, r *http.Request) {
	// No whole-request deadline for big Files on slow links (this route only): the read deadline
	// moves on as long as data keeps coming. A server without deadlines ignores this.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	body := &stallReader{r: r.Body, rc: rc}
	r.Body = http.MaxBytesReader(w, body, maxUploadFiles*(rip.MaxUploadSize+(1<<20)))
	mr, err := r.MultipartReader()
	if err != nil {
		_ = rest.RespondWithError(w, http.StatusBadRequest, "Send the Files as multipart/form-data, in \"file\" parts")
		return
	}

	// The Files outlive the request once they are in.
	ctx := context.WithoutCancel(r.Context())
	jobID := r.URL.Query().Get("job")
	res := uploadResponse{Files: []UploadedFile{}}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			api.respondUploadError(w, r, res, err)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if len(res.Files) == maxUploadFiles {
			// The rest isn't read: say so, rather than leave them out unnoticed.
			res.Files = append(res.Files, UploadedFile{Name: part.FileName(),
				Error: fmt.Sprintf("%s: at most %d Files can be sent at once; send this one and the rest again", part.FileName(), maxUploadFiles)})
			_ = part.Close()
			w.Header().Set("Connection", "close")
			break
		}
		f, stop, err := api.uploadPart(ctx, jobID, part)
		if err != nil {
			api.respondUploadError(w, r, res, err)
			return
		}
		res.Files = append(res.Files, f.file)
		if f.job != nil {
			res.Job, jobID = f.job, f.job.ID
		}
		if stop {
			// The rest of a too-big File isn't read: the request ends here.
			w.Header().Set("Connection", "close")
			break
		}
	}
	switch {
	case res.Job != nil:
		_ = rest.RespondWithJSON(w, http.StatusCreated, res)
	case len(res.Files) == 0:
		res.Error = "Send at least one File, in a \"file\" part"
		_ = rest.RespondWithJSON(w, http.StatusBadRequest, res)
	default:
		res.Error = res.Files[0].Error
		if len(res.Files) > 1 {
			res.Error = "None of the Files could be uploaded"
		}
		_ = rest.RespondWithJSON(w, http.StatusUnprocessableEntity, res)
	}
}

type partResult struct {
	file UploadedFile
	job  *model.RipJob
}

// uploadPart uploads one File. A refused File is a result, not an error; stop is true when the
// request can't go on (the File was too big, and the rest of it is still unread).
func (api *Router) uploadPart(ctx context.Context, jobID string, part *multipart.Part) (partResult, bool, error) {
	defer part.Close()
	res := partResult{file: UploadedFile{Name: part.FileName()}}
	job, item, err := api.ripper.Upload(ctx, jobID, part.FileName(), part)
	var refused *rip.UploadRefusedError
	switch {
	case errors.As(err, &refused):
		res.file.Error = refused.Error()
		return res, refused.TooBig, nil
	case err != nil:
		return res, false, err
	}
	res.file.ItemID, res.file.Path, res.job = item.ID, item.Path, job
	return res, false, nil
}

func (api *Router) respondUploadError(w http.ResponseWriter, r *http.Request, res uploadResponse, err error) {
	var tooBig *http.MaxBytesError
	status, msg := http.StatusInternalServerError, "Could not upload the Files: "+err.Error()
	switch {
	case errors.As(err, &tooBig):
		status, msg = http.StatusRequestEntityTooLarge, fmt.Sprintf("Send at most %d Files of up to %d MB at once", maxUploadFiles, rip.MaxUploadSize>>20)
	case errors.Is(err, model.ErrNotFound):
		status, msg = http.StatusNotFound, "This upload is not in the Inbox any more"
	case errors.Is(err, rip.ErrNotAnUpload), errors.Is(err, rip.ErrNoInbox):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, errStalled):
		status, msg = http.StatusRequestTimeout, err.Error()
	default:
		log.Error(r.Context(), "Could not upload Files into the Inbox", err)
	}
	res.Error = msg
	if res.Job != nil {
		// Some Files are on a card already: say which, and why the request stopped.
		status = http.StatusCreated
	}
	_ = rest.RespondWithJSON(w, status, res)
}

var errStalled = fmt.Errorf("the upload stopped sending for %s", uploadStall)

// stallReader gives each read of the request body uploadStall to arrive, instead of one deadline
// for the whole of a big upload.
type stallReader struct {
	r  io.ReadCloser
	rc *http.ResponseController
}

func (s *stallReader) Read(p []byte) (int, error) {
	if err := s.rc.SetReadDeadline(time.Now().Add(uploadStall)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	n, err := s.r.Read(p)
	if isTimeout(err) {
		err = errStalled
	}
	return n, err
}

func (s *stallReader) Close() error { return s.r.Close() }

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}
