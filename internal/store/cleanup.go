package store

import (
	"context"
	"time"

	"benevole-ape/internal/model"
)

// lastDayExpr is the last day ("YYYY-MM-DD") an event is relevant: the latest
// of its date, the end of its last slot and its creation date. It expects the
// events table to be aliased as e.
const lastDayExpr = `substr(max(
	e.created_at,
	e.date,
	COALESCE((SELECT max(sl.ends_at) FROM slots sl JOIN stands st ON st.id = sl.stand_id WHERE st.event_id = e.id), '')
), 1, 10)`

// EventLastDay returns the last day of activity of an event (see lastDayExpr).
func (s *Store) EventLastDay(ctx context.Context, eventID int64) (time.Time, error) {
	var day string
	if err := s.db.QueryRowContext(ctx, `SELECT `+lastDayExpr+` FROM events e WHERE e.id = ?`, eventID).Scan(&day); err != nil {
		return time.Time{}, notFound(err)
	}
	return time.ParseInLocation(model.DateLayout, day, time.Local)
}

// ExpiresOn returns the first day on which Cleanup deletes an event whose last
// day of activity is lastDay.
func ExpiresOn(lastDay time.Time, retentionDays int) time.Time {
	return lastDay.AddDate(0, 0, retentionDays+1)
}

// Cleanup deletes, with all their data, the events whose last day of activity
// is more than retentionDays days before now. It returns how many were deleted.
func (s *Store) Cleanup(ctx context.Context, retentionDays int, now time.Time) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := now.AddDate(0, 0, -retentionDays).Format(model.DateLayout)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM events WHERE id IN (SELECT e.id FROM events e WHERE `+lastDayExpr+` < ?)`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
