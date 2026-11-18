package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"benevole-ape/internal/store"
)

type env struct {
	t     *testing.T
	ts    *httptest.Server
	store *store.Store
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	// The tests use fixed dates in June 2026: validate them against a fixed
	// clock so they keep passing in the future.
	now = func() time.Time { return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if cfg.PostsPerMinute == 0 {
		cfg.PostsPerMinute = 10000
	}
	srv, err := New(st, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); st.Close() })
	return &env{t: t, ts: ts, store: st}
}

// client returns a browser-like client with its own cookie jar that does
// not follow redirects automatically.
func (e *env) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type resp struct {
	code     int
	body     string
	location string
	header   http.Header
}

func (e *env) do(c *http.Client, method, path string, form url.Values, hdr map[string]string) resp {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{code: res.StatusCode, body: string(b), location: res.Header.Get("Location"), header: res.Header}
}

func (e *env) get(c *http.Client, path string) resp { return e.do(c, "GET", path, nil, nil) }
func (e *env) post(c *http.Client, path string, form url.Values) resp {
	return e.do(c, "POST", path, form, nil)
}

func expect(t *testing.T, r resp, code int, contains ...string) {
	t.Helper()
	if r.code != code {
		t.Fatalf("want status %d, got %d (location %q)\n%s", code, r.code, r.location, r.body)
	}
	for _, s := range contains {
		if !strings.Contains(r.body, s) {
			t.Fatalf("response does not contain %q:\n%s", s, r.body)
		}
	}
}

var slotLinkRe = regexp.MustCompile(`/slots/(\d+)/signup`)

// setupEvent creates an open event with "Pêche aux canards" (14h-17h, 2/h,
// max 2) and returns admin path, public path and slot ids.
func setupEvent(t *testing.T, e *env, admin *http.Client) (adminPath, publicPath string, slots []string) {
	t.Helper()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Fête de l'école"}, "date": {"2026-06-20"}, "location": {"Cour"}})
	expect(t, r, http.StatusSeeOther)
	adminPath = r.location
	if !strings.HasPrefix(adminPath, "/a/") {
		t.Fatalf("unexpected redirect %q", adminPath)
	}
	r = e.get(admin, adminPath)
	expect(t, r, 200, "Événement créé", "Fête de l&#39;école", "Brouillon")
	m := regexp.MustCompile(`href="(/e/[^"]+)" target="_blank"`).FindStringSubmatch(r.body)
	if m == nil {
		t.Fatal("public link not found on admin page")
	}
	publicPath = m[1]

	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Pêche aux canards"}, "color": {"#d9534f"}}), http.StatusSeeOther)
	r = e.get(admin, adminPath)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(r.body)[1]
	r = e.post(admin, adminPath+"/stands/"+standID+"/slots", url.Values{
		"date": {"2026-06-20"}, "from": {"14:00"}, "to": {"17:00"}, "step": {"60"}, "min": {"2"}, "max": {"2"},
	})
	expect(t, r, http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "3 créneau(x) ajouté(s)", "il manque 6")

	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)

	r = e.get(e.client(), publicPath)
	expect(t, r, 200, "Pêche aux canards", "0/2", "manque encore 6 personnes", "st-empty")
	for _, m := range slotLinkRe.FindAllStringSubmatch(r.body, -1) {
		if len(slots) == 0 || slots[len(slots)-1] != m[1] {
			slots = append(slots, m[1])
		}
	}
	// grid + timeline both list the slots; keep unique ids
	seen := map[string]bool{}
	uniq := slots[:0]
	for _, s := range slots {
		if !seen[s] {
			seen[s] = true
			uniq = append(uniq, s)
		}
	}
	if len(uniq) != 3 {
		t.Fatalf("want 3 slots, got %v", uniq)
	}
	return adminPath, publicPath, uniq
}

// openEventWithSlot creates an event from form (dated 2026-06-20), adds a
// "Buvette" stand with one 14h–15h slot for 2 people, opens the event, and
// returns the admin path, the public path and the slot ID.
func openEventWithSlot(t *testing.T, e *env, admin *http.Client, form url.Values) (adminPath, pub, slot string) {
	t.Helper()
	form.Set("date", "2026-06-20")
	r := e.post(admin, "/events", form)
	expect(t, r, http.StatusSeeOther)
	adminPath = r.location
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Buvette"}}), http.StatusSeeOther)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	expect(t, e.post(admin, adminPath+"/stands/"+standID+"/slots", url.Values{"date": {"2026-06-20"}, "from": {"14:00"}, "to": {"15:00"}, "step": {"60"}, "min": {"2"}}), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	pub = regexp.MustCompile(`href="(/e/[^"]+)" target="_blank"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	slot = slotLinkRe.FindStringSubmatch(e.get(e.client(), pub).body)[1]
	return adminPath, pub, slot
}

func TestFullFlow(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slots := setupEvent(t, e, admin)

	marie := e.client()
	// Sign-up form, as an htmx partial and as a full page.
	expect(t, e.do(marie, "GET", pub+"/slots/"+slots[0]+"/signup", nil, map[string]string{"HX-Request": "true"}), 200, "<dialog", "Je m'inscris")
	expect(t, e.get(marie, pub+"/slots/"+slots[0]+"/signup"), 200, "<!doctype html>", "Prénom et nom")

	// Volunteers are not asked for an e-mail; only the name is required.
	if strings.Contains(e.get(marie, pub+"/slots/"+slots[0]+"/signup").body, `name="email"`) {
		t.Error("the sign-up form still has an e-mail field")
	}
	expect(t, e.post(marie, pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {" "}}), 400, "votre nom")

	// First sign-up: redirected to the personal page with welcome message.
	// An e-mail sent anyway is ignored.
	r := e.post(marie, pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Marie Dupont"}, "phone": {"07 11 22 33 44"}, "email": {"marie@example.org"}})
	expect(t, r, http.StatusSeeOther)
	if !strings.Contains(r.location, "/me/") || !strings.Contains(r.location, "welcome=1") {
		t.Fatalf("unexpected redirect %q", r.location)
	}
	mePath := strings.TrimSuffix(r.location, "?welcome=1")
	expect(t, e.get(marie, r.location), 200, "Merci Marie Dupont", "Mes créneaux", "14h – 15h")

	// Public page shows "Marie D." and never the e-mail.
	r = e.get(e.client(), pub)
	expect(t, r, 200, "Marie D.", "1/2", "st-partial")
	if strings.Contains(r.body, "marie@example.org") || strings.Contains(r.body, "Dupont") {
		t.Fatal("public page leaks personal data")
	}

	// Second sign-up by Marie (cookie): flash + back to stand tab.
	r = e.post(marie, pub+"/slots/"+slots[1]+"/signup", url.Values{"name": {"Marie Dupont"}, "phone": {"07 11 22 33 44"}})
	expect(t, r, http.StatusSeeOther)
	if !strings.Contains(r.location, "stand=") {
		t.Fatalf("expected stand tab redirect, got %q", r.location)
	}
	expect(t, e.get(marie, r.location), 200, "C&#39;est noté", "(vous)")

	// Duplicate on same slot is refused.
	expect(t, e.post(marie, pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Marie Dupont"}, "phone": {"07 11 22 33 44"}}), 409, "déjà inscrit")

	// Paul fills slot 0; Léa is refused (max 2).
	expect(t, e.post(e.client(), pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Paul Martin"}, "phone": {"06 12 34 56 78"}}), http.StatusSeeOther)
	expect(t, e.post(e.client(), pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Léa"}, "phone": {"0600000000"}}), 409, "complet")
	expect(t, e.get(e.client(), pub), 200, "2/2", "st-full")

	// Stand tab and filter.
	r = e.get(e.client(), pub+"?todo=1")
	expect(t, r, 200, "Créneaux à compléter")

	// Admin sees contact details; CSV and print view work.
	r = e.get(admin, adminPath)
	expect(t, r, 200, "07 11 22 33 44", "06 12 34 56 78", "Marie Dupont")
	if strings.Contains(r.body, "marie@example.org") {
		t.Error("an e-mail posted by a volunteer was stored")
	}
	r = e.get(admin, adminPath+"/export.csv")
	expect(t, r, 200, "Stand;Date;Début;Fin;Nom;Téléphone;Commentaire", "Pêche aux canards;20/06/2026;14:00;15:00;Marie Dupont;07 11 22 33 44", "(place libre)")
	if strings.Contains(r.body, "E-mail") {
		t.Error("the CSV still has an e-mail column")
	}
	if !strings.HasPrefix(r.body, "\ufeff") || r.header.Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Error("CSV must start with a BOM and have the right content type")
	}
	expect(t, e.get(admin, adminPath+"/print"), 200, "Pêche aux canards", "Paul Martin", "class=\"page\"")

	// Marie cancels her second slot from her personal page.
	r = e.get(marie, mePath)
	m := regexp.MustCompile(`action="(`+regexp.QuoteMeta(mePath)+`/signups/\d+/delete)"`).FindAllStringSubmatch(r.body, -1)
	if len(m) != 2 {
		t.Fatalf("want 2 cancel forms, got %d", len(m))
	}
	expect(t, e.post(marie, m[1][1], nil), http.StatusSeeOther)
	expect(t, e.get(marie, mePath), 200, "Votre inscription a été annulée")

	// Someone else cannot cancel Marie's sign-up through their own link.
	expect(t, e.post(e.client(), strings.Replace(m[0][1], mePath, pub+"/me/not-a-token", 1), nil), 404)

	// Closing the event blocks sign-ups.
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"closed"}}), http.StatusSeeOther)
	expect(t, e.post(e.client(), pub+"/slots/"+slots[2]+"/signup", url.Values{"name": {"Zoé"}, "phone": {"0600000000"}}), 409, "pas ouvertes")
	expect(t, e.get(e.client(), pub), 200, "Les inscriptions sont closes")

	// Admin can still add someone manually.
	expect(t, e.post(admin, adminPath+"/signups", url.Values{"slot": {slots[2]}, "name": {"Grand-père Jules"}}), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Grand-père Jules a été inscrit")

	// Purge removes everybody.
	expect(t, e.post(admin, adminPath+"/purge", url.Values{"confirm": {"yes"}}), http.StatusSeeOther)
	r = e.get(admin, adminPath)
	expect(t, r, 200, "Données personnelles effacées (3 bénévole(s))")
	if strings.Contains(r.body, "Marie Dupont") {
		t.Error("purge left personal data")
	}
}

func TestAdminTokenGating(t *testing.T) {
	e := newEnv(t, Config{})
	c := e.client()
	adminPath, _, _ := setupEvent(t, e, c)
	for _, p := range []string{"/a/nope", "/a/nope/export.csv", "/a/nope/print"} {
		expect(t, e.get(c, p), 404)
	}
	expect(t, e.post(c, "/a/nope/stands", url.Values{"name": {"X"}}), 404)
	expect(t, e.get(c, adminPath), 200)
	r := e.get(c, adminPath)
	if r.header.Get("Cache-Control") != "no-store" {
		t.Error("admin pages must not be cached")
	}
}

func TestCrossEventIDs(t *testing.T) {
	e := newEnv(t, Config{})
	c := e.client()
	admin1, _, slots1 := setupEvent(t, e, c)
	admin2, pub2, _ := setupEvent(t, e, c)
	// Slot of event 1 through event 2 (public and admin).
	expect(t, e.get(c, pub2+"/slots/"+slots1[0]+"/signup"), 404)
	expect(t, e.post(c, admin2+"/slots/"+slots1[0]+"/delete", nil), 404)
	expect(t, e.get(c, admin1), 200, "0/2")
}

func TestCSRF(t *testing.T) {
	e := newEnv(t, Config{})
	c := e.client()
	form := url.Values{"title": {"Evil"}, "admin_email": {"orga@example.org"}}
	expect(t, e.do(c, "POST", "/events", form, map[string]string{"Sec-Fetch-Site": "cross-site"}), 403)
	expect(t, e.do(c, "POST", "/events", form, map[string]string{"Origin": "https://evil.example"}), 403)
	expect(t, e.do(c, "POST", "/events", form, map[string]string{"Sec-Fetch-Site": "same-origin"}), http.StatusSeeOther)
	u, _ := url.Parse(e.ts.URL)
	expect(t, e.do(c, "POST", "/events", form, map[string]string{"Origin": "http://" + u.Host}), http.StatusSeeOther)
}

func TestCreatePassword(t *testing.T) {
	e := newEnv(t, Config{CreatePassword: "secret"})
	c := e.client()
	expect(t, e.get(c, "/"), 200, "Mot de passe de création")
	expect(t, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"X"}, "password": {"bad"}}), 400, "incorrect")
	expect(t, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"X"}, "password": {"secret"}}), http.StatusSeeOther)
}

func TestRateLimit(t *testing.T) {
	e := newEnv(t, Config{PostsPerMinute: 4}) // burst 2
	c := e.client()
	codes := []int{}
	for i := 0; i < 4; i++ {
		codes = append(codes, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"X"}}).code)
	}
	if codes[0] != http.StatusSeeOther || codes[3] != http.StatusTooManyRequests {
		t.Errorf("unexpected codes %v", codes)
	}
}

func TestDuplicateAndDelete(t *testing.T) {
	e := newEnv(t, Config{})
	c := e.client()
	adminPath, pub, _ := setupEvent(t, e, c)
	r := e.post(c, adminPath+"/duplicate", url.Values{"title": {"Fête 2027"}, "date": {"2027-06-19"}})
	expect(t, r, http.StatusSeeOther)
	if r.location == adminPath {
		t.Fatal("duplicate must get a new admin link")
	}
	expect(t, e.get(c, r.location), 200, "Fête 2027", "samedi 19 juin 2027", "Pêche aux canards")

	expect(t, e.post(c, adminPath+"/delete", nil), http.StatusSeeOther) // no confirm: refused
	expect(t, e.get(c, pub), 200)
	expect(t, e.post(c, adminPath+"/delete", url.Values{"confirm": {"yes"}}), http.StatusSeeOther)
	expect(t, e.get(c, pub), 404)
	expect(t, e.get(c, adminPath), 404)
}

func TestMisc(t *testing.T) {
	e := newEnv(t, Config{})
	c := e.client()
	expect(t, e.get(c, "/healthz"), 200, "ok")
	expect(t, e.get(c, "/nope"), 404, "n&#39;existe pas")
	expect(t, e.get(c, "/e/unknown"), 404)
	r := e.get(c, "/static/app.css")
	expect(t, r, 200, "--primary")
	if !strings.Contains(r.header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("missing CSP")
	}
	if got := redactPath("/a/SECRET/stands"); got != "/a/***/stands" {
		t.Errorf("redact admin: %s", got)
	}
	if got := redactPath("/e/fete-x/me/SECRET/signups/1/delete"); got != "/e/fete-x/me/***/signups/1/delete" {
		t.Errorf("redact me: %s", got)
	}
}

func TestRetentionNotice(t *testing.T) {
	e := newEnv(t, Config{RetentionDays: 60})
	admin := e.client()
	adminPath, _, _ := setupEvent(t, e, admin)
	expect(t, e.get(admin, adminPath), 200, "supprimés automatiquement le", "60 jours après l'événement")

	e = newEnv(t, Config{})
	admin = e.client()
	adminPath, _, _ = setupEvent(t, e, admin)
	if r := e.get(admin, adminPath); strings.Contains(r.body, "supprimés automatiquement") {
		t.Error("retention notice shown although retention is disabled")
	}
}

// A name is enough: no phone, no e-mail.
func TestSignupNameOnly(t *testing.T) {
	e := newEnv(t, Config{})
	_, pub, slots := setupEvent(t, e, e.client())
	c := e.client()
	r := e.post(c, pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Jules"}})
	expect(t, r, http.StatusSeeOther)
	expect(t, e.get(c, r.location), 200, "Merci Jules", "Téléphone (facultatif)")
}
