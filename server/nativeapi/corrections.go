package nativeapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addCorrectionRoute serves the admin's Corrections (admin only, see routes): the saves that write
// to the Files, the Correction migration (#165), and what stays app data (cover blocks and picked
// releases, which can be undone).
func (api *Router) addCorrectionRoute(r chi.Router) {
	r.Route("/correction", func(r chi.Router) {
		r.Get("/", api.listCorrections)
		r.Post("/artistRename", api.renameArtist)
		r.Post("/albumCover", api.chooseAlbumCover)
		r.Post("/albumCoverUpload", api.uploadAlbumCover)
		r.Post("/albumCoverBlock", api.blockAlbumCover)
		r.Post("/artistMerge", api.mergeArtist)
		r.Post("/album", api.correctAlbum)
		r.Post("/trackNumbers", api.correctTrackNumbers)
		r.Get("/migration", api.planMigration)
		r.Post("/migration", api.migrateCorrections)
		r.Post("/{id}/undo", api.undoCorrection)
	})
}

func (api *Router) listCorrections(w http.ResponseWriter, r *http.Request) {
	all, err := api.corrections.GetAll(r.Context())
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	if all == nil {
		all = model.Corrections{}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(all)))
	_ = rest.RespondWithJSON(w, http.StatusOK, all)
}

// The album Edit dialog, Track order and artist rename and merge write to the Files (#163): each
// save is an edit job, which the request waits for a little, as a Track's edit does. So does a
// MusicBrainz release picked with Find on MusicBrainz (#170): its IDs are written into the
// album's Files, with the dialog's values, as one job.

func (api *Router) renameArtist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ArtistID string `json:"artistId"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: %w", model.ErrValidation, err))
		return
	}
	if api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	plan, err := api.corrections.PlanArtistRename(r.Context(), body.ArtistID, body.Name)
	if err != nil {
		writeWriteError(w, r, err)
		return
	}
	api.runWriteJob(w, r, plan, "")
}

func (api *Router) mergeArtist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ArtistID string `json:"artistId"`
		IntoID   string `json:"intoId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: %w", model.ErrValidation, err))
		return
	}
	if api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	plan, err := api.corrections.PlanArtistMerge(r.Context(), body.ArtistID, body.IntoID)
	if err != nil {
		writeWriteError(w, r, err)
		return
	}
	api.runWriteJob(w, r, plan, "")
}

// correctAlbum writes the album Edit dialog's title, album artist and year, and its Tracks'
// changed numbers (tracks, if any), into the album's Files, as one job. With a MusicBrainz release
// picked with Find on MusicBrainz (mbzAlbumId), the job writes the release's IDs too, and of its
// own values those the admin accepts (canonical), replacing track IDs that differ only with
// replaceTrackIds, and its cover as the album's cover.jpg with cover (#170).
func (api *Router) correctAlbum(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AlbumID string `json:"albumId"`
		corrections.AlbumFix
		Tracks          []corrections.TrackNumber `json:"tracks"`
		MbzAlbumID      string                    `json:"mbzAlbumId"`
		Canonical       corrections.Canonical     `json:"canonical"`
		ReplaceTrackIDs bool                      `json:"replaceTrackIds"`
		Cover           bool                      `json:"cover"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: %w", model.ErrValidation, err))
		return
	}
	ctx := r.Context()
	if id := strings.ToLower(strings.TrimSpace(body.MbzAlbumID)); id != "" {
		if api.editQueue == nil {
			writeTagEditError(w, r, tagedit.ErrEditingOff)
			return
		}
		rel, ok := api.lookUpRelease(w, r, id)
		if !ok {
			return
		}
		plan, err := api.corrections.PlanRelease(ctx, api.tagEditor, body.AlbumID, rel, corrections.ReleaseWrite{
			Fix: &body.AlbumFix, Tracks: body.Tracks, Accept: body.Canonical, ReplaceTrackIDs: body.ReplaceTrackIDs,
			Cover: body.Cover})
		if err != nil {
			writeWriteError(w, r, err)
			return
		}
		api.runWriteJob(w, r, plan, plan.PickCover)
		return
	}
	plan, err := api.corrections.PlanAlbumEdit(ctx, body.AlbumID, &body.AlbumFix, body.Tracks)
	switch {
	case err != nil:
		writeWriteError(w, r, err)
		return
	case api.editQueue == nil:
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	api.runWriteJob(w, r, plan, "")
}

// correctTrackNumbers writes Track order's disc and track numbers into each Track's File, as one
// job.
func (api *Router) correctTrackNumbers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AlbumID string                    `json:"albumId"`
		Tracks  []corrections.TrackNumber `json:"tracks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCorrectionError(w, r, fmt.Errorf("%w: %w", model.ErrValidation, err))
		return
	}
	if api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	plan, err := api.corrections.PlanAlbumEdit(r.Context(), body.AlbumID, nil, body.Tracks)
	if err != nil {
		writeWriteError(w, r, err)
		return
	}
	api.runWriteJob(w, r, plan, "")
}

// writeResult is what a save that writes to the Files answers: its job, with the Files it skipped
// (and why), once done; or, with 202 and Background, the job still running.
type writeResult struct {
	Job        *model.EditJob `json:"job,omitempty"`
	JobID      string         `json:"jobId,omitempty"`
	Done       bool           `json:"done"`
	Background bool           `json:"background,omitempty"`
	// AlbumID and ArtistID are the album's and the Artist's IDs once the job is done: they come
	// from the tags it wrote.
	AlbumID   string                `json:"albumId,omitempty"`
	ArtistID  string                `json:"artistId,omitempty"`
	PickCover corrections.PickCover `json:"pickCover,omitempty"`
	// RolledBack says a MusicBrainz release couldn't be written into every File of the album (Job
	// lists those skipped), so the Files written were put back, by the job RollbackJobID, and the
	// album is as it was (see corrections.JobRelease). RollbackDone says that job is done.
	RolledBack    bool   `json:"rolledBack,omitempty"`
	RollbackJobID string `json:"rollbackJobId,omitempty"`
	RollbackDone  bool   `json:"rollbackDone,omitempty"`
}

// runWriteJob queues the plan's job and waits a little for it (see runFileJob).
func (api *Router) runWriteJob(w http.ResponseWriter, r *http.Request, plan *corrections.Plan, pickCover corrections.PickCover) {
	ctx := r.Context()
	job, err := api.editQueue.EnqueueWith(ctx, plan.Kind, plan.Args, plan.Steps)
	if err != nil {
		writeWriteError(w, r, err)
		return
	}
	queued := job
	if job, err = api.editQueue.Wait(ctx, job.ID, cmp.Or(api.editWait, defaultEditWait)); err != nil {
		// The job is queued and runs anyway: an error here would have the admin save it again.
		log.Warn(ctx, "Could not wait for an edit job; it carries on in the background", "job", queued.ID, err)
		_ = rest.RespondWithJSON(w, http.StatusAccepted, writeResult{JobID: queued.ID, Background: true, PickCover: pickCover})
		return
	}
	res := writeResult{JobID: job.ID, PickCover: pickCover}
	if job.State != model.EditJobDone {
		res.Background = true
		_ = rest.RespondWithJSON(w, http.StatusAccepted, res)
		return
	}
	skipped := model.EditJobSteps{}
	// The album's new ID is a written Track's album's: a skipped one is still on the old album.
	albumTrack := plan.TrackID
	written := false
	for _, s := range job.Steps {
		if s.State == model.EditStepSkipped {
			skipped = append(skipped, s)
		} else if !written && plan.TrackID != "" && s.MediaFileID != "" {
			albumTrack, written = s.MediaFileID, true
		}
	}
	job.Steps = skipped
	res.Job, res.Done, res.ArtistID = job, true, plan.ArtistID
	if rollback, err := corrections.ReleaseRollback(job); plan.Kind == corrections.JobRelease && err == nil && rollback != "" {
		// The album is back on its old ID once the rollback is done: the dialog reloads it.
		res.RolledBack, res.RollbackJobID, albumTrack = true, rollback, ""
		if u, err := api.editQueue.Wait(ctx, rollback, cmp.Or(api.editWait, defaultEditWait)); err == nil {
			res.RollbackDone = u.State == model.EditJobDone
		}
	}
	if albumTrack != "" {
		if mf, err := api.ds.MediaFile(ctx).Get(albumTrack); err == nil {
			res.AlbumID = mf.AlbumID
		}
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// writeWriteError answers a refused save that writes to the Files: with the edit's code when it
// has one (editingOff, unsupportedFormat, ...), else as a refused Correction.
func writeWriteError(w http.ResponseWriter, r *http.Request, err error) {
	if editjob.Code(err) != tagedit.CodeInternal {
		writeTagEditError(w, r, err)
		return
	}
	writeCorrectionError(w, r, err)
}

// planMigration lists the Corrections the migration writes into the Files, what each changes, and
// the Files it can't write (it writes nothing).
func (api *Router) planMigration(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	m, err := api.corrections.PlanMigration(r.Context(), api.tagEditor)
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, m)
}

// migrateCorrections queues the migration as one edit job, and waits a little for it, as a save
// that writes to the Files does. With nothing to write it answers done (it removed what applied
// to nothing).
func (api *Router) migrateCorrections(w http.ResponseWriter, r *http.Request) {
	if api.tagEditor == nil || api.editQueue == nil {
		writeTagEditError(w, r, tagedit.ErrEditingOff)
		return
	}
	plan, err := api.corrections.MigrationJob(r.Context(), api.tagEditor)
	if errors.Is(err, corrections.ErrNothingToWrite) {
		_ = rest.RespondWithJSON(w, http.StatusOK, writeResult{Done: true})
		return
	}
	if err != nil {
		writeWriteError(w, r, err)
		return
	}
	api.runWriteJob(w, r, plan, "")
}

func (api *Router) undoCorrection(w http.ResponseWriter, r *http.Request) {
	res, err := api.corrections.Undo(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeCorrectionError(w, r, err)
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// writeCorrectionError answers with a status and an error code the UI translates. The underlying
// error is only logged.
func writeCorrectionError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "internal"
	switch {
	case errors.Is(err, corrections.ErrArtistExists):
		status, code = http.StatusConflict, "artistExists"
	case errors.Is(err, corrections.ErrRenamedAway):
		status, code = http.StatusBadRequest, "renamedAway"
	case errors.Is(err, corrections.ErrMigrateFirst):
		status, code = http.StatusBadRequest, "migrateFirst"
	case errors.Is(err, corrections.ErrCoverChanged):
		status, code = http.StatusConflict, "coverChanged"
	case errors.Is(err, model.ErrAlreadyExists):
		status, code = http.StatusConflict, "alreadyCorrected"
	case errors.Is(err, model.ErrValidation):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, model.ErrNotFound):
		status, code = http.StatusNotFound, "notFound"
	}
	if status == http.StatusInternalServerError {
		log.Error(r.Context(), "Error handling a Correction", err)
	} else {
		log.Debug(r.Context(), "Correction refused", "code", code, err)
	}
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code})
}
