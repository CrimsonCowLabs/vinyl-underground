package nativeapi

import (
	"net/http"
	"strconv"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/req"
)

// addEditHistoryRoute lists the Edit journal, newest first, and undoes one entry (#161). It is
// admin only (see routes). The list is paged by react-admin's _start and _end.
func (api *Router) addEditHistoryRoute(r chi.Router) {
	r.Route("/editHistory", func(r chi.Router) {
		r.Get("/", api.listEditHistory)
		r.Post("/{id}/undo", api.undoEdit)
	})
}

// maxHistoryPage bounds one page of the Edit history.
const maxHistoryPage = 100

func (api *Router) listEditHistory(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	p := req.Params(r)
	start := max(p.IntOr("_start", 0), 0)
	end := p.IntOr("_end", start+50)
	limit := min(max(end-start, 1), maxHistoryPage)
	entries, total, err := api.tagEditor.History(r.Context(), start, limit)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	_ = rest.RespondWithJSON(w, http.StatusOK, entries)
}

// undoEdit undoes one entry as an edit job of one File (#162), as the per-Track Edit runs: it
// waits a little for the job, then answers that it runs in the background.
func (api *Router) undoEdit(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil || api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	step, err := editjob.UndoStep(chi.URLParam(r, "id"))
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	api.runFileJob(w, r, model.EditUndo, step)
}
