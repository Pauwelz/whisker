-- thumbnail is the small JPEG that WhatsApp sent with image, video and document Media. Empty when none.
ALTER TABLE media ADD COLUMN thumbnail BLOB NOT NULL DEFAULT x'';
