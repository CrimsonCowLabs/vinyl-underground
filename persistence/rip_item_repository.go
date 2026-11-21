package persistence

import (
	"context"
	"strings"
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

// SetProposal updates the proposal column alone. Put leaves it out (structs:"-"), so the Ripper
// saving an item it loaded earlier never undoes a proposal saved meanwhile.
func (r *ripItemRepository) SetProposal(id string, p model.RipProposal) error {
	n, err := r.executeSQL(Update(r.tableName).Set("proposal", p).Where(Eq{"id": id}))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (r *ripItemRepository) SetAlreadyHave(id string, matches model.RipMatches) error {
	n, err := r.executeSQL(Update(r.tableName).Set("already_have", matches).Where(Eq{"id": id}))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (r *ripItemRepository) Paths() ([]string, error) {
	var rows []struct {
		Path string `db:"path"`
	}
	err := r.queryAll(Select("path").From(r.tableName).Where(NotEq{"path": ""}), &rows)
	res := make([]string, len(rows))
	for i := range rows {
		res[i] = rows[i].Path
	}
	return res, err
}

func (r *ripItemRepository) PromotesInterrupted() (model.RipItems, error) {
	res := model.RipItems{}
	err := r.queryAll(Select("*").From(r.tableName).Where(NotEq{"promote_dest": ""}).
		OrderBy("job_id", "position", "rowid"), &res)
	return res, err
}

func (r *ripItemRepository) MoveMatches(trackID, libraryName string) error {
	var items model.RipItems
	// A cheap first cut; each candidate's snapshots are then matched exactly.
	like := `%"id":"` + strings.NewReplacer(`%`, `\%`, `_`, `\_`, `\`, `\\`).Replace(trackID) + `"%`
	err := r.queryAll(Select("*").From(r.tableName).Where(Expr(`already_have LIKE ? ESCAPE '\'`, like)), &items)
	if err != nil {
		return err
	}
	for _, it := range items {
		changed := false
		for i := range it.AlreadyHave {
			if it.AlreadyHave[i].ID == trackID && it.AlreadyHave[i].Library != libraryName {
				it.AlreadyHave[i].Library = libraryName
				changed = true
			}
		}
		if !changed {
			continue
		}
		upd := Update(r.tableName).Set("already_have", it.AlreadyHave).Where(Eq{"id": it.ID})
		if _, err := r.executeSQL(upd); err != nil {
			return err
		}
	}
	return nil
}

var _ model.RipItemRepository = (*ripItemRepository)(nil)
