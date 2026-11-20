CREATE TABLE moments (
    id        INTEGER PRIMARY KEY,
    event_id  INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    title     TEXT NOT NULL,
    starts_at TEXT NOT NULL,
    ends_at   TEXT NOT NULL,
    -- Empty (none) or "#rrggbb".
    color     TEXT NOT NULL DEFAULT '',
    CHECK (ends_at > starts_at),
    UNIQUE (event_id, starts_at, ends_at)
);
