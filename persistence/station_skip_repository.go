package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type stationSkipRepository struct {
	sqlRepository
}

func NewStationSkipRepository(ctx context.Context, db dbx.Builder) model.StationSkipRepository {
	r := &stationSkipRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "station_skip"
	return r
}

func (r *stationSkipRepository) userID() string {
	return loggedUser(r.ctx).ID
}

func (r *stationSkipRepository) Add(s model.StationSkip) error {
	ins := Insert(r.tableName).Columns("user_id", "dj_id", "media_file_id", "artist", "skipped_at").
		Values(r.userID(), s.DJID, s.MediaFileID, s.Artist, s.SkippedAt)
	_, err := r.executeSQL(ins)
	return err
}

func (r *stationSkipRepository) Since(djID string, since time.Time) (model.StationSkips, error) {
	var res model.StationSkips
	sq := Select("*").From(r.tableName).
		Where(And{Eq{"user_id": r.userID(), "dj_id": djID}, GtOrEq{"skipped_at": since}}).
		OrderBy("skipped_at desc", "rowid desc")
	err := r.queryAll(sq, &res)
	return res, err
}

func (r *stationSkipRepository) DeleteBefore(t time.Time) error {
	_, err := r.executeSQL(Delete(r.tableName).Where(And{Eq{"user_id": r.userID()}, Lt{"skipped_at": t}}))
	return err
}

var _ model.StationSkipRepository = (*stationSkipRepository)(nil)
