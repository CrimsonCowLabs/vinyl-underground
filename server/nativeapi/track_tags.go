package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addTrackTagsRoute lets the admin read and edit one Track's tags in its File (#160, ADR-0011).
// It is admin only (see routes), and answers editingOff while the Track's library is read-only.
func (api *Router) addTrackTagsRoute(r chi.Router) {
	r.Route("/trackTags/{id}", func(r chi.Router) {
		r.Get("/", api.readTrackTags)
		r.Put("/", api.editTrackTags)
	})
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

func (api *Router) editTrackTags(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil {
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
	res, err := api.tagEditor.Edit(r.Context(), chi.URLParam(r, "id"), body.Values)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// writeTagEditError answers a failed read or edit with a code the dialog translates, and the
// error's own words for the admin.
func writeTagEditError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "internal"
	switch {
	case errors.Is(err, model.ErrNotFound):
		status, code = http.StatusNotFound, "notFound"
	case errors.Is(err, tagedit.ErrEditingOff):
		status, code = http.StatusForbidden, "editingOff"
	case errors.Is(err, tagedit.ErrChangedOnDisk):
		status, code = http.StatusConflict, "changedOnDisk"
	case errors.Is(err, tagedit.ErrInvalidTags):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, tagedit.ErrUnsupportedFormat):
		status, code = http.StatusUnprocessableEntity, "unsupportedFormat"
	case errors.Is(err, tagedit.ErrNotVerified):
		status, code = http.StatusUnprocessableEntity, "notVerified"
	case errors.Is(err, safefile.ErrLinked):
		status, code = http.StatusUnprocessableEntity, "linked"
	case errors.Is(err, tagedit.ErrChangedLater):
		status, code = http.StatusConflict, "changedLater"
	case errors.Is(err, tagedit.ErrNotUndoable):
		status, code = http.StatusUnprocessableEntity, "notUndoable"
	case errors.Is(err, tagedit.ErrOldCopyLeft):
		status, code = http.StatusConflict, "oldCopyLeft"
	}
	if status == http.StatusInternalServerError {
		log.Error(r.Context(), "Error editing a Track's tags", err)
	} else {
		log.Debug(r.Context(), "Tag edit refused", "code", code, err)
	}
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code, "message": err.Error()})
}
