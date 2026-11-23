-- +goose Up
-- The Correction migration (#165) renames and merges several Artists in one edit job, each keeping
-- its own stars and ratings for the job's undo: part tells them apart (the job's other kinds
-- rename one Artist, and use part '').
CREATE TABLE edit_job_annotation_new (
    job_id VARCHAR(255) NOT NULL REFERENCES edit_job (id) ON DELETE CASCADE,
    part VARCHAR(255) NOT NULL DEFAULT '',
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
    PRIMARY KEY (job_id, part, user_id)
);
INSERT INTO edit_job_annotation_new (job_id, part, user_id, play_count, play_date, rating, rated_at, starred,
    starred_at, merged, star_taken, rating_taken)
SELECT job_id, '', user_id, play_count, play_date, rating, rated_at, starred, starred_at, merged, star_taken,
    rating_taken
FROM edit_job_annotation;
DROP TABLE edit_job_annotation;
ALTER TABLE edit_job_annotation_new RENAME TO edit_job_annotation;

-- +goose Down
CREATE TABLE edit_job_annotation_old (
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
INSERT INTO edit_job_annotation_old (job_id, user_id, play_count, play_date, rating, rated_at, starred,
    starred_at, merged, star_taken, rating_taken)
SELECT job_id, user_id, play_count, play_date, rating, rated_at, starred, starred_at, merged, star_taken,
    rating_taken
FROM edit_job_annotation WHERE part = '';
DROP TABLE edit_job_annotation;
ALTER TABLE edit_job_annotation_old RENAME TO edit_job_annotation;
