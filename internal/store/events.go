package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"benevole-ape/internal/model"
)

const eventCols = `id, slug, title, description, location, date, admin_token, status, created_at`

func scanEvent(row interface{ Scan(...any) error }) (*model.Event, error) {
	var e model.Event
	var created string
	if err := row.Scan(&e.ID, &e.Slug, &e.Title, &e.Description, &e.Location, &e.Date, &e.AdminToken, &e.Status, &created); err != nil {
		return nil, notFound(err)
	}
	e.CreatedAt = parseTime(created)
	return &e, nil
}

// Slugify turns a title into a lowercase ASCII slug ("Fête de l'école" -> "fete-de-l-ecole").
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// drop accents
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 40 {
		out = strings.TrimRight(out[:40], "-")
	}
	if out == "" {
		out = "evenement"
	}
	return out
}

// CreateEvent inserts e, filling in ID, Slug, AdminToken and Status.
func (s *Store) CreateEvent(ctx context.Context, e *model.Event) error {
	e.Slug = Slugify(e.Title) + "-" + shortID()
	e.AdminToken = Token()
	if e.Status == "" {
		e.Status = model.StatusDraft
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO events (slug, title, description, location, date, admin_token, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Slug, e.Title, e.Description, e.Location, e.Date, e.AdminToken, e.Status)
	if err != nil {
		return err
	}
	e.ID, err = res.LastInsertId()
	e.CreatedAt = time.Now().UTC()
	return err
}

func (s *Store) EventBySlug(ctx context.Context, slug string) (*model.Event, error) {
	return scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE slug = ?`, slug))
}

func (s *Store) EventByAdminToken(ctx context.Context, token string) (*model.Event, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE admin_token = ?`, token))
}

// UpdateEvent saves the editable fields of e (title, description, location, date, status).
func (s *Store) UpdateEvent(ctx context.Context, e *model.Event) error {
	return affectedOne(s.db.ExecContext(ctx,
		`UPDATE events SET title = ?, description = ?, location = ?, date = ?, status = ? WHERE id = ?`,
		e.Title, e.Description, e.Location, e.Date, e.Status, e.ID))
}

func (s *Store) DeleteEvent(ctx context.Context, id int64) error {
	return affectedOne(s.db.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id))
}

// PurgeVolunteers deletes every volunteer (and so every sign-up) of an event.
func (s *Store) PurgeVolunteers(ctx context.Context, eventID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM volunteers WHERE event_id = ?`, eventID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DuplicateEvent copies the structure (stands and slots, no volunteers) of
// src into a new draft event. If both events have a date, slots are moved by
// the same number of days.
func (s *Store) DuplicateEvent(ctx context.Context, src *model.Event, title, date string) (*model.Event, error) {
	dst := &model.Event{
		Title: title, Date: date, Description: src.Description, Location: src.Location,
		Slug: Slugify(title) + "-" + shortID(), AdminToken: Token(), Status: model.StatusDraft,
	}
	var shift time.Duration
	if a, b := src.ParsedDate(), dst.ParsedDate(); !a.IsZero() && !b.IsZero() {
		shift = b.Sub(a)
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO events (slug, title, description, location, date, admin_token, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			dst.Slug, dst.Title, dst.Description, dst.Location, dst.Date, dst.AdminToken, dst.Status)
		if err != nil {
			return err
		}
		if dst.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		stands, err := listStands(ctx, tx, src.ID)
		if err != nil {
			return err
		}
		slots, err := listSlots(ctx, tx, src.ID)
		if err != nil {
			return err
		}
		newID := map[int64]int64{}
		for _, st := range stands {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO stands (event_id, name, description, position, color) VALUES (?, ?, ?, ?, ?)`,
				dst.ID, st.Name, st.Description, st.Position, st.Color)
			if err != nil {
				return err
			}
			if newID[st.ID], err = res.LastInsertId(); err != nil {
				return err
			}
		}
		for _, sl := range slots {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO slots (stand_id, starts_at, ends_at, min_people, max_people) VALUES (?, ?, ?, ?, ?)`,
				newID[sl.StandID], fmtTime(sl.StartsAt.Add(shift)), fmtTime(sl.EndsAt.Add(shift)), sl.MinPeople, nullInt(sl.MaxPeople)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dst, nil
}
