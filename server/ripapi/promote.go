package ripapi

// Promote of a reviewed job (#36), admin only like the rest of the Rip API, and there only when a
// promote folder is set up (Inbox.PromotePath):
//
//	POST /api/rip/{jobId}/promote   moves the job's Files into the promote folder, File by File.
//	                                200: {promoted: [{itemId, path}], failed: [{itemId, error}],
//	                                      review}, where review is the card for the Files left
//	                                      (null once the job is gone)
//
// It answers 409 while the job is queued or downloading or when it has no Files, and 404 for a
// job that isn't there. With Promote off it isn't mounted at all (404).

import (
	"context"
	"errors"
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/rip"
)

// Promoter is the part of *rip.Ripper that promotes jobs.
type Promoter interface {
	PromoteEnabled() bool
	Promote(ctx context.Context, jobID string) (*rip.PromoteResult, error)
}

func (api *Router) promoteRoutes(r chi.Router) {
	if !api.ripper.PromoteEnabled() {
		return
	}
	r.Post("/{jobId}/promote", api.promote)
}

func (api *Router) promote(w http.ResponseWriter, r *http.Request) {
	// Once Files are moving, finish them even if the tab is closed.
	res, err := api.ripper.Promote(context.WithoutCancel(r.Context()), chi.URLParam(r, "jobId"))
	switch {
	case errors.Is(err, rip.ErrPromoteOff):
		http.NotFound(w, r)
	case errors.Is(err, rip.ErrNothingToPromote):
		_ = rest.RespondWithError(w, http.StatusConflict, err.Error())
	case err != nil:
		respondReviewError(w, r, "Could not promote the Rip", err)
	default:
		_ = rest.RespondWithJSON(w, http.StatusOK, res)
	}
}
