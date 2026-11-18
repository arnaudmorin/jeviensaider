package web

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"
)

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

func TestDurationFR(t *testing.T) {
	for d, want := range map[time.Duration]string{45 * time.Minute: "45 min", 2 * time.Hour: "2 h", 150 * time.Minute: "2 h 30"} {
		if got := durationFR(d); got != want {
			t.Errorf("durationFR(%v) = %q, want %q", d, got, want)
		}
	}
}
