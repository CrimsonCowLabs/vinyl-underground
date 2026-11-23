package nativeapi

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// defaultEditWait is how long an edit waits for its job to end before it answers that the job
// runs in the background: long enough for a single File on a slow share.
const defaultEditWait = 5 * time.Second

// SetEditQueue gives the router the edit job queue its edits are queued on (#162). With none
// (editing off at startup), edits answer editingOff.
func (api *Router) SetEditQueue(q *editjob.Queue) {
	api.editQueue = q
}

// addTrackTagsRoute lets the admin read and edit one Track's tags in its File (#160, ADR-0011),
// and follow the edit jobs (#162). It is admin only (see routes), and answers editingOff while
// the Track's library is read-only.
func (api *Router) addTrackTagsRoute(r chi.Router) {
	r.Route("/trackTags/{id}", func(r chi.Router) {
		r.Get("/", api.readTrackTags)
		r.Put("/", api.editTrackTags)
	})
	r.Get("/editJob", api.listEditJobs)
	r.Get("/editJob/{id}", api.getEditJob)
	r.Post("/editJob/{id}/undo", api.undoEditJob)
}

func (api *Router) readTrackTags(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	tags, err := api.tagEditor.Read(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, tags)
}

// editResult is what an edit answers: the edit's outcome, once its job has ended, or only the
// job (Background) while it is still running.
type editResult struct {
	tagedit.Result
	JobID      string `json:"jobId"`
	Background bool   `json:"background,omitempty"`
}

// editTrackTags queues the edit as a job of one File, and waits a little for it: a single File
// is usually written in that time, and the answer is the edit's outcome, as if it had been made
// inline. Otherwise it answers 202, and the job carries on in the background.
func (api *Router) editTrackTags(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil || api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	var body struct {
		// Values are the fields to change, by key; a field left out is left as it is.
		Values tagedit.Values `json:"values"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeTagEditError(w, r, tagedit.ErrInvalidTags)
		return
	}
	step, err := editjob.TagsStep(chi.URLParam(r, "id"), body.Values)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	api.runFileJob(w, r, model.EditTags, step)
}

// runFileJob queues a job of one File's step, and waits a little for it (see editTrackTags).
// A job that ends in time answers the step's outcome, or its refusal as an error; a longer one
// answers 202 with the job's ID.
func (api *Router) runFileJob(w http.ResponseWriter, r *http.Request, kind model.EditKind, step model.EditJobStep) {
	ctx := r.Context()
	job, err := api.editQueue.Enqueue(ctx, kind, []model.EditJobStep{step})
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	job, err = api.editQueue.Wait(ctx, job.ID, cmp.Or(api.editWait, defaultEditWait))
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	if job.State != model.EditJobDone || len(job.Steps) == 0 || !ended(job.Steps[0].State) {
		_ = rest.RespondWithJSON(w, http.StatusAccepted, editResult{JobID: job.ID, Background: true})
		return
	}
	s := job.Steps[0]
	if s.State == model.EditStepSkipped {
		writeTagEditCode(w, r, s.Code, s.Error)
		return
	}
	res := editResult{JobID: job.ID, Result: tagedit.Result{
		Changed: s.State == model.EditStepWritten, Entry: s.EntryID, Warning: s.Warning}}
	if mf, err := api.ds.MediaFile(ctx).Get(s.MediaFileID); err == nil {
		res.Track = mf
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// ended reports whether a step's outcome is recorded. A done job's step may still read as running
// when its outcome couldn't be recorded; the queue settles it later.
func ended(s model.EditStepState) bool {
	return s == model.EditStepWritten || s == model.EditStepUnchanged || s == model.EditStepSkipped
}

// listEditJobs returns the latest edit jobs (?_end, default 20), newest first, each with the
// Files it skipped.
func (api *Router) listEditJobs(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		_ = rest.RespondWithJSON(w, http.StatusOK, model.EditJobs{})
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("_end"))
	if err != nil || limit <= 0 || limit > 200 {
		limit = 20
	}
	jobs, err := api.editQueue.Jobs(r.Context(), limit)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, jobs)
}

// getEditJob returns an edit job, with all its steps.
func (api *Router) getEditJob(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeTagEditError(w, r, model.ErrNotFound)
		return
	}
	job, err := api.editQueue.Job(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, job)
}

// undoEditJob undoes every File an edit job wrote, as a job of its own (newest File first), and
// waits a little for it. It answers the undo job: done, with the Files it skipped and why, or,
// with 202, still running in the background.
func (api *Router) undoEditJob(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	ctx := r.Context()
	steps, err := editjob.UndoJobSteps(ctx, api.ds, chi.URLParam(r, "id"))
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	job, err := api.editQueue.Enqueue(ctx, model.EditUndo, steps)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	if job, err = api.editQueue.Wait(ctx, job.ID, cmp.Or(api.editWait, defaultEditWait)); err != nil {
		writeTagEditError(w, r, err)
		return
	}
	status := http.StatusOK
	if job.State != model.EditJobDone {
		status = http.StatusAccepted
	}
	// The answer lists the skipped Files only, as the job list does.
	skipped := model.EditJobSteps{}
	for _, s := range job.Steps {
		if s.State == model.EditStepSkipped {
			skipped = append(skipped, s)
		}
	}
	job.Steps = skipped
	_ = rest.RespondWithJSON(w, status, job)
}

// tagEditStatus is the HTTP status of each tagedit.Code.
var tagEditStatus = map[string]int{
	"notFound":              http.StatusNotFound,
	"editingOff":            http.StatusForbidden,
	"changedOnDisk":         http.StatusConflict,
	"invalid":               http.StatusBadRequest,
	"unsupportedFormat":     http.StatusUnprocessableEntity,
	"notVerified":           http.StatusUnprocessableEntity,
	"linked":                http.StatusUnprocessableEntity,
	"oldCopyLeft":           http.StatusConflict,
	"alreadyUndone":         http.StatusConflict,
	"unfinished":            http.StatusConflict,
	"tagsChanged":           http.StatusConflict,
	"changedLater":          http.StatusConflict,
	"notUndoable":           http.StatusUnprocessableEntity,
	editjob.CodeInterrupted: http.StatusServiceUnavailable,
	tagedit.CodeInternal:    http.StatusInternalServerError,
	"sameFileTwice":         http.StatusUnprocessableEntity,
	"jobUnfinished":         http.StatusConflict,
}

// writeTagEditError answers a failed read or edit with a code the dialog translates, and the
// error's own words for the admin.
func writeTagEditError(w http.ResponseWriter, r *http.Request, err error) {
	code := editjob.Code(err)
	if code == tagedit.CodeInternal {
		log.Error(r.Context(), "Error editing a Track's tags", err)
	} else {
		log.Debug(r.Context(), "Tag edit refused", "code", code, err)
	}
	writeTagEditCode(w, r, code, err.Error())
}

func writeTagEditCode(w http.ResponseWriter, _ *http.Request, code, message string) {
	status, ok := tagEditStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code, "message": message})
}
