-- A Draft holds a list of Attachments: a JSON array of absolute paths, in order. The old single path becomes a one-item list.
ALTER TABLE drafts ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]';
UPDATE drafts SET attachments = json_array(attachment) WHERE attachment <> '';
ALTER TABLE drafts DROP COLUMN attachment;
