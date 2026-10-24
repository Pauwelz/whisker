-- read_pending is 1 for a Message of another Participant that arrived live and has no Read Receipt sent yet.
-- History Sync Messages and the user's own Messages stay 0.
ALTER TABLE messages ADD COLUMN read_pending INTEGER NOT NULL DEFAULT 0;
