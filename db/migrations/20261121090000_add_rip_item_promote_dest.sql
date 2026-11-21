-- +goose Up
-- Where a Promote (#36) is moving the item's File, as an absolute path, set before the File is
-- copied there and cleared once it is done or undone. A restart finds the moves it interrupted
-- by it, and finishes or undoes each one.
ALTER TABLE rip_item ADD COLUMN promote_dest VARCHAR NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE rip_item DROP COLUMN promote_dest;
