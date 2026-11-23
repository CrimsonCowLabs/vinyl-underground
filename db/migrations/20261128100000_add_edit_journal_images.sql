-- +goose Up
-- Cover writes (#164): the image a cover write or an embed replaced, and the one it wrote, as JSON
-- ({sum, mime, type, description}). The bytes are kept by their SHA-256 in the data folder's
-- edit-journal/images, so an undo puts the replaced image back byte for byte.
ALTER TABLE edit_journal ADD COLUMN original_image VARCHAR NOT NULL DEFAULT '{}';
ALTER TABLE edit_journal ADD COLUMN image VARCHAR NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE edit_journal DROP COLUMN image;
ALTER TABLE edit_journal DROP COLUMN original_image;
