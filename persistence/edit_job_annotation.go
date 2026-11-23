package persistence

import (
	. "github.com/Masterminds/squirrel"
	"github.com/pocketbase/dbx"
)

// The SQL below moves the stars and ratings of an artist rename (or merge) job (#163), for every
// Listener, as the artist merge Correction's SQL does (see correction_repository.go), but from
// what the job kept of the renamed Artist before writing its Files: by the time they are written,
// the scans after each File have deleted the Artist, and its annotations with it. Play counts
// are left alone: every scan counts an Artist's plays again from its Tracks'
// (artistRepository.RefreshPlayCounts), so they follow the Tracks to the new name, and back.

// upsertArtistAnnotation ends an insert into annotation that joins a Listener's star and rating
// to the row the Artist already has, which keeps its own unless it has none.
const upsertArtistAnnotation = `
	on conflict (user_id, item_id, item_type) do update set
		starred = annotation.starred or excluded.starred,
		starred_at = case when not annotation.starred and excluded.starred then excluded.starred_at else annotation.starred_at end,
		rating = case when coalesce(annotation.rating, 0) > 0 then annotation.rating else excluded.rating end,
		rated_at = case when coalesce(annotation.rating, 0) = 0 and excluded.rating > 0
			then excluded.rated_at else annotation.rated_at end`

var jobKeepArtistSQL = []string{
	`delete from edit_job_annotation where job_id = {:job} and part = {:part}`,
	`insert into edit_job_annotation (job_id, part, user_id, play_count, play_date, rating, rated_at, starred, starred_at)
	select {:job}, {:part}, user_id, coalesce(play_count, 0), play_date, coalesce(rating, 0), rated_at, coalesce(starred, 0), starred_at
	from annotation where item_type = 'artist' and item_id = {:from} and (coalesce(starred, 0) or coalesce(rating, 0) > 0)`,
}

// jobMergeArtistSQL notes whether toID takes the kept star and rating (it has none of its own),
// joins them to toID's, and takes them from what fromID has left. Only what isn't merged yet is
// merged.
var jobMergeArtistSQL = []string{
	`update edit_job_annotation set
		star_taken = starred and not coalesce((select b.starred from annotation b where b.user_id = edit_job_annotation.user_id
			and b.item_type = 'artist' and b.item_id = {:to}), 0),
		rating_taken = rating > 0 and coalesce((select b.rating from annotation b where b.user_id = edit_job_annotation.user_id
			and b.item_type = 'artist' and b.item_id = {:to}), 0) = 0
	where job_id = {:job} and part = {:part} and not merged`,
	`insert into annotation (user_id, item_id, item_type, play_count, rating, rated_at, starred, starred_at)
	select user_id, {:to}, 'artist', 0, rating, rated_at, starred, starred_at
	from edit_job_annotation
	where job_id = {:job} and part = {:part} and not merged and user_id in (select id from user)` + upsertArtistAnnotation,
	`update annotation set starred = 0, starred_at = null, rating = 0, rated_at = null
	where item_type = 'artist' and item_id = {:from}
		and user_id in (select user_id from edit_job_annotation where job_id = {:job} and part = {:part} and not merged)`,
	`update edit_job_annotation set merged = 1 where job_id = {:job} and part = {:part}`,
}

// jobSplitArtistSQL gives fromID back the kept star and rating (unless it has its own since), and
// takes from toID the star and rating it took, unless they have changed since.
var jobSplitArtistSQL = []string{
	`insert into annotation (user_id, item_id, item_type, play_count, rating, rated_at, starred, starred_at)
	select user_id, {:from}, 'artist', 0, rating, rated_at, starred, starred_at
	from edit_job_annotation
	where job_id = {:job} and part = {:part} and merged and user_id in (select id from user)` + upsertArtistAnnotation,
	`update annotation set
		starred = case when s.star_taken and annotation.starred and annotation.starred_at is s.starred_at
			then 0 else annotation.starred end,
		starred_at = case when s.star_taken and annotation.starred and annotation.starred_at is s.starred_at
			then null else annotation.starred_at end,
		rating = case when s.rating_taken and annotation.rating = s.rating and annotation.rated_at is s.rated_at
			then 0 else annotation.rating end,
		rated_at = case when s.rating_taken and annotation.rating = s.rating and annotation.rated_at is s.rated_at
			then null else annotation.rated_at end
	from edit_job_annotation s
	where s.job_id = {:job} and s.part = {:part} and s.merged and s.user_id = annotation.user_id
		and annotation.item_type = 'artist' and annotation.item_id = {:to}`,
}

// jobFoldArtistSQL joins what is left of toID's stars and ratings to fromID's: what a renamed
// Artist got under its new name goes back with the old name.
var jobFoldArtistSQL = []string{
	`insert into annotation (user_id, item_id, item_type, play_count, rating, rated_at, starred, starred_at)
	select user_id, {:from}, 'artist', 0, coalesce(rating, 0), rated_at, coalesce(starred, 0), starred_at
	from annotation where item_type = 'artist' and item_id = {:to}
		and (coalesce(starred, 0) or coalesce(rating, 0) > 0)` + upsertArtistAnnotation,
	`update annotation set starred = 0, starred_at = null, rating = 0, rated_at = null
	where item_type = 'artist' and item_id = {:to}`,
}

const jobForgetArtistSQL = `delete from edit_job_annotation where job_id = {:job} and part = {:part}`

func (r *editJobRepository) KeepArtistAnnotations(jobID, fromID string) error {
	return r.KeepArtistAnnotationsIn(jobID, "", fromID)
}

func (r *editJobRepository) MergeArtistAnnotations(jobID, fromID, toID string) error {
	return r.MergeArtistAnnotationsIn(jobID, "", fromID, toID)
}

func (r *editJobRepository) SplitArtistAnnotations(jobID, fromID, toID string, all bool) error {
	return r.SplitArtistAnnotationsIn(jobID, "", fromID, toID, all)
}

func (r *editJobRepository) KeepArtistAnnotationsIn(jobID, part, fromID string) error {
	return r.runAnnotationSQL(jobKeepArtistSQL, jobID, part, fromID, "")
}

func (r *editJobRepository) MergeArtistAnnotationsIn(jobID, part, fromID, toID string) error {
	return r.runAnnotationSQL(jobMergeArtistSQL, jobID, part, fromID, toID)
}

func (r *editJobRepository) SplitArtistAnnotationsIn(jobID, part, fromID, toID string, all bool) error {
	stmts := jobSplitArtistSQL
	if all {
		stmts = append(append([]string{}, stmts...), jobFoldArtistSQL...)
	}
	return r.runAnnotationSQL(append(stmts, jobForgetArtistSQL), jobID, part, fromID, toID)
}

// jobAdoptCorrectionSQL keeps with the job's part what an artist Correction kept (see
// correction_repository.go): what a merge merged, as merged already, or the stars a plain rename
// moved to its new name, to merge into it again. The Correction goes once its Files are written,
// and its rows with it, but the job's undo still splits them (jobSplitArtistSQL).
var jobAdoptCorrectionSQL = []string{
	`delete from edit_job_annotation where job_id = {:job} and part = {:part}`,
	`insert into edit_job_annotation (job_id, part, user_id, play_count, play_date, rating, rated_at, starred,
		starred_at, merged, star_taken, rating_taken)
	select {:job}, {:part}, user_id, play_count, play_date, rating, rated_at, starred, starred_at, {:merged}, star_taken,
		rating_taken
	from correction_annotation where correction_id = {:correction}`,
}

func (r *editJobRepository) AdoptCorrectionAnnotations(jobID, part, correctionID string, merged bool) (int64, error) {
	var n int64
	params := dbx.Params{"job": jobID, "part": part, "correction": correctionID, "merged": merged}
	for _, stmt := range jobAdoptCorrectionSQL {
		res, err := r.db.NewQuery(stmt).Bind(params).Execute()
		if err != nil {
			return 0, err
		}
		n, _ = res.RowsAffected()
	}
	return n, nil
}

func (r *editJobRepository) runAnnotationSQL(stmts []string, jobID, part, fromID, toID string) error {
	params := dbx.Params{"job": jobID, "part": part, "from": fromID, "to": toID}
	for _, stmt := range stmts {
		if _, err := r.db.NewQuery(stmt).Bind(params).Execute(); err != nil {
			return err
		}
	}
	if toID == "" {
		return nil
	}
	// Both Artists' rating populations changed, so their cached averages no longer match.
	for _, id := range []string{fromID, toID} {
		upd := Update("artist").Where(Eq{"id": id}).Set("average_rating", Expr(
			"coalesce((select round(avg(rating), 2) from annotation where item_id = ? and item_type = 'artist' and rating > 0), 0)", id))
		if _, err := r.executeSQL(upd); err != nil {
			return err
		}
	}
	return nil
}
