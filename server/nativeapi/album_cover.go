package nativeapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addAlbumCoverRoute serves the cover picker (admin only, see routes): each source's candidates
// for an album, and their previews. Choosing one is a Correction (POST /correction/albumCover).
func (api *Router) addAlbumCoverRoute(r chi.Router) {
	r.Route("/albumCover/{albumId}/candidates", func(r chi.Router) {
		r.Get("/", api.listCoverCandidates)
		r.Get("/{candidateId}/preview", api.previewCoverCandidate)
	})
}

func (api *Router) listCoverCandidates(w http.ResponseWriter, r *http.Request) {
	source := artwork.CoverSource(r.URL.Query().Get("source"))
	res, err := api.coverPicker.Candidates(r.Context(), chi.URLParam(r, "albumId"), source)
	if err != nil {
		writeCoverError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

func (api *Router) previewCoverCandidate(w http.ResponseWriter, r *http.Request) {
	img, err := api.coverPicker.Preview(r.Context(), chi.URLParam(r, "albumId"), chi.URLParam(r, "candidateId"))
	if err != nil {
		writeCoverError(w, r, err)
		return
	}
	defer img.Close()
	br := bufio.NewReader(img)
	head, _ := br.Peek(512)
	w.Header().Set("Content-Type", http.DetectContentType(head))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if _, err := io.Copy(w, br); err != nil && !errors.Is(err, context.Canceled) {
		log.Debug(r.Context(), "Cover picker: could not send a preview", err)
	}
}

// chooseAlbumCover stores the candidate's image and saves it as the album's cover Correction.
func (api *Router) chooseAlbumCover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AlbumID     string `json:"albumId"`
		CandidateID string `json:"candidateId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: %w", model.ErrValidation, err))
		return
	}
	stored, cand, err := api.coverPicker.Store(r.Context(), body.AlbumID, body.CandidateID)
	if err != nil {
		writeCoverError(w, r, err)
		return
	}
	res, err := api.corrections.ChooseAlbumCover(r.Context(), body.AlbumID,
		corrections.AlbumCover{Hash: stored.Hash, Mime: stored.Mime, Source: cand.Description()})
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// writeCoverError is writeCorrectionError, plus "unavailable" for a source that couldn't be reached.
func writeCoverError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrValidation) {
		writeCorrectionError(w, r, err)
		return
	}
	log.Warn(r.Context(), "Cover picker: could not fetch a cover", err)
	_ = rest.RespondWithJSON(w, http.StatusBadGateway, map[string]string{"error": "unavailable"})
}
