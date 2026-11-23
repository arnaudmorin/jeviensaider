package store

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"jeviensaider/internal/model"
)

// A database created before migration 003 loses the volunteers' e-mails,
// and keeps everything else.
func TestDropVolunteerEmail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"migrations/001_init.sql", "migrations/002_admin_email.sql"} {
		body, _ := migrationsFS.ReadFile(name)
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 2;
		INSERT INTO events (id, slug, title, admin_token) VALUES (1, 'fete', 'Fête', 'tok');
		INSERT INTO volunteers (event_id, name, email, phone, edit_token) VALUES (1, 'Marie', 'marie@example.org', '0601020304', 'edit');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('volunteers') WHERE name = 'email'`).Scan(&n)
	if n != 0 {
		t.Error("volunteers.email still exists")
	}
	v, err := s.VolunteerByToken(ctx, 1, "edit")
	if err != nil || v.Name != "Marie" || v.Phone != "0601020304" {
		t.Errorf("volunteer after migration: %+v %v", v, err)
	}
}

// A database from before the moments (migrations 001 to 004, with an event)
// gets the moments table, with its color column, and keeps its events.
func TestUpgradeToMoments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_admin_email.sql", "003_drop_volunteer_email.sql", "004_retention_days.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 4;
		INSERT INTO events (id, slug, title, admin_token, date) VALUES (1, 'fete', 'Fête', 'tok', '2026-06-20');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if ev, err := s.EventBySlug(ctx, "fete"); err != nil || ev.Title != "Fête" {
		t.Fatalf("event after the upgrade: %+v, %v", ev, err)
	}
	m := model.Moment{EventID: 1, Title: "Spectacle", Color: "#e8c93a", StartsAt: at("2027-01-15 10:00"), EndsAt: at("2027-01-15 11:00")}
	if err := s.CreateMoment(ctx, &m); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListMoments(ctx, 1)
	if err != nil || len(list) != 1 || list[0].Color != "#e8c93a" {
		t.Fatalf("moments after the upgrade: %+v, %v", list, err)
	}
	if got, err := s.EventLastDay(ctx, 1); err != nil || !got.Equal(day("2027-01-15")) {
		t.Errorf("EventLastDay = %v, %v; want 2027-01-15", got, err)
	}
	// migrate() numbers the files by their place in the sorted list, so the
	// version is the number of files.
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var version int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != len(files) {
		t.Errorf("user_version = %d, want %d (the number of migrations)", version, len(files))
	}
}

// A database from before the fill setting keeps the look of its planning: a
// colored moment with no slot at its time range is still tinted, and the other
// moments are not.
func TestUpgradeToFill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_admin_email.sql", "003_drop_volunteer_email.sql", "004_retention_days.sql", "005_moments.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 5;
		INSERT INTO events (id, slug, title, admin_token, date) VALUES (1, 'fete', 'Fête', 'tok', '2026-06-20');
		INSERT INTO stands (id, event_id, name) VALUES (1, 1, 'Pêche');
		INSERT INTO slots (stand_id, starts_at, ends_at) VALUES (1, '2026-06-20 10:00', '2026-06-20 11:00');
		INSERT INTO moments (id, event_id, title, color, starts_at, ends_at) VALUES
			(1, 1, 'With a slot', '#e8c93a', '2026-06-20 10:00', '2026-06-20 11:00'),
			(2, 1, 'No slot', '#e8c93a', '2026-06-20 11:00', '2026-06-20 12:00'),
			(3, 1, 'No color', '', '2026-06-20 13:00', '2026-06-20 14:00');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for id, want := range map[int64]bool{1: false, 2: true, 3: false} {
		m, err := s.GetMoment(ctx, 1, id)
		if err != nil || m.Fill != want {
			t.Errorf("moment %d: fill = %v, want %v (%v)", id, m.Fill, want, err)
		}
	}
}

// Events have no draft status any more: migration 008 opens the drafts and
// leaves the other events as they are.
func TestUpgradeOpensDrafts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	names, _ := fs.Glob(migrationsFS, "migrations/00[1-7]_*.sql")
	for _, name := range names {
		body, _ := migrationsFS.ReadFile(name)
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 7;
		INSERT INTO events (id, slug, title, admin_token, status) VALUES
			(1, 'draft', 'Draft', 'tok1', 'draft'),
			(2, 'open', 'Open', 'tok2', 'open'),
			(3, 'closed', 'Closed', 'tok3', 'closed');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for slug, want := range map[string]string{"draft": model.StatusOpen, "open": model.StatusOpen, "closed": model.StatusClosed} {
		var got string
		s.db.QueryRow(`SELECT status FROM events WHERE slug = ?`, slug).Scan(&got)
		if got != want {
			t.Errorf("%s: status %q, want %q", slug, got, want)
		}
	}
}
