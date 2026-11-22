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

// addStationRoute exposes Stations. Stations need no LLM; with one, the DJ's persona and the
// Station's Theme choose its Tracks, the way a Prompt does for Generated Playlists, the DJ
// writes Blurbs, and a Listener's DJs get the genres the LLM maps them to.
func (api *Router) addStationRoute(r chi.Router) {
	var selector stations.Selector
	var writer stations.BlurbWriter
	var opts []stations.Option
	if client := newLLMClient(); client != nil {
		selector = generatedplaylist.NewSelector(api.ds, client, api.provider)
		writer = stations.NewBlurbWriter(client)
		opts = append(opts, stations.WithGenreLLM(client))
	}
	stationRoutes(r, stations.New(api.ds, selector, writer, opts...))
}

// stationRoutes mounts the Station endpoints, all for the logged-in Listener:
//   - GET /station/dj lists the DJs they can start a Station with, built-in then their own;
//   - POST /station/dj creates one of their own DJs, GET /station/dj/{id} opens it for the DJ
//     form (mapping its genres with the LLM first when they still need it), PUT and DELETE
//     /station/dj/{id} edit and delete it (built-in DJs are read-only);
//   - GET /station/genres lists their Library's genres, which their DJs' genres are picked
//     from, and GET /station/genres/match?name=…&description=… the ones a DJ would get;
//   - POST /station/next serves their next Station Tracks; with "start": true it starts the
//     Station, waiting up to stations.StartWait for the DJ's first picks from the LLM;
//   - POST /station/skip records that they skipped one;
//   - GET /station/blurb gives the Blurb for a Track change.
func stationRoutes(r chi.Router, svc stations.Service) {
	r.Route("/station", func(r chi.Router) {
		r.Get("/dj", listDJs(svc))
		r.Post("/dj", createDJ(svc))
		r.Get("/dj/{id}", openDJ(svc))
		r.Put("/dj/{id}", updateDJ(svc))
		r.Delete("/dj/{id}", deleteDJ(svc))
		r.Get("/genres", listGenres(svc))
		r.Get("/genres/match", matchGenres(svc))
		r.Post("/next", nextStationTracks(svc))
		r.Post("/skip", skipStationTrack(svc))
		r.Get("/blurb", stationBlurb(svc))
	})
}

type nextStationRequest struct {
	DJ    string `json:"dj"`
	Theme string `json:"theme"`
	Count int    `json:"count"`
	// Start is set for a Station's first Tracks, which may wait for the LLM's picks.
	Start bool `json:"start"`
}

func listDJs(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		djs, err := svc.DJs(r.Context())
		if err != nil {
			writeStationError(w, r, http.StatusInternalServerError, "internal", err)
			return
		}
		writeStationJSON(w, r, djs)
	}
}

// djRequest creates or edits a Listener's DJ. Genres are the genres the Listener picked by
// hand: left out (or null) when they didn't touch them, so they are worked out from the name
// and description, or kept when already picked. Resuggest works them out again even when
// picked, as the form's "Re-suggest from description" asks.
type djRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Genres      []string `json:"genres"`
	Resuggest   bool     `json:"resuggest"`
}

func (b djRequest) edit() stations.DJEdit {
	return stations.DJEdit{Name: b.Name, Description: b.Description, Genres: b.Genres, Resuggest: b.Resuggest}
}

func openDJ(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dj, err := svc.OpenDJ(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeDJError(w, r, err)
			return
		}
		writeStationJSON(w, r, dj)
	}
}

func createDJ(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body djRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeStationError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		dj, err := svc.CreateDJ(r.Context(), body.edit())
		if err != nil {
			writeDJError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		writeStationJSON(w, r, dj)
	}
}

func updateDJ(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body djRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeStationError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		dj, err := svc.UpdateDJ(r.Context(), chi.URLParam(r, "id"), body.edit())
		if err != nil {
			writeDJError(w, r, err)
			return
		}
		writeStationJSON(w, r, dj)
	}
}

func deleteDJ(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := svc.DeleteDJ(r.Context(), chi.URLParam(r, "id")); err != nil {
			writeDJError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listGenres(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		genres, err := svc.Genres(r.Context())
		if err != nil {
			writeStationError(w, r, http.StatusInternalServerError, "internal", err)
			return
		}
		writeStationJSON(w, r, genres)
	}
}

func matchGenres(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		genres, err := svc.MatchGenres(r.Context(), q.Get("name"), q.Get("description"))
		if err != nil {
			writeStationError(w, r, http.StatusInternalServerError, "internal", err)
			return
		}
		writeStationJSON(w, r, genres)
	}
}

// writeDJError answers a DJ that couldn't be saved or deleted.
func writeDJError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, stations.ErrUnknownDJ):
		writeStationError(w, r, http.StatusNotFound, "unknownDJ", err)
	case errors.Is(err, stations.ErrBuiltInDJ):
		writeStationError(w, r, http.StatusForbidden, "builtInDJ", err)
	case errors.Is(err, stations.ErrInvalidDJName):
		writeStationError(w, r, http.StatusBadRequest, "invalidDJName", err)
	case errors.Is(err, stations.ErrInvalidDJDescription):
		writeStationError(w, r, http.StatusBadRequest, "invalidDJDescription", err)
	default:
		writeStationError(w, r, http.StatusInternalServerError, "internal", err)
	}
}

func nextStationTracks(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body nextStationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeStationError(w, r, http.StatusBadRequest, "badRequest", err)
			return
		}
		next := svc.Next
		if body.Start {
			next = svc.Start
		}
		tracks, err := next(r.Context(), body.DJ, body.Theme, body.Count)
		switch {
		case r.Context().Err() != nil:
			// The Listener left while their Station was starting; there's no one to answer.
			log.Debug(r.Context(), "Station start abandoned", err)
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

// stationBlurb answers GET /station/blurb?dj=…&track=…&after=… with the Blurb ({djName, text}) the
// DJ shows as the Station changes from the Track after into track, or 204 No Content when there
// is none (not written yet, or no LLM): the Track then plays without one.
func stationBlurb(svc stations.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		blurb, ok := svc.Blurb(r.Context(), q.Get("dj"), q.Get("track"), q.Get("after"))
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeStationJSON(w, r, blurb)
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
