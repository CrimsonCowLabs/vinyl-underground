-- +goose Up
-- The Edit journal (ADR-0011, #160): every write the app makes to a File, recorded before the
-- File is touched and kept forever, with its original tags, so it can be undone (#161).
CREATE TABLE IF NOT EXISTS edit_journal (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    job_id VARCHAR(255) NOT NULL DEFAULT '',
    kind VARCHAR(32) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'pending',
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    library_id INTEGER NOT NULL,
    media_file_id VARCHAR(255) NOT NULL DEFAULT '',
    path VARCHAR NOT NULL,
    new_path VARCHAR NOT NULL DEFAULT '',
    original_tags VARCHAR NOT NULL DEFAULT '{}',
    changes VARCHAR NOT NULL DEFAULT '{}',
    copy_sum VARCHAR NOT NULL DEFAULT '',
    undo_of VARCHAR(255) NOT NULL DEFAULT '',
    error VARCHAR NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS edit_journal_state ON edit_journal (state);
CREATE INDEX IF NOT EXISTS edit_journal_job_id ON edit_journal (job_id);
CREATE INDEX IF NOT EXISTS edit_journal_file ON edit_journal (library_id, path);
CREATE INDEX IF NOT EXISTS edit_journal_created_at ON edit_journal (created_at);

-- +goose Down
DROP TABLE IF EXISTS edit_journal;
