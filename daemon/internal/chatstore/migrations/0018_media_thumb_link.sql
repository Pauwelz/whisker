-- The link to the thumbnail that WhatsApp keeps apart from the Message, for Media that came without an inline thumbnail.
ALTER TABLE media ADD COLUMN thumb_direct_path TEXT NOT NULL DEFAULT '';
ALTER TABLE media ADD COLUMN thumb_sha256 BLOB;
ALTER TABLE media ADD COLUMN thumb_enc_sha256 BLOB;
