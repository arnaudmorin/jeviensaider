package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

var testCtx = context.Background()

// retentionEnv has a default of 60 days and a maximum of 180 days.
func retentionEnv(t *testing.T) *env {
	t.Helper()
	return newEnv(t, Config{RetentionDays: 60, MaxRetentionDays: 180})
}

func createForm(extra url.Values) url.Values {
	f := url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}, "date": {"2026-06-20"}}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func TestCreateFormOffersTheRetention(t *testing.T) {
	e := retentionEnv(t)
	expect(t, e.get(e.client(), "/"), 200, `name="retention_days"`, `value="60"`, `min="1"`, `max="180"`, "Maximum : 180 jours")

	// Automatic deletion is off: there is nothing to choose.
	off := newEnv(t, Config{})
	if r := off.get(off.client(), "/"); strings.Contains(r.body, "retention_days") {
		t.Error("the retention field is shown although events are kept forever")
	}
}

func TestCreateEventWithRetention(t *testing.T) {
	e := retentionEnv(t)
	admin := e.client()
	r := e.post(admin, "/events", createForm(url.Values{"retention_days": {"90"}}))
	expect(t, r, http.StatusSeeOther)

	expect(t, e.get(admin, r.location), 200, "90 jours après l'événement", `name="retention_days"`, `value="90"`)
	ev, err := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(r.location, "/a/"))
	if err != nil || ev.RetentionDays != 90 {
		t.Fatalf("stored retention: %+v, %v", ev, err)
	}

	// The default value of the form is stored too: the organiser sees a
	// deletion date and keeps it, even if the operator changes the default.
	r = e.post(admin, "/events", createForm(url.Values{"retention_days": {"60"}}))
	expect(t, r, http.StatusSeeOther)
	ev, _ = e.store.EventByAdminToken(testCtx, strings.TrimPrefix(r.location, "/a/"))
	if ev.RetentionDays != 60 {
		t.Errorf("stored retention %d, want 60", ev.RetentionDays)
	}

	// No field (a script, an old form): the default applies.
	r = e.post(admin, "/events", createForm(nil))
	expect(t, r, http.StatusSeeOther)
	expect(t, e.get(admin, r.location), 200, "60 jours après l'événement")
	ev, _ = e.store.EventByAdminToken(testCtx, strings.TrimPrefix(r.location, "/a/"))
	if ev.RetentionDays != 0 {
		t.Errorf("stored retention %d, want 0 (server default)", ev.RetentionDays)
	}

	// The maximum itself is allowed.
	expect(t, e.post(admin, "/events", createForm(url.Values{"retention_days": {"180"}})), http.StatusSeeOther)
}

func TestCreateEventRefusesBadRetention(t *testing.T) {
	e := retentionEnv(t)
	c := e.client()
	for _, v := range []string{"181", "100000", "99999999999999999999"} {
		expect(t, e.post(c, "/events", createForm(url.Values{"retention_days": {v}})), http.StatusBadRequest,
			"La durée de conservation ne peut pas dépasser 180 jours.")
	}
	for _, v := range []string{"0", "-5", "abc", "60 jours", "1.5"} {
		expect(t, e.post(c, "/events", createForm(url.Values{"retention_days": {v}})), http.StatusBadRequest,
			"entre 1 et 180")
	}
	// The page keeps what was typed.
	expect(t, e.post(c, "/events", createForm(url.Values{"retention_days": {"500"}, "title": {"Gala"}})), http.StatusBadRequest, `value="Gala"`, `value="500"`)

	sums, err := e.store.ListEventSummaries(testCtx)
	if err != nil || len(sums) != 0 {
		t.Fatalf("events created despite the errors: %d, %v", len(sums), err)
	}
}

// With automatic deletion off, a posted value is ignored: it must not be
// stored, in case the operator turns deletion on later.
func TestRetentionIgnoredWhenKeptForever(t *testing.T) {
	e := newEnv(t, Config{})
	r := e.post(e.client(), "/events", createForm(url.Values{"retention_days": {"5"}}))
	expect(t, r, http.StatusSeeOther)
	ev, _ := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(r.location, "/a/"))
	if ev.RetentionDays != 0 {
		t.Errorf("stored retention %d, want 0", ev.RetentionDays)
	}
}

func updateForm(extra url.Values) url.Values {
	f := url.Values{"title": {"Kermesse"}, "date": {"2026-06-20"}, "admin_email": {"orga@example.org"}, "notify": {"1"}}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func TestAdminChangesTheRetention(t *testing.T) {
	e := retentionEnv(t)
	admin := e.client()
	adminPath, _, _ := setupEvent(t, e, admin)
	token := strings.TrimPrefix(adminPath, "/a/")
	stored := func() int {
		ev, err := e.store.EventByAdminToken(testCtx, token)
		if err != nil {
			t.Fatal(err)
		}
		return ev.RetentionDays
	}

	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {"120"}})), http.StatusSeeOther)
	if got := stored(); got != 120 {
		t.Fatalf("stored %d, want 120", got)
	}
	expect(t, e.get(admin, adminPath), 200, "Événement mis à jour", "120 jours après l'événement", `value="120"`)

	// Over the maximum: refused, and the old value stays.
	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {"181"}})), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "La durée de conservation ne peut pas dépasser 180 jours.")
	if got := stored(); got != 120 {
		t.Errorf("stored %d after a refused change, want 120", got)
	}
	for _, v := range []string{"0", "-1", "abc"} {
		expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {v}})), http.StatusSeeOther)
		expect(t, e.get(admin, adminPath), 200, "entre 1 et 180")
	}
	if got := stored(); got != 120 {
		t.Errorf("stored %d after refused changes, want 120", got)
	}

	// A form without the field (an old page in a browser tab) changes nothing.
	expect(t, e.post(admin, adminPath+"/event", updateForm(nil)), http.StatusSeeOther)
	if got := stored(); got != 120 {
		t.Errorf("stored %d after a form without the field, want 120", got)
	}
}

// Saving the form without touching the field keeps an event on the server
// default: only a real change stores a value.
func TestAdminSaveKeepsTheDefault(t *testing.T) {
	e := retentionEnv(t)
	admin := e.client()
	adminPath, _, _ := setupEvent(t, e, admin) // created without the field
	token := strings.TrimPrefix(adminPath, "/a/")

	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {"60"}, "title": {"Autre titre"}})), http.StatusSeeOther)
	ev, _ := e.store.EventByAdminToken(testCtx, token)
	if ev.RetentionDays != 0 || ev.Title != "Autre titre" {
		t.Errorf("event after save: retention %d, title %q; want 0 and the new title", ev.RetentionDays, ev.Title)
	}
}

// If the operator lowers the maximum, the page shows the days that really
// apply, not the stored ones.
func TestAdminShowsTheDaysThatApply(t *testing.T) {
	e := retentionEnv(t)
	admin := e.client()
	adminPath, _, _ := setupEvent(t, e, admin)
	ev, _ := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(adminPath, "/a/"))
	ev.RetentionDays = 400
	if err := e.store.UpdateEvent(testCtx, ev); err != nil {
		t.Fatal(err)
	}
	expect(t, e.get(admin, adminPath), 200, "180 jours après l'événement", `value="180"`)
}

func TestAdminHidesTheFieldWhenKeptForever(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := setupEvent(t, e, admin)
	if r := e.get(admin, adminPath); strings.Contains(r.body, "retention_days") {
		t.Error("the retention field is shown although events are kept forever")
	}
	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {"5"}})), http.StatusSeeOther)
	ev, _ := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(adminPath, "/a/"))
	if ev.RetentionDays != 0 {
		t.Errorf("stored retention %d, want 0", ev.RetentionDays)
	}
}

// A duplicate keeps the retention of its source.
func TestDuplicateKeepsTheRetention(t *testing.T) {
	e := retentionEnv(t)
	admin := e.client()
	r := e.post(admin, "/events", createForm(url.Values{"retention_days": {"90"}}))
	expect(t, r, http.StatusSeeOther)
	dup := e.post(admin, r.location+"/duplicate", url.Values{"title": {"Kermesse 2027"}, "date": {"2027-06-19"}})
	expect(t, dup, http.StatusSeeOther)
	ev, err := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(dup.location, "/a/"))
	if err != nil || ev.RetentionDays != 90 {
		t.Errorf("duplicate: %+v, %v", ev, err)
	}
}

// Events can be kept up to the maximum, so the past window of dates covers
// at least that period.
func TestDateLimitsFollowTheMaximum(t *testing.T) {
	// Clock fixed at 2026-05-01. A year back is always allowed.
	e := newEnv(t, Config{RetentionDays: 60, MaxRetentionDays: 3 * 365})
	expect(t, e.post(e.client(), "/events", createForm(url.Values{"date": {"2023-06-01"}})), http.StatusSeeOther)
	e = newEnv(t, Config{RetentionDays: 60, MaxRetentionDays: 180})
	expect(t, e.post(e.client(), "/events", createForm(url.Values{"date": {"2023-06-01"}})), http.StatusBadRequest, "date hors limites")
}
