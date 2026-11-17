package store

import (
	"context"
	"database/sql"

	"benevole-ape/internal/model"
)

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func listStands(ctx context.Context, q querier, eventID int64) ([]model.Stand, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, event_id, name, description, position, color FROM stands WHERE event_id = ? ORDER BY position, id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Stand
	for rows.Next() {
		var st model.Stand
		if err := rows.Scan(&st.ID, &st.EventID, &st.Name, &st.Description, &st.Position, &st.Color); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ListStands returns the stands of an event in display order.
func (s *Store) ListStands(ctx context.Context, eventID int64) ([]model.Stand, error) {
	return listStands(ctx, s.db, eventID)
}

func (s *Store) GetStand(ctx context.Context, eventID, id int64) (*model.Stand, error) {
	var st model.Stand
	err := s.db.QueryRowContext(ctx,
		`SELECT id, event_id, name, description, position, color FROM stands WHERE id = ? AND event_id = ?`, id, eventID).
		Scan(&st.ID, &st.EventID, &st.Name, &st.Description, &st.Position, &st.Color)
	if err != nil {
		return nil, notFound(err)
	}
	return &st, nil
}

// CreateStand appends a stand at the end of the event's list.
func (s *Store) CreateStand(ctx context.Context, st *model.Stand) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO stands (event_id, name, description, color, position)
		 VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM stands WHERE event_id = ?))`,
		st.EventID, st.Name, st.Description, st.Color, st.EventID)
	if err != nil {
		return err
	}
	st.ID, err = res.LastInsertId()
	return err
}

func (s *Store) UpdateStand(ctx context.Context, st *model.Stand) error {
	return affectedOne(s.db.ExecContext(ctx,
		`UPDATE stands SET name = ?, description = ?, color = ? WHERE id = ? AND event_id = ?`,
		st.Name, st.Description, st.Color, st.ID, st.EventID))
}

func (s *Store) DeleteStand(ctx context.Context, eventID, id int64) error {
	return affectedOne(s.db.ExecContext(ctx, `DELETE FROM stands WHERE id = ? AND event_id = ?`, id, eventID))
}

// MoveStand moves a stand one step up (delta=-1) or down (delta=+1).
func (s *Store) MoveStand(ctx context.Context, eventID, id int64, delta int) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		stands, err := listStands(ctx, tx, eventID)
		if err != nil {
			return err
		}
		i := -1
		for k, st := range stands {
			if st.ID == id {
				i = k
			}
		}
		if i < 0 {
			return ErrNotFound
		}
		j := i + delta
		if j < 0 || j >= len(stands) {
			return nil
		}
		stands[i], stands[j] = stands[j], stands[i]
		for k, st := range stands {
			if _, err := tx.ExecContext(ctx, `UPDATE stands SET position = ? WHERE id = ?`, k+1, st.ID); err != nil {
				return err
			}
		}
		return nil
	})
}
