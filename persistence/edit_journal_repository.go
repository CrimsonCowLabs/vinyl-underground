package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type editJournalRepository struct {
	sqlRepository
}

func NewEditJournalRepository(ctx context.Context, db dbx.Builder) model.EditJournalRepository {
	r := &editJournalRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "edit_journal"
	return r
}

func (r *editJournalRepository) Put(e *model.EditEntry) error {
	now := time.Now()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	id, err := r.put(e.ID, e)
	if err != nil {
		return err
	}
	e.ID = id
	return nil
}

func (r *editJournalRepository) Get(id string) (*model.EditEntry, error) {
	var res model.EditEntry
	if err := r.queryOne(Select("*").From(r.tableName).Where(Eq{"id": id}), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *editJournalRepository) LastWritten(libraryID int, path string) (*model.EditEntry, error) {
	var res model.EditEntry
	sq := Select("*").From(r.tableName).
		Where(Eq{"library_id": libraryID, "new_path": path, "state": model.EditWritten}).
		OrderBy("created_at desc", "rowid desc").Limit(1)
	if err := r.queryOne(sq, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *editJournalRepository) Pending() ([]model.EditEntry, error) {
	res := []model.EditEntry{}
	err := r.queryAll(Select("*").From(r.tableName).Where(Eq{"state": model.EditPending}).
		OrderBy("created_at", "rowid"), &res)
	return res, err
}
