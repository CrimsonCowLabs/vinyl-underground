package ripapi

// Promote of a reviewed job (#171), admin only like the rest of the Rip API, and there only while
// Promote is on (an Inbox, and a writable Library: editing on, ADR-0011). It is Organize from the
// Inbox, and answers as Organize's API does:
//
//	POST /api/rip/{jobId}/promote/plan?_start&_end  the plan: where each of the job's Files lands
//	                                                in the Library, and the Files it can't place,
//	                                                with why (organize.PlanPage)
//	POST /api/rip/{jobId}/promote {token}           runs the plan reviewed as one Promote job:
//	                                                200 with the job once done (its steps: the
//	                                                Files it skipped), or 202 while it runs on
//
// Refusals are {error: code, message}: 404 notFound, 409 notReady (queued or downloading),
// nothingToPromote (no File scanned in the Inbox), promoting (a Promote of the card is still
// queued or running) and planChanged (the Library changed since the
// plan: review it again), 422 nothingToMove. With Promote off it answers 404.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/organize"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// promoteWait is how long a Promote request waits for its job before answering 202.
const promoteWait = 5 * time.Second

// maxPlanRows bounds the rows of one page of a plan's lists.
const maxPlanRows = 500

// Promoter is the part of *rip.Ripper that promotes jobs.
type Promoter interface {
	PromoteEnabled(ctx context.Context) bool
	PromotePlan(ctx context.Context, jobID string) (*organize.Plan, error)
	Promote(ctx context.Context, jobID, token string, wait time.Duration) (*model.EditJob, error)
}

func (api *Router) promoteRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(api.promoteOn)
		r.Post("/{jobId}/promote/plan", api.planPromote)
		r.Post("/{jobId}/promote", api.promote)
	})
}

// promoteOn answers 404 unless there is an Inbox and Promote is on.
func (api *Router) promoteOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !inboxOn() || !api.ripper.PromoteEnabled(r.Context()) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (api *Router) planPromote(w http.ResponseWriter, r *http.Request) {
	start, _ := strconv.Atoi(r.URL.Query().Get("_start"))
	end, err := strconv.Atoi(r.URL.Query().Get("_end"))
	if err != nil || end <= start {
		end = start + 100
	}
	start, limit := max(start, 0), min(end-start, maxPlanRows)
	p, err := api.ripper.PromotePlan(r.Context(), chi.URLParam(r, "jobId"))
	if err != nil {
		writePromoteError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, p.Page(start, limit))
}

func (api *Router) promote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		_ = rest.RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "noToken", "message": "the plan's token is missing"})
		return
	}
	// Once the job is queued, it runs whether or not the tab stays open.
	job, err := api.ripper.Promote(context.WithoutCancel(r.Context()), chi.URLParam(r, "jobId"), req.Token, promoteWait)
	if err != nil {
		writePromoteError(w, r, err)
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

func writePromoteError(w http.ResponseWriter, r *http.Request, err error) {
	code, status := organize.Code(err), http.StatusInternalServerError
	switch {
	case errors.Is(err, model.ErrNotFound):
		code, status = "notFound", http.StatusNotFound
	case errors.Is(err, rip.ErrPromoteOff):
		code, status = "editingOff", http.StatusNotFound
	case errors.Is(err, rip.ErrNotReady), errors.Is(err, rip.ErrNoInbox):
		code, status = "notReady", http.StatusConflict
	case errors.Is(err, rip.ErrPromoting):
		code, status = "promoting", http.StatusConflict
	case errors.Is(err, rip.ErrNothingToPromote):
		code, status = "nothingToPromote", http.StatusConflict
	case errors.Is(err, organize.ErrPlanChanged):
		status = http.StatusConflict
	case errors.Is(err, organize.ErrNothingToMove):
		status = http.StatusUnprocessableEntity
	case code != tagedit.CodeInternal:
		status = http.StatusConflict
	default:
		log.Error(r.Context(), "Could not promote a Rip", err)
	}
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code, "message": err.Error()})
}
