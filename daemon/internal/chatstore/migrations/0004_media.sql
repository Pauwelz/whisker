-- One row per Message that carries Media. The file itself is in the Profile media folder, encrypted with media_key.
-- state: none (not downloaded), downloading, ready or failed.
CREATE TABLE media (
  chat_jid        TEXT NOT NULL,
  id              TEXT NOT NULL,
  kind            TEXT NOT NULL,
  mime            TEXT NOT NULL DEFAULT '',
  name            TEXT NOT NULL DEFAULT '',
  size            INTEGER NOT NULL DEFAULT 0,
  media_key       BLOB NOT NULL,
  direct_path     TEXT NOT NULL DEFAULT '',
  file_sha256     BLOB,
  file_enc_sha256 BLOB,
  state           TEXT NOT NULL DEFAULT 'none',
  PRIMARY KEY (chat_jid, id)
);
