-- The device ids of the user's own account, as WhatsApp listed them at the last connection.
-- A device id that is not here is new. Only ids are stored.
CREATE TABLE IF NOT EXISTS own_devices (device INTEGER NOT NULL PRIMARY KEY);
