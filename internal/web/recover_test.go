package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// recoverNotice is in the answer to every request, whether or not the
// address belongs to an organiser.
const recoverNotice = "un e-mail avec ses liens"

func recoverEnv(t *testing.T, limits *MailLimits) (*env, *fakeMailer) {
	t.Helper()
	fm := &fakeMailer{}
	// TrustProxy lets a test choose the address of the client with the
	// X-Forwarded-For header: see postFrom.
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, TrustProxy: true, BaseURL: "https://jeviensaider.example", MailLimits: limits})
	return e, fm
}

// postFrom posts a form as a client that has the address ip.
func (e *env) postFrom(c *http.Client, ip, path string, form url.Values) resp {
	return e.do(c, "POST", path, form, map[string]string{"X-Forwarded-For": ip})
}

// createFor creates an event for an organiser and returns its admin path.
func createFor(t *testing.T, e *env, title, email string) string {
	t.Helper()
	r := e.post(e.client(), "/events", url.Values{"title": {title}, "admin_email": {email}})
	expect(t, r, http.StatusSeeOther)
	return r.location
}

func TestRecoverNeedsMail(t *testing.T) {
	for name, cfg := range map[string]Config{
		"no mailer":              {},
		"mailer, mails disabled": {Mailer: &fakeMailer{}},
		// Without a public URL, the links would take their host from the
		// request, which a visitor controls: the page does not exist.
		"mails enabled, no public URL": {Mailer: &fakeMailer{}, MailEnabled: true},
	} {
		e := newEnv(t, cfg)
		c := e.client()
		expect(t, e.get(c, "/recover"), 404)
		expect(t, e.post(c, "/recover", url.Values{"email": {"orga@example.org"}}), 404)
		if strings.Contains(e.get(c, "/").body, "/recover") {
			t.Errorf("%s: the home page links to a page that does not exist", name)
		}
	}

	e, _ := recoverEnv(t, nil)
	c := e.client()
	expect(t, e.get(c, "/recover"), 200, `name="email"`, `action="/recover"`)
	expect(t, e.get(c, "/"), 200, `href="/recover"`)
}

func TestRecoverSendsAdminLinks(t *testing.T) {
	e, fm := recoverEnv(t, nil)
	a := createFor(t, e, "Fête de l'école", "orga@example.org")
	b := createFor(t, e, "Marché de Noël", "Orga@example.org")
	other := createFor(t, e, "Kermesse", "autre@example.org")
	fm.take()

	// The address is matched without regard to case and spaces.
	c := e.client()
	expect(t, e.post(c, "/recover", url.Values{"email": {"  ORGA@example.org "}}), http.StatusSeeOther)
	expect(t, e.get(c, "/recover"), 200, recoverNotice)

	msgs := fm.take()
	if len(msgs) != 1 {
		t.Fatalf("%d messages, want 1", len(msgs))
	}
	m := msgs[0]
	// The mail goes to the address stored with the most recent event, never to
	// a string that a visitor typed in this form.
	if m.To != "Orga@example.org" {
		t.Errorf("To = %q", m.To)
	}
	if m.Subject != "Vos liens d'administration" {
		t.Errorf("Subject = %q", m.Subject)
	}
	for _, want := range []string{"Fête de l'école", "Marché de Noël", "https://jeviensaider.example" + a, "https://jeviensaider.example" + b} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("mail lacks %q:\n%s", want, m.Body)
		}
	}
	for _, not := range []string{"Kermesse", strings.TrimPrefix(other, "/a/")} {
		if strings.Contains(m.Body, not) {
			t.Errorf("mail holds %q, which belongs to another organiser:\n%s", not, m.Body)
		}
	}
	// The most recent event comes first.
	if strings.Index(m.Body, "Marché de Noël") > strings.Index(m.Body, "Fête de l'école") {
		t.Errorf("events are not in order:\n%s", m.Body)
	}
}

// The page must not tell whether an address belongs to an organiser.
func TestRecoverAnswerIsTheSameForAnyAddress(t *testing.T) {
	e, fm := recoverEnv(t, nil)
	createFor(t, e, "Fête", "orga@example.org")
	fm.take()

	known, unknown := e.client(), e.client()
	r1 := e.post(known, "/recover", url.Values{"email": {"orga@example.org"}})
	r2 := e.post(unknown, "/recover", url.Values{"email": {"personne@example.org"}})
	if r1.code != r2.code || r1.location != r2.location ||
		strings.Join(r1.header.Values("Set-Cookie"), "|") != strings.Join(r2.header.Values("Set-Cookie"), "|") {
		t.Errorf("the two answers differ:\n%+v\n%+v", r1, r2)
	}
	p1, p2 := e.get(known, r1.location), e.get(unknown, r2.location)
	expect(t, p1, 200, recoverNotice)
	if p1.body != p2.body {
		t.Errorf("the two pages differ:\n%s\n----\n%s", p1.body, p2.body)
	}

	msgs := fm.take()
	if len(msgs) != 1 || msgs[0].To != "orga@example.org" {
		t.Errorf("messages: %+v", msgs)
	}
}

func TestRecoverInvalidAddress(t *testing.T) {
	e, fm := recoverEnv(t, nil)
	createFor(t, e, "Fête", "orga@example.org")
	fm.take()
	c := e.client()
	expect(t, e.post(c, "/recover", url.Values{"email": {""}}), http.StatusBadRequest, "Indiquez")
	expect(t, e.post(c, "/recover", url.Values{"email": {"pas une adresse"}}), http.StatusBadRequest, "ne semble pas valide")
	expect(t, e.post(c, "/recover", url.Values{"email": {"orga@example.org?bcc=z@evil.example"}}), http.StatusBadRequest, "ne semble pas valide")
	if msgs := fm.take(); len(msgs) != 0 {
		t.Errorf("messages for invalid addresses: %+v", msgs)
	}
}

func TestRecoverLimitPerAddress(t *testing.T) {
	// One client sends all the requests here, so the limit of one client is
	// off: it has its own test.
	limits := DefaultMailLimits()
	limits.RecoverPerAddressPerClientPerDay = 0
	e, fm := recoverEnv(t, &limits)
	limit := limits.RecoverPerAddressPerDay
	createFor(t, e, "Fête", "Victim@example.org")
	fm.take()

	c := e.client()
	for i := 0; i < limit+2; i++ {
		// The answer does not change when the limit is reached.
		r := e.post(c, "/recover", url.Values{"email": {"victim@example.org"}})
		expect(t, r, http.StatusSeeOther)
	}
	if n := len(fm.take()); n != limit {
		t.Errorf("%d mails to one address, want %d", n, limit)
	}

	// A day later, the address can ask again.
	e.setClock(time.Date(2026, 5, 2, 12, 0, 1, 0, time.UTC))
	expect(t, e.post(c, "/recover", url.Values{"email": {"victim@example.org"}}), http.StatusSeeOther)
	if n := len(fm.take()); n != 1 {
		t.Errorf("%d mails the next day, want 1", n)
	}
}

// Anyone can ask for the mail of any address. One client can use only one
// of the daily mails of an address, so that a client that sends many requests
// cannot use up the quota of the organiser.
func TestRecoverOneClientCannotUseUpTheAddress(t *testing.T) {
	e, fm := recoverEnv(t, nil)
	createFor(t, e, "Fête", "victim@example.org")
	fm.take()
	form := url.Values{"email": {"victim@example.org"}}

	attacker, organiser := e.client(), e.client()
	for i := 0; i < 3; i++ {
		expect(t, e.postFrom(attacker, "203.0.113.1", "/recover", form), http.StatusSeeOther)
	}
	expect(t, e.postFrom(organiser, "203.0.113.2", "/recover", form), http.StatusSeeOther)
	if n := len(fm.take()); n != 2 {
		t.Errorf("%d mails, want 2: one for the first client and one for the organiser", n)
	}
}

// The limit per address stays the hard limit against a flood of mails: many
// clients still cannot send more than it allows.
func TestRecoverManyClientsStillHitTheAddressLimit(t *testing.T) {
	e, fm := recoverEnv(t, nil)
	createFor(t, e, "Fête", "victim@example.org")
	fm.take()
	limit := DefaultMailLimits().RecoverPerAddressPerDay

	for i := 1; i <= limit+3; i++ {
		ip := fmt.Sprintf("203.0.113.%d", i)
		expect(t, e.postFrom(e.client(), ip, "/recover", url.Values{"email": {"victim@example.org"}}), http.StatusSeeOther)
	}
	if n := len(fm.take()); n != limit {
		t.Errorf("%d mails to one address from many clients, want %d", n, limit)
	}
}

func TestRecoverLimitPerHourForAllAddresses(t *testing.T) {
	// Limits that are not set here are off.
	e, fm := recoverEnv(t, &MailLimits{RecoverPerAddressPerDay: 1, RecoverPerHour: 2})
	for _, to := range []string{"a@example.org", "b@example.org", "c@example.org"} {
		createFor(t, e, "Fête "+to, to)
	}
	fm.take()

	c := e.client()
	// A request refused for its address must not use the budget of the hour.
	for _, to := range []string{"a@example.org", "a@example.org", "a@example.org", "b@example.org", "c@example.org"} {
		expect(t, e.post(c, "/recover", url.Values{"email": {to}}), http.StatusSeeOther)
	}
	msgs := fm.take()
	if len(msgs) != 2 || msgs[0].To != "a@example.org" || msgs[1].To != "b@example.org" {
		t.Errorf("messages: %+v", msgs)
	}

	// An hour later, the budget is back.
	e.setClock(time.Date(2026, 5, 1, 13, 0, 1, 0, time.UTC))
	expect(t, e.post(c, "/recover", url.Values{"email": {"c@example.org"}}), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 1 || msgs[0].To != "c@example.org" {
		t.Errorf("after an hour: %+v", msgs)
	}
}

// When the limit of all the mails refuses a recovery mail, the address keeps
// its quota: the organiser can ask again later.
func TestRecoverRefusedMailKeepsTheQuota(t *testing.T) {
	e, fm := recoverEnv(t, &MailLimits{PerHour: 1, RecoverPerAddressPerDay: 1, RecoverPerHour: 1})
	createFor(t, e, "Fête", "a@example.org") // uses the only mail of the hour
	fm.take()

	c := e.client()
	expect(t, e.post(c, "/recover", url.Values{"email": {"a@example.org"}}), http.StatusSeeOther)
	if n := len(fm.take()); n != 0 {
		t.Fatalf("%d mails although the hour is full", n)
	}

	e.setClock(time.Date(2026, 5, 1, 13, 0, 1, 0, time.UTC))
	expect(t, e.post(c, "/recover", url.Values{"email": {"a@example.org"}}), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 1 || msgs[0].To != "a@example.org" {
		t.Errorf("an hour later: %+v", msgs)
	}
}

// The recovery mails have their own budget: they must not use the one of
// the sign-up notifications.
func TestRecoverBudgetIsSeparate(t *testing.T) {
	e, fm := recoverEnv(t, &MailLimits{RecoverPerHour: 1})
	createFor(t, e, "Fête A", "a@example.org")
	createFor(t, e, "Fête B", "b@example.org")
	fm.take()
	c := e.client()
	for _, to := range []string{"a@example.org", "b@example.org"} {
		expect(t, e.post(c, "/recover", url.Values{"email": {to}}), http.StatusSeeOther)
	}
	if n := len(fm.take()); n != 1 {
		t.Fatalf("%d recovery mails, want 1", n)
	}
	// The creation of an event still sends its mail.
	createFor(t, e, "Fête C", "c@example.org")
	if msgs := fm.take(); len(msgs) != 1 || msgs[0].To != "c@example.org" {
		t.Errorf("creation mail: %+v", msgs)
	}
}

// The limit of one client is for one address, and it ignores the case of the
// address, like the other limits.
func TestRecoverClientLimitIsPerAddress(t *testing.T) {
	e, fm := recoverEnv(t, nil)
	createFor(t, e, "Fête A", "a@example.org")
	createFor(t, e, "Fête B", "b@example.org")
	fm.take()

	c := e.client()
	for _, to := range []string{"a@example.org", "A@Example.org", "b@example.org"} {
		expect(t, e.postFrom(c, "203.0.113.1", "/recover", url.Values{"email": {to}}), http.StatusSeeOther)
	}
	msgs := fm.take()
	if len(msgs) != 2 || msgs[0].To != "a@example.org" || msgs[1].To != "b@example.org" {
		t.Errorf("messages: %+v", msgs)
	}
}

// When the limit of all the mails refuses a recovery mail, the client keeps
// its quota for the address too: it can ask again later.
func TestRecoverRefusedMailKeepsTheQuotaOfTheClient(t *testing.T) {
	e, fm := recoverEnv(t, &MailLimits{PerHour: 1, RecoverPerAddressPerClientPerDay: 1})
	createFor(t, e, "Fête", "a@example.org") // uses the only mail of the hour
	fm.take()

	c := e.client()
	expect(t, e.postFrom(c, "203.0.113.1", "/recover", url.Values{"email": {"a@example.org"}}), http.StatusSeeOther)
	if n := len(fm.take()); n != 0 {
		t.Fatalf("%d mails although the hour is full", n)
	}

	e.setClock(time.Date(2026, 5, 1, 13, 0, 1, 0, time.UTC))
	expect(t, e.postFrom(c, "203.0.113.1", "/recover", url.Values{"email": {"a@example.org"}}), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 1 || msgs[0].To != "a@example.org" {
		t.Errorf("an hour later: %+v", msgs)
	}
}

// An invalid address counts against the limit of the client like any other
// request. If it did not, a client could send unlimited requests that way.
func TestRecoverInvalidRequestsCountAgainstTheClientLimit(t *testing.T) {
	e, _ := recoverEnv(t, &MailLimits{RecoverPerIPPerHour: 2})
	c := e.client()
	expect(t, e.post(c, "/recover", url.Values{"email": {""}}), http.StatusBadRequest)
	expect(t, e.post(c, "/recover", url.Values{"email": {"pas une adresse"}}), http.StatusBadRequest)
	r := e.post(c, "/recover", url.Values{"email": {"pas une adresse"}})
	expect(t, r, http.StatusTooManyRequests, "Trop de demandes")
	if r.header.Get("Retry-After") == "" {
		t.Error("no Retry-After header on the 429 answer")
	}
}

func TestRecoverLimitPerIP(t *testing.T) {
	e, fm := recoverEnv(t, &MailLimits{RecoverPerIPPerHour: 2})
	c := e.client()
	// Unknown addresses count too: the limit is on the visitor.
	for i, want := range []int{http.StatusSeeOther, http.StatusSeeOther, http.StatusTooManyRequests} {
		r := e.post(c, "/recover", url.Values{"email": {fmt.Sprintf("x%d@example.org", i)}})
		expect(t, r, want)
		if want == http.StatusTooManyRequests && !strings.Contains(r.body, "Trop de demandes") {
			t.Errorf("429 page: %s", r.body)
		}
	}
	// Reading the form is not limited.
	expect(t, e.get(c, "/recover"), 200)
	if n := len(fm.take()); n != 0 {
		t.Errorf("%d mails for unknown addresses", n)
	}

	e.setClock(time.Date(2026, 5, 1, 13, 0, 1, 0, time.UTC))
	expect(t, e.post(c, "/recover", url.Values{"email": {"x@example.org"}}), http.StatusSeeOther)
}

func TestRecoverListsTwentyEventsAtMost(t *testing.T) {
	e, fm := recoverEnv(t, &MailLimits{})
	for i := 1; i <= 21; i++ {
		createFor(t, e, fmt.Sprintf("Evt %02d", i), "orga@example.org")
	}
	fm.take()
	expect(t, e.post(e.client(), "/recover", url.Values{"email": {"orga@example.org"}}), http.StatusSeeOther)
	msgs := fm.take()
	if len(msgs) != 1 {
		t.Fatalf("%d messages, want 1", len(msgs))
	}
	body := msgs[0].Body
	if n := strings.Count(body, "https://jeviensaider.example/a/"); n != 20 {
		t.Errorf("%d links, want 20:\n%s", n, body)
	}
	if !strings.Contains(body, "Evt 21") || strings.Contains(body, "Evt 01") {
		t.Errorf("the 20 most recent events must be listed:\n%s", body)
	}
	if !strings.Contains(body, "les 20 événements les plus récents") {
		t.Errorf("the mail does not say that the list is cut:\n%s", body)
	}

	// With 20 events or fewer, nothing is said.
	e, fm = recoverEnv(t, &MailLimits{})
	createFor(t, e, "Evt A", "few@example.org")
	fm.take()
	expect(t, e.post(e.client(), "/recover", url.Values{"email": {"few@example.org"}}), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 1 || strings.Contains(msgs[0].Body, "les plus récents") {
		t.Errorf("short list: %+v", msgs)
	}
}

func TestRecoverSkipsDeletedEvents(t *testing.T) {
	e, fm := recoverEnv(t, nil)
	admin := e.client()
	a := createFor(t, e, "Fête supprimée", "orga@example.org")
	createFor(t, e, "Fête gardée", "orga@example.org")
	fm.take()
	expect(t, e.post(admin, a+"/delete", url.Values{"confirm": {"yes"}}), http.StatusSeeOther)
	expect(t, e.post(e.client(), "/recover", url.Values{"email": {"orga@example.org"}}), http.StatusSeeOther)
	msgs := fm.take()
	if len(msgs) != 1 || strings.Contains(msgs[0].Body, "Fête supprimée") || !strings.Contains(msgs[0].Body, "Fête gardée") {
		t.Errorf("messages: %+v", msgs)
	}
}
