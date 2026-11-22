-- +goose Up
-- Find on MusicBrainz (#125): the MusicBrainz release and release group an album Correction sets.
-- Empty for what it leaves as tagged, and for other kinds.
ALTER TABLE correction ADD COLUMN mbz_album_id VARCHAR NOT NULL DEFAULT '';
ALTER TABLE correction ADD COLUMN mbz_release_group_id VARCHAR NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE correction DROP COLUMN mbz_release_group_id;
ALTER TABLE correction DROP COLUMN mbz_album_id;
