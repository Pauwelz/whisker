-- JIDs that the text of a Message mentions as "@<user part>", separated by spaces.
ALTER TABLE messages ADD COLUMN mentions TEXT NOT NULL DEFAULT '';
