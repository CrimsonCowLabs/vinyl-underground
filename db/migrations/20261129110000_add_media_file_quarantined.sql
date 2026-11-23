-- +goose Up
-- Quarantine from the app (#168): a Track whose File the app moved into Quarantine stays a missing
-- row, so a restore (the File back at its path) brings the same Track back with its favorites,
-- Playlist entries and plays. This flag keeps such rows off the Missing files page and out of its
-- deletes and of PurgeMissing; only Empty Quarantine deletes them, with their Files.
ALTER TABLE media_file ADD COLUMN quarantined BOOL NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE media_file DROP COLUMN quarantined;
