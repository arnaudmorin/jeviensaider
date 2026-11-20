package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

var momentIDRe = regexp.MustCompile(`id="moment-(\d+)"`)

func TestAdminAddsEditsAndDeletesAMoment(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})

	// The day field starts as the event's date.
	form := e.get(admin, adminPath).body
	expect(t, resp{code: 200, body: form}, 200, "Déroulé de la journée")
	if !regexp.MustCompile(`(?s)/moments" .*?name="date" value="2026-06-20"`).MatchString(form) {
		t.Error("the add form does not start with the event date")
	}

	expect(t, e.post(admin, adminPath+"/moments", url.Values{
		"date": {"2026-06-20"}, "from": {"12:00"}, "to": {"13:00"}, "title": {"Spectacle des enfants"},
	}), http.StatusSeeOther)
	body := e.get(admin, adminPath).body
	expect(t, resp{code: 200, body: body}, 200,
		"Moment ajouté", "Spectacle des enfants", "Déroulé de la journée", `class="col-title">Spectacle des enfants`)
	expect(t, e.get(e.client(), pub), 200, "Spectacle des enfants", `class="moment-cell"`)
	id := momentIDRe.FindStringSubmatch(body)[1]

	expect(t, e.post(admin, adminPath+"/moments/"+id, url.Values{
		"date": {"2026-06-20"}, "from": {"12:00"}, "to": {"12:30"}, "title": {"Spectacle"},
	}), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Moment mis à jour", "12h – 12h30")

	expect(t, e.post(admin, adminPath+"/moments/"+id+"/delete", nil), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Moment supprimé")
	if body := e.get(e.client(), pub).body; strings.Contains(body, "Spectacle") {
		t.Error("a deleted moment is still on the public page")
	}
}

func TestAdminMomentErrors(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	form := func(title, from, to string) url.Values {
		return url.Values{"date": {"2026-06-20"}, "from": {from}, "to": {to}, "title": {title}}
	}
	fails := func(f url.Values, want string) {
		t.Helper()
		expect(t, e.post(admin, adminPath+"/moments", f), http.StatusSeeOther)
		expect(t, e.get(admin, adminPath), 200, want)
	}

	fails(form("Goûter", "16:00", "15:00"), "L&#39;heure de fin doit être après")
	fails(form("   ", "10:00", "11:00"), "Le titre du moment est obligatoire")
	fails(form("Goûter", "", "11:00"), "Début :")
	fails(form(strings.Repeat("a", model.MaxMomentTitle+1), "10:00", "11:00"), "caractères au maximum")

	expect(t, e.post(admin, adminPath+"/moments", form("Goûter", "10:00", "11:00")), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Moment ajouté")
	fails(form("Autre", "10:00", "11:00"), "Un moment existe déjà à cette heure")

	// Up to the limit.
	start := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	for i := 0; i < store.MaxMoments-1; i++ {
		from := start.Add(time.Duration(i) * time.Hour)
		addMoment(t, e, adminPath, "Moment", from.Format(model.TimeLayout), from.Add(time.Hour).Format(model.TimeLayout))
	}
	fails(form("De trop", "17:00", "18:00"), "plus de 30 moments")
}

func TestMomentsBelongToTheirEvent(t *testing.T) {
	e := newEnv(t, Config{})
	admin1, admin2 := e.client(), e.client()
	path1, _, _ := openEventWithSlot(t, e, admin1, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	path2, _, _ := openEventWithSlot(t, e, admin2, url.Values{"title": {"Marché"}, "admin_email": {"orga@example.org"}})
	m := addMoment(t, e, path1, "Goûter", "2026-06-20 16:00", "2026-06-20 17:00")
	id := i64(m.ID)
	edit := url.Values{"date": {"2026-06-20"}, "from": {"16:00"}, "to": {"17:00"}, "title": {"Piraté"}}

	// Control: the right organiser can change it.
	edit.Set("title", "Goûter des enfants")
	expect(t, e.post(admin1, path1+"/moments/"+id, edit), http.StatusSeeOther)

	// The other organiser gets "not found", and nothing changes.
	edit.Set("title", "Piraté")
	expect(t, e.post(admin2, path2+"/moments/"+id, edit), http.StatusNotFound)
	expect(t, e.post(admin2, path2+"/moments/"+id+"/delete", nil), http.StatusNotFound)

	ev, err := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(path1, "/a/"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.store.GetMoment(testCtx, ev.ID, m.ID)
	if err != nil || got.Title != "Goûter des enfants" {
		t.Errorf("the moment must be unchanged by the other organiser: %+v %v", got, err)
	}
}

func TestMomentTitleIsEscaped(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	const raw = "<script>alert(1)</script>"
	expect(t, e.post(admin, adminPath+"/moments", url.Values{
		"date": {"2026-06-20"}, "from": {"12:00"}, "to": {"13:00"}, "title": {raw},
	}), http.StatusSeeOther)

	for name, body := range map[string]string{
		"public": e.get(e.client(), pub).body,
		"admin":  e.get(admin, adminPath).body,
	} {
		if strings.Contains(body, raw) {
			t.Errorf("%s page: the title is not escaped", name)
		}
		if !strings.Contains(body, "&lt;script&gt;") {
			t.Errorf("%s page: the escaped title is missing", name)
		}
	}
}

func TestMomentDefaultDate(t *testing.T) {
	today := time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)
	at := func(s string) time.Time { v, _ := time.Parse(model.TimeLayout, s); return v }
	withSlot := model.BuildPlanning(
		[]model.Stand{{ID: 1, Name: "Buvette"}},
		[]model.Slot{{ID: 1, StandID: 1, StartsAt: at("2026-07-04 10:00"), EndsAt: at("2026-07-04 11:00"), MinPeople: 1}},
		[]model.Moment{{ID: 1, Title: "Spectacle", StartsAt: at("2026-07-03 10:00"), EndsAt: at("2026-07-03 11:00")}},
		nil, 0, true)
	empty := model.BuildPlanning(nil, nil, nil, nil, 0, true)

	if got := momentDefaultDate("2026-06-20", withSlot, today); got != "2026-06-20" {
		t.Errorf("event date: got %q", got)
	}
	if got := momentDefaultDate("", withSlot, today); got != "2026-07-04" {
		t.Errorf("first slot day (a moment-only column on 07-03 must not count): got %q", got)
	}
	if got := momentDefaultDate("", empty, today); got != "2026-03-05" {
		t.Errorf("today: got %q", got)
	}
}
