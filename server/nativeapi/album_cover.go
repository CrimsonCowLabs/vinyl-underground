package nativeapi

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
)

// addAlbumCoverRoute serves the cover picker (admin only, see routes): each source's candidates
// for an album, and their previews. Choosing one (POST /correction/albumCover) writes it as the
// album's folder cover. A preview URL with the login token in it is refused
// (server.NoTokenInURL). POST /albumCover/{albumId}/embed is Embed in Files.
func (api *Router) addAlbumCoverRoute(r chi.Router) {
	r.Route("/albumCover/{albumId}/candidates", func(r chi.Router) {
		r.Get("/", api.listCoverCandidates)
		r.With(server.NoTokenInURL).Get("/{candidateId}/preview", api.previewCoverCandidate)
	})
	r.Post("/albumCover/{albumId}/embed", api.embedAlbumCover)
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

// chooseAlbumCover writes the candidate's image as the album's folder cover (cover.jpg, #164),
// as an edit job of one File it waits a little for (see runFileJob). With editing off it answers
// editingOff: no cover Correction is made any more.
func (api *Router) chooseAlbumCover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AlbumID     string `json:"albumId"`
		CandidateID string `json:"candidateId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: %w", model.ErrValidation, err))
		return
	}
	if api.tagEditor == nil || api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	stored, _, err := api.coverPicker.Store(r.Context(), body.AlbumID, body.CandidateID)
	if err != nil {
		writeCoverError(w, r, err)
		return
	}
	img, err := artwork.ChosenCoverStore().Open(stored.Hash, stored.Mime)
	if err != nil {
		writeCoverError(w, r, err)
		return
	}
	defer img.Close()
	data, err := io.ReadAll(img)
	if err != nil {
		writeCoverError(w, r, err)
		return
	}
	api.writeAlbumCover(w, r, body.AlbumID, data)
}

// writeAlbumCover queues the write of the image as the album's folder cover, and answers as an
// edit does (runFileJob).
func (api *Router) writeAlbumCover(w http.ResponseWriter, r *http.Request, albumID string, data []byte) {
	if api.tagEditor == nil || api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	step, err := editjob.CoverStep(api.tagEditor, albumID, data)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	api.runFileJob(w, r, model.EditCover, step)
}

// embedAlbumCover embeds the album's folder cover into each of its Files (Embed in Files, #164),
// as one edit job of a step per File, and waits a little for it. It answers the job: done, with
// the Files it skipped and why, or, with 202, still running in the background.
func (api *Router) embedAlbumCover(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil || api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	ctx := r.Context()
	steps, err := editjob.EmbedSteps(ctx, api.ds, api.tagEditor, chi.URLParam(r, "albumId"))
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	job, err := api.editQueue.Enqueue(ctx, model.EditEmbed, steps)
	if err != nil {
		writeTagEditError(w, r, err)
		return
	}
	if job, err = api.editQueue.Wait(ctx, job.ID, cmp.Or(api.editWait, defaultEditWait)); err != nil {
		writeTagEditError(w, r, err)
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

// uploadAlbumCover writes an uploaded image as the album's folder cover, as if it were a chosen
// candidate (see chooseAlbumCover). Its size is capped like other uploads (MaxImageUploadSize),
// and it must decode as an image (invalidImage).
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
	file, _, err := r.FormFile("image")
	if err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: no image: %w", model.ErrValidation, err))
		return
	}
	defer file.Close()
	// Before keeping it: the journal's images are never swept, so an image for no album would
	// stay there
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
	api.writeAlbumCover(w, r, albumID, data)
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
