-- A reply keeps a snapshot of the quoted Message: its id, its author (empty for the user) and its text.
ALTER TABLE messages ADD COLUMN quote_id TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN quote_sender TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN quote_text TEXT NOT NULL DEFAULT '';
-- One Reaction per Participant per Message. sender_jid is empty for the user.
CREATE TABLE reactions (
  chat_jid   TEXT NOT NULL,
  message_id TEXT NOT NULL,
  sender_jid TEXT NOT NULL,
  emoji      TEXT NOT NULL,
  PRIMARY KEY (chat_jid, message_id, sender_jid)
);
