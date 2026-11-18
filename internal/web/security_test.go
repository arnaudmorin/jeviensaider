package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
		r := e.do(c, "POST", "/events", url.Values{"title": {"x"}},
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
	r := e.post(e.client(), "/events", url.Values{"title": {"Fête"}})
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
	r = e.post(e.client(), "/events", url.Values{"title": {"Fête"}})
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
	r := e.post(admin, "/events", url.Values{"title": {"Big"}})
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
