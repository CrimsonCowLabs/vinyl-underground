-- +goose Up
-- The Tracks each Listener's Station has served lately, so it can keep Tracks from repeating
-- within ~48h and artists within ~5 Tracks. Rows older than that are pruned as the Station plays.
CREATE TABLE IF NOT EXISTS station_track (
    user_id VARCHAR(255) NOT NULL REFERENCES user(id) ON DELETE CASCADE,
    media_file_id VARCHAR(255) NOT NULL,
    artist VARCHAR NOT NULL DEFAULT '',
    served_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS station_track_user_served_at ON station_track (user_id, served_at);

-- +goose Down
DROP TABLE IF EXISTS station_track;
