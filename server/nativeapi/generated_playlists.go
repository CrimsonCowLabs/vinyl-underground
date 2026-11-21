package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/generatedplaylist"
	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addGeneratedPlaylistRoute exposes Generated Playlists: POST /generatedPlaylist/preview turns a
// Prompt into a preview of real Tracks, and POST /generatedPlaylist saves one as a Playlist.
// POST /generatedPlaylist/{id}/preview regenerates a saved one from its stored Prompt and length,
// and PUT /generatedPlaylist/{id}/tracks replaces its Tracks with a preview's.
// All answer 404 when no LLM is configured.
func (api *Router) addGeneratedPlaylistRoute(r chi.Router) {
	var client llm.Client
	if opts := llm.FromConfig(); llm.Configured(opts) {
		client = llm.New(opts)
	}
	gen := generatedplaylist.New(api.ds, client, api.provider)
	r.Route("/generatedPlaylist", func(r chi.Router) {
		r.Post("/", saveGeneratedPlaylist(gen))
		r.Post("/preview", previewGeneratedPlaylist(gen))
		r.Post("/{id}/preview", regenerateGeneratedPlaylist(gen))
		r.Put("/{id}/tracks", replaceGeneratedPlaylist(gen))
	})
}

type previewRequest struct {
	Prompt string `json:"prompt"`
	Length int    `json:"length"`
}

type saveRequest struct {
	Name     string   `json:"name"`
	Prompt   string   `json:"prompt"`
	Length   int      `json:"length"`
	TrackIDs []string `json:"trackIds"`
}

type replaceRequest struct {
	TrackIDs []string `json:"trackIds"`
}

func previewGeneratedPlaylist(gen generatedplaylist.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gen.Enabled() {
			http.NotFound(w, r)
			return
		}
		var body previewRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeGeneratedError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		preview, err := gen.Preview(r.Context(), body.Prompt, body.Length)
		if err != nil {
			writeGeneratedFailure(w, r, err)
			return
		}
		writeGeneratedJSON(w, r, preview)
	}
}

func saveGeneratedPlaylist(gen generatedplaylist.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gen.Enabled() {
			http.NotFound(w, r)
			return
		}
		var body saveRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeGeneratedError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		id, err := gen.Save(r.Context(), body.Name, body.Prompt, body.Length, body.TrackIDs)
		if err != nil {
			writeGeneratedFailure(w, r, err)
			return
		}
		writeGeneratedJSON(w, r, map[string]string{"id": id})
	}
}

func regenerateGeneratedPlaylist(gen generatedplaylist.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gen.Enabled() {
			http.NotFound(w, r)
			return
		}
		preview, err := gen.Regenerate(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeGeneratedFailure(w, r, err)
			return
		}
		writeGeneratedJSON(w, r, preview)
	}
}

func replaceGeneratedPlaylist(gen generatedplaylist.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gen.Enabled() {
			http.NotFound(w, r)
			return
		}
		var body replaceRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeGeneratedError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		id := chi.URLParam(r, "id")
		if err := gen.Replace(r.Context(), id, body.TrackIDs); err != nil {
			writeGeneratedFailure(w, r, err)
			return
		}
		writeGeneratedJSON(w, r, map[string]string{"id": id})
	}
}

// writeGeneratedFailure maps a Generated Playlist error to a status and a code the UI translates.
func writeGeneratedFailure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, llm.ErrUnavailable):
		writeGeneratedError(w, r, http.StatusServiceUnavailable, "aiUnavailable", err)
	case errors.Is(err, llm.ErrInvalidReply):
		writeGeneratedError(w, r, http.StatusBadGateway, "aiInvalidReply", err)
	case errors.Is(err, generatedplaylist.ErrInvalidPrompt):
		writeGeneratedError(w, r, http.StatusBadRequest, "invalidPrompt", err)
	case errors.Is(err, generatedplaylist.ErrInvalidLength):
		writeGeneratedError(w, r, http.StatusBadRequest, "invalidLength", err)
	case errors.Is(err, generatedplaylist.ErrNoTracks):
		writeGeneratedError(w, r, http.StatusUnprocessableEntity, "noTracks", err)
	case errors.Is(err, generatedplaylist.ErrNotGenerated):
		writeGeneratedError(w, r, http.StatusConflict, "notGenerated", err)
	case errors.Is(err, model.ErrNotFound):
		writeGeneratedError(w, r, http.StatusNotFound, "notFound", err)
	case errors.Is(err, model.ErrNotAuthorized):
		writeGeneratedError(w, r, http.StatusForbidden, "notAuthorized", err)
	default:
		writeGeneratedError(w, r, http.StatusInternalServerError, "internal", err)
	}
}

// writeGeneratedError answers with {"error": code}. The underlying error, which can name the
// LLM host, is only logged.
func writeGeneratedError(w http.ResponseWriter, r *http.Request, status int, code string, err error) {
	if status >= http.StatusInternalServerError {
		log.Error(r.Context(), "Generated Playlist failed", "code", code, err)
	} else {
		log.Warn(r.Context(), "Generated Playlist request rejected", "code", code, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": code}); err != nil {
		log.Error(r.Context(), "Error writing Generated Playlist error", err)
	}
}

func writeGeneratedJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error(r.Context(), "Error writing Generated Playlist response", err)
	}
}
