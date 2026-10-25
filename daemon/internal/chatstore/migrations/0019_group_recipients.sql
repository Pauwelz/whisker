-- The Delivery State of a Message in a Group Chat is the lowest state that every Recipient reached.
-- track_recipients is 1 for a Message that whisker sent, or that another device of the user sent while whisker was linked.
ALTER TABLE messages ADD COLUMN track_recipients INTEGER NOT NULL DEFAULT 0;
-- recipients_set is 1 once the set of Recipients is frozen. An empty set is final: it has no row.
ALTER TABLE messages ADD COLUMN recipients_set INTEGER NOT NULL DEFAULT 0;
-- The Participants of the Group Chat when the send succeeded.
CREATE TABLE recipients (
	chat_jid TEXT NOT NULL,
	id TEXT NOT NULL,
	phone TEXT NOT NULL,
	lid TEXT NOT NULL,
	PRIMARY KEY (chat_jid, id, phone, lid)
);
-- Each state that a sender (a phone JID or a LID, no device part) reached. A receipt can come before the set.
CREATE TABLE group_receipts (
	chat_jid TEXT NOT NULL,
	id TEXT NOT NULL,
	sender TEXT NOT NULL,
	state TEXT NOT NULL,
	PRIMARY KEY (chat_jid, id, sender, state)
);
CREATE TRIGGER messages_delete_recipients AFTER DELETE ON messages BEGIN
	DELETE FROM recipients WHERE chat_jid = old.chat_jid AND id = old.id;
	DELETE FROM group_receipts WHERE chat_jid = old.chat_jid AND id = old.id;
END;
