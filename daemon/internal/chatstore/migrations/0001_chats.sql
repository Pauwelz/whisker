-- last_active, muted_until are Unix seconds. muted_until: 0 not muted, -1 for ever.
-- unread is the Read Marker. Flags come from App State.
CREATE TABLE chats (
  jid         TEXT PRIMARY KEY,
  kind        TEXT NOT NULL DEFAULT 'direct',
  subject     TEXT NOT NULL DEFAULT '',
  last_active INTEGER NOT NULL DEFAULT 0,
  pinned      INTEGER NOT NULL DEFAULT 0,
  muted_until INTEGER NOT NULL DEFAULT 0,
  unread      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE messages (
  chat_jid   TEXT NOT NULL,
  id         TEXT NOT NULL,
  sender_jid TEXT NOT NULL,
  from_me    INTEGER NOT NULL,
  time       INTEGER NOT NULL,
  text       TEXT NOT NULL,
  PRIMARY KEY (chat_jid, id)
);
CREATE INDEX messages_by_time ON messages (chat_jid, time);

-- The two names that feed the name rule. An empty string means not set.
CREATE TABLE names (
  jid          TEXT PRIMARY KEY,
  contact_name TEXT NOT NULL DEFAULT '',
  push_name    TEXT NOT NULL DEFAULT ''
);
