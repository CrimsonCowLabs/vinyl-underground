package nativeapi

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// maxPreviewRows bounds the rows of one page of a bulk edit's preview.
const maxPreviewRows = 500

// addLibraryManagerRoute is the Library manager (#166), admin only (see routes):
//   - GET /libraryTrack: the Track listing, with the Library manager's filters (folder, artists_id,
//     album_id, format, rip, missing_tag, title for free text), paged and sorted as react-admin asks;
//   - GET /libraryManager/fields: the fields a bulk edit can set;
//   - POST /libraryManager/preview?_start&_end: what a bulk edit would do, Track by Track;
//   - POST /libraryManager/edit: runs it, as one edit job.
func (api *Router) addLibraryManagerRoute(r chi.Router) {
	api.R(r, "/libraryTrack", model.MediaFile{}, false)
	r.Route("/libraryManager", func(r chi.Router) {
		r.Get("/fields", func(w http.ResponseWriter, r *http.Request) {
			_ = rest.RespondWithJSON(w, http.StatusOK, librarymanager.Fields())
		})
		r.Post("/preview", api.previewBulkEdit)
		r.Post("/edit", api.runBulkEdit)
	})
}

func (api *Router) readBulkEdit(w http.ResponseWriter, r *http.Request) (librarymanager.BulkEdit, bool) {
	var edit librarymanager.BulkEdit
	// A selection names at most librarymanager.MaxSelected Tracks; more go by filter.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
		writeBulkEditError(w, r, tagedit.ErrInvalidTags)
		return edit, false
	}
	return edit, true
}

func (api *Router) previewBulkEdit(w http.ResponseWriter, r *http.Request) {
	edit, ok := api.readBulkEdit(w, r)
	if !ok {
		return
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("_start"))
	end, err := strconv.Atoi(r.URL.Query().Get("_end"))
	if err != nil || end <= start {
		end = start + 100
	}
	p, err := librarymanager.New(api.ds, api.editQueue).Preview(r.Context(), edit, start, min(end-start, maxPreviewRows))
	if err != nil {
		writeBulkEditError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, p)
}

// runBulkEdit queues the bulk edit as one job and waits a little for it, as an undo of a whole
// job does: it answers the job once done, with the Files it skipped and why, or, with 202,
// while it runs on in the background (the Activity panel follows it).
func (api *Router) runBulkEdit(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeBulkEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	edit, ok := api.readBulkEdit(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	job, err := librarymanager.New(api.ds, api.editQueue).Run(ctx, edit)
	if err != nil {
		writeBulkEditError(w, r, err)
		return
	}
	if job, err = api.editQueue.Wait(ctx, job.ID, cmp.Or(api.editWait, defaultEditWait)); err != nil {
		writeBulkEditError(w, r, err)
		return
	}
	status := http.StatusOK
	if job.State != model.EditJobDone {
		status = http.StatusAccepted
	}
	skipped := model.EditJobSteps{}
	for _, s := range job.Steps {
		if s.State == model.EditStepSkipped {
			skipped = append(skipped, s)
		}
	}
	job.Steps = skipped
	_ = rest.RespondWithJSON(w, status, job)
}

// bulkEditStatus is the HTTP status of the Library manager's own codes; the others are an
// edit's (tagEditStatus).
var bulkEditStatus = map[string]int{
	"noSelection":     http.StatusBadRequest,
	"tooMany":         http.StatusUnprocessableEntity,
	"nothingToChange": http.StatusUnprocessableEntity,
}

func writeBulkEditError(w http.ResponseWriter, r *http.Request, err error) {
	code := librarymanager.Code(err)
	status, ok := bulkEditStatus[code]
	if !ok {
		writeTagEditError(w, r, err)
		return
	}
	log.Debug(r.Context(), "Bulk edit refused", "code", code, err)
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code, "message": err.Error()})
}
