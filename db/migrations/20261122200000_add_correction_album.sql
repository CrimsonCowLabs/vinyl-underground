-- +goose Up
-- Album Corrections (#124): the title, album artist and year an album Correction sets. Empty (or 0)
-- for what it leaves as tagged, and for other kinds.
ALTER TABLE correction ADD COLUMN album_name VARCHAR NOT NULL DEFAULT '';
ALTER TABLE correction ADD COLUMN album_artist VARCHAR NOT NULL DEFAULT '';
ALTER TABLE correction ADD COLUMN year INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE correction DROP COLUMN year;
ALTER TABLE correction DROP COLUMN album_artist;
ALTER TABLE correction DROP COLUMN album_name;
