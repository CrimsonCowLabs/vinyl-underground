-- +goose Up
-- A Rip job can now be a whole playlist or album (#30). Each of its videos is a rip_item, which
-- becomes one File in the Inbox. A single video is a job with one item.
CREATE TABLE IF NOT EXISTS rip_item (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    job_id VARCHAR(255) NOT NULL REFERENCES rip_job (id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    url VARCHAR NOT NULL,
    title VARCHAR NOT NULL DEFAULT '',
    artist VARCHAR NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    error VARCHAR NOT NULL DEFAULT '',
    path VARCHAR NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS rip_item_job_id ON rip_item (job_id, position);
CREATE INDEX IF NOT EXISTS rip_item_status ON rip_item (status);

-- The File's path moves to the item. The job gets the Inbox subfolder of a playlist's Files.
ALTER TABLE rip_job ADD COLUMN folder VARCHAR NOT NULL DEFAULT '';
ALTER TABLE rip_job DROP COLUMN path;

-- +goose Down
ALTER TABLE rip_job ADD COLUMN path VARCHAR NOT NULL DEFAULT '';
ALTER TABLE rip_job DROP COLUMN folder;
DROP TABLE IF EXISTS rip_item;
