package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"jeviensaider/internal/model"
)

// shareToday is the day of the tests of the link preview line.
var shareToday = time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

func TestShareDescription(t *testing.T) {
	open := func(date, location string) *model.Event {
		return &model.Event{Date: date, Location: location, Status: model.StatusOpen}
	}
	tests := []struct {
		name string
		ev   *model.Event
		p    *model.Planning
		want string
	}{
		{"places left", open("2026-06-20", "Cour"), &model.Planning{Needed: 6, Missing: 6}, "samedi 20 juin 2026 · Cour · Il reste 6 places"},
		{"one place left", open("2026-06-20", "Cour"), &model.Planning{Needed: 6, Missing: 1}, "samedi 20 juin 2026 · Cour · Il reste 1 place"},
		{"team complete", open("2026-06-20", "Cour"), &model.Planning{Needed: 6, Missing: 0}, "samedi 20 juin 2026 · Cour · Équipe complète"},
		{"closed", &model.Event{Date: "2026-06-20", Location: "Cour", Status: model.StatusClosed}, &model.Planning{Needed: 6, Missing: 3}, "samedi 20 juin 2026 · Cour · Inscriptions fermées"},
		{"no date, no place", open("", ""), &model.Planning{Needed: 4, Missing: 2}, "Il reste 2 places"},
		{"no slot", open("2026-06-20", "Cour"), &model.Planning{}, "samedi 20 juin 2026 · Cour"},
		{"nothing at all", open("", ""), &model.Planning{}, ""},
		{"event day is today", open("2026-06-01", "Cour"), &model.Planning{Needed: 6, Missing: 2}, "lundi 1 juin 2026 · Cour · Il reste 2 places"},
		{"past event has no state", open("2026-05-31", "Cour"), &model.Planning{Needed: 6, Missing: 6}, "dimanche 31 mai 2026 · Cour"},
		{"past closed event", &model.Event{Date: "2026-05-31", Location: "Cour", Status: model.StatusClosed}, &model.Planning{Needed: 6, Missing: 3}, "dimanche 31 mai 2026 · Cour · Inscriptions fermées"},
		{"slots full, list lacks items", open("2026-06-20", "Cour"), planningWithList(6, 0, 10, 4), "samedi 20 juin 2026 · Cour"},
		{"slots full, list complete", open("2026-06-20", "Cour"), planningWithList(6, 0, 10, 10), "samedi 20 juin 2026 · Cour · Équipe complète"},
		{"places left, list lacks items", open("2026-06-20", "Cour"), planningWithList(6, 2, 10, 4), "samedi 20 juin 2026 · Cour · Il reste 2 places"},
		{"line break in the place", open("", "Salle\n des  fêtes"), &model.Planning{}, "Salle des fêtes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shareDescription(tc.ev, tc.p, shareToday); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// planningWithList builds a planning with the given slot counts and one
// pledge list that wants minItems items and holds total of them.
func planningWithList(needed, missing, minItems, total int) *model.Planning {
	p := &model.Planning{Needed: needed, Missing: missing}
	l := model.PledgeList{ID: 1, UnitOne: "gâteau", Unit: "gâteaux", MinItems: minItems}
	p.AddPledges([]model.PledgeList{l}, []model.PledgeRow{{Pledge: model.Pledge{ListID: l.ID, Quantity: total}}})
	return p
}

func TestShareDescriptionIsCut(t *testing.T) {
	ev := &model.Event{Location: strings.Repeat("é", 300), Status: model.StatusOpen}
	got := shareDescription(ev, &model.Planning{}, shareToday)
	if n := utf8.RuneCountInString(got); n > 200 {
		t.Fatalf("description has %d runes, want at most 200", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("cut description must end with an ellipsis, got %q", got[len(got)-10:])
	}
}

func TestShareDescriptionCutLeavesNoSeparator(t *testing.T) {
	// The cut falls just after " · ": the line must not end with it.
	place := strings.Repeat("a", 190) + " " + strings.Repeat("b", 30)
	ev := &model.Event{Date: "2026-06-20", Location: place, Status: model.StatusOpen}
	got := shareDescription(ev, &model.Planning{Needed: 6, Missing: 6}, shareToday)
	for _, bad := range []string{" …", "·…", "· …"} {
		if strings.Contains(got, bad) {
			t.Fatalf("the cut line holds %q: %q", bad, got)
		}
	}
	// A separator right before the cut point is dropped.
	s := strings.Repeat("a", 196) + " · " + strings.Repeat("b", 20)
	ev = &model.Event{Location: s, Status: model.StatusOpen}
	got = shareDescription(ev, &model.Planning{}, shareToday)
	if want := strings.Repeat("a", 196) + "…"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// headOf returns the <head> of a page.
func headOf(t *testing.T, page string) string {
	t.Helper()
	start, end := strings.Index(page, "<head>"), strings.Index(page, "</head>")
	if start < 0 || end < start {
		t.Fatalf("page has no <head>:\n%s", page)
	}
	return page[start:end]
}

func TestLinkPreviewOpenEvent(t *testing.T) {
	e := newEnv(t, Config{})
	_, pub, _ := setupEvent(t, e, e.client())
	const line = "samedi 20 juin 2026 · Cour · Il reste 6 places"
	expect(t, e.get(e.client(), pub), 200,
		`property="og:title" content="Fête de l&#39;école"`,
		`property="og:description" content="`+line+`"`,
		`name="description" content="`+line+`"`,
		`property="og:url" content="`+e.ts.URL+pub+`"`,
		`property="og:site_name" content="Je viens aider"`,
		`property="og:type" content="website"`,
		`property="og:locale" content="fr_FR"`,
		`name="twitter:card" content="summary"`,
	)
}

func TestLinkPreviewFullEvent(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	_, pub, slot := openEventWithSlot(t, e, admin, url.Values{"admin_email": {"orga@example.org"}, "title": {"Buvette du soir"}})
	e.signUp(e.client(), pub, slot, "Ana")
	e.signUp(e.client(), pub, slot, "Louis")
	expect(t, e.get(e.client(), pub), 200, `property="og:description" content="samedi 20 juin 2026 · Équipe complète"`)
}

func TestLinkPreviewClosedEvent(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := setupEvent(t, e, admin)
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"closed"}}), http.StatusSeeOther)
	expect(t, e.get(e.client(), pub), 200, `property="og:description" content="samedi 20 juin 2026 · Cour · Inscriptions fermées"`)
}

func TestLinkPreviewEmptyEvent(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Loto"}})
	expect(t, r, http.StatusSeeOther)
	pub := publicPathOf(t, e, admin, r.location)
	page := e.get(e.client(), pub)
	expect(t, page, 200, `property="og:title" content="Loto"`)
	head := headOf(t, page.body)
	for _, tag := range []string{"og:description", `name="description"`} {
		if strings.Contains(head, tag) {
			t.Fatalf("an event with no date, place or slot must have no %s:\n%s", tag, head)
		}
	}
}

func TestLinkPreviewEscapesTheTitle(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {`Fête "Noël" <b>x</b>`}})
	expect(t, r, http.StatusSeeOther)
	pub := publicPathOf(t, e, admin, r.location)
	expect(t, e.get(e.client(), pub), 200, `property="og:title" content="Fête &#34;Noël&#34; &lt;b&gt;x&lt;/b&gt;"`)
}

func TestLinkPreviewHidesOrganizerText(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}, "description": {"Appelez le 06 12 34 56 78"}})
	expect(t, r, http.StatusSeeOther)
	pub := publicPathOf(t, e, admin, r.location)
	page := e.get(e.client(), pub)
	expect(t, page, 200, "06 12 34 56 78", `property="og:title" content="Kermesse"`)
	if head := headOf(t, page.body); strings.Contains(head, "06 12 34 56 78") {
		t.Fatalf("the organizer text must stay out of the head:\n%s", head)
	}
}

func TestLinkPreviewUsesBaseURL(t *testing.T) {
	e := newEnv(t, Config{BaseURL: "https://aider.example.org"})
	_, pub, _ := setupEvent(t, e, e.client())
	expect(t, e.get(e.client(), pub), 200, `property="og:url" content="https://aider.example.org`+pub+`"`)
}

func TestLinkPreviewOnlyOnThePublicPage(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slots := setupEvent(t, e, admin)
	personal := e.signUp(e.client(), pub, slots[0], "Ana")
	pages := map[string]string{
		"personal link": personal,
		"sign-up form":  pub + "/slots/" + slots[1] + "/signup",
		"admin page":    adminPath,
		"home page":     "/",
	}
	for name, path := range pages {
		r := e.get(admin, path)
		if r.code != 200 {
			t.Fatalf("%s: want status 200, got %d (%s)", name, r.code, path)
		}
		if strings.Contains(r.body, "og:") {
			t.Fatalf("%s must have no link preview tag", name)
		}
	}
}
