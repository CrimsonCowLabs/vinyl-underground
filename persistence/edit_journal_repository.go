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
		Where(And{destLibrary(libraryID), Eq{"new_path": path, "state": model.EditWritten}}).
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

func (r *editJournalRepository) ForJob(jobID string) ([]model.EditEntry, error) {
	res := []model.EditEntry{}
	err := r.queryAll(Select("*").From(r.tableName).Where(Eq{"job_id": jobID}).
		OrderBy("created_at", "rowid"), &res)
	return res, err
}

// After orders by rowid: entries are only ever added (kept forever), so it is the order they were
// recorded in, even within one timestamp.
func (r *editJournalRepository) After(e *model.EditEntry) ([]model.EditEntry, error) {
	lib := e.DestLibraryID()
	file := Or{
		And{Eq{"library_id": lib}, Eq{"path": e.NewPath}},
		And{destLibrary(lib), Eq{"new_path": e.NewPath}},
	}
	if e.MediaFileID != "" {
		file = append(file, Eq{"media_file_id": e.MediaFileID})
	}
	res := []model.EditEntry{}
	sq := Select("*").From(r.tableName).Where(And{
		file,
		NotEq{"state": model.EditFailed},
		Expr("rowid > (SELECT rowid FROM edit_journal WHERE id = ?)", e.ID),
	}).OrderBy("rowid")
	err := r.queryAll(sq, &res)
	return res, err
}

// destLibrary matches the entries that left their File in the library libraryID: their
// new_library_id, or their library_id when it is 0 (model.EditEntry.DestLibraryID).
func destLibrary(libraryID int) Sqlizer {
	return Expr("(CASE WHEN new_library_id = 0 THEN library_id ELSE new_library_id END) = ?", libraryID)
}

func (r *editJournalRepository) GetAll(options ...model.QueryOptions) ([]model.EditEntry, error) {
	sq := r.newSelect(options...).Columns("*")
	if len(options) == 0 || options[0].Sort == "" {
		sq = sq.OrderBy("created_at desc", "rowid desc")
	}
	res := []model.EditEntry{}
	err := r.queryAll(sq, &res)
	return res, err
}

func (r *editJournalRepository) CountAll(options ...model.QueryOptions) (int64, error) {
	return r.count(Select(), options...)
}
