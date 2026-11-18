package store

import (
	"context"
	"database/sql"
	"strings"

	"benevole-ape/internal/model"
)

const volunteerCols = `id, event_id, name, phone, edit_token, created_at`

func scanVolunteer(row interface{ Scan(...any) error }) (*model.Volunteer, error) {
	var v model.Volunteer
	var created string
	if err := row.Scan(&v.ID, &v.EventID, &v.Name, &v.Phone, &v.EditToken, &created); err != nil {
		return nil, notFound(err)
	}
	v.CreatedAt = parseTime(created)
	return &v, nil
}

// VolunteerByToken finds the volunteer owning an edit token for this event.
func (s *Store) VolunteerByToken(ctx context.Context, eventID int64, token string) (*model.Volunteer, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanVolunteer(s.db.QueryRowContext(ctx,
		`SELECT `+volunteerCols+` FROM volunteers WHERE edit_token = ? AND event_id = ?`, token, eventID))
}

// UpdateVolunteer saves the name and phone number.
func (s *Store) UpdateVolunteer(ctx context.Context, v *model.Volunteer) error {
	return affectedOne(s.db.ExecContext(ctx,
		`UPDATE volunteers SET name = ?, phone = ? WHERE id = ?`, v.Name, v.Phone, v.ID))
}

// RenewVolunteerToken gives v a new edit token; the old one stops working.
func (s *Store) RenewVolunteerToken(ctx context.Context, v *model.Volunteer) error {
	tok := Token()
	if err := affectedOne(s.db.ExecContext(ctx,
		`UPDATE volunteers SET edit_token = ? WHERE id = ?`, tok, v.ID)); err != nil {
		return err
	}
	v.EditToken = tok
	return nil
}

// SignupRequest is what a volunteer submits to take a slot.
type SignupRequest struct {
	EventID   int64
	SlotID    int64
	EditToken string // existing volunteer, may be empty
	Name      string
	Phone     string
	Comment   string
	// Admin sign-ups are allowed whatever the event status (an organiser
	// registering a parent who phoned in). Capacity still applies.
	Admin bool
}

// SignupResult reports what SignUp did.
type SignupResult struct {
	Volunteer    *model.Volunteer
	NewVolunteer bool
	Overlapping  []model.Slot // other slots of this volunteer that overlap
}

// SignUp registers a volunteer on a slot. Everything happens in one
// IMMEDIATE transaction so the capacity check cannot race.
func (s *Store) SignUp(ctx context.Context, req SignupRequest) (*SignupResult, error) {
	res := &SignupResult{}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM events WHERE id = ?`, req.EventID).Scan(&status); err != nil {
			return notFound(err)
		}
		if status != model.StatusOpen && !req.Admin {
			return ErrEventNotOpen
		}
		slot, err := getSlot(ctx, tx, req.EventID, req.SlotID)
		if err != nil {
			return err
		}

		var vol *model.Volunteer
		if req.EditToken != "" {
			vol, err = scanVolunteer(tx.QueryRowContext(ctx,
				`SELECT `+volunteerCols+` FROM volunteers WHERE edit_token = ? AND event_id = ?`, req.EditToken, req.EventID))
			if err != nil && err != ErrNotFound {
				return err
			}
			// The browser remembers someone else (a shared device, or a
			// personal link someone shared): sign up a new volunteer rather
			// than renaming that person and exposing their sign-ups.
			if vol != nil && req.Name != "" && !sameName(vol.Name, req.Name) {
				vol = nil
			}
		}
		if vol == nil {
			vol = &model.Volunteer{EventID: req.EventID, Name: req.Name, Phone: req.Phone, EditToken: Token()}
			r, err := tx.ExecContext(ctx,
				`INSERT INTO volunteers (event_id, name, phone, edit_token) VALUES (?, ?, ?, ?)`,
				vol.EventID, vol.Name, vol.Phone, vol.EditToken)
			if err != nil {
				return err
			}
			if vol.ID, err = r.LastInsertId(); err != nil {
				return err
			}
			res.NewVolunteer = true
		} else if req.Name != "" && (req.Name != vol.Name || req.Phone != vol.Phone) {
			// Same person (same name): keep their details up to date.
			vol.Name, vol.Phone = req.Name, req.Phone
			if _, err := tx.ExecContext(ctx, `UPDATE volunteers SET name = ?, phone = ? WHERE id = ?`,
				vol.Name, vol.Phone, vol.ID); err != nil {
				return err
			}
		}
		res.Volunteer = vol

		var already, count int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(volunteer_id = ?), 0), COUNT(*) FROM signups WHERE slot_id = ?`, vol.ID, slot.ID).
			Scan(&already, &count); err != nil {
			return err
		}
		if already > 0 {
			return ErrAlreadySigned
		}
		if slot.IsFull(count) {
			return ErrSlotFull
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO signups (slot_id, volunteer_id, comment) VALUES (?, ?, ?)`,
			slot.ID, vol.ID, req.Comment); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return ErrAlreadySigned
			}
			return err
		}

		mine, err := volunteerSlots(ctx, tx, vol.ID)
		if err != nil {
			return err
		}
		for _, o := range mine {
			if o.ID != slot.ID && o.Overlaps(slot) {
				res.Overlapping = append(res.Overlapping, o)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func volunteerSlots(ctx context.Context, q querier, volunteerID int64) ([]model.Slot, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+slotCols+` FROM slots sl JOIN signups su ON su.slot_id = sl.id WHERE su.volunteer_id = ? ORDER BY sl.starts_at`, volunteerID)
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

// CancelSignup removes one of the volunteer's own sign-ups.
// CancelSignup removes one of the volunteer's sign-ups and returns its slot.
func (s *Store) CancelSignup(ctx context.Context, volunteerID, signupID int64) (slotID int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`DELETE FROM signups WHERE id = ? AND volunteer_id = ? RETURNING slot_id`, signupID, volunteerID).Scan(&slotID)
	return slotID, notFound(err)
}

// AdminDeleteSignup removes any sign-up of the event.
func (s *Store) AdminDeleteSignup(ctx context.Context, eventID, signupID int64) error {
	return affectedOne(s.db.ExecContext(ctx,
		`DELETE FROM signups WHERE id = ? AND volunteer_id IN (SELECT id FROM volunteers WHERE event_id = ?)`, signupID, eventID))
}

// ListSignups returns all sign-ups of an event with volunteer details,
// in sign-up order.
func (s *Store) ListSignups(ctx context.Context, eventID int64) ([]model.SignupRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT su.id, su.slot_id, su.volunteer_id, su.comment, su.created_at, v.name, v.phone
		 FROM signups su JOIN volunteers v ON v.id = su.volunteer_id
		 WHERE v.event_id = ? ORDER BY su.created_at, su.id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SignupRow
	for rows.Next() {
		var r model.SignupRow
		var created string
		if err := rows.Scan(&r.ID, &r.SlotID, &r.VolunteerID, &r.Comment, &created, &r.Name, &r.Phone); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountVolunteers returns the number of distinct volunteers of an event.
func (s *Store) CountVolunteers(ctx context.Context, eventID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT su.volunteer_id) FROM signups su JOIN volunteers v ON v.id = su.volunteer_id WHERE v.event_id = ?`, eventID).Scan(&n)
	return n, err
}

// sameName compares two names ignoring case and spacing.
func sameName(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}
