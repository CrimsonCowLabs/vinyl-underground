package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addCorrectionRoute serves the admin's Corrections (admin only, see routes).
func (api *Router) addCorrectionRoute(r chi.Router) {
	r.Route("/correction", func(r chi.Router) {
		r.Get("/", api.listCorrections)
		r.Post("/artistRename", api.renameArtist)
		r.Post("/{id}/undo", api.undoCorrection)
	})
}

func (api *Router) listCorrections(w http.ResponseWriter, r *http.Request) {
	all, err := api.corrections.GetAll(r.Context())
	if err != nil {
		correctionError(w, r, err)
		return
	}
	if all == nil {
		all = model.Corrections{}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(all)))
	_ = rest.RespondWithJSON(w, http.StatusOK, all)
}

func (api *Router) renameArtist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ArtistID string `json:"artistId"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		correctionError(w, r, model.ErrValidation)
		return
	}
	res, err := api.corrections.RenameArtist(r.Context(), body.ArtistID, body.Name)
	if err != nil {
		correctionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

func (api *Router) undoCorrection(w http.ResponseWriter, r *http.Request) {
	res, err := api.corrections.Undo(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		correctionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// correctionError answers with a status and a code the UI translates; message is for logs and
// other clients.
func correctionError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "error"
	switch {
	case errors.Is(err, corrections.ErrArtistExists):
		status, code = http.StatusConflict, "artistExists"
	case errors.Is(err, corrections.ErrRenamedAway):
		status, code = http.StatusBadRequest, "renamedAway"
	case errors.Is(err, model.ErrAlreadyExists):
		status, code = http.StatusConflict, "alreadyCorrected"
	case errors.Is(err, model.ErrValidation):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, model.ErrNotFound):
		status, code = http.StatusNotFound, "notFound"
	default:
		log.Error(r.Context(), "Error handling Correction", err)
	}
	_ = rest.RespondWithJSON(w, status, map[string]string{"code": code, "message": err.Error()})
}
