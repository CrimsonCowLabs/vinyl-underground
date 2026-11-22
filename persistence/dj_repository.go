package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/pocketbase/dbx"
)

// djRepository holds the logged-in Listener's own DJs. A Listener's DJs are private, even from
// an admin, so every query is limited to their user_id.
type djRepository struct {
	sqlRepository
}

func NewDJRepository(ctx context.Context, db dbx.Builder) model.DJRepository {
	r := &djRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "dj"
	return r
}

func (r *djRepository) mine() Eq {
	return Eq{"user_id": loggedUser(r.ctx).ID}
}

func (r *djRepository) GetAll() (model.DJs, error) {
	var res model.DJs
	sq := Select("*").From(r.tableName).Where(r.mine()).OrderBy("name collate nocase", "created_at")
	err := r.queryAll(sq, &res)
	return res, err
}

func (r *djRepository) Get(id string) (*model.DJ, error) {
	var res model.DJ
	sq := Select("*").From(r.tableName).Where(And{r.mine(), Eq{"id": id}})
	if err := r.queryOne(sq, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *djRepository) Put(dj *model.DJ) error {
	now := time.Now()
	if dj.ID == "" {
		dj.ID = id.NewRandom()
		dj.UserID = loggedUser(r.ctx).ID
		dj.CreatedAt, dj.UpdatedAt = now, now
		ins := Insert(r.tableName).
			Columns("id", "user_id", "name", "description", "genres", "genre_source", "created_at", "updated_at").
			Values(dj.ID, dj.UserID, dj.Name, dj.Description, dj.Genres, dj.GenreSource, dj.CreatedAt, dj.UpdatedAt)
		_, err := r.executeSQL(ins)
		return err
	}
	dj.UpdatedAt = now
	upd := Update(r.tableName).
		Set("name", dj.Name).Set("description", dj.Description).
		Set("genres", dj.Genres).Set("genre_source", dj.GenreSource).Set("updated_at", dj.UpdatedAt).
		Where(And{r.mine(), Eq{"id": dj.ID}})
	count, err := r.executeSQL(upd)
	if err != nil {
		return err
	}
	if count == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (r *djRepository) Delete(id string) error {
	count, err := r.executeSQL(Delete(r.tableName).Where(And{r.mine(), Eq{"id": id}}))
	if err != nil {
		return err
	}
	if count == 0 {
		return model.ErrNotFound
	}
	return nil
}

var _ model.DJRepository = (*djRepository)(nil)
