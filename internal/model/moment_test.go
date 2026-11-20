package model

import (
	"errors"
	"strings"
	"testing"
)

func TestMomentCheck(t *testing.T) {
	cases := []struct {
		name string
		m    Moment
		want error
	}{
		{"valid", Moment{Title: "Goûter", StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 16:00")}, nil},
		{"end equals start", Moment{Title: "Goûter", StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 15:00")}, ErrMomentRange},
		{"end before start", Moment{Title: "Goûter", StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 14:00")}, ErrMomentRange},
		{"blank title", Moment{Title: "  ", StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 16:00")}, ErrMomentTitle},
		{"81 characters", Moment{Title: strings.Repeat("é", MaxMomentTitle+1), StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 16:00")}, ErrMomentTitle},
		{"80 characters", Moment{Title: strings.Repeat("é", MaxMomentTitle), StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 16:00")}, nil},
	}
	for _, c := range cases {
		if got := c.m.Check(); !errors.Is(got, c.want) {
			t.Errorf("%s: Check() = %v, want %v", c.name, got, c.want)
		}
	}
}

func momentTestStands() []Stand { return []Stand{{ID: 1, Name: "Pêche"}} }

func TestPlanningMomentTitlesAColumn(t *testing.T) {
	slots := []Slot{
		{ID: 1, StandID: 1, StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 11:00"), MinPeople: 1},
		{ID: 2, StandID: 1, StartsAt: at("2026-06-20 11:00"), EndsAt: at("2026-06-20 12:00"), MinPeople: 1},
	}
	moments := []Moment{{ID: 1, Title: "Goûter", StartsAt: at("2026-06-20 11:00"), EndsAt: at("2026-06-20 12:00")}}
	p := BuildPlanning(momentTestStands(), slots, moments, nil, 0, true)

	if len(p.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(p.Columns))
	}
	if p.Columns[1].Title != "Goûter" || p.Columns[1].MomentOnly {
		t.Errorf("second column = %+v, want the title Goûter and not moment-only", p.Columns[1])
	}
	if p.Columns[0].Title != "" {
		t.Errorf("first column title = %q, want none", p.Columns[0].Title)
	}
	if p.Groups[1].Title != "Goûter" {
		t.Errorf("group title = %q, want Goûter", p.Groups[1].Title)
	}
	if len(p.Rows[0].Cells[1]) != 1 {
		t.Errorf("the slot must stay in its titled column, cells = %d", len(p.Rows[0].Cells[1]))
	}
	if p.HasMomentColumns() {
		t.Error("HasMomentColumns = true, but the moment joined a slot column")
	}
}

func TestPlanningMomentOnlyColumn(t *testing.T) {
	slots := []Slot{
		{ID: 1, StandID: 1, StartsAt: at("2026-06-20 08:00"), EndsAt: at("2026-06-20 09:30"), MinPeople: 1},
		{ID: 2, StandID: 1, StartsAt: at("2026-06-20 12:00"), EndsAt: at("2026-06-20 13:00"), MinPeople: 1},
	}
	moments := []Moment{{ID: 1, Title: "Spectacle des enfants", StartsAt: at("2026-06-20 10:45"), EndsAt: at("2026-06-20 12:00")}}
	p := BuildPlanning(momentTestStands(), slots, moments, nil, 0, true)

	if len(p.Columns) != 3 {
		t.Fatalf("columns = %d, want 3", len(p.Columns))
	}
	if !p.Columns[1].MomentOnly || p.Columns[1].Title != "Spectacle des enfants" {
		t.Errorf("middle column = %+v, want the moment-only show", p.Columns[1])
	}
	if len(p.Rows[0].Cells) != 3 || len(p.Rows[0].Cells[1]) != 0 {
		t.Errorf("cells = %v, want 3 cells and an empty one in the middle", p.Rows[0].Cells)
	}
	if len(p.Groups[1].Slots) != 0 || p.Groups[1].Title != "Spectacle des enfants" {
		t.Errorf("middle group = %+v, want no slots and the show title", p.Groups[1])
	}
	if !p.Groups[1].MomentOnly {
		t.Error("the middle group must be moment-only")
	}
	if !p.HasSlots() || !p.HasMomentColumns() {
		t.Errorf("HasSlots = %v, HasMomentColumns = %v, want both true", p.HasSlots(), p.HasMomentColumns())
	}
}

func TestPlanningWithMomentsOnly(t *testing.T) {
	moments := []Moment{
		{ID: 1, Title: "Tirage", StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 16:00")},
		{ID: 2, Title: "Accueil", StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 11:00")},
	}
	p := BuildPlanning(momentTestStands(), nil, moments, nil, 0, true)

	if len(p.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(p.Columns))
	}
	if p.Columns[0].Title != "Accueil" || p.Columns[1].Title != "Tirage" {
		t.Errorf("columns = %+v, want 10:00 first, then 15:00", p.Columns)
	}
	if p.HasSlots() {
		t.Error("HasSlots = true with no slot")
	}
	if !p.HasMomentColumns() {
		t.Error("HasMomentColumns = false with two moments")
	}
	if p.Needed != 0 {
		t.Errorf("Needed = %d, want 0", p.Needed)
	}
	if len(p.Rows) != 1 || len(p.Rows[0].Cells) != 2 {
		t.Errorf("rows = %+v, want one row with a cell per column", p.Rows)
	}
}

func TestPlanningMomentsKeepTheirOrderAtTheSameStart(t *testing.T) {
	moments := []Moment{
		{ID: 1, Title: "Long", StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 12:00")},
		{ID: 2, Title: "Court", StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 11:00")},
	}
	p := BuildPlanning(nil, nil, moments, nil, 0, true)
	if len(p.Columns) != 2 || p.Columns[0].Title != "Court" || p.Columns[1].Title != "Long" {
		t.Errorf("columns = %+v, want the shorter one first", p.Columns)
	}
}

func TestPlanningMomentOnAnotherDay(t *testing.T) {
	slots := []Slot{{ID: 1, StandID: 1, StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 11:00"), MinPeople: 1}}
	moments := []Moment{{ID: 1, Title: "Brunch", StartsAt: at("2026-06-21 10:00"), EndsAt: at("2026-06-21 11:00")}}
	p := BuildPlanning(momentTestStands(), slots, moments, nil, 0, true)
	if !p.MultiDay {
		t.Error("MultiDay = false, want true because a moment is on another day")
	}
}

func TestMineHasNoMomentColumns(t *testing.T) {
	const me = 7
	slots := []Slot{{ID: 1, StandID: 1, StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 11:00"), MinPeople: 1}}
	moments := []Moment{
		{ID: 1, Title: "Goûter", StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 11:00")},
		{ID: 2, Title: "Spectacle", StartsAt: at("2026-06-20 12:00"), EndsAt: at("2026-06-20 13:00")},
	}
	signups := []SignupRow{{Signup: Signup{SlotID: 1, VolunteerID: me}}}
	p := BuildPlanning(momentTestStands(), slots, moments, signups, me, true)
	if !p.HasMomentColumns() {
		t.Fatal("setup: the full planning must have a moment-only column")
	}

	mine := p.Mine()
	if mine.HasMomentColumns() || len(mine.Columns) != 1 {
		t.Errorf("Mine() columns = %+v, want only the volunteer's slot column", mine.Columns)
	}
	if mine.Columns[0].Title != "" {
		t.Errorf("Mine() column title = %q, want none", mine.Columns[0].Title)
	}
}

func TestPlanningMomentColor(t *testing.T) {
	slots := []Slot{{ID: 1, StandID: 1, StartsAt: at("2026-06-20 14:00"), EndsAt: at("2026-06-20 15:00"), MinPeople: 1}}
	moments := []Moment{
		{ID: 1, Title: "Goûter", Color: "#e8c93a", StartsAt: at("2026-06-20 14:00"), EndsAt: at("2026-06-20 15:00")},
		{ID: 2, Title: "Spectacle", Color: "#3d7cc9", StartsAt: at("2026-06-20 10:00"), EndsAt: at("2026-06-20 11:00")},
		{ID: 3, Title: "Tirage", StartsAt: at("2026-06-20 16:00"), EndsAt: at("2026-06-20 17:00")},
	}
	p := BuildPlanning(momentTestStands(), slots, moments, nil, 0, true)

	if len(p.Columns) != 3 {
		t.Fatalf("columns = %d, want 3", len(p.Columns))
	}
	if c := p.Columns[0]; c.Color != "#3d7cc9" || !c.MomentOnly {
		t.Errorf("moment-only column = %+v, want its own color", c)
	}
	if c := p.Columns[1]; c.Color != "#e8c93a" || c.MomentOnly {
		t.Errorf("joined column = %+v, want the moment color and not moment-only", c)
	}
	if c := p.Columns[2]; c.Color != "" {
		t.Errorf("moment with no color: column color = %q, want none", c.Color)
	}
	if p.Groups[1].Color != "#e8c93a" {
		t.Errorf("group color = %q, want the moment color", p.Groups[1].Color)
	}
}
