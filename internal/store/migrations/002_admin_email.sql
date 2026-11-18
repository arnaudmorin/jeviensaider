-- Organiser's e-mail: receives the admin link when the event is created and,
-- if notify_signups is set, a message for each sign-up or cancellation.
ALTER TABLE events ADD COLUMN admin_email TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN notify_signups INTEGER NOT NULL DEFAULT 1;
