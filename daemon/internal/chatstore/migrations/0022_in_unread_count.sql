-- in_unread_count is 1 for a live Message of another Participant that counts in the unreadCount of its Chat.
-- It clears with the Read Marker. read_pending stays the "waits for a Read Receipt" flag.
ALTER TABLE messages ADD COLUMN in_unread_count INTEGER NOT NULL DEFAULT 0;
UPDATE messages SET in_unread_count = 1 WHERE read_pending = 1 AND chat_jid IN (SELECT jid FROM chats WHERE unread = 1);
