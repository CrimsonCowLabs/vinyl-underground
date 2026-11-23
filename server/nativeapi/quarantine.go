package nativeapi

import (
	"cmp"
	"encoding/json"
	"net/http"

	"github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addQuarantineRoute is Quarantine from the app (#168), admin only (see routes):
//   - GET /quarantine: what is in Quarantine (quarantine.Summary): every File, and their count and
//     total size;
//   - POST /quarantine/send: sends Tracks to Quarantine as one edit job: {ids} (at most
//     librarymanager.MaxSelected), {albumId}, or the Library manager's {filter};
//   - POST /quarantine/restore: restores one File, {libraryId, path}, as a job of one File;
//   - POST /quarantine/empty: Empty Quarantine, {count, size} as the admin confirmed them, as one
//     job; refused (quarantineChanged) when Quarantine holds other Files now.
//
// Each answers as the other edits do: the job, or the File's outcome, once done, or 202 while it
// runs on in the background.
func (api *Router) addQuarantineRoute(r chi.Router) {
	r.Route("/quarantine", func(r chi.Router) {
		r.Get("/", api.listQuarantine)
		r.Post("/send", api.sendToQuarantine)
		r.Post("/restore", api.restoreFromQuarantine)
		r.Post("/empty", api.emptyQuarantine)
	})
}

func (api *Router) listQuarantine(w http.ResponseWriter, r *http.Request) {
	c, err := quarantine.Contents(r.Context(), api.ds, safefile.OS{})
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, c)
}

// readQuarantineBody decodes the request's JSON body into v, answering invalid if it can't.
func readQuarantineBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeTagEditError(w, r, tagedit.ErrInvalidTags)
		return false
	}
	return true
}

func (api *Router) sendToQuarantine(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	var body struct {
		librarymanager.Selection
		AlbumID string `json:"albumId,omitempty"`
	}
	if !readQuarantineBody(w, r, &body) {
		return
	}
	ctx := r.Context()
	var tracks model.MediaFiles
	var err error
	if body.AlbumID != "" {
		tracks, err = api.ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "path",
			Filters: squirrel.Eq{"media_file.album_id": body.AlbumID, "media_file.missing": false}})
	} else {
		tracks, err = librarymanager.New(api.ds, api.editQueue).Tracks(ctx, body.Selection)
	}
	if err != nil {
		writeBulkEditError(w, r, err)
		return
	}
	if len(tracks) == 0 {
		writeBulkEditError(w, r, librarymanager.ErrNoSelection)
		return
	}
	steps := make([]model.EditJobStep, len(tracks))
	for i, mf := range tracks {
		steps[i] = editjob.QuarantineStep(mf.ID)
	}
	job, err := api.editQueue.Enqueue(ctx, model.EditQuarantine, steps)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	api.answerJob(w, r, job)
}

func (api *Router) restoreFromQuarantine(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	var body struct {
		LibraryID int    `json:"libraryId"`
		Path      string `json:"path"`
	}
	if !readQuarantineBody(w, r, &body) {
		return
	}
	step, err := editjob.RestoreFileStep(r.Context(), api.ds, body.LibraryID, body.Path)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	api.runFileJob(w, r, step.Kind, step)
}

func (api *Router) emptyQuarantine(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	var body struct {
		Count int   `json:"count"`
		Size  int64 `json:"size"`
	}
	if !readQuarantineBody(w, r, &body) {
		return
	}
	ctx := r.Context()
	step, err := editjob.ConfirmedEmptyStep(ctx, api.ds, body.Count, body.Size)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	job, err := api.editQueue.Enqueue(ctx, model.EditEmptyQuarantine, []model.EditJobStep{step})
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	api.answerJob(w, r, job)
}

// answerJob waits a little for the queued job, and answers it with the Files it skipped: done
// (200), or still running in the background (202), as a bulk edit does.
func (api *Router) answerJob(w http.ResponseWriter, r *http.Request, queued *model.EditJob) {
	ctx := r.Context()
	job, err := api.editQueue.Wait(ctx, queued.ID, cmp.Or(api.editWait, defaultEditWait))
	if err != nil {
		// The job is queued, and runs: an error here would have the admin queue it again.
		log.Warn(ctx, "Could not follow an edit job; it runs in the background", "job", queued.ID, err)
		queued.Steps = model.EditJobSteps{}
		_ = rest.RespondWithJSON(w, http.StatusAccepted, queued)
		return
	}
	status := http.StatusOK
	if job.State != model.EditJobDone {
		status = http.StatusAccepted
	}
	// The Files it skipped, and those with a note (Empty Quarantine's says what it left).
	skipped := model.EditJobSteps{}
	for _, s := range job.Steps {
		if s.State == model.EditStepSkipped || s.Warning != "" {
			skipped = append(skipped, s)
		}
	}
	job.Steps = skipped
	_ = rest.RespondWithJSON(w, status, job)
}
