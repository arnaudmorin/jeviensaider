package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBodyLimit(t *testing.T) {
	e := newEnv(t, Config{})
	c := e.client()
	big := url.Values{"title": {strings.Repeat("x", maxBodyBytes+1)}}
	expect(t, e.post(c, "/events", big), http.StatusRequestEntityTooLarge)

	// Without Content-Length (chunked), the body is cut at the limit: the
	// form arrives empty and is refused, nothing is created.
	req, _ := http.NewRequest("POST", e.ts.URL+"/events", strings.NewReader(big.Encode()))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode < 400 {
		t.Errorf("chunked oversized body: status %d", res.StatusCode)
	}
}

func TestClientIP(t *testing.T) {
	direct := &Server{cfg: Config{}}
	proxied := &Server{cfg: Config{TrustProxy: true}}
	req := func(remote string, xff ...string) *http.Request {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = remote
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	cases := []struct {
		s    *Server
		r    *http.Request
		want string
	}{
		{direct, req("203.0.113.7:1234", "10.0.0.1"), "203.0.113.7"},
		// Only the entry appended by the proxy (the last one) is trusted.
		{proxied, req("127.0.0.1:1234", "10.0.9.1, 203.0.113.7"), "203.0.113.7"},
		{proxied, req("127.0.0.1:1234", "10.0.9.1", "203.0.113.8"), "203.0.113.8"},
		{proxied, req("127.0.0.1:1234"), "127.0.0.1"},
		// IPv6 clients are grouped by /64.
		{direct, req("[2001:db8:1:2:aaaa::1]:1234"), "2001:db8:1:2::/64"},
		{proxied, req("127.0.0.1:1234", "2001:db8:1:2:bbbb::9"), "2001:db8:1:2::/64"},
	}
	for i, c := range cases {
		if got := c.s.clientIP(c.r); got != c.want {
			t.Errorf("case %d: clientIP = %q, want %q", i, got, c.want)
		}
	}
}

func TestSpoofedForwardedForIsLimited(t *testing.T) {
	e := newEnv(t, Config{TrustProxy: true, PostsPerMinute: 4}) // burst 2
	c := e.client()
	limited := false
	for i := 0; i < 5; i++ {
		r := e.do(c, "POST", "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"x"}},
			map[string]string{"X-Forwarded-For": "10.0." + strconv.Itoa(i) + ".1, 127.0.0.1"})
		limited = limited || r.code == http.StatusTooManyRequests
	}
	if !limited {
		t.Error("varying the first X-Forwarded-For entry bypassed the rate limit")
	}
}

func TestLimiterIsBounded(t *testing.T) {
	l := newLimiter(60)
	now := time.Now()
	for i := 0; i < maxBuckets; i++ {
		l.allow(strconv.Itoa(i), now)
	}
	if l.allow("newcomer", now) {
		t.Error("a new key was accepted although the limiter is full of active buckets")
	}
	if len(l.buckets) > maxBuckets {
		t.Errorf("limiter grew to %d buckets", len(l.buckets))
	}
	// Once the buckets have refilled, they are forgotten to make room.
	if !l.allow("newcomer", now.Add(time.Minute)) {
		t.Error("a new key was refused although every bucket had refilled")
	}
}

func TestHTTPSHeaders(t *testing.T) {
	e := newEnv(t, Config{BaseURL: "https://example.org"})
	r := e.post(e.client(), "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Fête"}})
	expect(t, r, http.StatusSeeOther)
	if !strings.HasPrefix(r.header.Get("Strict-Transport-Security"), "max-age=") {
		t.Error("missing HSTS")
	}
	cookies := r.header.Values("Set-Cookie")
	if len(cookies) == 0 {
		t.Fatal("no cookie set")
	}
	for _, c := range cookies {
		if !strings.Contains(c, "; Secure") {
			t.Errorf("cookie without Secure: %s", c)
		}
	}

	// Plain HTTP without an https BaseURL: no HSTS, no Secure (it would
	// break the app).
	e = newEnv(t, Config{})
	r = e.post(e.client(), "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Fête"}})
	if r.header.Get("Strict-Transport-Security") != "" || strings.Contains(strings.Join(r.header.Values("Set-Cookie"), "\n"), "Secure") {
		t.Error("HSTS or Secure sent over plain HTTP")
	}
}

func TestPersonalPagesNotCached(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	_, publicPath, slots := setupEvent(t, e, admin)
	for _, p := range []string{publicPath, publicPath + "/slots/" + slots[0] + "/signup"} {
		if got := e.get(e.client(), p).header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", p, got)
		}
	}
	expect(t, e.get(e.client(), publicPath), 200, `"historyCacheSize":0`)
}

func TestTooManyStandsMessage(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Big"}})
	adminPath := r.location
	for i := 0; i < 50; i++ {
		expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"S" + strconv.Itoa(i)}}), http.StatusSeeOther)
	}
	r = e.post(admin, adminPath+"/stands", url.Values{"name": {"One too many"}})
	if r.code != http.StatusSeeOther && r.code != http.StatusBadRequest {
		t.Fatalf("51st stand: status %d", r.code)
	}
	expect(t, e.get(admin, adminPath), 200, "pas avoir plus de 50 stands")
}

// The attack from the security review: Mallory shares her personal link,
// Alice opens it and then signs up. Mallory must not see Alice's details.
func TestPlantedPersonalLink(t *testing.T) {
	e := newEnv(t, Config{})
	_, publicPath, slots := setupEvent(t, e, e.client())

	mallory := e.client()
	r := e.post(mallory, publicPath+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Mallory"}, "email": {"m@evil.test"}})
	expect(t, r, http.StatusSeeOther)
	malloryLink := strings.TrimSuffix(r.location, "?welcome=1")

	alice := e.client()
	expect(t, e.get(alice, malloryLink), 200) // the browser now remembers Mallory
	r = e.post(alice, publicPath+"/slots/"+slots[1]+"/signup",
		url.Values{"name": {"Alice Victim"}, "email": {"alice@home.test"}, "phone": {"0601020304"}})
	expect(t, r, http.StatusSeeOther)
	if strings.HasPrefix(r.location, malloryLink) {
		t.Fatal("Alice was signed up as Mallory")
	}

	page := e.get(mallory, malloryLink)
	expect(t, page, 200, "Mallory")
	for _, leak := range []string{"alice@home.test", "0601020304"} {
		if strings.Contains(page.body, leak) {
			t.Errorf("Mallory's page shows %q", leak)
		}
	}
	if strings.Contains(page.body, `value="Alice Victim"`) {
		t.Error("Mallory's record was renamed to Alice")
	}
}

func TestValidEmail(t *testing.T) {
	for _, ok := range []string{"marie@example.org", "marie.dupont+ape@ecole.fr", "o'brien@mail.co.uk", "a-b_c@sub-domain.example.fr"} {
		if !validEmail(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{
		"x@y.fr?bcc=evil@z.fr", "x@y.fr&body=hi", "x@y.fr#frag", "x%40y@z.fr", "a/b@c.fr", `"q"@c.fr`,
		"Marie <m@example.org>", "m@example", "m@@example.org", "@example.org", "m@example.org.", "m @example.org", "m@exa_mple.org",
	} {
		if validEmail(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDateLimits(t *testing.T) {
	e := newEnv(t, Config{RetentionDays: 60}) // clock fixed at 2026-05-01
	c := e.client()
	expect(t, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Far"}, "date": {"9999-12-31"}}), http.StatusBadRequest, "date hors limites")
	expect(t, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Old"}, "date": {"2025-04-30"}}), http.StatusBadRequest, "date hors limites")
	expect(t, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Soon"}, "date": {"2028-05-01"}}), http.StatusSeeOther)

	admin := e.client()
	adminPath, _, _ := setupEvent(t, e, admin)
	r := e.get(admin, adminPath)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(r.body)[1]
	e.post(admin, adminPath+"/stands/"+standID+"/slots", url.Values{
		"date": {"2099-06-20"}, "from": {"10:00"}, "to": {"11:00"}, "step": {"60"}, "min": {"1"},
	})
	expect(t, e.get(admin, adminPath), 200, "date hors limites")
}

func TestOverlapWarningAndMyPlanning(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, publicPath, slots := setupEvent(t, e, admin) // 14h, 15h, 16h on one stand
	marie := e.client()
	form := url.Values{"name": {"Marie Dupont"}, "email": {"m@example.org"}}
	r := e.post(marie, publicPath+"/slots/"+slots[0]+"/signup", form)
	expect(t, r, http.StatusSeeOther)
	meLink := strings.TrimSuffix(r.location, "?welcome=1")

	// "Mes créneaux" shows the overview table, restricted to my stands.
	r = e.get(marie, meLink)
	expect(t, r, 200, "Mon planning", "1 créneau · 1 h · 1 stand", `<table class="grid">`, `<span class="cell-me">Vous</span>`)
	// Only my slots: the stand's 15h and 16h slots are not shown, and mine
	// can be cancelled from the table.
	if mine := r.body[strings.Index(r.body, "Mon planning"):]; strings.Contains(mine, "15h<small>") || strings.Contains(mine, "16h<small>") {
		t.Error("slots I am not signed up for are in my planning")
	}
	expect(t, r, 200, `class="cell-cancel"`, "Annuler votre inscription : Pêche aux canards, 14h – 15h ?")
	// The next slot does not overlap: no warning.
	if strings.Contains(e.get(marie, publicPath+"/slots/"+slots[1]+"/signup").body, "même horaire") {
		t.Error("warning shown for a non-overlapping slot")
	}
	expect(t, e.post(marie, publicPath+"/slots/"+slots[1]+"/signup", form), http.StatusSeeOther)
	expect(t, e.get(marie, meLink), 200, "Mon planning", "2 créneaux · 2 h · 1 stand")

	// A second stand with a 14:30-15:30 slot, overlapping both.
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Buvette"}}), http.StatusSeeOther)
	ids := regexp.MustCompile(`id="stand-(\d+)"`).FindAllStringSubmatch(e.get(admin, adminPath).body, -1)
	buvette := ids[len(ids)-1][1]
	expect(t, e.post(admin, adminPath+"/stands/"+buvette+"/slots", url.Values{
		"date": {"2026-06-20"}, "from": {"14:30"}, "to": {"15:30"}, "step": {"0"}, "min": {"1"},
	}), http.StatusSeeOther)
	newest := 0 // the Buvette slot is the most recent one
	for _, m := range regexp.MustCompile(`/slots/(\d+)/signup`).FindAllStringSubmatch(e.get(marie, publicPath).body, -1) {
		if n, _ := strconv.Atoi(m[1]); n > newest {
			newest = n
		}
	}
	buvetteSlot := strconv.Itoa(newest)
	r = e.get(marie, publicPath+"/slots/"+buvetteSlot+"/signup")
	expect(t, r, 200, "même horaire", "Pêche aux canards", "14h – 15h", "15h – 16h", "M'inscrire quand même")

	// Warn, never block: the sign-up goes through, and the grid flags it.
	expect(t, e.post(marie, publicPath+"/slots/"+buvetteSlot+"/signup", form), http.StatusSeeOther)
	r = e.get(marie, meLink)
	expect(t, r, 200, "3 créneaux", "2 stands", "créneaux en même temps", "mine clash", "Vous ⚠")
	// Only my stands: a third stand where I am not signed up is not shown.
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Tombola"}}), http.StatusSeeOther)
	ids = regexp.MustCompile(`id="stand-(\d+)"`).FindAllStringSubmatch(e.get(admin, adminPath).body, -1)
	expect(t, e.post(admin, adminPath+"/stands/"+ids[len(ids)-1][1]+"/slots", url.Values{
		"date": {"2026-06-20"}, "from": {"10:00"}, "to": {"11:00"}, "step": {"0"}, "min": {"1"},
	}), http.StatusSeeOther)
	r = e.get(marie, meLink)
	if mine := r.body[strings.Index(r.body, "Mon planning"):]; strings.Contains(mine, "Tombola") {
		t.Error("a stand without my sign-up is in my planning")
	}
	expect(t, e.get(marie, publicPath), 200, "Tombola")
}

func TestDurationFR(t *testing.T) {
	for d, want := range map[time.Duration]string{45 * time.Minute: "45 min", 2 * time.Hour: "2 h", 150 * time.Minute: "2 h 30"} {
		if got := durationFR(d); got != want {
			t.Errorf("durationFR(%v) = %q, want %q", d, got, want)
		}
	}
}

// With automatic deletion disabled, past dates stay editable (old events are
// kept), but absurd future dates are still refused.
func TestDateLimitsKeepForever(t *testing.T) {
	e := newEnv(t, Config{RetentionDays: 0}) // clock fixed at 2026-05-01
	c := e.client()
	expect(t, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Old"}, "date": {"2019-06-15"}}), http.StatusSeeOther)
	expect(t, e.post(c, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Far"}, "date": {"9999-12-31"}}), http.StatusBadRequest, "date hors limites")

	// A long retention also widens the past window (here 3 years).
	e = newEnv(t, Config{RetentionDays: 3 * 365})
	expect(t, e.post(e.client(), "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Old"}, "date": {"2024-01-15"}}), http.StatusSeeOther)
}

// A panic in a handler gives a 500 page and a log line without the
// secret token of the URL.
func TestRecoverPanics(t *testing.T) {
	var logs bytes.Buffer
	s := &Server{log: slog.New(slog.NewTextHandler(&logs, nil))}
	h := s.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/a/SECRETTOKEN/print", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status %d", w.Code)
	}
	if !strings.Contains(logs.String(), "boom") || strings.Contains(logs.String(), "SECRETTOKEN") {
		t.Errorf("log: %s", logs.String())
	}

	// http.ErrAbortHandler is passed on: it is how a handler aborts a response.
	abort := s.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v", v)
		}
	}()
	abort.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

// Names, comments and stand texts are shown on many pages, in text and in
// attributes: none of the classic injections may come out unescaped.
func TestNoScriptInjection(t *testing.T) {
	payloads := []string{
		`<script>alert(1)</script>`,
		`"><img src=x onerror=alert(1)>`,
		`' onmouseover='alert(1)' x='`,
		`</textarea><svg onload=alert(1)>`,
		`javascript:alert(1)`,
	}
	// What must never appear in a page as is.
	raw := []string{`<script>alert`, `<img src=x`, `<svg onload`, `onmouseover='alert`, `href="javascript:`, `src="javascript:`}

	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slots := setupEvent(t, e, admin)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]

	// The organiser's stand name and description.
	evil := strings.Join(payloads, " ")
	expect(t, e.post(admin, adminPath+"/stands/"+standID, url.Values{"name": {payloads[1]}, "description": {evil}}), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/event", url.Values{"title": {payloads[0]}, "location": {payloads[2]}, "description": {evil}, "admin_email": {"orga@example.org"}}), http.StatusSeeOther)

	// Volunteers whose name, phone and comment are attacks.
	var meLinks []string
	for i, p := range payloads {
		v := e.client()
		r := e.post(v, pub+"/slots/"+slots[i%len(slots)]+"/signup", url.Values{"name": {p + " " + p}, "comment": {p}})
		expect(t, r, http.StatusSeeOther)
		meLinks = append(meLinks, strings.TrimSuffix(r.location, "?welcome=1"))
	}

	htmx := map[string]string{"HX-Request": "true"}
	pages := map[string]resp{
		"event":         e.get(e.client(), pub),
		"event todo":    e.get(e.client(), pub+"?todo=1"),
		"me":            e.get(e.client(), meLinks[0]),
		"signup page":   e.get(e.client(), pub+"/slots/"+slots[0]+"/signup"),
		"signup dialog": e.do(e.client(), "GET", pub+"/slots/"+slots[0]+"/signup", nil, htmx),
		"stand page":    e.get(e.client(), pub+"/stands/"+standID),
		"stand dialog":  e.do(e.client(), "GET", pub+"/stands/"+standID, nil, htmx),
		"admin":         e.get(admin, adminPath),
		"print":         e.get(admin, adminPath+"/print"),
		"signup error":  e.post(e.client(), pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {payloads[0]}, "phone": {payloads[1]}}),
	}
	for name, r := range pages {
		if r.code != 200 && name != "signup error" {
			t.Errorf("%s: status %d", name, r.code)
		}
		for _, bad := range raw {
			if strings.Contains(r.body, bad) {
				i := strings.Index(r.body, bad)
				t.Errorf("%s: unescaped %q in …%s…", name, bad, r.body[max(0, i-80):min(len(r.body), i+80)])
			}
		}
	}
	// The texts are really there, escaped (not dropped).
	expect(t, pages["admin"], 200, `&lt;script&gt;alert(1)&lt;/script&gt;`, `&#34;&gt;&lt;img src=x onerror=alert(1)&gt;`)
	expect(t, pages["stand dialog"], 200, `&lt;svg onload=alert(1)&gt;`)
}
