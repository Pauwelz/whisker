-- duration is the length in seconds that WhatsApp sent with audio, voice and video Media. 0 when it sent none.
-- played is 1 once the user played a voice note in whisker.
ALTER TABLE media ADD COLUMN duration INTEGER NOT NULL DEFAULT 0;
ALTER TABLE media ADD COLUMN played INTEGER NOT NULL DEFAULT 0;
