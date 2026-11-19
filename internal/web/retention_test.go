package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"benevole-ape/internal/store"
)

var testCtx = context.Background()

// retentionEnv has a default of 60 days and a maximum of 180 days.
func retentionEnv(t *testing.T) *env {
	t.Helper()
	return newEnv(t, Config{Retention: store.Retention{DefaultDays: 60, MaxDays: 180}})
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
	// Empty, with the default as a placeholder: an event made without
	// touching it follows the server default.
	r := e.get(e.client(), "/")
	expect(t, r, 200, `name="retention_days"`, `placeholder="60"`, `min="1"`, `max="180"`, "Vide : 60 jours", "Maximum : 180 jours")
	if field := retentionInputTag(r.body); strings.Contains(field, "value=") || strings.Contains(field, "required") {
		t.Errorf("the field is filled in or required: %s", field)
	}

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

	// Typing the default number pins it: the organiser keeps it even if the
	// operator changes the default.
	r = e.post(admin, "/events", createForm(url.Values{"retention_days": {"60"}}))
	expect(t, r, http.StatusSeeOther)
	ev, _ = e.store.EventByAdminToken(testCtx, strings.TrimPrefix(r.location, "/a/"))
	if ev.RetentionDays != 60 {
		t.Errorf("stored retention %d, want 60", ev.RetentionDays)
	}

	// An empty field, or none (a script): the default applies.
	for _, f := range []url.Values{createForm(url.Values{"retention_days": {""}}), createForm(nil)} {
		r = e.post(admin, "/events", f)
		expect(t, r, http.StatusSeeOther)
		expect(t, e.get(admin, r.location), 200, "60 jours après l'événement")
		ev, _ = e.store.EventByAdminToken(testCtx, strings.TrimPrefix(r.location, "/a/"))
		if ev.RetentionDays != 0 {
			t.Errorf("stored retention %d, want 0 (server default)", ev.RetentionDays)
		}
	}

	// The maximum itself is allowed.
	expect(t, e.post(admin, "/events", createForm(url.Values{"retention_days": {"180"}})), http.StatusSeeOther)
}

func TestCreateEventRefusesBadRetention(t *testing.T) {
	e := retentionEnv(t)
	c := e.client()
	for _, v := range []string{"181", "100000", "99999999999999999999", "0", "-5", "abc", "60 jours", "1.5"} {
		r := e.post(c, "/events", createForm(url.Values{"retention_days": {v}, "title": {"Gala"}}))
		expect(t, r, http.StatusBadRequest, "entre 1 et 180", `value="Gala"`)
		// The refused number is not shown again: the field is back to empty.
		if field := retentionInputTag(r.body); strings.Contains(field, "value=") {
			t.Errorf("%q: the refused value is shown again: %s", v, field)
		}
	}
	// A valid number is kept when another field is wrong.
	expect(t, e.post(c, "/events", createForm(url.Values{"retention_days": {"90"}, "admin_email": {"x"}})), http.StatusBadRequest, `value="90"`)

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

	// Refused values: the old value stays.
	for _, v := range []string{"181", "0", "-1", "abc"} {
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

	// Emptying the field goes back to the server default.
	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {""}})), http.StatusSeeOther)
	if got := stored(); got != 0 {
		t.Errorf("stored %d after emptying the field, want 0", got)
	}
	expect(t, e.get(admin, adminPath), 200, "60 jours après l'événement")
}

// An event on the server default shows an empty field, so saving the form
// untouched keeps it on the default; typing the default number pins it.
func TestAdminSaveKeepsTheDefault(t *testing.T) {
	e := retentionEnv(t)
	admin := e.client()
	adminPath, _, _ := setupEvent(t, e, admin) // created without the field
	token := strings.TrimPrefix(adminPath, "/a/")

	if field := retentionInputTag(e.get(admin, adminPath).body); field == "" || strings.Contains(field, "value=") {
		t.Errorf("an event on the default must show an empty field: %q", field)
	}
	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {""}, "title": {"Autre titre"}})), http.StatusSeeOther)
	ev, _ := e.store.EventByAdminToken(testCtx, token)
	if ev.RetentionDays != 0 || ev.Title != "Autre titre" {
		t.Errorf("event after save: retention %d, title %q; want 0 and the new title", ev.RetentionDays, ev.Title)
	}

	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {"60"}})), http.StatusSeeOther)
	if ev, _ = e.store.EventByAdminToken(testCtx, token); ev.RetentionDays != 60 {
		t.Errorf("typing the default: stored %d, want 60", ev.RetentionDays)
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

	// Saving the page as shown brings the stored days down to the maximum.
	expect(t, e.post(admin, adminPath+"/event", updateForm(url.Values{"retention_days": {"180"}})), http.StatusSeeOther)
	if ev, _ = e.store.EventByAdminToken(testCtx, ev.AdminToken); ev.RetentionDays != 180 {
		t.Errorf("stored %d, want 180", ev.RetentionDays)
	}
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
	e := newEnv(t, Config{Retention: store.Retention{DefaultDays: 60, MaxDays: 3 * 365}})
	expect(t, e.post(e.client(), "/events", createForm(url.Values{"date": {"2023-06-01"}})), http.StatusSeeOther)
	e = newEnv(t, Config{Retention: store.Retention{DefaultDays: 60, MaxDays: 180}})
	expect(t, e.post(e.client(), "/events", createForm(url.Values{"date": {"2023-06-01"}})), http.StatusBadRequest, "date hors limites")
}

// retentionInputTag is the retention <input> tag of a page, "" if none.
func retentionInputTag(body string) string {
	i := strings.Index(body, `name="retention_days"`)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(body[:i], "<input")
	return body[start : i+strings.Index(body[i:], ">")+1]
}
