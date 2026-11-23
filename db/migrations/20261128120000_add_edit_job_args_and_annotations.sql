-- +goose Up
-- Write-through Corrections (#163): an edit job's own input, read by its kind (e.g. the Artist an
-- artist rename job renames), and the stars and ratings an artist rename or merge
-- job moves from the renamed Artist to its new name, one row per Listener (user), kept so undoing
-- the job can move them back (play counts follow the Tracks: each scan counts them again).
-- merged says whether they have been moved yet; star_taken and
-- rating_taken whether the new name took this Artist's star or rating (it had none of its own).
ALTER TABLE edit_job ADD COLUMN args VARCHAR NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS edit_job_annotation (
    job_id VARCHAR(255) NOT NULL REFERENCES edit_job (id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL,
    play_count INTEGER NOT NULL DEFAULT 0,
    play_date DATETIME,
    rating INTEGER NOT NULL DEFAULT 0,
    rated_at DATETIME,
    starred BOOL NOT NULL DEFAULT FALSE,
    starred_at DATETIME,
    merged BOOL NOT NULL DEFAULT FALSE,
    star_taken BOOL NOT NULL DEFAULT FALSE,
    rating_taken BOOL NOT NULL DEFAULT FALSE,
    PRIMARY KEY (job_id, user_id)
);

-- +goose Down
DROP TABLE IF EXISTS edit_job_annotation;
ALTER TABLE edit_job DROP COLUMN args;
