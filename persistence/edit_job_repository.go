package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type editJobRepository struct {
	sqlRepository
	// steps writes edit_job_step rows.
	steps sqlRepository
}

func NewEditJobRepository(ctx context.Context, db dbx.Builder) model.EditJobRepository {
	r := &editJobRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "edit_job"
	r.steps.ctx, r.steps.db, r.steps.tableName = ctx, db, "edit_job_step"
	return r
}

func (r *editJobRepository) Put(job *model.EditJob) error {
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if job.Args == "" {
		job.Args = "{}"
	}
	id, err := r.put(job.ID, job)
	if err != nil {
		return err
	}
	job.ID = id
	return nil
}

func (r *editJobRepository) Get(id string) (*model.EditJob, error) {
	var res model.EditJob
	if err := r.queryOne(Select("*").From(r.tableName).Where(Eq{"id": id}), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *editJobRepository) GetAll(limit int) (model.EditJobs, error) {
	res := model.EditJobs{}
	sq := Select("*").From(r.tableName).OrderBy("created_at desc", "rowid desc")
	if limit > 0 {
		sq = sq.Limit(uint64(limit))
	}
	err := r.queryAll(sq, &res)
	return res, err
}

func (r *editJobRepository) Next() (*model.EditJob, error) {
	var res model.EditJob
	sq := Select("*").From(r.tableName).
		Where(Eq{"state": []model.EditJobState{model.EditJobRunning, model.EditJobQueued}}).
		OrderBy("state = 'running' desc", "created_at", "rowid").Limit(1)
	if err := r.queryOne(sq, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *editJobRepository) PutStep(step *model.EditJobStep) error {
	now := time.Now()
	if step.CreatedAt.IsZero() {
		step.CreatedAt = now
	}
	step.UpdatedAt = now
	if step.Args == "" {
		step.Args = "{}"
	}
	id, err := r.steps.put(step.ID, step)
	if err != nil {
		return err
	}
	step.ID = id
	return nil
}

func (r *editJobRepository) Steps(jobID string, states ...model.EditStepState) (model.EditJobSteps, error) {
	res := model.EditJobSteps{}
	where := And{Eq{"job_id": jobID}}
	if len(states) > 0 {
		where = append(where, Eq{"state": states})
	}
	err := r.queryAll(Select("*").From(r.steps.tableName).Where(where).OrderBy("position", "rowid"), &res)
	return res, err
}

var _ model.EditJobRepository = (*editJobRepository)(nil)
