package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"jeviensaider/internal/model"
)

// addMoment stores a moment of 2026 in the event that adminPath belongs to.
// from and to are "YYYY-MM-DD HH:MM".
func addMoment(t *testing.T, e *env, adminPath, title, from, to string) model.Moment {
	t.Helper()
	ev, err := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(adminPath, "/a/"))
	if err != nil {
		t.Fatal(err)
	}
	start, err := time.Parse(model.TimeLayout, from)
	if err != nil {
		t.Fatal(err)
	}
	end, err := time.Parse(model.TimeLayout, to)
	if err != nil {
		t.Fatal(err)
	}
	m := model.Moment{EventID: ev.ID, Title: title, StartsAt: start, EndsAt: end}
	if err := e.store.CreateMoment(testCtx, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var publicLinkRe = regexp.MustCompile(`href="(/e/[^"]+)" target="_blank"`)

func TestPublicTableShowsMomentTitles(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	addMoment(t, e, adminPath, "Goûter", "2026-06-20 14:00", "2026-06-20 15:00")
	addMoment(t, e, adminPath, "Spectacle des enfants", "2026-06-20 11:00", "2026-06-20 12:00")

	r := e.get(e.client(), pub)
	expect(t, r, 200,
		`class="col-title">Goûter`,
		`class="col-title">Spectacle des enfants`,
		`class="moment-cell"`)
	if i, j := strings.Index(r.body, "Spectacle des enfants"), strings.Index(r.body, "Goûter"); i > j {
		t.Error("the 11:00 moment must come before the 14:00 one")
	}
}

func TestPhoneListShowsMoments(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	addMoment(t, e, adminPath, "Goûter", "2026-06-20 14:00", "2026-06-20 15:00")
	addMoment(t, e, adminPath, "Spectacle des enfants", "2026-06-20 11:00", "2026-06-20 12:00")

	const note = "Pas d'inscription à ce moment" // literal template text keeps its apostrophe
	r := e.get(e.client(), pub)
	expect(t, r, 200, note)
	if n := strings.Count(r.body, note); n != 1 {
		t.Errorf("the note appears %d times, want 1 (one moment-only group)", n)
	}

	// The filter hides a group with nothing to take, so the moment-only
	// group goes. The table keeps its columns.
	r = e.get(e.client(), pub+"?todo=1")
	expect(t, r, 200, "Spectacle des enfants")
	if strings.Contains(r.body, note) {
		t.Error("the moment-only group must be hidden by the filter")
	}
}

func TestEventWithOnlyMoments(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"title": {"Kermesse"}, "admin_email": {"orga@example.org"}, "date": {"2026-06-20"}})
	expect(t, r, http.StatusSeeOther)
	adminPath := r.location
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	addMoment(t, e, adminPath, "Spectacle", "2026-06-20 14:00", "2026-06-20 15:00")

	pub := publicLinkRe.FindStringSubmatch(e.get(admin, adminPath).body)[1]
	body := e.get(e.client(), pub).body
	for _, want := range []string{`class="timeline always"`, "Spectacle"} {
		if !strings.Contains(body, want) {
			t.Errorf("public page does not contain %q", want)
		}
	}
	for _, not := range []string{"Aucun créneau n'a encore été proposé", `class="grid"`} {
		if strings.Contains(body, not) {
			t.Errorf("public page must not contain %q", not)
		}
	}
	if strings.Contains(e.get(admin, adminPath).body, "Qui est où, et quand") {
		t.Error("the admin grid is empty without slots: it must not show")
	}
}

// A stand with no slot gives the table nothing to show: the moments are the
// planning, as for an event with no stand.
func TestEventWithAStandButNoSlotShowsTheMomentList(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"title": {"Kermesse"}, "admin_email": {"orga@example.org"}, "date": {"2026-06-20"}})
	expect(t, r, http.StatusSeeOther)
	adminPath := r.location
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Buvette"}}), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	addMoment(t, e, adminPath, "Spectacle", "2026-06-20 14:00", "2026-06-20 15:00")

	pub := publicLinkRe.FindStringSubmatch(e.get(admin, adminPath).body)[1]
	body := e.get(e.client(), pub).body
	if !strings.Contains(body, `class="timeline always"`) || !strings.Contains(body, "Spectacle") {
		t.Error("the public page must show the moment as a list")
	}
	if strings.Contains(body, `class="grid"`) {
		t.Error("the table has no slot to show: it must not appear")
	}
}

func TestDraftEventHidesMoments(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"title": {"Kermesse"}, "admin_email": {"orga@example.org"}, "date": {"2026-06-20"}})
	expect(t, r, http.StatusSeeOther)
	adminPath := r.location
	addMoment(t, e, adminPath, "Spectacle secret", "2026-06-20 14:00", "2026-06-20 15:00")
	pub := publicLinkRe.FindStringSubmatch(e.get(admin, adminPath).body)[1]

	if body := e.get(e.client(), pub).body; strings.Contains(body, "Spectacle secret") {
		t.Error("a draft event must not show its moments")
	}
	// Control: once the event is open, the same page shows the moment.
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	expect(t, e.get(e.client(), pub), 200, "Spectacle secret")
}

// A moment is public information on the page only: it must not reach the
// planning PDF, the CSV export or a volunteer's calendar file.
func TestMomentsStayOutOfTheExports(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})

	pdfBefore := e.get(admin, adminPath+"/planning.pdf")
	expect(t, pdfBefore, 200)
	if again := e.get(admin, adminPath+"/planning.pdf"); again.body != pdfBefore.body {
		t.Skip("the PDF is not byte-for-byte stable, so it cannot be compared")
	}

	// A moment on another day would turn the PDF's "several days" wording on.
	addMoment(t, e, adminPath, "Spectacle secret", "2026-06-21 11:00", "2026-06-21 12:00")
	pdfAfter := e.get(admin, adminPath+"/planning.pdf")
	expect(t, pdfAfter, 200)
	if pdfAfter.body != pdfBefore.body {
		t.Error("a moment changed the planning PDF")
	}
	csv := e.get(admin, adminPath+"/export.csv")
	expect(t, csv, 200)
	if strings.Contains(csv.body, "Spectacle secret") {
		t.Error("a moment is in the CSV export")
	}
}

// With moments and no slot, the "places à prendre" filter has nothing to
// keep: the list must not turn empty.
func TestTodoFilterKeepsMomentsWhenThereIsNoSlot(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"title": {"Kermesse"}, "admin_email": {"orga@example.org"}, "date": {"2026-06-20"}})
	expect(t, r, http.StatusSeeOther)
	adminPath := r.location
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	addMoment(t, e, adminPath, "Spectacle", "2026-06-20 14:00", "2026-06-20 15:00")

	pub := publicLinkRe.FindStringSubmatch(e.get(admin, adminPath).body)[1]
	expect(t, e.get(e.client(), pub+"?todo=1"), 200, "Spectacle")
}

// The legend explains the slots only: a column that exists only for a
// moment shows its title, so it needs no swatch.
func TestLegendHasNoMomentSwatch(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	addMoment(t, e, adminPath, "Spectacle", "2026-06-20 11:00", "2026-06-20 12:00")

	page := e.get(e.client(), pub)
	expect(t, page, 200, `class="moment-cell"`, `class="legend"`)
	if strings.Contains(page.body, "Moment sans créneau") || strings.Contains(page.body, "lg moment") {
		t.Error("the legend names the moment columns")
	}
}
