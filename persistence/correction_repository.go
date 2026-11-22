package persistence

import (
	"context"
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

func (r *correctionRepository) GetAll() (model.Corrections, error) {
	var res model.Corrections
	sq := Select("correction.*", "coalesce(tc.track_count, 0) as track_count").From(r.tableName).
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

func (r *correctionRepository) Add(c *model.Correction) error {
	c.ID = id.NewRandom()
	c.CreatedAt = time.Now()
	c.CreatedBy = loggedUser(r.ctx).UserName
	ins := Insert(r.tableName).
		Columns("id", "kind", "match_key", "from_value", "to_value", "created_by", "created_at", "undoing").
		Values(c.ID, c.Kind, c.MatchKey, c.From, c.To, c.CreatedBy, c.CreatedAt, false).
		Suffix("ON CONFLICT (kind, match_key) DO NOTHING")
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

// The SQL below works on the artist annotations of both Artists, for every user. Dates compare as
// stored, which is how the split recognises a star or rating unchanged since the merge.

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
		play_count = correction_annotation.play_count + excluded.play_count`,
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
	// Users who annotated only fromID: their row moves whole. The others' were merged above.
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
