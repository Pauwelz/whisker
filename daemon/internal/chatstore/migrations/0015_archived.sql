-- archived: the Chat is archived (live_flags bit 16 once a live event set it).
ALTER TABLE chats ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
-- App State settings of the user, one row each. unarchive_chats is 1 when a new Message unarchives its Chat.
-- No row means the WhatsApp default: keep the Chat archived.
CREATE TABLE app_state_settings (
  name  TEXT NOT NULL PRIMARY KEY,
  value INTEGER NOT NULL
);
