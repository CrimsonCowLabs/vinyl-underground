-- +goose Up
-- The Library genres a Listener's DJ plays without an LLM (#106), as a JSON array of genre
-- names, and where they came from: '' (never worked out, as for the DJs already here), 'words'
-- (matched from the DJ's name and description) or 'listener' (picked by hand, so editing the
-- description leaves them alone).
ALTER TABLE dj ADD COLUMN genres VARCHAR NOT NULL DEFAULT '[]';
ALTER TABLE dj ADD COLUMN genre_source VARCHAR NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE dj DROP COLUMN genre_source;
ALTER TABLE dj DROP COLUMN genres;
