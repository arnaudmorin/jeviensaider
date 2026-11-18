package store

import (
	"testing"

	"benevole-ape/internal/model"
)

func TestRetentionDays(t *testing.T) {
	policy := Retention{DefaultDays: 60, MaxDays: 180}
	for _, c := range []struct {
		name  string
		r     Retention
		event int
		want  int
	}{
		{"no choice: the default", policy, 0, 60},
		{"a shorter choice", policy, 10, 10},
		{"the maximum itself", policy, 180, 180},
		{"a stored value above the maximum is cut", policy, 400, 180},
		{"no maximum set: the default is the limit", Retention{DefaultDays: 60}, 400, 60},
		{"a maximum below the default: the default wins", Retention{DefaultDays: 90, MaxDays: 30}, 0, 90},
		{"automatic deletion off: kept forever", Retention{DefaultDays: 0, MaxDays: 180}, 30, 0},
	} {
		if got := c.r.Days(c.event); got != c.want {
			t.Errorf("%s: Days(%d) = %d, want %d", c.name, c.event, got, c.want)
		}
	}
	if got := (Retention{DefaultDays: 60}).Max(); got != 60 {
		t.Errorf("Max without MaxDays = %d, want 60", got)
	}
	if got := policy.Max(); got != 180 {
		t.Errorf("Max = %d, want 180", got)
	}
	if (Retention{}).Enabled() || !policy.Enabled() {
		t.Error("Enabled must be true only when the default is above 0")
	}
}

// newDatedEvent creates an event dated 2026-06-20 whose creation date is
// older, so that its last day of activity is its date.
func newDatedEvent(t *testing.T, s *Store, retentionDays int) *model.Event {
	t.Helper()
	ev := &model.Event{Title: "Fête", Date: "2026-06-20", RetentionDays: retentionDays}
	if err := s.CreateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE events SET created_at = '2026-01-01 10:00:00' WHERE id = ?`, ev.ID); err != nil {
		t.Fatal(err)
	}
	return ev
}

func exists(s *Store, ev *model.Event) bool {
	_, err := s.EventBySlug(ctx, ev.Slug)
	return err == nil
}

// ExpiresOn must be exactly the first day on which Cleanup deletes the
// event, whatever number of days the event chose.
func TestCleanupUsesTheEventRetention(t *testing.T) {
	policy := Retention{DefaultDays: 60, MaxDays: 180}
	for _, c := range []struct {
		name   string
		stored int
		days   int // the days that apply
	}{
		{"default", 0, 60},
		{"short", 10, 10},
		{"long", 120, 120},
		{"above the maximum", 400, 180},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := openTest(t)
			ev := newDatedEvent(t, s, c.stored)
			last, err := s.EventLastDay(ctx, ev.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := policy.Days(c.stored); got != c.days {
				t.Fatalf("Days(%d) = %d, want %d", c.stored, got, c.days)
			}
			expires := ExpiresOn(last, c.days)

			if n, err := s.Cleanup(ctx, policy, expires.AddDate(0, 0, -1)); n != 0 || err != nil || !exists(s, ev) {
				t.Fatalf("the day before %s: deleted %d, %v, exists=%v", expires.Format(model.DateLayout), n, err, exists(s, ev))
			}
			if n, err := s.Cleanup(ctx, policy, expires); n != 1 || err != nil || exists(s, ev) {
				t.Fatalf("on %s: deleted %d, %v, exists=%v", expires.Format(model.DateLayout), n, err, exists(s, ev))
			}
		})
	}
}

func TestCleanupMixedRetentions(t *testing.T) {
	s := openTest(t)
	policy := Retention{DefaultDays: 60, MaxDays: 180}
	byDefault := newDatedEvent(t, s, 0)
	short := newDatedEvent(t, s, 10)
	long := newDatedEvent(t, s, 120)

	// 2026-07-01 is the first day after the 10 days of "short".
	if n, err := s.Cleanup(ctx, policy, day("2026-07-01")); n != 1 || err != nil {
		t.Fatalf("cleanup 07-01: %d, %v", n, err)
	}
	if exists(s, short) || !exists(s, byDefault) || !exists(s, long) {
		t.Fatalf("after 07-01: short=%v default=%v long=%v", exists(s, short), exists(s, byDefault), exists(s, long))
	}

	// An operator who lowers the default to 30 days only affects the events
	// that made no choice.
	lower := Retention{DefaultDays: 30, MaxDays: 180}
	if n, err := s.Cleanup(ctx, lower, day("2026-07-21")); n != 1 || err != nil {
		t.Fatalf("cleanup 07-21: %d, %v", n, err)
	}
	if exists(s, byDefault) || !exists(s, long) {
		t.Fatalf("after 07-21: default=%v long=%v", exists(s, byDefault), exists(s, long))
	}

	// Lowering the maximum cuts the events that chose more.
	cut := Retention{DefaultDays: 30, MaxDays: 50}
	if n, err := s.Cleanup(ctx, cut, day("2026-08-10")); n != 1 || err != nil {
		t.Fatalf("cleanup 08-10: %d, %v", n, err)
	}
	if exists(s, long) {
		t.Error("an event above the new maximum was kept")
	}
}

// The default being 0 is the "keep everything" switch: it wins over the
// days stored with the events.
func TestCleanupOffKeepsEveryEvent(t *testing.T) {
	s := openTest(t)
	ev := newDatedEvent(t, s, 10)
	if n, err := s.Cleanup(ctx, Retention{DefaultDays: 0, MaxDays: 180}, day("2030-01-01")); n != 0 || err != nil || !exists(s, ev) {
		t.Fatalf("deleted %d, %v, exists=%v", n, err, exists(s, ev))
	}
}

func TestEventRetentionDaysIsStored(t *testing.T) {
	s := openTest(t)
	ev := newDatedEvent(t, s, 90)
	got, err := s.EventBySlug(ctx, ev.Slug)
	if err != nil || got.RetentionDays != 90 {
		t.Fatalf("after create: %+v, %v", got, err)
	}

	got.RetentionDays = 30
	if err := s.UpdateEvent(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.EventByAdminToken(ctx, got.AdminToken); again.RetentionDays != 30 {
		t.Errorf("after update: %d, want 30", again.RetentionDays)
	}

	// 0 means "the server default": it is stored as NULL.
	got.RetentionDays = 0
	if err := s.UpdateEvent(ctx, got); err != nil {
		t.Fatal(err)
	}
	var stored *int
	if err := s.db.QueryRow(`SELECT retention_days FROM events WHERE id = ?`, got.ID).Scan(&stored); err != nil || stored != nil {
		t.Errorf("stored %v, %v; want NULL", stored, err)
	}

	sums, err := s.ListEventSummaries(ctx)
	if err != nil || len(sums) != 1 || sums[0].RetentionDays != 0 {
		t.Errorf("summaries: %+v, %v", sums, err)
	}
}

func TestDuplicateEventKeepsRetention(t *testing.T) {
	s := openTest(t)
	src := newDatedEvent(t, s, 90)
	dup, err := s.DuplicateEvent(ctx, src, "Fête 2027", "2027-06-19")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.EventBySlug(ctx, dup.Slug)
	if err != nil || got.RetentionDays != 90 {
		t.Errorf("duplicate: %+v, %v", got, err)
	}
}

// A stored value of 0 would mean "forever" for the cleanup: the database
// refuses it.
func TestRetentionDaysCheckConstraint(t *testing.T) {
	s := openTest(t)
	_, err := s.db.Exec(`INSERT INTO events (slug, title, admin_token, retention_days) VALUES ('a', 'A', 'tok', 0)`)
	if err == nil {
		t.Error("retention_days = 0 accepted")
	}
	_, err = s.db.Exec(`INSERT INTO events (slug, title, admin_token, retention_days) VALUES ('b', 'B', 'tok2', -5)`)
	if err == nil {
		t.Error("retention_days = -5 accepted")
	}
}
