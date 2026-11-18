package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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

func TestOverlapWarningAndMyPlanning(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, publicPath, slots := setupEvent(t, e, admin) // 14h, 15h, 16h on one stand
	marie := e.client()
	form := url.Values{"name": {"Marie Dupont"}}
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
