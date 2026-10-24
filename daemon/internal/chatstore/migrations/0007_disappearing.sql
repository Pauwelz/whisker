-- expires_at: Unix seconds when a Disappearing Message goes, 0 for a Message that stays.
-- timer: the Disappearing timer of the Chat in seconds, 0 for off.
ALTER TABLE messages ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chats ADD COLUMN timer INTEGER NOT NULL DEFAULT 0;
CREATE INDEX messages_expires ON messages (expires_at) WHERE expires_at > 0;
