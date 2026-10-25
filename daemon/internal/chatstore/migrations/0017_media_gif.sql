-- gif is 1 for a video that WhatsApp plays as a looping GIF.
ALTER TABLE media ADD COLUMN gif INTEGER NOT NULL DEFAULT 0;
