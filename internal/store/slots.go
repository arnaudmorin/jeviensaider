package store

import (
	"context"
	"database/sql"

	"benevole-ape/internal/model"
)

const slotCols = `sl.id, sl.stand_id, sl.starts_at, sl.ends_at, sl.min_people, COALESCE(sl.max_people, 0)`

func scanSlot(row interface{ Scan(...any) error }) (model.Slot, error) {
	var sl model.Slot
	var start, end string
	err := row.Scan(&sl.ID, &sl.StandID, &start, &end, &sl.MinPeople, &sl.MaxPeople)
	sl.StartsAt, sl.EndsAt = parseTime(start), parseTime(end)
	return sl, err
}

// querySlots runs a query selecting slotCols.
func querySlots(ctx context.Context, q querier, query string, args ...any) ([]model.Slot, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Slot
	for rows.Next() {
		sl, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

func listSlots(ctx context.Context, q querier, eventID int64) ([]model.Slot, error) {
	return querySlots(ctx, q, `SELECT `+slotCols+` FROM slots sl JOIN stands st ON st.id = sl.stand_id
		 WHERE st.event_id = ? ORDER BY sl.starts_at, sl.ends_at, st.position`, eventID)
}

// ListSlots returns every slot of an event, chronologically.
func (s *Store) ListSlots(ctx context.Context, eventID int64) ([]model.Slot, error) {
	return listSlots(ctx, s.db, eventID)
}

func getSlot(ctx context.Context, q querier, eventID, id int64) (model.Slot, error) {
	sl, err := scanSlot(q.QueryRowContext(ctx,
		`SELECT `+slotCols+` FROM slots sl JOIN stands st ON st.id = sl.stand_id WHERE sl.id = ? AND st.event_id = ?`, id, eventID))
	return sl, notFound(err)
}

// GetSlot returns a slot, checking it belongs to the event.
func (s *Store) GetSlot(ctx context.Context, eventID, id int64) (model.Slot, error) {
	return getSlot(ctx, s.db, eventID, id)
}

// CreateSlots inserts slots for a stand of the event.
func (s *Store) CreateSlots(ctx context.Context, eventID, standID int64, slots []model.Slot) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM stands WHERE id = ? AND event_id = ?`, standID, eventID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		var total int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM slots sl JOIN stands st ON st.id = sl.stand_id WHERE st.event_id = ?`, eventID).Scan(&total); err != nil {
			return err
		}
		if total+len(slots) > MaxSlots {
			return ErrTooManySlots
		}
		for _, sl := range slots {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO slots (stand_id, starts_at, ends_at, min_people, max_people) VALUES (?, ?, ?, ?, ?)`,
				standID, fmtTime(sl.StartsAt), fmtTime(sl.EndsAt), sl.MinPeople, nullInt(sl.MaxPeople)); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateSlot saves times and capacities of a slot of the event.
func (s *Store) UpdateSlot(ctx context.Context, eventID int64, sl model.Slot) error {
	return affectedOne(s.db.ExecContext(ctx,
		`UPDATE slots SET starts_at = ?, ends_at = ?, min_people = ?, max_people = ?
		 WHERE id = ? AND stand_id IN (SELECT id FROM stands WHERE event_id = ?)`,
		fmtTime(sl.StartsAt), fmtTime(sl.EndsAt), sl.MinPeople, nullInt(sl.MaxPeople), sl.ID, eventID))
}

// DeleteSlot deletes a slot of the event with its sign-ups.
func (s *Store) DeleteSlot(ctx context.Context, eventID, id int64) error {
	return affectedOne(s.db.ExecContext(ctx,
		`DELETE FROM slots WHERE id = ? AND stand_id IN (SELECT id FROM stands WHERE event_id = ?)`, id, eventID))
}
