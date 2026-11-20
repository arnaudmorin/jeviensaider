-- When stands.fill_row is set, the planning colours the whole row of the
-- stand, not only the dot next to its name. The organiser chooses it for each
-- stand.
ALTER TABLE stands ADD COLUMN fill_row INTEGER NOT NULL DEFAULT 0;

-- When moments.fill is set, the planning tints the column of the moment with
-- its color, not only the dot next to its title. It needs a color to have an
-- effect.
ALTER TABLE moments ADD COLUMN fill INTEGER NOT NULL DEFAULT 0;

-- Before this setting, the planning tinted the column of every colored moment
-- that had no slot at its exact time range, and no other column. Set the fill
-- for these moments, so that existing events keep their look.
UPDATE moments SET fill = 1
WHERE color != ''
  AND NOT EXISTS (
    SELECT 1 FROM slots sl JOIN stands st ON st.id = sl.stand_id
    WHERE st.event_id = moments.event_id
      AND sl.starts_at = moments.starts_at
      AND sl.ends_at = moments.ends_at);
