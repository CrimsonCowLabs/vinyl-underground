-- +goose Up
-- Tag proposals (#34). origin is what the site said about the item's video when it was ripped
-- (its title, uploader, length, and YouTube Music's artist, track, album and year for "Topic"
-- uploads), the starting point of the proposal. proposal is the proposed tags, worked out in the
-- background once the File has landed. Both are JSON objects; a proposal is never written to the
-- File until the admin saves the review card.
ALTER TABLE rip_item ADD COLUMN origin VARCHAR NOT NULL DEFAULT '{}';
ALTER TABLE rip_item ADD COLUMN proposal VARCHAR NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE rip_item DROP COLUMN proposal;
ALTER TABLE rip_item DROP COLUMN origin;
