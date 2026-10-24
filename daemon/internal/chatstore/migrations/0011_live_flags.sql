-- live_flags: a bit for each Chat flag that a live event set (1 pin, 2 mute, 4 unread, 8 timer).
-- History Sync fills a flag only while its bit is 0.
ALTER TABLE chats ADD COLUMN live_flags INTEGER NOT NULL DEFAULT 0;
