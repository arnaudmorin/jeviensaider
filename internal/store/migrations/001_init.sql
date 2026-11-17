CREATE TABLE events (
    id          INTEGER PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location    TEXT NOT NULL DEFAULT '',
    date        TEXT NOT NULL DEFAULT '',
    admin_token TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'open', 'closed')),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now'))
);

CREATE TABLE stands (
    id          INTEGER PRIMARY KEY,
    event_id    INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    color       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX stands_event ON stands(event_id, position);

CREATE TABLE slots (
    id         INTEGER PRIMARY KEY,
    stand_id   INTEGER NOT NULL REFERENCES stands(id) ON DELETE CASCADE,
    starts_at  TEXT NOT NULL,
    ends_at    TEXT NOT NULL,
    min_people INTEGER NOT NULL DEFAULT 1 CHECK (min_people >= 1),
    max_people INTEGER CHECK (max_people IS NULL OR max_people >= min_people),
    CHECK (ends_at > starts_at)
);
CREATE INDEX slots_stand ON slots(stand_id, starts_at);

CREATE TABLE volunteers (
    id         INTEGER PRIMARY KEY,
    event_id   INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    phone      TEXT NOT NULL DEFAULT '',
    edit_token TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now'))
);
CREATE INDEX volunteers_event ON volunteers(event_id);

CREATE TABLE signups (
    id           INTEGER PRIMARY KEY,
    slot_id      INTEGER NOT NULL REFERENCES slots(id) ON DELETE CASCADE,
    volunteer_id INTEGER NOT NULL REFERENCES volunteers(id) ON DELETE CASCADE,
    comment      TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    UNIQUE (slot_id, volunteer_id)
);
CREATE INDEX signups_volunteer ON signups(volunteer_id);
