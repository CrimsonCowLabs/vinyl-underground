-- +goose Up
-- How a job's Files reached the Inbox (#35): 'rip' (yt-dlp), 'upload' (the web app) or 'drop'
-- (copied straight into the Inbox folder). Every job before this was a Rip. The ripping worker
-- only ever picks up Rips; uploads and drops are ready for review from the start.
ALTER TABLE rip_job ADD COLUMN kind VARCHAR NOT NULL DEFAULT 'rip';

-- +goose Down
ALTER TABLE rip_job DROP COLUMN kind;
