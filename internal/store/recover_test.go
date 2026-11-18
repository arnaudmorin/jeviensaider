package store

import (
	"testing"

	"benevole-ape/internal/model"
)

func TestEventsByAdminEmail(t *testing.T) {
	s := openTest(t)
	mk := func(title, email, date string) *model.Event {
		t.Helper()
		ev := &model.Event{Title: title, AdminEmail: email, Date: date}
		if err := s.CreateEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
		// The last day of an event is its latest date: put the creation date
		// before the event dates, so that the dates decide the order.
		if _, err := s.db.Exec(`UPDATE events SET created_at = '2026-01-01 10:00:00' WHERE id = ?`, ev.ID); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	old := mk("Ancien", "Orga@Example.org", "2026-03-01")
	recent := mk("Récent", "orga@example.org", "2026-09-01")
	mk("Autre organisateur", "autre@example.org", "2026-09-01")
	mk("Sans e-mail", "", "2026-09-01")

	titles := func(evs []model.Event) []string {
		var out []string
		for _, e := range evs {
			out = append(out, e.Title)
		}
		return out
	}
	same := func(got []model.Event, want ...string) bool {
		g := titles(got)
		if len(g) != len(want) {
			return false
		}
		for i := range g {
			if g[i] != want[i] {
				return false
			}
		}
		return true
	}

	// The match ignores case and spaces, and the most recent event comes first.
	for _, in := range []string{"orga@example.org", " ORGA@Example.ORG ", "Orga@Example.org"} {
		got, err := s.EventsByAdminEmail(ctx, in, 10)
		if err != nil || !same(got, "Récent", "Ancien") {
			t.Errorf("EventsByAdminEmail(%q) = %v, %v", in, titles(got), err)
		}
	}

	// The events carry what the e-mail needs.
	got, _ := s.EventsByAdminEmail(ctx, "orga@example.org", 10)
	if got[0].AdminToken != recent.AdminToken || got[0].Slug != recent.Slug || got[0].Date != "2026-09-01" {
		t.Errorf("first event = %+v", got[0])
	}

	// The limit keeps the most recent events. Zero means no limit.
	if got, err := s.EventsByAdminEmail(ctx, "orga@example.org", 1); err != nil || !same(got, "Récent") {
		t.Errorf("limit 1: %v, %v", titles(got), err)
	}
	if got, err := s.EventsByAdminEmail(ctx, "orga@example.org", 0); err != nil || len(got) != 2 {
		t.Errorf("no limit: %v, %v", titles(got), err)
	}

	// An empty address never matches the events that have no address.
	for _, in := range []string{"", "   "} {
		if got, err := s.EventsByAdminEmail(ctx, in, 10); err != nil || len(got) != 0 {
			t.Errorf("EventsByAdminEmail(%q) = %v, %v", in, titles(got), err)
		}
	}
	// No partial match.
	for _, in := range []string{"orga", "orga@example", "%", "orga@example.org%", "_rga@example.org"} {
		if got, err := s.EventsByAdminEmail(ctx, in, 10); err != nil || len(got) != 0 {
			t.Errorf("EventsByAdminEmail(%q) = %v, %v", in, titles(got), err)
		}
	}

	// A deleted event is gone.
	if err := s.DeleteEvent(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.EventsByAdminEmail(ctx, "orga@example.org", 10); err != nil || !same(got, "Récent") {
		t.Errorf("after delete: %v, %v", titles(got), err)
	}
}
