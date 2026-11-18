package model

import (
	"sort"
	"time"
)

// SlotView is a slot with everything needed to render it.
type SlotView struct {
	Slot
	Stand   *Stand
	Signups []SignupRow
	Count   int
	Status  string // one of the Fill* constants
	Missing int
	Full    bool
	Mine    bool // the current volunteer is signed up
	Clash   bool // Mine, and overlaps another slot of the current volunteer
}

// StandView is a stand with its slots, in chronological order.
type StandView struct {
	Stand
	Slots   []*SlotView
	Missing int // people still needed over all slots
	Needed  int // sum of minimums
	Count   int // sum of sign-ups
}

// Column is a distinct time interval of the planning grid.
type Column struct {
	StartsAt time.Time
	EndsAt   time.Time
}

// Row is one stand in the grid; Cells is aligned with Planning.Columns.
type Row struct {
	Stand *StandView
	Cells [][]*SlotView // usually zero or one slot per cell
}

// TimeGroup lists, for one column, the slots of every stand (mobile view).
type TimeGroup struct {
	Column
	Slots []*SlotView
}

// Planning is the full, ready-to-render view of an event.
type Planning struct {
	Stands   []*StandView
	Columns  []Column
	Rows     []Row
	Groups   []TimeGroup
	MultiDay bool
	Missing  int
	Needed   int
	Count    int
	MySlots  []*SlotView
	MyID     int64 // the current volunteer, 0 if unknown
}

// BuildPlanning assembles stands, slots and sign-ups into a Planning.
// stands must be in display order. myVolunteerID may be 0.
func BuildPlanning(stands []Stand, slots []Slot, signups []SignupRow, myVolunteerID int64) *Planning {
	p := &Planning{MyID: myVolunteerID}
	byStand := make(map[int64]*StandView, len(stands))
	for i := range stands {
		sv := &StandView{Stand: stands[i]}
		p.Stands = append(p.Stands, sv)
		byStand[sv.ID] = sv
	}

	bySlot := make(map[int64][]SignupRow)
	for _, su := range signups {
		bySlot[su.SlotID] = append(bySlot[su.SlotID], su)
	}

	sorted := append([]Slot(nil), slots...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].StartsAt.Equal(sorted[j].StartsAt) {
			return sorted[i].StartsAt.Before(sorted[j].StartsAt)
		}
		return sorted[i].EndsAt.Before(sorted[j].EndsAt)
	})

	colIndex := map[Column]int{}
	var all []*SlotView
	for _, sl := range sorted {
		st := byStand[sl.StandID]
		if st == nil {
			continue
		}
		v := &SlotView{Slot: sl, Stand: &st.Stand, Signups: bySlot[sl.ID]}
		v.Count = len(v.Signups)
		v.Status = sl.FillStatus(v.Count)
		v.Missing = sl.Missing(v.Count)
		v.Full = sl.IsFull(v.Count)
		for _, su := range v.Signups {
			if myVolunteerID != 0 && su.VolunteerID == myVolunteerID {
				v.Mine = true
			}
		}
		st.Slots = append(st.Slots, v)
		st.Missing += v.Missing
		st.Needed += sl.MinPeople
		st.Count += v.Count
		all = append(all, v)
		if v.Mine {
			p.MySlots = append(p.MySlots, v)
		}

		c := Column{StartsAt: sl.StartsAt, EndsAt: sl.EndsAt}
		if _, ok := colIndex[c]; !ok {
			colIndex[c] = len(p.Columns)
			p.Columns = append(p.Columns, c)
		}
	}

	for _, st := range p.Stands {
		p.Missing += st.Missing
		p.Needed += st.Needed
		p.Count += st.Count
		row := Row{Stand: st, Cells: make([][]*SlotView, len(p.Columns))}
		for _, v := range st.Slots {
			i := colIndex[Column{StartsAt: v.StartsAt, EndsAt: v.EndsAt}]
			row.Cells[i] = append(row.Cells[i], v)
		}
		p.Rows = append(p.Rows, row)
	}

	groups := make([]TimeGroup, len(p.Columns))
	for i, c := range p.Columns {
		groups[i].Column = c
	}
	for _, v := range all {
		i := colIndex[Column{StartsAt: v.StartsAt, EndsAt: v.EndsAt}]
		groups[i].Slots = append(groups[i].Slots, v)
	}
	p.Groups = groups

	for i, a := range p.MySlots {
		for _, b := range p.MySlots[i+1:] {
			if a.Overlaps(b.Slot) {
				a.Clash, b.Clash = true, true
			}
		}
	}

	if len(p.Columns) > 0 {
		first := p.Columns[0].StartsAt
		for _, c := range p.Columns {
			if c.StartsAt.YearDay() != first.YearDay() || c.StartsAt.Year() != first.Year() {
				p.MultiDay = true
				break
			}
		}
	}
	return p
}

// Slot returns the slot view with the given id, or nil.
func (p *Planning) Slot(id int64) *SlotView {
	for _, st := range p.Stands {
		for _, v := range st.Slots {
			if v.ID == id {
				return v
			}
		}
	}
	return nil
}

// Mine is the planning restricted to the current volunteer's slots (and
// their stands): their personal overview.
func (p *Planning) Mine() *Planning {
	var stands []Stand
	var slots []Slot
	var signups []SignupRow
	for _, st := range p.Stands {
		added := false
		for _, v := range st.Slots {
			if !v.Mine {
				continue
			}
			if !added {
				stands = append(stands, st.Stand)
				added = true
			}
			slots = append(slots, v.Slot)
			signups = append(signups, v.Signups...)
		}
	}
	return BuildPlanning(stands, slots, signups, p.MyID)
}

// MyOverlaps returns the current volunteer's other slots that overlap v.
func (p *Planning) MyOverlaps(v *SlotView) []*SlotView {
	var out []*SlotView
	for _, m := range p.MySlots {
		if m.ID != v.ID && m.Overlaps(v.Slot) {
			out = append(out, m)
		}
	}
	return out
}

// MyDuration is the total time of the current volunteer's slots.
func (p *Planning) MyDuration() time.Duration {
	var d time.Duration
	for _, v := range p.MySlots {
		d += v.EndsAt.Sub(v.StartsAt)
	}
	return d
}
