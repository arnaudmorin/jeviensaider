package store

import (
	"database/sql"
	"path/filepath"
	"testing"
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
