-- +goose Up
CREATE TABLE IF NOT EXISTS rip_job (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    url VARCHAR NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    error VARCHAR NOT NULL DEFAULT '',
    title VARCHAR NOT NULL DEFAULT '',
    source VARCHAR NOT NULL DEFAULT '',
    path VARCHAR NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS rip_job_status_created_at ON rip_job (status, created_at);

-- +goose Down
DROP TABLE IF EXISTS rip_job;
