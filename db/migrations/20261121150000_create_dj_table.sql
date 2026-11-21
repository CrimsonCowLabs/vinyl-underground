-- +goose Up
-- The DJs Listeners create for their Stations (#17), from a name and a personality description.
-- Built-in DJs aren't stored here. Each DJ is private to its Listener. Station history doesn't
-- refer to this table (station_skip.dj_id has no foreign key), so deleting a DJ leaves play
-- history as it is.
CREATE TABLE IF NOT EXISTS dj (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL REFERENCES user(id) ON DELETE CASCADE,
    name VARCHAR NOT NULL,
    description VARCHAR NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS dj_user_name ON dj (user_id, name);

-- +goose Down
DROP TABLE IF EXISTS dj;
