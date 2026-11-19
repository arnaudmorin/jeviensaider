package store

import (
	"context"
	"time"

	"jeviensaider/internal/model"
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
// is more than their number of days before now. Each event has its own number
// of days (see Retention.Days). Cleanup returns how many events it deleted.
//
// The date of now is passed as a parameter, not read by SQLite, because
// SQLite uses UTC and the dates of events are local dates.
func (s *Store) Cleanup(ctx context.Context, r Retention, now time.Time) (int64, error) {
	if !r.Enabled() {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM events WHERE id IN (SELECT e.id FROM events e
			WHERE date(`+lastDayExpr+`, '+' || min(COALESCE(e.retention_days, ?), ?) || ' days') < ?)`,
		r.DefaultDays, r.Max(), now.Format(model.DateLayout))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
