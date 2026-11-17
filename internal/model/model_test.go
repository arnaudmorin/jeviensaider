package model

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(TimeLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"Marie Dupont":            "Marie D.",
		"  jean   de la fontaine": "jean F.",
		"Zoé":                     "Zoé",
		"Anne élodie":             "Anne É.",
		"":                        "",
	}
	for in, want := range cases {
		if got := DisplayName(in); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOverlaps(t *testing.T) {
	a := Slot{StartsAt: at("2026-06-20 14:00"), EndsAt: at("2026-06-20 15:00")}
	b := Slot{StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 16:00")}
	c := Slot{StartsAt: at("2026-06-20 14:30"), EndsAt: at("2026-06-20 15:30")}
	if a.Overlaps(b) || b.Overlaps(a) {
		t.Error("adjacent slots must not overlap")
	}
	if !a.Overlaps(c) || !c.Overlaps(b) {
		t.Error("expected overlap")
	}
}

func TestFillStatus(t *testing.T) {
	s := Slot{MinPeople: 2, MaxPeople: 3}
	for count, want := range map[int]string{0: FillEmpty, 1: FillPartial, 2: FillOK, 3: FillFull} {
		if got := s.FillStatus(count); got != want {
			t.Errorf("count %d: got %s want %s", count, got, want)
		}
	}
	unlimited := Slot{MinPeople: 2}
	if unlimited.FillStatus(10) != FillOK {
		t.Error("no max: never full")
	}
}

func TestBuildPlanning(t *testing.T) {
	stands := []Stand{{ID: 1, Name: "Pêche aux canards"}, {ID: 2, Name: "Buvette"}}
	slots := []Slot{
		{ID: 11, StandID: 1, StartsAt: at("2026-06-20 15:00"), EndsAt: at("2026-06-20 16:00"), MinPeople: 2},
		{ID: 10, StandID: 1, StartsAt: at("2026-06-20 14:00"), EndsAt: at("2026-06-20 15:00"), MinPeople: 2},
		{ID: 20, StandID: 2, StartsAt: at("2026-06-20 14:00"), EndsAt: at("2026-06-20 15:00"), MinPeople: 3},
	}
	signups := []SignupRow{
		{Signup: Signup{ID: 1, SlotID: 10, VolunteerID: 99}, Name: "Marie Dupont"},
		{Signup: Signup{ID: 2, SlotID: 20, VolunteerID: 98}, Name: "Paul"},
	}
	p := BuildPlanning(stands, slots, signups, 99)
	if len(p.Columns) != 2 {
		t.Fatalf("want 2 columns, got %d", len(p.Columns))
	}
	if p.Needed != 7 || p.Count != 2 || p.Missing != 5 {
		t.Errorf("totals: needed %d count %d missing %d", p.Needed, p.Count, p.Missing)
	}
	peche := p.Stand(1)
	if peche.Slots[0].ID != 10 || peche.Slots[0].Status != FillPartial || !peche.Slots[0].Mine {
		t.Errorf("unexpected first slot: %+v", peche.Slots[0])
	}
	if len(p.Rows[1].Cells[1]) != 0 || len(p.Rows[1].Cells[0]) != 1 {
		t.Error("buvette grid cells misaligned")
	}
	if len(p.MySlots) != 1 || len(p.Groups[0].Slots) != 2 {
		t.Errorf("my slots %d, group0 %d", len(p.MySlots), len(p.Groups[0].Slots))
	}
	if p.MultiDay {
		t.Error("single day expected")
	}
}
