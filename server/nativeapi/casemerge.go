package nativeapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/organize"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addCaseMergeRoute is the case merge through Organize (#169), admin only, for every library the
// app can write:
//   - POST /organize/case/plan?_start&_end: the next phase's plan (Phase A's renames, or Phase B's
//     merge), with a page of the sets of case-colliding siblings, of its steps and of the
//     conflicts left for the admin;
//   - POST /organize/case/run: runs the plan the admin reviewed ({token}) as one edit job.
func (api *Router) addCaseMergeRoute(r chi.Router) {
	r.Route("/case", func(r chi.Router) {
		r.Post("/plan", api.planCaseMerge)
		r.Post("/run", api.runCaseMerge)
	})
}

// casePlanPage is a case merge plan as the dialog shows it: its counts, and a page of each list.
type casePlanPage struct {
	Token string `json:"token"`
	Phase string `json:"phase"`
	// Counts are the plan's steps, by kind (rename, move, rmdir, quarantine).
	Counts       map[string]int       `json:"counts"`
	Clashes      []organize.CaseClash `json:"clashes"`
	Steps        []organize.CaseStep  `json:"steps"`
	Decide       []organize.CaseStep  `json:"decide"`
	TotalClashes int                  `json:"totalClashes"`
	TotalSteps   int                  `json:"totalSteps"`
	TotalDecide  int                  `json:"totalDecide"`
}

func pageOf[T any](rows []T, start, limit int) []T {
	start = min(start, len(rows))
	return rows[start:min(start+limit, len(rows))]
}

func (api *Router) planCaseMerge(w http.ResponseWriter, r *http.Request) {
	start, _ := strconv.Atoi(r.URL.Query().Get("_start"))
	end, err := strconv.Atoi(r.URL.Query().Get("_end"))
	if err != nil || end <= start {
		end = start + 100
	}
	start, limit := max(start, 0), min(end-start, maxPlanRows)
	p, err := organize.New(api.ds, api.editQueue, nil).CasePlan(r.Context())
	if err != nil {
		writeOrganizeError(w, r, err)
		return
	}
	res := casePlanPage{Token: p.Token, Phase: p.Phase, Counts: map[string]int{},
		Clashes: pageOf(p.Clashes, start, limit), Steps: pageOf(p.Steps, start, limit), Decide: pageOf(p.Decide, start, limit),
		TotalClashes: len(p.Clashes), TotalSteps: len(p.Steps), TotalDecide: len(p.Decide)}
	for _, s := range p.Steps {
		res.Counts[s.Op]++
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, res)
}

// runCaseMerge queues the reviewed plan as one job and waits a little for it, as Organize does.
func (api *Router) runCaseMerge(w http.ResponseWriter, r *http.Request) {
	if api.editQueue == nil {
		writeOrganizeError(w, r, tagedit.ErrEditingOff)
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOrganizeError(w, r, tagedit.ErrInvalidTags)
		return
	}
	ctx := r.Context()
	job, err := organize.New(api.ds, api.editQueue, nil).RunCase(ctx, req.Token)
	if errors.Is(err, organize.ErrNoCaseMerge) {
		_ = rest.RespondWithJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "nothingToMerge", "message": err.Error()})
		return
	}
	if err != nil {
		writeOrganizeError(w, r, err)
		return
	}
	queued := job
	if job, err = api.editQueue.Wait(ctx, job.ID, cmp.Or(api.editWait, defaultEditWait)); err != nil {
		log.Warn(ctx, "Could not follow a case merge job; it runs in the background", "job", queued.ID, err)
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
