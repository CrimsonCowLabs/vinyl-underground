-- +goose Up
-- Album cover choices (#126): the image a Correction makes the album's cover, named by its hash and
-- mime type in the chosen-cover store (DataFolder/artwork/chosen). Empty for other kinds.
ALTER TABLE correction ADD COLUMN image_hash VARCHAR NOT NULL DEFAULT '';
ALTER TABLE correction ADD COLUMN image_mime VARCHAR NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE correction DROP COLUMN image_mime;
ALTER TABLE correction DROP COLUMN image_hash;
