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
// the Tracks (media_file.correction_ids) as the scanner applies it.
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
	sq := Select("correction.*", "case when correction.kind in ('"+string(model.CorrectionAlbumCover)+"', '"+
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
			"mbz_album_id", "mbz_release_group_id").
		Values(c.ID, c.Kind, c.MatchKey, c.From, c.To, c.CreatedBy, c.CreatedAt, false, c.ImageHash, c.ImageMime,
			c.AlbumName, c.AlbumArtist, c.Year, c.MbzAlbumID, c.MbzReleaseGroupID).
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

// The SQL below works on the artist annotations of both Artists, for every Listener. Dates compare
// as stored, which is how the split recognises a star or rating unchanged since the merge. A merge
// can run again (in a later scan reaching more of the merged Artist's folders): what fromID got
// meanwhile joins what was kept.

// mergeArtistAnnotationsSQL keeps (adding to it) what fromID has, then merges it into toID's: play
// counts added, the later play date, and toID's own star and rating unless it has none.
var mergeArtistAnnotationsSQL = []string{
	`insert into correction_annotation (correction_id, user_id, play_count, play_date, rating, rated_at,
		starred, starred_at, star_taken, rating_taken)
	select {:id}, a.user_id, coalesce(a.play_count, 0), a.play_date, coalesce(a.rating, 0), a.rated_at,
		a.starred, a.starred_at,
		a.starred and not coalesce(b.starred, 0), coalesce(a.rating, 0) > 0 and coalesce(b.rating, 0) = 0
	from annotation a left join annotation b
		on b.user_id = a.user_id and b.item_type = 'artist' and b.item_id = {:to}
	where a.item_type = 'artist' and a.item_id = {:from}
	on conflict (correction_id, user_id) do update set
		play_count = correction_annotation.play_count + excluded.play_count,
		star_taken = correction_annotation.star_taken or excluded.star_taken,
		starred = case when excluded.star_taken then excluded.starred else correction_annotation.starred end,
		starred_at = case when excluded.star_taken then excluded.starred_at else correction_annotation.starred_at end,
		rating_taken = correction_annotation.rating_taken or excluded.rating_taken,
		rating = case when excluded.rating_taken then excluded.rating else correction_annotation.rating end,
		rated_at = case when excluded.rating_taken then excluded.rated_at else correction_annotation.rated_at end`,
	`update annotation set
		play_count = coalesce(annotation.play_count, 0) + coalesce(a.play_count, 0),
		play_date = case when annotation.play_date is null or a.play_date > annotation.play_date
			then a.play_date else annotation.play_date end,
		starred = annotation.starred or a.starred,
		starred_at = case when not annotation.starred and a.starred then a.starred_at else annotation.starred_at end,
		rating = case when coalesce(annotation.rating, 0) > 0 then annotation.rating else coalesce(a.rating, 0) end,
		rated_at = case when coalesce(annotation.rating, 0) = 0 and coalesce(a.rating, 0) > 0
			then a.rated_at else annotation.rated_at end
	from annotation a
	where annotation.item_type = 'artist' and annotation.item_id = {:to}
		and a.item_type = 'artist' and a.item_id = {:from} and a.user_id = annotation.user_id`,
	// Listeners who annotated only fromID: their row moves whole. The others' were merged above.
	`update or ignore annotation set item_id = {:to} where item_type = 'artist' and item_id = {:from}`,
	`delete from annotation where item_type = 'artist' and item_id = {:from}`,
}

// splitArtistAnnotationsSQL gives toID back what the merge kept, joining whatever toID has got
// since, and takes from fromID the play counts the merge added (but not those since), and the star
// and rating it gave, unless they have changed since.
var splitArtistAnnotationsSQL = []string{
	`insert into annotation (user_id, item_id, item_type, play_count, play_date, rating, rated_at, starred, starred_at)
	select user_id, {:to}, 'artist', play_count, play_date, rating, rated_at, starred, starred_at
	from correction_annotation
	where correction_id = {:id} and user_id in (select id from user)
	on conflict (user_id, item_id, item_type) do update set
		play_count = coalesce(annotation.play_count, 0) + excluded.play_count,
		play_date = case when annotation.play_date is null or excluded.play_date > annotation.play_date
			then excluded.play_date else annotation.play_date end,
		starred = annotation.starred or excluded.starred,
		starred_at = case when not annotation.starred and excluded.starred then excluded.starred_at else annotation.starred_at end,
		rating = case when coalesce(annotation.rating, 0) > 0 then annotation.rating else excluded.rating end,
		rated_at = case when coalesce(annotation.rating, 0) = 0 and excluded.rating > 0
			then excluded.rated_at else annotation.rated_at end`,
	`update annotation set
		play_count = max(coalesce(annotation.play_count, 0) - s.play_count, 0),
		starred = case when s.star_taken and annotation.starred and annotation.starred_at is s.starred_at
			then 0 else annotation.starred end,
		starred_at = case when s.star_taken and annotation.starred and annotation.starred_at is s.starred_at
			then null else annotation.starred_at end,
		rating = case when s.rating_taken and annotation.rating = s.rating and annotation.rated_at is s.rated_at
			then 0 else annotation.rating end,
		rated_at = case when s.rating_taken and annotation.rating = s.rating and annotation.rated_at is s.rated_at
			then null else annotation.rated_at end
	from correction_annotation s
	where s.correction_id = {:id} and s.user_id = annotation.user_id
		and annotation.item_type = 'artist' and annotation.item_id = {:from}`,
	`delete from correction_annotation where correction_id = {:id}`,
}

func (r *correctionRepository) MergeArtistAnnotations(correctionID, fromID, toID string) error {
	return r.moveArtistAnnotations(mergeArtistAnnotationsSQL, correctionID, fromID, toID)
}

func (r *correctionRepository) SplitArtistAnnotations(correctionID, fromID, toID string) error {
	return r.moveArtistAnnotations(splitArtistAnnotationsSQL, correctionID, fromID, toID)
}

func (r *correctionRepository) moveArtistAnnotations(stmts []string, correctionID, fromID, toID string) error {
	params := dbx.Params{"id": correctionID, "from": fromID, "to": toID}
	for _, stmt := range stmts {
		if _, err := r.db.NewQuery(stmt).Bind(params).Execute(); err != nil {
			return err
		}
	}
	// Both Artists' rating populations changed, so their cached averages no longer match
	for _, id := range []string{fromID, toID} {
		upd := Update("artist").Where(Eq{"id": id}).Set("average_rating", Expr(
			"coalesce((select round(avg(rating), 2) from annotation where item_id = ? and item_type = 'artist' and rating > 0), 0)", id))
		if _, err := r.executeSQL(upd); err != nil {
			return err
		}
	}
	return nil
}

var _ model.CorrectionRepository = (*correctionRepository)(nil)
