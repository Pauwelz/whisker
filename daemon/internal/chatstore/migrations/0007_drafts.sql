-- One Draft per Chat: the unsent text and a pending Quote or Edit target (a Message id, or empty).
-- A Chat with an empty Draft has no row.
CREATE TABLE drafts (
  chat_jid TEXT NOT NULL PRIMARY KEY,
  text     TEXT NOT NULL,
  quote_id TEXT NOT NULL,
  edit_id  TEXT NOT NULL
);
