package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The organiser chooses, for each stand, whether its whole row is coloured
// or only its stripe. The choice shows on the public planning and on the admin grid.
func TestStandFillRowSetting(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Tombola"}, "color": {"#d9534f"}, "fill_row": {"1"}}), http.StatusSeeOther)
	ids := regexp.MustCompile(`id="stand-(\d+)"`).FindAllStringSubmatch(e.get(admin, adminPath).body, -1)
	buvette, tombola := ids[0][1], ids[1][1]
	filled := func(body string) int { return strings.Count(body, `<tr class="fill"`) }

	// A new stand is off unless the box was ticked; the edit form shows the state.
	adminBody := e.get(admin, adminPath).body
	if n := filled(adminBody); n != 1 {
		t.Errorf("admin grid: %d filled rows, want 1 (Tombola)", n)
	}
	if n := strings.Count(adminBody, `name="fill_row" value="1" checked`); n != 1 {
		t.Errorf("%d ticked boxes, want 1", n)
	}
	expect(t, e.get(e.client(), pub), 200, `<tr class="fill"`)

	// Tick the other stand: both rows fill. A form without the box turns it off.
	expect(t, e.post(admin, adminPath+"/stands/"+buvette, url.Values{"name": {"Buvette"}, "color": {"#3d7cc9"}, "fill_row": {"1"}}), http.StatusSeeOther)
	if n := filled(e.get(e.client(), pub).body); n != 2 {
		t.Errorf("public page: %d filled rows, want 2", n)
	}
	expect(t, e.post(admin, adminPath+"/stands/"+tombola, url.Values{"name": {"Tombola"}, "color": {"#d9534f"}}), http.StatusSeeOther)
	body := e.get(e.client(), pub).body
	if n := filled(body); n != 1 {
		t.Errorf("public page: %d filled rows after unticking, want 1", n)
	}
	// The fill is for the table: a card of the phone list never gets the class.
	if regexp.MustCompile(`class="slot [^"]*\bfill\b`).MatchString(body) {
		t.Error("a card of the phone list has the class of the row fill")
	}
}

// The fill of a row needs a color, like the fill of a moment. A stand with no
// color has no fill, so its row has no class that would give it padding and
// round corners without a tint.
func TestStandFillRowNeedsAColor(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Tombola"}, "nocolor": {"1"}, "fill_row": {"1"}}), http.StatusSeeOther)

	for name, body := range map[string]string{"public": e.get(e.client(), pub).body, "admin": e.get(admin, adminPath).body} {
		if strings.Contains(body, `<tr class="fill"`) {
			t.Errorf("%s page: a stand with no color has a filled row", name)
		}
	}
	if strings.Contains(e.get(admin, adminPath).body, `name="fill_row" value="1" checked`) {
		t.Error("the box of a stand with no color is ticked")
	}
}
