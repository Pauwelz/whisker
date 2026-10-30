-- Sent Messages, kept so the Daemon can answer a retry receipt after a restart. whatsmeow writes and reads the rows.
-- Whisker deletes a row with its Message, which whatsmeow's own table cannot do. There is no foreign key:
-- an edit, a reaction or a revoke has an id that is not in messages.
CREATE TABLE retry_messages (
	-- chat_jid is the JID that whatsmeow sent to: a phone-number JID or a LID. It can differ from messages.chat_jid.
	chat_jid TEXT NOT NULL,
	id TEXT NOT NULL,
	-- target_id is the id that an edit, a revoke or a reaction acts on. NULL for any other Message.
	target_id TEXT,
	-- format is "wa" or "fb": the proto type of data.
	format TEXT NOT NULL,
	-- data is the marshalled Message proto.
	data BLOB NOT NULL,
	-- timestamp is the store time in Unix milliseconds. The age limit uses it.
	timestamp INTEGER NOT NULL,
	PRIMARY KEY (chat_jid, id)
);
CREATE INDEX retry_messages_id ON retry_messages (id);
CREATE INDEX retry_messages_target ON retry_messages (target_id);
