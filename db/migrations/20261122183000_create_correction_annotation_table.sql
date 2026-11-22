-- +goose Up
-- Artist merges (#123): the stars, ratings and play counts an artistMerge Correction moved into the
-- Artist it merges into, one row per Listener (user), so undoing it can move them back. star_taken and
-- rating_taken say whether the merged Artist took this Artist's star or rating (it had none of its
-- own). Rows are deleted once the merge is undone, or with their Correction.
CREATE TABLE IF NOT EXISTS correction_annotation (
    correction_id VARCHAR(255) NOT NULL REFERENCES correction (id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL,
    play_count INTEGER NOT NULL DEFAULT 0,
    play_date DATETIME,
    rating INTEGER NOT NULL DEFAULT 0,
    rated_at DATETIME,
    starred BOOL NOT NULL DEFAULT FALSE,
    starred_at DATETIME,
    star_taken BOOL NOT NULL DEFAULT FALSE,
    rating_taken BOOL NOT NULL DEFAULT FALSE,
    PRIMARY KEY (correction_id, user_id)
);

-- +goose Down
DROP TABLE IF EXISTS correction_annotation;
