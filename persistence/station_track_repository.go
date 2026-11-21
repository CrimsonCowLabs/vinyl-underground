package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type stationTrackRepository struct {
	sqlRepository
}

func NewStationTrackRepository(ctx context.Context, db dbx.Builder) model.StationTrackRepository {
	r := &stationTrackRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "station_track"
	return r
}

func (r *stationTrackRepository) userID() string {
	return loggedUser(r.ctx).ID
}

func (r *stationTrackRepository) Add(tracks ...model.StationTrack) error {
	if len(tracks) == 0 {
		return nil
	}
	ins := Insert(r.tableName).Columns("user_id", "media_file_id", "artist", "served_at")
	for _, t := range tracks {
		ins = ins.Values(r.userID(), t.MediaFileID, t.Artist, t.ServedAt)
	}
	_, err := r.executeSQL(ins)
	return err
}

func (r *stationTrackRepository) Since(since time.Time) (model.StationTracks, error) {
	var res model.StationTracks
	sq := Select("*").From(r.tableName).
		Where(And{Eq{"user_id": r.userID()}, GtOrEq{"served_at": since}}).
		OrderBy("served_at desc", "rowid desc")
	err := r.queryAll(sq, &res)
	return res, err
}

func (r *stationTrackRepository) DeleteBefore(t time.Time) error {
	_, err := r.executeSQL(Delete(r.tableName).Where(And{Eq{"user_id": r.userID()}, Lt{"served_at": t}}))
	return err
}

var _ model.StationTrackRepository = (*stationTrackRepository)(nil)
