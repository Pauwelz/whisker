-- pictures: the small Profile picture (JPEG) of a Chat, kept in the encrypted chat store.
-- An empty picture_id and NULL data mean the Chat has no picture, or WhatsApp hides it. fetched_at is Unix seconds.
CREATE TABLE pictures (
  jid        TEXT PRIMARY KEY,
  picture_id TEXT NOT NULL,
  data       BLOB,
  fetched_at INTEGER NOT NULL
);
