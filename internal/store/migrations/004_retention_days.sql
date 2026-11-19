-- Days that the event is kept after its last day of activity. NULL means that
-- the server default applies. The value 0 is refused: it would mean "keep
-- forever" and organisers must not be able to choose that. The upper bound is
-- store.CeilingRetentionDays (ten years).
ALTER TABLE events ADD COLUMN retention_days INTEGER CHECK (retention_days IS NULL OR retention_days BETWEEN 1 AND 3650);
