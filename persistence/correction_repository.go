package persistence

import (
	"context"
	"slices"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/pocketbase/dbx"
)

// correctionRepository holds the admins' Corrections. Which Tracks each applies to is recorded on
// the Tracks (media_file.correction_ids) as the scanner applied it, until the overlay was retired
// (#165): a rescan clears them.
type correctionRepository struct {
	sqlRepository
}

func NewCorrectionRepository(ctx context.Context, db dbx.Builder) model.CorrectionRepository {
	r := &correctionRepository{}
	r.ctx = ctx
	r.db = db
	r.tableName = "correction"
	return r
}

// trackCounts counts, per Correction, the present Tracks it applies to.
const trackCounts = `(select je.value as correction_id, count(*) as track_count
	from media_file, json_each(nullif(media_file.correction_ids, '')) je
	where media_file.missing = 0
	group by je.value) tc`

// albumFolderTrackCount counts the present Tracks of the albums in a Correction's folder (an album
// Correction's MatchKey, see model.AlbumFolderKey), whichever of their folders they are in.
const albumFolderTrackCount = `(select count(*) from media_file
	where media_file.missing = 0 and media_file.album_id in
		(select album.id from album, json_each(album.folder_ids) af where af.value = correction.match_key))`

func (r *correctionRepository) GetAll() (model.Corrections, error) {
	var res model.Corrections
	// Nothing applies a Correction on read any more (#165), so the Tracks' correction_ids only
	// keep what the overlay last applied: an album's Corrections count the album's Tracks instead.
	sq := Select("correction.*", "case when correction.kind in ('"+string(model.CorrectionAlbumCover)+"', '"+string(model.CorrectionAlbum)+"', '"+
		string(model.CorrectionAlbumCoverBlock)+"') then "+albumFolderTrackCount+
		" else coalesce(tc.track_count, 0) end as track_count").From(r.tableName).
		LeftJoin(trackCounts+" on tc.correction_id = correction.id").
		OrderBy("correction.created_at desc", "correction.rowid desc")
	err := r.queryAll(sq, &res)
	return res, err
}

func (r *correctionRepository) Get(id string) (*model.Correction, error) {
	var res model.Correction
	if err := r.queryOne(Select("*").From(r.tableName).Where(Eq{"id": id}), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *correctionRepository) GetByMatchKey(kind model.CorrectionKind, key string) (*model.Correction, error) {
	var res model.Correction
	if err := r.queryOne(Select("*").From(r.tableName).Where(Eq{"kind": kind, "match_key": key}), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *correctionRepository) FindByMatchKeys(keys []string, kinds ...model.CorrectionKind) (model.Corrections, error) {
	res := model.Corrections{}
	if len(keys) == 0 {
		return res, nil
	}
	sq := Select("*").From(r.tableName).Where(Eq{"kind": kinds, "match_key": keys}).OrderBy("created_at", "rowid")
	err := r.queryAll(sq, &res)
	return res, err
}

func (r *correctionRepository) Add(c *model.Correction) error {
	c.ID = id.NewRandom()
	c.CreatedAt = time.Now()
	c.CreatedBy = loggedUser(r.ctx).UserName
	ins := Insert(r.tableName).
		Columns("id", "kind", "match_key", "from_value", "to_value", "created_by", "created_at", "undoing",
			"image_hash", "image_mime", "album_name", "album_artist", "year",
			"mbz_album_id", "mbz_release_group_id", "track_numbers").
		Values(c.ID, c.Kind, c.MatchKey, c.From, c.To, c.CreatedBy, c.CreatedAt, false, c.ImageHash, c.ImageMime,
			c.AlbumName, c.AlbumArtist, c.Year, c.MbzAlbumID, c.MbzReleaseGroupID, c.TrackNumbers).
		// Whichever unique index it breaks: one per kind and key, or one per image for cover blocks
		Suffix("ON CONFLICT DO NOTHING")
	count, err := r.executeSQL(ins)
	if err != nil {
		return err
	}
	if count == 0 {
		return model.ErrAlreadyExists
	}
	return nil
}

func (r *correctionRepository) SetUndoing(id string) error {
	count, err := r.executeSQL(Update(r.tableName).Set("undoing", true).Where(Eq{"id": id}))
	if err == nil && count == 0 {
		return model.ErrNotFound
	}
	return err
}

func (r *correctionRepository) Delete(id string) error {
	// Foreign keys may be off (they are only turned on for in-memory databases)
	if _, err := r.executeSQL(Delete("correction_annotation").Where(Eq{"correction_id": id})); err != nil {
		return err
	}
	return r.deleteByID(id)
}

func (r *correctionRepository) FolderIDs(id string) ([]string, error) {
	var res []string
	sq := Select("distinct media_file.folder_id").From("media_file").
		Join("json_each(nullif(media_file.correction_ids, '')) je").
		Where(And{Eq{"media_file.missing": false}, Eq{"je.value": id}}).
		OrderBy("media_file.folder_id")
	err := r.queryAllSlice(sq, &res)
	return res, err
}

func (r *correctionRepository) AlbumFolderIDs(key string) ([]string, error) {
	var res []string
	sq := Select("distinct af.value").From("album").
		Join("json_each(album.folder_ids) af").
		Where(Expr("exists (select 1 from json_each(album.folder_ids) k where k.value = ?)", key))
	if err := r.queryAllSlice(sq, &res); err != nil {
		return nil, err
	}
	if !slices.Contains(res, key) {
		res = append(res, key)
	}
	slices.Sort(res)
	return res, nil
}

func (r *correctionRepository) KeepArtistAnnotations(correctionID, artistID string) error {
	_, err := r.db.NewQuery(`insert into correction_annotation (correction_id, user_id, play_count, play_date,
		rating, rated_at, starred, starred_at, star_taken, rating_taken)
	select {:id}, user_id, 0, play_date, coalesce(rating, 0), rated_at, coalesce(starred, 0), starred_at,
		coalesce(starred, 0), coalesce(rating, 0) > 0
	from annotation where item_type = 'artist' and item_id = {:artist}
		and (coalesce(starred, 0) or coalesce(rating, 0) > 0)
	on conflict (correction_id, user_id) do nothing`).Bind(dbx.Params{"id": correctionID, "artist": artistID}).Execute()
	return err
}

var _ model.CorrectionRepository = (*correctionRepository)(nil)
