-- Volunteers are no longer asked for an e-mail: drop the stored addresses.
ALTER TABLE volunteers DROP COLUMN email;
