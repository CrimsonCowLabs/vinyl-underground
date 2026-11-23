package nativeapi

import (
	"net/http"
	"strconv"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/tagedit"
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

func (api *Router) undoEdit(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	res, err := api.tagEditor.Undo(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}
