-- +goose Up
-- Track numbers Corrections (#128): the Files a track numbers Correction renumbers, as a JSON
-- array of {libraryId, path, title, fromDisc, fromTrack, disc, track}. Empty for other kinds.
ALTER TABLE correction ADD COLUMN track_numbers VARCHAR NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE correction DROP COLUMN track_numbers;
