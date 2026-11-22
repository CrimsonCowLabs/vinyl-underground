-- +goose Up
-- Corrections (#122): an admin's fixes to wrong tag values, kept here and never written to the
-- (read-only) Files. The scanner applies them whenever it reads tags. match_key is what a
-- Correction matches, in a form that depends on its kind (for an artistRename, the normalised
-- Artist name). An undoing Correction is no longer applied, and is deleted once its folders are
-- rescanned. created_by is the admin's user name, kept if the user is deleted.
CREATE TABLE IF NOT EXISTS correction (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    kind VARCHAR NOT NULL,
    match_key VARCHAR NOT NULL,
    from_value VARCHAR NOT NULL DEFAULT '',
    to_value VARCHAR NOT NULL DEFAULT '',
    created_by VARCHAR NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    undoing BOOL NOT NULL DEFAULT FALSE
);
CREATE UNIQUE INDEX IF NOT EXISTS correction_kind_match_key ON correction (kind, match_key);

-- The Corrections the scanner applied to each Track, as a JSON array of correction ids (empty when
-- none), so the Corrections page can tell which match nothing, and undo knows what to rescan.
ALTER TABLE media_file ADD COLUMN correction_ids VARCHAR NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE media_file DROP COLUMN correction_ids;
DROP TABLE IF EXISTS correction;
