package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/streamquality"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model/request"
)

type streamingQualityPayload struct {
	Preference streamquality.Preference `json:"preference"`
}

// addStreamingQualityRoute exposes the calling Listener's streaming-quality setting.
func (api *Router) addStreamingQualityRoute(r chi.Router) {
	quality := streamquality.New(api.ds)
	r.Route("/streamingQuality", func(r chi.Router) {
		r.Get("/", getStreamingQuality(quality))
		r.Put("/", setStreamingQuality(quality))
	})
}

func getStreamingQuality(quality streamquality.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user, _ := request.UserFrom(ctx)
		pref, err := quality.Preference(ctx, user.ID)
		if err != nil {
			log.Error(ctx, "Error reading streaming quality preference", "username", user.UserName, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		writeStreamingQuality(w, r, pref)
	}
}

func setStreamingQuality(quality streamquality.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user, _ := request.UserFrom(ctx)
		var body streamingQualityPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		err := quality.SetPreference(ctx, user.ID, body.Preference)
		if errors.Is(err, streamquality.ErrInvalidPreference) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			log.Error(ctx, "Error saving streaming quality preference", "username", user.UserName, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		writeStreamingQuality(w, r, body.Preference)
	}
}

func writeStreamingQuality(w http.ResponseWriter, r *http.Request, pref streamquality.Preference) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(streamingQualityPayload{Preference: pref}); err != nil {
		log.Error(r.Context(), "Error writing streaming quality response", err)
	}
}
