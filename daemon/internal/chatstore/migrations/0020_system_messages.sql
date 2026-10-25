-- A System Message records a change to a Chat. The actor is sender_jid (from_me for the user).
-- system_kind is empty for any other Message. system_targets is a JSON array of Participant JIDs, "" for the user.
-- system_value is the new subject, the timer in seconds, or "deleted". The Daemon builds the text when it reads the Message.
ALTER TABLE messages ADD COLUMN system_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN system_targets TEXT NOT NULL DEFAULT '[]';
ALTER TABLE messages ADD COLUMN system_value TEXT NOT NULL DEFAULT '';
