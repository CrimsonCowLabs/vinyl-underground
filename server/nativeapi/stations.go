package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/generatedplaylist"
	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/log"
)

// addStationRoute exposes Stations: GET /station/dj lists the DJs, POST /station/next serves
// the logged-in Listener's next Station Tracks, and POST /station/skip records that they skipped
// one. Stations need no LLM; with one, the DJ's persona and the Station's Theme choose its
// Tracks, the way a Prompt does for Generated Playlists.
func (api *Router) addStationRoute(r chi.Router) {
	var selector stations.Selector
	if client := newLLMClient(); client != nil {
		selector = generatedplaylist.NewSelector(api.ds, client, api.provider)
	}
	svc := stations.New(api.ds, selector)
	r.Route("/station", func(r chi.Router) {
		r.Get("/dj", listDJs(svc))
		r.Post("/next", nextStationTracks(svc))
		r.Post("/skip", skipStationTrack(svc))
	})
}

type nextStationRequest struct {
	DJ    string `json:"dj"`
	Theme string `json:"theme"`
	Count int    `json:"count"`
}

func listDJs(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeStationJSON(w, r, svc.DJs())
	}
}

func nextStationTracks(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body nextStationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeStationError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		tracks, err := svc.Next(r.Context(), body.DJ, body.Theme, body.Count)
		switch {
		case err == nil:
			writeStationJSON(w, r, map[string]any{"tracks": tracks})
		case errors.Is(err, stations.ErrUnknownDJ):
			writeStationError(w, r, http.StatusNotFound, "unknownDJ", err)
		case errors.Is(err, stations.ErrInvalidCount):
			writeStationError(w, r, http.StatusBadRequest, "invalidCount", err)
		case errors.Is(err, stations.ErrInvalidTheme):
			writeStationError(w, r, http.StatusBadRequest, "invalidTheme", err)
		case errors.Is(err, stations.ErrNoTracks):
			writeStationError(w, r, http.StatusUnprocessableEntity, "noTracks", err)
		default:
			writeStationError(w, r, http.StatusInternalServerError, "internal", err)
		}
	}
}

type skipStationRequest struct {
	DJ    string `json:"dj"`
	Track string `json:"track"`
}

func skipStationTrack(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body skipStationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeStationError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		err := svc.Skip(r.Context(), body.DJ, body.Track)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, stations.ErrUnknownDJ):
			writeStationError(w, r, http.StatusNotFound, "unknownDJ", err)
		case errors.Is(err, stations.ErrNotServed):
			writeStationError(w, r, http.StatusNotFound, "notServed", err)
		default:
			writeStationError(w, r, http.StatusInternalServerError, "internal", err)
		}
	}
}

// writeStationError answers with {"error": code}, which the UI translates.
func writeStationError(w http.ResponseWriter, r *http.Request, status int, code string, err error) {
	if status >= http.StatusInternalServerError {
		log.Error(r.Context(), "Station failed", "code", code, err)
	} else {
		log.Warn(r.Context(), "Station request rejected", "code", code, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": code}); err != nil {
		log.Error(r.Context(), "Error writing Station error", err)
	}
}

func writeStationJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error(r.Context(), "Error writing Station response", err)
	}
}
