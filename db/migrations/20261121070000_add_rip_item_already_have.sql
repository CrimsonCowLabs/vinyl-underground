-- +goose Up
-- The Tracks the Library or the Inbox already seemed to have for a Rip item when it was queued
-- ("already have: …", #32), as a JSON array of snapshots. Only a warning: the item is ripped anyway.
ALTER TABLE rip_item ADD COLUMN already_have VARCHAR NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE rip_item DROP COLUMN already_have;
