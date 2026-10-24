-- edited: the text was changed after it was sent. revoked: the Message is a Tombstone, its text is empty.
ALTER TABLE messages ADD COLUMN edited INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN revoked INTEGER NOT NULL DEFAULT 0;
