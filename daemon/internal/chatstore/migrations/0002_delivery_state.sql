-- state is the Delivery State of the user's own Messages: pending, sent, delivered, read, played or failed.
-- A Message of another Participant has an empty state.
ALTER TABLE messages ADD COLUMN state TEXT NOT NULL DEFAULT '';
UPDATE messages SET state = 'sent' WHERE from_me = 1;
