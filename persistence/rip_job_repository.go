package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type ripJobRepository struct {
	sqlRepository
}

func NewRipJobRepository(ctx context.Context, db dbx.Builder) model.RipJobRepository {
	r := &ripJobRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "rip_job"
	return r
}

func (r *ripJobRepository) Put(job *model.RipJob) error {
	id, err := r.put(job.ID, job)
	if err != nil {
		return err
	}
	job.ID = id
	return nil
}

func (r *ripJobRepository) Get(id string) (*model.RipJob, error) {
	var res model.RipJob
	if err := r.queryOne(Select("*").From(r.tableName).Where(Eq{"id": id}), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *ripJobRepository) GetAll() (model.RipJobs, error) {
	var res model.RipJobs
	err := r.queryAll(Select("*").From(r.tableName).OrderBy("created_at desc", "rowid desc"), &res)
	return res, err
}

func (r *ripJobRepository) NextQueued() (*model.RipJob, error) {
	var res model.RipJob
	sq := Select("*").From(r.tableName).Where(Eq{"status": model.RipQueued}).
		OrderBy("created_at", "rowid").Limit(1)
	if err := r.queryOne(sq, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *ripJobRepository) RequeueInterrupted() (int64, error) {
	return r.executeSQL(Update(r.tableName).
		Set("status", model.RipQueued).
		Set("updated_at", time.Now()).
		Where(Eq{"status": model.RipDownloading}))
}

var _ model.RipJobRepository = (*ripJobRepository)(nil)
