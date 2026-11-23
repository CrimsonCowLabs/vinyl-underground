-- +goose Up
-- Promote through Organize (#171). A Promote is now a journaled move from the Inbox (a library of
-- its own) into the Library, so the move changes the File's library as well as its path:
-- new_library_id is the library new_path is in, or 0 when it is the entry's own library (every
-- other write).
ALTER TABLE edit_journal ADD COLUMN new_library_id INTEGER NOT NULL DEFAULT 0;

-- The old Promote (#36) recorded where it was copying an item's File, to finish or undo the move
-- at the next start. The Edit journal and its startup settle do that now, so the column goes.
-- A destination still set belongs to an old Promote interrupted and not resumed since (resumed
-- at every start with Promote on). Dropping it never loses a File: the old Promote deleted the
-- Inbox copy only once the Track pointed at the copy in the Library, so the File is in the Inbox,
-- or in the Library, or (at worst) in both, where the next scan shows each as a Track.
ALTER TABLE rip_item DROP COLUMN promote_dest;

-- +goose Down
ALTER TABLE rip_item ADD COLUMN promote_dest VARCHAR NOT NULL DEFAULT '';
ALTER TABLE edit_journal DROP COLUMN new_library_id;
