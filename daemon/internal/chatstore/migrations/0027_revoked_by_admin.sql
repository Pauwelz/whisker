-- revoked_by_admin: a group admin revoked the Message. The Tombstone then says so.
ALTER TABLE messages ADD COLUMN revoked_by_admin INTEGER NOT NULL DEFAULT 0;
