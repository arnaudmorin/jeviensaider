package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

// pledgeView builds a list view the way the planning does: one pledge row
// holds the total, so that Missing, Left and Full come from the model.
func pledgeView(one, many string, total, minItems, maxItems int) *model.PledgeListView {
	l := model.PledgeList{ID: 1, UnitOne: one, Unit: many, MinItems: minItems, MaxItems: maxItems}
	var rows []model.PledgeRow
	if total > 0 {
		rows = append(rows, model.PledgeRow{Pledge: model.Pledge{ListID: l.ID, Quantity: total}})
	}
	p := &model.Planning{}
	p.AddPledges([]model.PledgeList{l}, rows)
	return p.Pledges[0]
}

func TestUnitCount(t *testing.T) {
	for _, c := range []struct {
		n         int
		one, many string
		want      string
	}{
		{0, "gâteau", "gâteaux", "0 gâteau"},
		{1, "gâteau", "gâteaux", "1 gâteau"},
		{2, "gâteau", "gâteaux", "2 gâteaux"},
		{1, "cheval", "chevaux", "1 cheval"},
		{3, "cheval", "chevaux", "3 chevaux"},
		{14, "", "", "14"},
		{1, "", "", "1"},
	} {
		if got := unitCount(c.n, c.one, c.many); got != c.want {
			t.Errorf("unitCount(%d, %q, %q) = %q, want %q", c.n, c.one, c.many, got, c.want)
		}
	}
}

// The e-mail templates write quantities with the count function.
func TestMailCountFunc(t *testing.T) {
	tpl, err := template.New("mail").Funcs(mailFuncs()).Parse(`{{count .N "gâteau" "gâteaux"}}|{{count .One "gâteau" "gâteaux"}}`)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := tpl.Execute(&b, map[string]int{"N": 3, "One": 1}); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "3 gâteaux|1 gâteau"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPledgeStatus(t *testing.T) {
	for _, c := range []struct {
		one, many       string
		total, min, max int
		want            string
	}{
		{"gâteau", "gâteaux", 0, 0, 0, "Rien de promis pour l'instant"},
		{"gâteau", "gâteaux", 1, 0, 0, "1 gâteau au total"},
		{"gâteau", "gâteaux", 14, 0, 0, "14 gâteaux au total"},
		{"gâteau", "gâteaux", 14, 21, 0, "14 gâteaux au total, il en manque 7"},
		{"gâteau", "gâteaux", 21, 21, 0, "21 gâteaux au total, c'est bon"},
		{"gâteau", "gâteaux", 14, 0, 30, "14 gâteaux au total, 16 de plus au maximum"},
		{"gâteau", "gâteaux", 14, 21, 30, "14 gâteaux au total, il en manque 7, 16 de plus au maximum"},
		{"gâteau", "gâteaux", 30, 0, 30, "Complet : 30 gâteaux au total"},
		{"gâteau", "gâteaux", 30, 21, 30, "Complet : 30 gâteaux au total"},
		{"gâteau", "gâteaux", 0, 21, 0, "Rien de promis pour l'instant, il en manque 21"},
		{"gâteau", "gâteaux", 29, 0, 30, "29 gâteaux au total, 1 de plus au maximum"},
		{"gâteau", "gâteaux", 25, 21, 30, "25 gâteaux au total, c'est bon, 5 de plus au maximum"},
		{"bouteille", "bouteilles", 6, 0, 0, "6 bouteilles au total"},
		{"", "", 14, 0, 0, "14 au total"},
	} {
		v := pledgeView(c.one, c.many, c.total, c.min, c.max)
		if got := pledgeStatus(v); got != c.want {
			t.Errorf("unit %q/%q, total %d, min %d, max %d: got %q, want %q", c.one, c.many, c.total, c.min, c.max, got, c.want)
		}
	}
}

func TestPledgeKind(t *testing.T) {
	for _, c := range []struct {
		name            string
		total, min, max int
		want            string
	}{
		{"empty, no minimum", 0, 0, 0, "room"},
		{"empty, with a minimum", 0, 5, 0, "need"},
		{"under the minimum", 3, 5, 10, "need"},
		{"minimum reached", 5, 5, 10, "room"},
		{"full", 10, 5, 10, "full"},
	} {
		if got := pledgeKind(pledgeView("", "", c.total, c.min, c.max)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPledgeWanted(t *testing.T) {
	for _, c := range []struct {
		name            string
		total, min, max int
		want            bool
	}{
		{"no limits", 3, 0, 0, true},
		{"minimum reached, no maximum", 21, 21, 0, false},
		{"minimum not reached", 14, 21, 0, true},
		{"full", 30, 0, 30, false},
		{"maximum only, not full", 14, 0, 30, true},
	} {
		if got := pledgeWanted(pledgeView("", "", c.total, c.min, c.max)); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDropOffHelpers(t *testing.T) {
	for in, want := range map[string]string{"08:30": "Dépôt à 8h30", "14:00": "Dépôt à 14h", "": ""} {
		if got := dropOffLabel(in); got != want {
			t.Errorf("dropOffLabel(%q) = %q, want %q", in, got, want)
		}
	}

	ev := &model.Event{Date: "2026-06-27"}
	got, ok := dropOffStart(ev, "08:30")
	if want := time.Date(2026, 6, 27, 8, 30, 0, 0, time.UTC); !ok || !got.Equal(want) {
		t.Errorf("dropOffStart = %v, %v, want %v, true", got, ok, want)
	}
	if _, ok := dropOffStart(&model.Event{}, "08:30"); ok {
		t.Error("an event with no date must give false")
	}
	if _, ok := dropOffStart(ev, ""); ok {
		t.Error("an empty drop-off must give false")
	}
}

func TestPledgeNames(t *testing.T) {
	rows := []model.PledgeRow{
		{Pledge: model.Pledge{Quantity: 2}, Name: "Marie Dupont"},
		{Pledge: model.Pledge{Quantity: 1}, Name: "Paul Lambert"},
	}
	if got, want := pledgeNames(rows), "Marie D. (2), Paul L."; got != want {
		t.Errorf("pledgeNames = %q, want %q", got, want)
	}
	if got := pledgeNames(nil); got != "" {
		t.Errorf("pledgeNames(nil) = %q, want an empty text", got)
	}
}

func TestUserMessagePledges(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{&store.PledgeFullError{Free: 0}, "Cette liste est complète."},
		{&store.PledgeFullError{Free: 1}, "Vous ne pouvez en apporter que 1 sur cette liste."},
		{&store.PledgeFullError{Free: 3}, "Vous ne pouvez en apporter que 3 sur cette liste."},
		{&store.PledgeMaxError{Total: 14}, "Il y en a déjà 14 au total : le maximum doit être au moins 14."},
		{model.ErrPledgeRange, "Le maximum doit être vide ou supérieur ou égal au minimum."},
		{model.ErrPledgeNumber, "Indiquez un nombre entre 1 et 1000."},
		{model.ErrPledgeDropOff, "Heure de dépôt invalide (exemple : 08:30)."},
		{store.ErrPledgeQuantity, "Indiquez un nombre entre 1 et 20."},
		{store.ErrTooManyPledgeLists, "Un événement ne peut pas avoir plus de 10 listes à apporter."},
		{store.ErrTooManyPledges, "Cette liste ne peut pas avoir plus de 200 inscriptions."},
		// The typed errors are found even when another error wraps them.
		{fmt.Errorf("pledge: %w", &store.PledgeFullError{Free: 2}), "Vous ne pouvez en apporter que 2 sur cette liste."},
		{fmt.Errorf("update: %w", &store.PledgeMaxError{Total: 5}), "Il y en a déjà 5 au total : le maximum doit être au moins 5."},
	} {
		got, ok := userMessage(c.err)
		if !ok || got != c.want {
			t.Errorf("userMessage(%v) = %q, %v, want %q, true", c.err, got, ok, c.want)
		}
	}
	if got, ok := userMessage(errors.New("disk full")); ok {
		t.Errorf("an internal error must not give a message, got %q", got)
	}
}

// pledgeForm builds a POST request with the given form fields.
func pledgeForm(fields url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fields.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestReadPledgeForms(t *testing.T) {
	// A unit of 40 characters is cut to 30, in both forms.
	l, err := readPledgeList(pledgeForm(url.Values{"name": {"Gâteaux"}, "unit_one": {strings.Repeat("é", 40)}, "unit": {strings.Repeat("é", 40)}}))
	if err != nil {
		t.Fatal(err)
	}
	if got, one := len([]rune(l.Unit)), len([]rune(l.UnitOne)); got != maxPledgeUnit || one != maxPledgeUnit {
		t.Errorf("unit has %d characters and %d for the singular, want %d", got, one, maxPledgeUnit)
	}

	// The unit has both forms or none.
	for _, f := range []url.Values{
		{"name": {"Gâteaux"}, "unit": {"gâteaux"}},
		{"name": {"Gâteaux"}, "unit_one": {"gâteau"}},
	} {
		if _, err := readPledgeList(pledgeForm(f)); err == nil {
			t.Errorf("form %v: a unit with one form was accepted", f)
		}
	}

	// All the fields are read. An empty minimum and an empty maximum mean 0.
	l, err = readPledgeList(pledgeForm(url.Values{
		"name": {"  Gâteaux  "}, "description": {"Sucrés\nou salés"}, "unit_one": {"gâteau"}, "unit": {"gâteaux"},
		"drop_off": {"08:30"}, "min": {"21"}, "max": {"30"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := (model.PledgeList{Name: "Gâteaux", Description: "Sucrés\nou salés", UnitOne: "gâteau", Unit: "gâteaux", DropOff: "08:30", MinItems: 21, MaxItems: 30}); l != want {
		t.Errorf("got %+v, want %+v", l, want)
	}
	l, err = readPledgeList(pledgeForm(url.Values{"name": {"Boissons"}, "min": {""}, "max": {" "}}))
	if err != nil || l.MinItems != 0 || l.MaxItems != 0 || l.DropOff != "" {
		t.Errorf("empty fields: got %+v, %v, want no limits and no drop-off", l, err)
	}

	// A long name and a long description are cut.
	l, err = readPledgeList(pledgeForm(url.Values{
		"name": {strings.Repeat("n", 200)}, "description": {strings.Repeat("d", 2000)},
	}))
	if err != nil || len([]rune(l.Name)) != maxStandName || len([]rune(l.Description)) != maxStandDescription {
		t.Errorf("name %d, description %d, err %v", len([]rune(l.Name)), len([]rune(l.Description)), err)
	}

	for _, c := range []struct {
		name   string
		fields url.Values
		want   string
	}{
		{"no name", url.Values{"name": {"  "}}, "Le nom de la liste est obligatoire."},
		{"bad drop-off", url.Values{"name": {"x"}, "drop_off": {"8h"}}, "Heure de dépôt invalide (exemple : 08:30)."},
		// The drop-off is not cut before the check: "08:30xyz" is not valid.
		{"long drop-off", url.Values{"name": {"x"}, "drop_off": {"08:30xyz"}}, "Heure de dépôt invalide (exemple : 08:30)."},
		{"minimum is not a number", url.Values{"name": {"x"}, "min": {"abc"}}, "Minimum : nombre invalide : abc"},
		{"minimum of 0", url.Values{"name": {"x"}, "min": {"0"}}, "Minimum : nombre invalide : 0"},
		{"maximum too big", url.Values{"name": {"x"}, "max": {"1001"}}, "Maximum : nombre invalide : 1001"},
		{"maximum below minimum", url.Values{"name": {"x"}, "min": {"10"}, "max": {"5"}}, "Le maximum doit être vide ou supérieur ou égal au minimum."},
	} {
		_, err := readPledgeList(pledgeForm(c.fields))
		got, ok := userMessage(err)
		if err == nil || !ok || got != c.want {
			t.Errorf("%s: got %v (%q, %v), want the message %q", c.name, err, got, ok, c.want)
		}
	}

	for _, c := range []struct {
		quantity string
		want     int
		wantErr  string
	}{
		{"", 1, ""},
		{"1", 1, ""},
		{"20", 20, ""},
		{"0", 0, "Indiquez un nombre entre 1 et 20."},
		{"21", 0, "Indiquez un nombre entre 1 et 20."},
		{"abc", 0, "Indiquez un nombre entre 1 et 20."},
	} {
		n, err := readPledgeQuantity(pledgeForm(url.Values{"quantity": {c.quantity}}))
		var ue userError
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("quantity %q: unexpected error %v", c.quantity, err)
		case c.wantErr != "" && (!errors.As(err, &ue) || err.Error() != c.wantErr):
			t.Errorf("quantity %q: got error %v, want the user error %q", c.quantity, err, c.wantErr)
		case n != c.want:
			t.Errorf("quantity %q: got %d, want %d", c.quantity, n, c.want)
		}
	}
	// No quantity field at all also means 1.
	if n, err := readPledgeQuantity(pledgeForm(url.Values{})); err != nil || n != 1 {
		t.Errorf("no quantity field: got %d, %v, want 1", n, err)
	}
}

// The planning holds the lists of the event with their pledges, and the
// pledges of the current volunteer.
func TestPlanningLoadsPledges(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv, err := New(st, Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	ev := &model.Event{Title: "Fête", Status: model.StatusOpen}
	if err := st.CreateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	cakes := &model.PledgeList{EventID: ev.ID, Name: "Gâteaux", UnitOne: "gâteau", Unit: "gâteaux", MinItems: 10}
	drinks := &model.PledgeList{EventID: ev.ID, Name: "Boissons"}
	for _, l := range []*model.PledgeList{cakes, drinks} {
		if err := st.CreatePledgeList(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	marie, err := st.Pledge(ctx, store.PledgeRequest{EventID: ev.ID, ListID: cakes.ID, Name: "Marie Dupont", Quantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pledge(ctx, store.PledgeRequest{EventID: ev.ID, ListID: cakes.ID, Name: "Paul Lambert", Quantity: 3}); err != nil {
		t.Fatal(err)
	}

	p, err := srv.planning(ctx, ev, marie.Volunteer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pledges) != 2 || p.Pledges[0].Name != "Gâteaux" || p.Pledges[1].Name != "Boissons" {
		t.Fatalf("got %d lists, want Gâteaux then Boissons", len(p.Pledges))
	}
	if v := p.Pledges[0]; v.Total != 5 || v.Missing != 5 || len(v.Pledges) != 2 || !v.Mine || v.MyQuantity != 2 {
		t.Errorf("cakes: got total %d, missing %d, %d rows, mine %v, my quantity %d", v.Total, v.Missing, len(v.Pledges), v.Mine, v.MyQuantity)
	}
	if v := p.Pledges[1]; v.Total != 0 || len(v.Pledges) != 0 || v.Mine {
		t.Errorf("drinks: got total %d, %d rows, mine %v, want none", v.Total, len(v.Pledges), v.Mine)
	}
	if len(p.MyPledges) != 1 || p.MyPledges[0].ID != cakes.ID {
		t.Errorf("got %d lists of mine, want the cakes only", len(p.MyPledges))
	}
	if got, want := pledgeNames(p.Pledges[0].Pledges), "Marie D. (2), Paul L. (3)"; got != want {
		t.Errorf("names = %q, want %q", got, want)
	}
}
