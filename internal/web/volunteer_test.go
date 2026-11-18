package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

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

// A volunteer manages their own details and sign-ups with their link.
func TestVolunteerSelfService(t *testing.T) {
	e := newEnv(t, Config{})
	_, pub, slots := setupEvent(t, e, e.client())
	v := e.client()
	me := strings.TrimSuffix(e.post(v, pub+"/slots/"+slots[0]+"/signup", url.Values{"name": {"Marie Dupont"}}).location, "?welcome=1")

	expect(t, e.post(v, me, url.Values{"name": {"Marie Durand"}, "phone": {"06 01 02 03 04"}}), http.StatusSeeOther)
	expect(t, e.get(v, me), 200, "Vos coordonnées ont été mises à jour", `value="Marie Durand"`, `value="06 01 02 03 04"`)
	expect(t, e.post(v, me, url.Values{"name": {"Marie"}, "phone": {"call me"}}), http.StatusSeeOther)
	expect(t, e.get(v, me), 200, "ne semble pas valide", `value="Marie Durand"`)

	// Someone else's sign-up cannot be cancelled with my link.
	other := strings.TrimSuffix(e.post(e.client(), pub+"/slots/"+slots[1]+"/signup", url.Values{"name": {"Paul"}}).location, "?welcome=1")
	theirs := regexp.MustCompile(`/signups/(\d+)/delete`).FindStringSubmatch(e.get(e.client(), other).body)[1]
	expect(t, e.post(v, me+"/signups/"+theirs+"/delete", nil), 404)

	// "Ce n'est pas moi": the device forgets me, the link still works.
	expect(t, e.get(v, pub), 200, "Mes créneaux")
	expect(t, e.post(v, pub+"/forget", nil), http.StatusSeeOther)
	if body := e.get(v, pub).body; strings.Contains(body, me) {
		t.Error("still recognised after forget")
	}
	expect(t, e.get(e.client(), me), 200, "Marie Durand")
}
