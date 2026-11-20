package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"jeviensaider/internal/model"
)

// storedStands lists the stands of the event that adminPath belongs to.
func storedStands(t *testing.T, e *env, adminPath string) []model.Stand {
	t.Helper()
	ev, err := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(adminPath, "/a/"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := e.store.ListStands(testCtx, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// The color of a stand shows as a stripe, set by the --stand variable. There is
// no dot of color any more, on any page.
const dotClass = `class="dot`

func TestStandColorFromTheForm(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	id := i64(storedStands(t, e, adminPath)[0].ID)

	expect(t, e.post(admin, adminPath+"/stands/"+id, url.Values{"name": {"Buvette"}, "color": {"#2e9e6a"}}), http.StatusSeeOther)
	if got := storedStands(t, e, adminPath)[0].Color; got != "#2e9e6a" {
		t.Fatalf("stand color = %q, want #2e9e6a", got)
	}
	expect(t, e.get(e.client(), pub), 200, "--stand: #2e9e6a")
	for name, body := range map[string]string{"public": e.get(e.client(), pub).body, "admin": e.get(admin, adminPath).body} {
		if strings.Contains(body, dotClass) {
			t.Errorf("%s page: the dot of color is gone, but the page still has one", name)
		}
	}
	// The stand has a stripe on its title in the admin page, and none without a color.
	expect(t, e.get(admin, adminPath), 200, `class="stand-head striped"`)
	expect(t, e.get(admin, adminPath), 200, `name="color_was" value="#2e9e6a"`)
	if body := e.get(admin, adminPath).body; strings.Contains(body, `name="nocolor" value="1" checked`) {
		t.Error("the box \"Sans couleur\" is ticked for a stand with a color")
	}
}

func TestStandWithoutColor(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})

	f := url.Values{"name": {"Tombola"}, "color": {"#d9534f"}, "color_was": {"#d9534f"}, "nocolor": {"1"}}
	expect(t, e.post(admin, adminPath+"/stands", f), http.StatusSeeOther)
	for _, st := range storedStands(t, e, adminPath) {
		if st.Color != "" {
			t.Errorf("stand %q has the color %q, want none", st.Name, st.Color)
		}
	}

	// No color: no stripe and no --stand variable, on every page.
	body := e.get(e.client(), pub).body
	if !strings.Contains(body, "Tombola") {
		t.Fatal("the stand is not on the public page")
	}
	if strings.Contains(body, "--stand") || strings.Contains(body, `class="stand-head striped"`) {
		t.Error("a stand with no color shows a color on the public page")
	}
	body = e.get(admin, adminPath).body
	if strings.Contains(body, "--stand") || strings.Contains(body, "striped") {
		t.Error("a stand with no color shows a color on the admin page")
	}
	// The panel shows the default color in the picker and the box ticked.
	expect(t, e.get(admin, adminPath), 200, `name="color_was" value="`+defaultColor+`"`, `name="nocolor" value="1" checked`)
}

func TestStandInvalidColorIsEmpty(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	id := i64(storedStands(t, e, adminPath)[0].ID)

	for _, color := range []string{"red", "#12", "#fff;background:url(x)", "#gggggg", ""} {
		expect(t, e.post(admin, adminPath+"/stands/"+id, url.Values{"name": {"Buvette"}, "color": {color}}), http.StatusSeeOther)
		if got := storedStands(t, e, adminPath)[0].Color; got != "" {
			t.Errorf("color %q stored as %q, want none", color, got)
		}
	}
	if body := e.get(e.client(), pub).body; strings.Contains(body, "--stand") {
		t.Error("an invalid color reached the public page")
	}
}

func TestStandPickedColorWinsOverTheTickedBox(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	id := i64(storedStands(t, e, adminPath)[0].ID)

	// The box is ticked, but the organizer picked red: red wins.
	f := url.Values{"name": {"Buvette"}, "color": {"#ff0000"}, "color_was": {defaultColor}, "nocolor": {"1"}}
	expect(t, e.post(admin, adminPath+"/stands/"+id, f), http.StatusSeeOther)
	if got := storedStands(t, e, adminPath)[0].Color; got != "#ff0000" {
		t.Errorf("color after picking red with the box ticked = %q, want #ff0000", got)
	}

	// The picker was not touched and the box is ticked: no color.
	f.Set("color_was", "#ff0000")
	expect(t, e.post(admin, adminPath+"/stands/"+id, f), http.StatusSeeOther)
	if got := storedStands(t, e, adminPath)[0].Color; got != "" {
		t.Errorf("color after ticking the box with an untouched picker = %q, want none", got)
	}

	// Unticking the box gives back the color that the picker shows.
	f = url.Values{"name": {"Buvette"}, "color": {defaultColor}, "color_was": {defaultColor}}
	expect(t, e.post(admin, adminPath+"/stands/"+id, f), http.StatusSeeOther)
	if got := storedStands(t, e, adminPath)[0].Color; got != defaultColor {
		t.Errorf("color after unticking the box = %q, want %s", got, defaultColor)
	}
}

func TestPDFStandWithoutColorIsGrey(t *testing.T) {
	if got := standColor(""); got != colHead {
		t.Errorf("standColor(\"\") = %v, want the header grey %v", got, colHead)
	}
	if got := standColor("#ff0000"); got != (rgb{255, 0, 0}) {
		t.Errorf("standColor(#ff0000) = %v", got)
	}
}
