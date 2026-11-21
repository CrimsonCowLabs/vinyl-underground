-- +goose Up
-- The Tracks each Listener skipped on each of their Stations (#14), so a skip makes that Station,
-- and only that one, a little less likely to play the Track and its artist. A skip fades over
-- ~30 days, and older rows are pruned as the Station plays. Skips never touch annotations, so
-- ratings, favorites and play counts stay as they are.
CREATE TABLE IF NOT EXISTS station_skip (
    user_id VARCHAR(255) NOT NULL REFERENCES user(id) ON DELETE CASCADE,
    dj_id VARCHAR(255) NOT NULL,
    media_file_id VARCHAR(255) NOT NULL,
    artist VARCHAR NOT NULL DEFAULT '',
    skipped_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS station_skip_user_dj_skipped_at ON station_skip (user_id, dj_id, skipped_at);

-- +goose Down
DROP TABLE IF EXISTS station_skip;
