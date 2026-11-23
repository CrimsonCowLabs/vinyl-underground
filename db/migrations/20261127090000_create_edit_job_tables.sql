-- +goose Up
-- Edit jobs (#162, ADR-0011): batches of writes to Files, run in the background one at a time,
-- one step per File, kept so an interrupted job carries on after a restart.
CREATE TABLE IF NOT EXISTS edit_job (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    kind VARCHAR(32) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'queued',
    total INTEGER NOT NULL DEFAULT 0,
    done INTEGER NOT NULL DEFAULT 0,
    skipped INTEGER NOT NULL DEFAULT 0,
    current VARCHAR NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS edit_job_state_created_at ON edit_job (state, created_at);
CREATE INDEX IF NOT EXISTS edit_job_created_at ON edit_job (created_at);

CREATE TABLE IF NOT EXISTS edit_job_step (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    job_id VARCHAR(255) NOT NULL REFERENCES edit_job (id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    kind VARCHAR(32) NOT NULL,
    media_file_id VARCHAR(255) NOT NULL DEFAULT '',
    path VARCHAR NOT NULL DEFAULT '',
    args VARCHAR NOT NULL DEFAULT '{}',
    state VARCHAR(32) NOT NULL DEFAULT 'queued',
    code VARCHAR(64) NOT NULL DEFAULT '',
    error VARCHAR NOT NULL DEFAULT '',
    warning VARCHAR NOT NULL DEFAULT '',
    entry_id VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS edit_job_step_job_position ON edit_job_step (job_id, position);

-- +goose Down
DROP TABLE IF EXISTS edit_job_step;
DROP TABLE IF EXISTS edit_job;
