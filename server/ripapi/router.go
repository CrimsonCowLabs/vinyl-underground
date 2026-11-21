// Package ripapi is the admin's HTTP API for Rips: start one from a URL, list the jobs, and see
// and update the yt-dlp that runs them.
//
//	GET  /api/rip                       every job, newest first, each with its items
//	POST /api/rip {url}                 201: the queued job, with its items
//	                                    409: {error, needsConfirmation: true, url, title, count,
//	                                         titles} for a playlist of more than 50 items; nothing
//	                                         is queued
//	                                    422: {error} for a playlist of more than 300 items
//	POST /api/rip {url, confirm: true}  queues a playlist of 51 to 300 items
//	GET  /api/rip/ytdlp                 the installed yt-dlp's version
//	POST /api/rip/ytdlp/update          updates yt-dlp
package ripapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/server"
)

// Ripper is the part of *rip.Ripper the API uses.
type Ripper interface {
	Enqueue(ctx context.Context, url string, confirmed bool) (*model.RipJob, error)
	Jobs(ctx context.Context) (model.RipJobs, error)
	YtDlpVersion(ctx context.Context) (string, error)
	UpdateYtDlp(ctx context.Context, wait bool) (rip.UpdateResult, error)
}

type Router struct {
	http.Handler
	ds     model.DataStore
	ripper Ripper
}

// New returns the Rip API, meant to be mounted only when ripping is on. It also answers 404 to
// everything while Ripping.Enabled is off.
func New(ds model.DataStore, ripper Ripper) *Router {
	r := &Router{ds: ds, ripper: ripper}
	r.Handler = r.routes()
	return r
}

func (api *Router) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(enabledOnly)
	r.Use(server.Authenticator(api.ds))
	r.Use(server.JWTRefresher)
	r.Use(adminOnly)
	r.Get("/", api.list)
	r.Post("/", api.create)
	r.Get("/ytdlp", api.ytDlpVersion)
	r.Post("/ytdlp/update", api.updateYtDlp)
	return r
}

func enabledOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !conf.Server.Ripping.Enabled {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, ok := request.UserFrom(r.Context()); !ok || !user.IsAdmin {
			_ = rest.RespondWithError(w, http.StatusForbidden, "Only the admin can rip")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (api *Router) list(w http.ResponseWriter, r *http.Request) {
	jobs, err := api.ripper.Jobs(r.Context())
	if err != nil {
		log.Error(r.Context(), "Could not list Rips", err)
		_ = rest.RespondWithError(w, http.StatusInternalServerError, "Could not list Rips")
		return
	}
	if jobs == nil {
		jobs = model.RipJobs{}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(jobs)))
	_ = rest.RespondWithJSON(w, http.StatusOK, jobs)
}

func (api *Router) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
		// Confirm is the admin's go-ahead for a playlist of more than 50 items.
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		_ = rest.RespondWithError(w, http.StatusBadRequest, "Send the URL as JSON: {\"url\": \"...\"}")
		return
	}
	// The job outlives the request: it runs in the background, whatever happens to the tab.
	ctx := context.WithoutCancel(r.Context())
	job, err := api.ripper.Enqueue(ctx, body.URL, body.Confirm)
	var confirm *rip.NeedsConfirmationError
	var tooMany *rip.TooManyItemsError
	switch {
	case errors.As(err, &confirm):
		_ = rest.RespondWithJSON(w, http.StatusConflict, struct {
			Error             string `json:"error"`
			NeedsConfirmation bool   `json:"needsConfirmation"`
			*rip.Preview
		}{err.Error(), true, confirm.Preview})
	case errors.As(err, &tooMany):
		_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, rip.ErrInvalidURL):
		_ = rest.RespondWithError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, rip.ErrNoInbox):
		_ = rest.RespondWithError(w, http.StatusConflict, err.Error())
	case err != nil:
		log.Error(r.Context(), "Could not queue the Rip", err)
		_ = rest.RespondWithError(w, http.StatusInternalServerError, "Could not queue the Rip")
	default:
		_ = rest.RespondWithJSON(w, http.StatusCreated, job)
	}
}

func (api *Router) ytDlpVersion(w http.ResponseWriter, r *http.Request) {
	version, err := api.ripper.YtDlpVersion(r.Context())
	if errors.Is(err, rip.ErrNoUpdater) {
		_ = rest.RespondWithError(w, http.StatusNotImplemented, err.Error())
		return
	}
	if err != nil {
		log.Error(r.Context(), "Could not read yt-dlp's version", err)
		_ = rest.RespondWithError(w, http.StatusInternalServerError, "Could not read yt-dlp's version: "+err.Error())
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, map[string]string{"version": version})
}

// updateYtDlp updates yt-dlp now. It refuses, rather than waits, while a Rip is running, as a
// Rip can take many minutes.
func (api *Router) updateYtDlp(w http.ResponseWriter, r *http.Request) {
	res, err := api.ripper.UpdateYtDlp(r.Context(), false)
	switch {
	case errors.Is(err, rip.ErrRipRunning), errors.Is(err, rip.ErrUpdateRunning):
		_ = rest.RespondWithError(w, http.StatusConflict, err.Error())
	case errors.Is(err, rip.ErrNoUpdater):
		_ = rest.RespondWithError(w, http.StatusNotImplemented, err.Error())
	case err != nil:
		log.Warn(r.Context(), "yt-dlp update failed", "kept", res.Version, err)
		msg := "Updating yt-dlp failed: " + err.Error()
		if res.Version != "" {
			msg += " (still using " + res.Version + ")"
		}
		_ = rest.RespondWithError(w, http.StatusBadGateway, msg)
	default:
		_ = rest.RespondWithJSON(w, http.StatusOK, res)
	}
}
