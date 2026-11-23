-- +goose Up
-- Undo (#161) looks for the later writes to an entry's File by its Track, as well as by its path.
CREATE INDEX IF NOT EXISTS edit_journal_media_file_id ON edit_journal (media_file_id);

-- +goose Down
DROP INDEX IF EXISTS edit_journal_media_file_id;
