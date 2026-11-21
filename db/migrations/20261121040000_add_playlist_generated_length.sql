-- +goose Up
-- How many Tracks the Listener asked for when a Generated Playlist was made, so it can be
-- regenerated at the same length. 0 for every other Playlist.
alter table playlist add generated_length integer default 0 not null;

-- Generated Playlists saved before this column existed: their Track count, kept within the
-- 10 to 100 a Prompt accepts, is the best guess.
update playlist set generated_length = min(max(song_count, 10), 100) where prompt != '';

-- +goose Down
alter table playlist drop column generated_length;
