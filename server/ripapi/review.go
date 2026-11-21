package ripapi

// Review and Discard of a job in the Inbox (#33), admin only like the rest of the Rip API:
//
//	GET    /api/rip/{jobId}/tags                 the job's review card: album-level tags and
//	                                             each Track's, read from its Files
//	PUT    /api/rip/{jobId}/tags {album?, albumArtist?, year?, tracks: [{itemId, title?,
//	                                artist?, trackNumber?}]}
//	                                             writes the edits into the Files (an absent field
//	                                             is left as it is) and rescans; 200: the card
//	DELETE /api/rip/{jobId}                      Discard: deletes all the job's Files and the job
//	DELETE /api/rip/{jobId}/items/{itemId}       deletes one Track's File and its item
//
// All of them answer 409 while the job is still queued or downloading, 404 for a job or item
// that isn't there, and 400 for an invalid edit or a path outside the Inbox.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Reviewer is the part of *rip.Ripper that reviews and discards jobs.
type Reviewer interface {
	Review(ctx context.Context, jobID string) (*rip.Review, error)
	SaveTags(ctx context.Context, jobID string, edits rip.ReviewEdits) (*rip.Review, error)
	Discard(ctx context.Context, jobID string) error
	DiscardItem(ctx context.Context, jobID, itemID string) error
}

func (api *Router) reviewRoutes(r chi.Router) {
	r.Get("/{jobId}/tags", api.review)
	r.Put("/{jobId}/tags", api.saveTags)
	r.Delete("/{jobId}", api.discard)
	r.Delete("/{jobId}/items/{itemId}", api.discardItem)
}

func (api *Router) review(w http.ResponseWriter, r *http.Request) {
	rev, err := api.ripper.Review(r.Context(), chi.URLParam(r, "jobId"))
	if err != nil {
		respondReviewError(w, r, "Could not read the Rip's tags", err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, rev)
}

func (api *Router) saveTags(w http.ResponseWriter, r *http.Request) {
	var edits rip.ReviewEdits
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&edits); err != nil {
		_ = rest.RespondWithError(w, http.StatusBadRequest, "Send the tags as JSON")
		return
	}
	// Once Files are being written, finish them even if the tab is closed.
	rev, err := api.ripper.SaveTags(context.WithoutCancel(r.Context()), chi.URLParam(r, "jobId"), edits)
	if err != nil {
		respondReviewError(w, r, "Could not save the tags", err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, rev)
}

func (api *Router) discard(w http.ResponseWriter, r *http.Request) {
	if err := api.ripper.Discard(context.WithoutCancel(r.Context()), chi.URLParam(r, "jobId")); err != nil {
		respondReviewError(w, r, "Could not discard the Rip", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *Router) discardItem(w http.ResponseWriter, r *http.Request) {
	err := api.ripper.DiscardItem(context.WithoutCancel(r.Context()), chi.URLParam(r, "jobId"), chi.URLParam(r, "itemId"))
	if err != nil {
		respondReviewError(w, r, "Could not discard the Track", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func respondReviewError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound):
		_ = rest.RespondWithError(w, http.StatusNotFound, "This Rip or Track is not in the Inbox")
	case errors.Is(err, rip.ErrNotReady), errors.Is(err, rip.ErrNoInbox):
		_ = rest.RespondWithError(w, http.StatusConflict, err.Error())
	case errors.Is(err, rip.ErrInvalidTags), errors.Is(err, rip.ErrOutsideInbox):
		_ = rest.RespondWithError(w, http.StatusBadRequest, err.Error())
	default:
		log.Error(r.Context(), msg, err)
		_ = rest.RespondWithError(w, http.StatusInternalServerError, msg+": "+err.Error())
	}
}
