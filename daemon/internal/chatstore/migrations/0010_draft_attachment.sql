-- The staged file of a Draft: an absolute path, or empty for none.
ALTER TABLE drafts ADD COLUMN attachment TEXT NOT NULL DEFAULT '';
