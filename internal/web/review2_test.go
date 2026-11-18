package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Creating events must not let anyone flood an address with mails.
var (
	createdPerRecipientPerDay = DefaultMailLimits().CreatedPerAddressPerDay
	createdPerHour            = DefaultMailLimits().CreatedPerHour
	signupMailsPerEventHour   = DefaultMailLimits().SignupsPerEventPerHour
)

func TestCreationMailLimits(t *testing.T) {
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://jeviensaider.example"})
	c := e.client()
	for i := 0; i < createdPerRecipientPerDay+2; i++ {
		r := e.post(c, "/events", url.Values{"title": {"Spam"}, "admin_email": {"Victim@example.org"}})
		expect(t, r, http.StatusSeeOther)
		body := e.get(c, r.location).body
		if sent := strings.Contains(body, "il vient de vous être envoyé"); sent != (i < createdPerRecipientPerDay) {
			t.Errorf("event %d: mail announced = %v", i, sent)
		}
	}
	if n := len(fm.take()); n != createdPerRecipientPerDay {
		t.Errorf("%d mails to one address, want %d", n, createdPerRecipientPerDay)
	}

	// The hourly cap over all addresses.
	for i := 0; i < createdPerHour+5; i++ {
		e.post(c, "/events", url.Values{"title": {"Spam"}, "admin_email": {"v" + strings.Repeat("x", i) + "@example.org"}})
	}
	if n := len(fm.take()); n != createdPerHour-createdPerRecipientPerDay {
		t.Errorf("%d creation mails in the hour, want %d", n, createdPerHour-createdPerRecipientPerDay)
	}
}

func TestSignupMailsPause(t *testing.T) {
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://jeviensaider.example"})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"title": {"Fête"}, "date": {"2026-06-20"}, "admin_email": {"orga@example.org"}, "notify": {"1"}})
	adminPath := r.location
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Buvette"}}), http.StatusSeeOther)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	expect(t, e.post(admin, adminPath+"/stands/"+standID+"/slots", url.Values{"date": {"2026-06-20"}, "from": {"14:00"}, "to": {"15:00"}, "step": {"60"}, "min": {"2"}}), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	pub := regexp.MustCompile(`href="(/e/[^"]+)" target="_blank"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	slot := regexp.MustCompile(`/slots/(\d+)/signup`).FindStringSubmatch(e.get(e.client(), pub).body)[1]
	fm.take()

	// Sign up and cancel in a loop.
	v := e.client()
	r = e.post(v, pub+"/slots/"+slot+"/signup", url.Values{"name": {"Spammer"}})
	me := strings.TrimSuffix(r.location, "?welcome=1")
	for i := 0; i < signupMailsPerEventHour; i++ {
		id := regexp.MustCompile(`/signups/(\d+)/delete`).FindStringSubmatch(e.get(v, me).body)[1]
		e.post(v, me+"/signups/"+id+"/delete", nil)
		e.post(v, pub+"/slots/"+slot+"/signup", url.Values{"name": {"Spammer"}})
	}
	msgs := fm.take()
	if len(msgs) != signupMailsPerEventHour+1 {
		t.Fatalf("%d mails, want %d and one pause notice", len(msgs), signupMailsPerEventHour)
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Subject, "notifications en pause") || !strings.Contains(last.Body, "jusqu'à") || !strings.Contains(last.Body, adminPath) {
		t.Errorf("pause mail: %+v", last)
	}

	// An hour later, notifications resume.
	now = func() time.Time { return time.Date(2026, 5, 1, 13, 0, 1, 0, time.UTC) }
	e.post(e.client(), pub+"/slots/"+slot+"/signup", url.Values{"name": {"Paul"}})
	if msgs := fm.take(); len(msgs) != 1 || !strings.HasPrefix(msgs[0].Subject, "Inscription : Paul") {
		t.Errorf("after an hour: %+v", msgs)
	}
}

func TestWindowCounterBounded(t *testing.T) {
	c := newWindowCounter(time.Hour)
	t0 := time.Now()
	for i := 0; i < maxCounterKeys; i++ {
		c.hit(strings.Repeat("k", 1)+string(rune(i+1000)), t0)
	}
	if n, _ := c.hit("new", t0); n != 0 {
		t.Error("counter grew past its bound")
	}
	if n, _ := c.hit("new", t0.Add(time.Hour)); n != 1 {
		t.Error("expired keys not reclaimed")
	}
}

func TestDuplicateNeedsCreatePassword(t *testing.T) {
	e := newEnv(t, Config{CreatePassword: "ape2026"})
	c := e.client()
	r := e.post(c, "/events", url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}, "password": {"ape2026"}})
	adminPath := r.location
	expect(t, e.get(c, adminPath), 200, `name="password"`)
	expect(t, e.post(c, adminPath+"/duplicate", url.Values{"title": {"Copie"}}), http.StatusSeeOther)
	expect(t, e.get(c, adminPath), 200, "Mot de passe de création incorrect")
	r = e.post(c, adminPath+"/duplicate", url.Values{"title": {"Copie"}, "password": {"ape2026"}})
	if expect(t, r, http.StatusSeeOther); r.location == adminPath {
		t.Error("duplicate with the password refused")
	}
}

func TestDraftHidesSlotsAndClosedLocksDetails(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slots := setupEvent(t, e, admin)

	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"draft"}}), http.StatusSeeOther)
	r := e.get(e.client(), pub+"/slots/"+slots[0]+"/signup")
	expect(t, r, 404, "pas encore ouvertes")
	if strings.Contains(r.body, "Pêche aux canards") {
		t.Error("draft sign-up form shows the stand")
	}
	expect(t, e.post(e.client(), pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Marie"}}), 404)

	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	v := e.client()
	me := strings.TrimSuffix(e.post(v, pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Marie"}}).location, "?welcome=1")
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"closed"}}), http.StatusSeeOther)
	expect(t, e.post(v, me, url.Values{"name": {"Someone Else"}}), http.StatusSeeOther)
	expect(t, e.get(v, me), 200, "Les inscriptions sont closes", `value="Marie"`)
}

func TestRenewPersonalLink(t *testing.T) {
	e := newEnv(t, Config{})
	_, pub, slots := setupEvent(t, e, e.client())
	v := e.client()
	old := strings.TrimSuffix(e.post(v, pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Marie"}}).location, "?welcome=1")
	expect(t, e.get(v, old), 200, "Changer de lien")
	r := e.post(v, old+"/renew", nil)
	expect(t, r, http.StatusSeeOther)
	if r.location == old || !strings.HasPrefix(r.location, pub+"/me/") {
		t.Fatalf("new link %q", r.location)
	}
	expect(t, e.get(e.client(), old), 404)
	expect(t, e.get(v, r.location), 200, "nouveau lien personnel", "Marie")
	// The browser's cookie follows the new link.
	expect(t, e.get(v, pub), 200, r.location)
}

func TestMailLimitsConfigurable(t *testing.T) {
	// Custom limits.
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://x.example", MailLimits: &MailLimits{CreatedPerAddressPerDay: 1, PerHour: 2}})
	c := e.client()
	for _, to := range []string{"a@example.org", "a@example.org", "b@example.org", "c@example.org"} {
		e.post(c, "/events", url.Values{"title": {"T"}, "admin_email": {to}})
	}
	if msgs := fm.take(); len(msgs) != 2 || msgs[0].To != "a@example.org" || msgs[1].To != "b@example.org" {
		t.Errorf("custom limits: %+v", msgs)
	}

	// No limits at all.
	fm = &fakeMailer{}
	e = newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://x.example", MailLimits: &MailLimits{}})
	c = e.client()
	for i := 0; i < 50; i++ {
		e.post(c, "/events", url.Values{"title": {"T"}, "admin_email": {"a@example.org"}})
	}
	if n := len(fm.take()); n != 50 {
		t.Errorf("unlimited: %d mails, want 50", n)
	}
}
