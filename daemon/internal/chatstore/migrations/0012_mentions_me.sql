-- mentions_me is 1 for a Message that mentions the user. The Chat list marks a Chat with an unread one.
ALTER TABLE messages ADD COLUMN mentions_me INTEGER NOT NULL DEFAULT 0;
