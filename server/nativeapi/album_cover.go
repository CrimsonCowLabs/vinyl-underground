package nativeapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
)

// addAlbumCoverRoute serves the cover picker (admin only, see routes): each source's candidates
// for an album, and their previews. Choosing one is a Correction (POST /correction/albumCover).
// A preview URL with the login token in it is refused (server.NoTokenInURL).
func (api *Router) addAlbumCoverRoute(r chi.Router) {
	r.Route("/albumCover/{albumId}/candidates", func(r chi.Router) {
		r.Get("/", api.listCoverCandidates)
		r.With(server.NoTokenInURL).Get("/{candidateId}/preview", api.previewCoverCandidate)
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
	// The bytes may come from a remote server: only ever serve them as an image
	contentType := http.DetectContentType(head)
	if !strings.HasPrefix(contentType, "image/") {
		writeCoverError(w, r, fmt.Errorf("the preview is %s, not an image", contentType))
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
		corrections.AlbumCover{StoredCover: stored, Source: cand.Description()})
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// uploadAlbumCover stores an uploaded image and saves it as the album's cover Correction, as if it
// were a chosen candidate. Its size is capped like other uploads (MaxImageUploadSize), and it must
// decode as an image (artwork.StoreChosenCover).
func (api *Router) uploadAlbumCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, artwork.MaxImageUploadSize())
	if err := r.ParseMultipartForm(min(artwork.MaxImageUploadSize(), 10<<20)); err != nil {
		log.Debug(ctx, "Cover upload refused", err)
		code := "invalid"
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			code = "tooLarge"
		}
		_ = rest.RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": code})
		return
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			log.Warn(ctx, "Error removing multipart temp files", err)
		}
	}()
	file, header, err := r.FormFile("image")
	if err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: no image: %w", model.ErrValidation, err))
		return
	}
	defer file.Close()
	// Before storing: nothing sweeps the store, so an image for no album would stay there
	albumID := r.FormValue("albumId")
	if _, err := api.ds.Album(ctx).Get(albumID); err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	stored, err := artwork.StoreChosenCover(data)
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	res, err := api.corrections.ChooseAlbumCover(ctx, albumID,
		corrections.AlbumCover{StoredCover: stored, Source: "Upload: " + filepath.Base(header.Filename)})
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// blockAlbumCover marks the image the album shows wrong for it: imageHash is the album's
// imageHash as the admin saw it.
func (api *Router) blockAlbumCover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AlbumID   string `json:"albumId"`
		ImageHash string `json:"imageHash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: %w", model.ErrValidation, err))
		return
	}
	res, err := api.corrections.BlockAlbumCover(r.Context(), body.AlbumID, body.ImageHash)
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
