package nativeapi

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/organize"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// maxPlanRows bounds the rows of one page of an Organize plan's lists.
const maxPlanRows = 500

// addOrganizeRoute is Organize (#167), admin only (see routes). Both take the scope as JSON:
// {albumId} for an album, {ids} for selected Tracks, or {filter} for all matching the Library
// manager's filter (an empty filter is the whole Library).
//   - POST /organize/plan?_start&_end: the plan, with a page of its moves and of the Files it
//     can't place;
//   - POST /organize/run: runs the plan the admin reviewed ({..., token}) as one edit job.
func (api *Router) addOrganizeRoute(r chi.Router) {
	r.Route("/organize", func(r chi.Router) {
		r.Post("/plan", api.planOrganize)
		r.Post("/run", api.runOrganize)
		// #169: the case merge (see addCaseMergeRoute).
		api.addCaseMergeRoute(r)
	})
}

// organizeRequest is a scope, and for a run the plan's token.
type organizeRequest struct {
	organize.Scope
	Token string `json:"token,omitempty"`
}

// planPage is a plan as the dialog shows it: its counts, and a page of each list.
type planPage struct {
	Token string `json:"token"`
	// Tracks and Files count the moves of Tracks, and of their albums' other Files.
	Tracks   int            `json:"tracks"`
	Files    int            `json:"files"`
	InPlace  int            `json:"inPlace"`
	Unplaced map[string]int `json:"unplaced"`
	Moves    []organize.Row `json:"moves"`
	Left     []organize.Row `json:"left"`
	// TotalMoves and TotalLeft are the lengths of the two lists, which Moves and Left page.
	TotalMoves int `json:"totalMoves"`
	TotalLeft  int `json:"totalLeft"`
}

func readOrganize(w http.ResponseWriter, r *http.Request) (organizeRequest, bool) {
	var req organizeRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOrganizeError(w, r, tagedit.ErrInvalidTags)
		return req, false
	}
	return req, true
}

func (api *Router) planOrganize(w http.ResponseWriter, r *http.Request) {
	req, ok := readOrganize(w, r)
	if !ok {
		return
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("_start"))
	end, err := strconv.Atoi(r.URL.Query().Get("_end"))
	if err != nil || end <= start {
		end = start + 100
	}
	start, limit := max(start, 0), min(end-start, maxPlanRows)
	p, err := organize.New(api.ds, api.editQueue, nil).Plan(r.Context(), req.Scope)
	if err != nil {
		writeOrganizeError(w, r, err)
		return
	}
	res := planPage{Token: p.Token, InPlace: p.InPlace, Unplaced: map[string]int{},
		Moves: page(p.Moves, start, limit), Left: page(p.Unplaced, start, limit),
		TotalMoves: len(p.Moves), TotalLeft: len(p.Unplaced)}
	for _, m := range p.Moves {
		if m.Status == organize.StatusFile {
			res.Files++
		} else {
			res.Tracks++
		}
	}
	for _, u := range p.Unplaced {
		res.Unplaced[u.Status]++
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

func page(rows []organize.Row, start, limit int) []organize.Row {
	start = min(start, len(rows))
	return rows[start:min(start+limit, len(rows))]
}

// runOrganize queues the reviewed plan as one job and waits a little for it, as a bulk edit
// does: it answers the job once done, with the Files it skipped and why, or, with 202, while it
// runs on in the background (the Activity panel follows it).
func (api *Router) runOrganize(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeOrganizeError(w, r, tagedit.ErrEditingOff)
		return
	}
	req, ok := readOrganize(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	job, err := organize.New(api.ds, api.editQueue, nil).Run(ctx, req.Scope, req.Token)
	if err != nil {
		writeOrganizeError(w, r, err)
		return
	}
	queued := job
	if job, err = api.editQueue.Wait(ctx, job.ID, cmp.Or(api.editWait, defaultEditWait)); err != nil {
		log.Warn(ctx, "Could not follow an Organize job; it runs in the background", "job", queued.ID, err)
		queued.Steps = model.EditJobSteps{}
		_ = rest.RespondWithJSON(w, http.StatusAccepted, queued)
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

// organizeStatus is the HTTP status of Organize's own codes; the others are a bulk edit's.
var organizeStatus = map[string]int{
	"planChanged":   http.StatusConflict,
	"nothingToMove": http.StatusUnprocessableEntity,
}

func writeOrganizeError(w http.ResponseWriter, r *http.Request, err error) {
	code := organize.Code(err)
	status, ok := organizeStatus[code]
	if !ok {
		writeBulkEditError(w, r, err)
		return
	}
	log.Debug(r.Context(), "Organize refused", "code", code, err)
	_ = rest.RespondWithJSON(w, status, map[string]string{"error": code, "message": err.Error()})
}
