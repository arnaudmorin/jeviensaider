-- Events have no draft status any more: a new event starts open. The drafts
-- that remain are opened. The CHECK of events.status still accepts 'draft',
-- because SQLite cannot change it without rebuilding the table; the app never
-- writes it.
UPDATE events SET status = 'open' WHERE status = 'draft';
