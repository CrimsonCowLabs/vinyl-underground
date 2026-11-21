-- +goose Up
-- The Prompt a Generated Playlist was made from. Empty for every other Playlist.
alter table playlist add prompt varchar default '' not null;

-- +goose Down
alter table playlist drop column prompt;
