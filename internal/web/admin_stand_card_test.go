package web

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

// The cards of the stands start closed. After a change to a stand, the page
// comes back with that card open, and only that one.
func TestStandCardOpensAfterAChange(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Tombola"}}), http.StatusSeeOther)
	stands := storedStands(t, e, adminPath)
	first, second := i64(stands[0].ID), i64(stands[1].ID)

	openCard := regexp.MustCompile(`<details class="stand" id="stand-(\d+)"[^>]*\bopen\b`)
	if m := openCard.FindAllStringSubmatch(e.get(admin, adminPath).body, -1); len(m) != 0 {
		t.Errorf("open cards on the plain page: %v, want none", m)
	}

	r := e.post(admin, adminPath+"/stands/"+first, url.Values{"name": {"Buvette"}})
	expect(t, r, http.StatusSeeOther)
	if want := adminPath + "?stand=" + first + "#stand-" + first; r.location != want {
		t.Fatalf("redirect = %q, want %q", r.location, want)
	}
	m := openCard.FindAllStringSubmatch(e.get(admin, adminPath+"?stand="+first).body, -1)
	if len(m) != 1 || m[0][1] != first {
		t.Errorf("open cards after changing stand %s: %v, want only that one", first, m)
	}
	if m := openCard.FindAllStringSubmatch(e.get(admin, adminPath+"?stand="+second).body, -1); len(m) != 1 || m[0][1] != second {
		t.Errorf("open cards with ?stand=%s: %v", second, m)
	}
	// A value that is not a stand opens nothing.
	if m := openCard.FindAllStringSubmatch(e.get(admin, adminPath+"?stand=abc").body, -1); len(m) != 0 {
		t.Errorf("open cards with ?stand=abc: %v", m)
	}
}
