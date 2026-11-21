package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type ripItemRepository struct {
	sqlRepository
}

func NewRipItemRepository(ctx context.Context, db dbx.Builder) model.RipItemRepository {
	r := &ripItemRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "rip_item"
	return r
}

func (r *ripItemRepository) Put(item *model.RipItem) error {
	id, err := r.put(item.ID, item)
	if err != nil {
		return err
	}
	item.ID = id
	return nil
}

func (r *ripItemRepository) ForJobs(jobIDs ...string) (model.RipItems, error) {
	res := model.RipItems{}
	if len(jobIDs) == 0 {
		return res, nil
	}
	err := r.queryAll(Select("*").From(r.tableName).Where(Eq{"job_id": jobIDs}).
		OrderBy("job_id", "position", "rowid"), &res)
	return res, err
}

func (r *ripItemRepository) RequeueInterrupted() (int64, error) {
	return r.executeSQL(Update(r.tableName).
		Set("status", model.RipQueued).
		Set("updated_at", time.Now()).
		Where(Eq{"status": model.RipDownloading}))
}

func (r *ripItemRepository) Delete(id string) error {
	return r.deleteByID(id)
}

var _ model.RipItemRepository = (*ripItemRepository)(nil)
