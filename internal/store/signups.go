package store

import (
	"context"
	"database/sql"
	"strings"

	"jeviensaider/internal/model"
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
	return volunteerByToken(ctx, s.db, eventID, token)
}

func volunteerByToken(ctx context.Context, q querier, eventID int64, token string) (*model.Volunteer, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanVolunteer(q.QueryRowContext(ctx,
		`SELECT `+volunteerCols+` FROM volunteers WHERE edit_token = ? AND event_id = ?`, token, eventID))
}

// UpdateVolunteer saves the name and phone number.
func (s *Store) UpdateVolunteer(ctx context.Context, v *model.Volunteer) error {
	return updateVolunteer(ctx, s.db, v)
}

func updateVolunteer(ctx context.Context, q querier, v *model.Volunteer) error {
	return affectedOne(q.ExecContext(ctx,
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
	// registering a volunteer who phoned in). Capacity still applies.
	Admin bool
}

// SignupResult reports what SignUp did.
type SignupResult struct {
	Slot         model.Slot
	Volunteer    *model.Volunteer
	NewVolunteer bool
	Overlapping  []model.Slot // other slots of this volunteer that overlap
}

// SignUp registers a volunteer on a slot. Everything happens in one
// IMMEDIATE transaction so the capacity check cannot race.
func (s *Store) SignUp(ctx context.Context, req SignupRequest) (*SignupResult, error) {
	res := &SignupResult{}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkOpen(ctx, tx, req); err != nil {
			return err
		}
		var err error
		if res.Slot, err = getSlot(ctx, tx, req.EventID, req.SlotID); err != nil {
			return err
		}
		if req, err = resolveAdminVolunteer(ctx, tx, req); err != nil {
			return err
		}
		if res.Volunteer, res.NewVolunteer, err = findOrCreateVolunteer(ctx, tx, req); err != nil {
			return err
		}
		if err := addSignup(ctx, tx, res.Slot, res.Volunteer.ID, req.Comment); err != nil {
			return err
		}
		res.Overlapping, err = overlapping(ctx, tx, res.Volunteer.ID, res.Slot)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// checkOpen refuses sign-ups on an event that is not open, except those made
// by the organiser.
func checkOpen(ctx context.Context, q querier, req SignupRequest) error {
	var status string
	if err := q.QueryRowContext(ctx, `SELECT status FROM events WHERE id = ?`, req.EventID).Scan(&status); err != nil {
		return notFound(err)
	}
	if status != model.StatusOpen && !req.Admin {
		return ErrEventNotOpen
	}
	return nil
}

// findOrCreateVolunteer returns the volunteer of req.EditToken, with their
// details updated, or a new volunteer. It reports whether it created one.
func findOrCreateVolunteer(ctx context.Context, q querier, req SignupRequest) (*model.Volunteer, bool, error) {
	vol, err := volunteerByToken(ctx, q, req.EventID, req.EditToken)
	if err != nil && err != ErrNotFound {
		return nil, false, err
	}
	// The browser remembers someone else (a shared device, or a personal link
	// someone shared): sign up a new volunteer rather than renaming that
	// person and exposing their sign-ups.
	if vol != nil && req.Name != "" && !sameName(vol.Name, req.Name) {
		vol = nil
	}
	if vol == nil {
		vol = &model.Volunteer{EventID: req.EventID, Name: req.Name, Phone: req.Phone, EditToken: Token()}
		r, err := q.ExecContext(ctx,
			`INSERT INTO volunteers (event_id, name, phone, edit_token) VALUES (?, ?, ?, ?)`,
			vol.EventID, vol.Name, vol.Phone, vol.EditToken)
		if err != nil {
			return nil, false, err
		}
		vol.ID, err = r.LastInsertId()
		return vol, true, err
	}
	// Same person (same name): keep their details up to date.
	if req.Name != "" && (req.Name != vol.Name || req.Phone != vol.Phone) {
		vol.Name, vol.Phone = req.Name, req.Phone
		if err := updateVolunteer(ctx, q, vol); err != nil {
			return nil, false, err
		}
	}
	return vol, false, nil
}

// addSignup puts a volunteer on a slot, unless they already are or the slot
// is full.
func addSignup(ctx context.Context, q querier, slot model.Slot, volunteerID int64, comment string) error {
	var already, count int
	if err := q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(volunteer_id = ?), 0), COUNT(*) FROM signups WHERE slot_id = ?`, volunteerID, slot.ID).
		Scan(&already, &count); err != nil {
		return err
	}
	if already > 0 {
		return ErrAlreadySigned
	}
	if slot.IsFull(count) {
		return ErrSlotFull
	}
	_, err := q.ExecContext(ctx, `INSERT INTO signups (slot_id, volunteer_id, comment) VALUES (?, ?, ?)`,
		slot.ID, volunteerID, comment)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrAlreadySigned
	}
	return err
}

// overlapping returns the volunteer's other slots that overlap slot.
func overlapping(ctx context.Context, q querier, volunteerID int64, slot model.Slot) ([]model.Slot, error) {
	mine, err := volunteerSlots(ctx, q, volunteerID)
	if err != nil {
		return nil, err
	}
	var out []model.Slot
	for _, o := range mine {
		if o.ID != slot.ID && o.Overlaps(slot) {
			out = append(out, o)
		}
	}
	return out, nil
}

func volunteerSlots(ctx context.Context, q querier, volunteerID int64) ([]model.Slot, error) {
	return querySlots(ctx, q,
		`SELECT `+slotCols+` FROM slots sl JOIN signups su ON su.slot_id = sl.id WHERE su.volunteer_id = ? ORDER BY sl.starts_at`, volunteerID)
}

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

// CountVolunteers returns the number of volunteers of an event (the people
// whose name and phone are stored).
func (s *Store) CountVolunteers(ctx context.Context, eventID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM volunteers WHERE event_id = ?`, eventID).Scan(&n)
	return n, err
}

// sameName compares two names ignoring case and spacing.
func sameName(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

// phoneDigits keeps the digits of a phone number, so that "06 11 22 33 44" and
// "0611223344" are the same number.
func phoneDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// pickVolunteer chooses, among volunteers with the same name (oldest first),
// the one that the organizer means. With a phone number, it takes the person
// with that number, else a person with no number yet. A person with another
// number is somebody else with the same name. Without a phone number, nothing
// tells them apart, so it takes the oldest. It returns nil if nobody fits.
func pickVolunteer(sameNamed []*model.Volunteer, phone string) *model.Volunteer {
	digits := phoneDigits(phone)
	if digits == "" {
		if len(sameNamed) > 0 {
			return sameNamed[0]
		}
		return nil
	}
	var blank *model.Volunteer
	for _, v := range sameNamed {
		switch have := phoneDigits(v.Phone); {
		case have == digits:
			return v
		case have == "" && blank == nil:
			blank = v
		}
	}
	return blank
}

// resolveAdminVolunteer completes a request of the organizer, who has no token
// for the person. If the event already has this person, it sets their token,
// so that one person has one volunteer record whatever the organizer adds: a
// slot or a pledge. What the organizer typed never changes that person: the
// name keeps its spelling, and the phone number is stored only if the person
// had none. Other requests come back unchanged.
func resolveAdminVolunteer(ctx context.Context, q querier, req SignupRequest) (SignupRequest, error) {
	if !req.Admin || req.EditToken != "" || req.Name == "" {
		return req, nil
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+volunteerCols+` FROM volunteers WHERE event_id = ? ORDER BY id`, req.EventID)
	if err != nil {
		return req, err
	}
	defer rows.Close()
	var sameNamed []*model.Volunteer
	for rows.Next() {
		v, err := scanVolunteer(rows)
		if err != nil {
			return req, err
		}
		if sameName(v.Name, req.Name) {
			sameNamed = append(sameNamed, v)
		}
	}
	if err := rows.Err(); err != nil {
		return req, err
	}
	if known := pickVolunteer(sameNamed, req.Phone); known != nil {
		req.EditToken, req.Name = known.EditToken, known.Name
		if known.Phone != "" {
			req.Phone = known.Phone
		}
	}
	return req, nil
}
