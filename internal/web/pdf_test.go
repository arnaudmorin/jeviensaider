package web

import (
	"bytes"
	"fmt"
	"regexp"
	"testing"
	"time"

	"jeviensaider/internal/model"
)

// planningWith builds a planning with one stand per entry of slotsPerStand,
// each slot from 8:00 for an hour, needing perSlot people.
func planningWith(slotsPerStand []int, perSlot int) *model.Planning {
	var stands []model.Stand
	var slots []model.Slot
	start := time.Date(2026, 6, 20, 8, 0, 0, 0, time.Local)
	id := int64(1)
	for i, n := range slotsPerStand {
		st := model.Stand{ID: int64(i + 1), Name: fmt.Sprintf("Stand %d", i+1), Color: "#2aa3b5",
			Description: "Une description assez longue pour tenir sur plusieurs lignes, avec des accents : fête, crêpes, œufs, à côté."}
		stands = append(stands, st)
		for j := range n {
			s := start.Add(time.Duration(j) * time.Hour)
			slots = append(slots, model.Slot{ID: id, StandID: st.ID, StartsAt: s, EndsAt: s.Add(time.Hour), MinPeople: perSlot})
			id++
		}
	}
	signups := []model.SignupRow{{Signup: model.Signup{ID: 1, SlotID: 1, VolunteerID: 1, Comment: "avec ma fille"}, Name: "Zoé Lefèvre", Phone: "06 12 34 56 78"}}
	return model.BuildPlanning(stands, slots, nil, signups, 0, true)
}

func TestRowHeightAndPagination(t *testing.T) {
	const first, next = 600.0, 680.0

	// Few lines: comfortable rows, one page.
	small := standBlocks(planningWith([]int{5}, 2).Stands[0]) // 10 lines
	if h := rowHeight(small, first, next); h != rowMax {
		t.Errorf("10 lines: row %v, want %v", h, rowMax)
	}
	// More lines: rows shrink to stay on one page.
	medium := standBlocks(planningWith([]int{15}, 2).Stands[0]) // 30 lines
	if h := rowHeight(medium, first, next); h != 20 || len(paginate(medium, h, first, next)) != 1 {
		t.Errorf("30 lines: row %v", h)
	}

	p := planningWith([]int{12}, 2) // 24 lines
	blocks := standBlocks(p.Stands[0])
	if n := countLines(blocks); n != 24 {
		t.Fatalf("%d lines, want 24", n)
	}
	h := rowHeight(blocks, first, next)
	if pages := paginate(blocks, h, first, next); len(pages) != 1 {
		t.Errorf("24 lines on %d pages, want 1", len(pages))
	}

	// 20 slots of 4: two pages, and no slot cut between them.
	p = planningWith([]int{20}, 4)
	blocks = standBlocks(p.Stands[0])
	h = rowHeight(blocks, first, next)
	pages := paginate(blocks, h, first, next)
	if len(pages) != len(paginate(blocks, rowMin, first, next)) || len(pages) != 2 || !pages[0].First || pages[1].First {
		t.Fatalf("%d pages", len(pages))
	}
	lines, seen := 0, map[int64]bool{}
	for _, pg := range pages {
		used := 0.0
		for _, b := range pg.Blocks {
			if seen[b.Slot.ID] {
				t.Errorf("slot %d cut across pages", b.Slot.ID)
			}
			seen[b.Slot.ID] = true
			lines += len(b.Lines)
			used += float64(len(b.Lines)) * h
		}
		if room := map[bool]float64{true: first, false: next}[pg.First]; used > room+1e-6 {
			t.Errorf("page overflows: %v > %v", used, room)
		}
	}
	if lines != 80 {
		t.Errorf("%d lines placed, want 80", lines)
	}

	// A slot longer than a page is split.
	p = planningWith([]int{1}, 60)
	blocks = standBlocks(p.Stands[0])
	if pages := paginate(blocks, rowMin, first, next); len(pages) != 2 {
		t.Errorf("a 60-person slot on %d pages, want 2", len(pages))
	}
}

func countPages(pdf []byte) int {
	return len(regexp.MustCompile(`/Type /Page\b[^s]`).FindAll(pdf, -1))
}

func TestPlanningPDF(t *testing.T) {
	ev := &model.Event{Title: "Fête de l'école", Date: "2026-06-20", Location: "Cour"}
	for _, c := range []struct {
		name  string
		p     *model.Planning
		pages int
	}{
		{"no stand", planningWith(nil, 2), 1},
		{"three small stands", planningWith([]int{3, 6, 9}, 2), 3},
		{"a stand without slots", planningWith([]int{0}, 2), 1},
		{"one long stand", planningWith([]int{30}, 4), 3},
	} {
		var buf bytes.Buffer
		if err := writePlanningPDF(&buf, ev, c.p, time.Now()); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
			t.Fatalf("%s: not a PDF", c.name)
		}
		if n := countPages(buf.Bytes()); n != c.pages {
			t.Errorf("%s: %d pages, want %d", c.name, n, c.pages)
		}
	}
}

func countLines(blocks []pdfBlock) int {
	n := 0
	for _, b := range blocks {
		n += len(b.Lines)
	}
	return n
}
