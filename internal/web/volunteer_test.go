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

// Stand descriptions are on the overview (under the name, no stand tab
// any more) and in full on the sign-up form.
func TestStandDescriptions(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slots := setupEvent(t, e, admin)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	desc := "Les enfants pêchent des canards ; un lot pour chacun."
	expect(t, e.post(admin, adminPath+"/stands/"+standID, url.Values{"name": {"Pêche aux canards"}, "description": {desc}, "color": {"#2aa3b5"}}), http.StatusSeeOther)

	r := e.get(e.client(), pub)
	expect(t, r, 200, `<span class="stand-desc">Les enfants pêchent des canards ; un lot pour chacun.</span>`,
		`<a class="stand-more" href="`+pub+`/stands/`+standID+`"`)
	if strings.Contains(r.body, "?stand=") || strings.Contains(r.body, "tabs-stands") {
		t.Error("stand tabs still shown")
	}
	// Old links to a stand tab still show the overview.
	expect(t, e.get(e.client(), pub+"?stand="+standID), 200, "stand-desc")
	expect(t, e.get(e.client(), pub+"/slots/"+slots[0]+"/signup"), 200, `<p class="desc signup-desc">Les enfants pêchent des canards ; un lot pour chacun.</p>`)
	// "Voir plus": the stand's description in a dialog (htmx) or a page.
	r = e.do(e.client(), "GET", pub+"/stands/"+standID, nil, map[string]string{"HX-Request": "true"})
	expect(t, r, 200, "<dialog", "Pêche aux canards", desc, "data-close autofocus>Fermer</button>")
	if strings.Contains(r.body, "<form") || strings.Contains(r.body, "<html") {
		t.Error("stand dialog has a form or a whole page")
	}
	expect(t, e.get(e.client(), pub+"/stands/"+standID), 200, "<html", "Retour au planning", desc)
	expect(t, e.get(e.client(), pub+"/stands/999999"), 404, "Stand introuvable")
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"draft"}}), http.StatusSeeOther)
	expect(t, e.get(e.client(), pub+"/stands/"+standID), 404, "pas encore ouvertes")
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)

	// The views bar only appears for a known volunteer.
	if strings.Contains(r.body, "Vue d&#39;ensemble") || strings.Contains(r.body, "Vue d'ensemble") {
		t.Error("views bar shown to an unknown visitor")
	}
}
