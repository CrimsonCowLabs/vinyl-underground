-- +goose Up
-- Album cover blocks (#127): an album can have several, one per blocked image, so the
-- one-Correction-per-kind-and-key index no longer holds for that kind.
DROP INDEX IF EXISTS correction_kind_match_key;
CREATE UNIQUE INDEX IF NOT EXISTS correction_kind_match_key ON correction (kind, match_key)
    WHERE kind <> 'albumCoverBlock';
CREATE UNIQUE INDEX IF NOT EXISTS correction_cover_block ON correction (match_key, image_hash)
    WHERE kind = 'albumCoverBlock';

-- +goose Down
DELETE FROM correction WHERE kind = 'albumCoverBlock';
DROP INDEX IF EXISTS correction_cover_block;
DROP INDEX IF EXISTS correction_kind_match_key;
CREATE UNIQUE INDEX IF NOT EXISTS correction_kind_match_key ON correction (kind, match_key);
