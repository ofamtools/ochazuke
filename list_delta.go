package ochazuke

import (
	"fmt"
	"slices"
)

// ListPatch is a record-level change to a list's rows: the rows that
// changed or were added, whole (Upsert), and the ids of the rows that went
// away (Remove). Every other row stays as it is, in its order.
//
// At, when given, is each upserted row's index in the patched list, one per
// Upsert and ascending: the rows neither upserted nor removed keep their
// relative order and fill the indexes the upserts leave free. Without At a
// row whose id the list has replaces it in place and a new row is
// appended, in Upsert order.
type ListPatch struct {
	Upsert []ListItem
	At     []int
	Remove []string
}

// Empty reports whether the patch changes nothing.
func (p ListPatch) Empty() bool { return len(p.Upsert) == 0 && len(p.Remove) == 0 }

// Patch applies a record-level change (ListPatch) without the caller
// rebuilding the rows: it is SetItems over the patched rows, so a row that
// stays keeps its instance (and the bindings and pending jobs on it), the
// selection and the marks follow their ids, and only a removed row's
// instance is closed. An empty patch does nothing; an invalid one (an
// index out of order or out of range, a repeated id) leaves the list
// unchanged.
func (l *List) Patch(p ListPatch) error {
	if p.Empty() {
		return nil
	}
	items, err := PatchItems(l.items, p)
	if err != nil {
		return fmt.Errorf("list %s: %w", l.id, err)
	}
	return l.SetItems(items)
}

// PatchItems is rows with p applied (ListPatch); rows is not modified.
func PatchItems(rows []ListItem, p ListPatch) ([]ListItem, error) {
	if p.At != nil && len(p.At) != len(p.Upsert) {
		return nil, fmt.Errorf("patch: %d indexes for %d upserted rows", len(p.At), len(p.Upsert))
	}
	gone := make(map[string]bool, len(p.Remove)+len(p.Upsert))
	for _, id := range p.Remove {
		gone[id] = true
	}
	upserted := make(map[string]int, len(p.Upsert))
	for i, item := range p.Upsert {
		if _, twice := upserted[item.ID]; twice || gone[item.ID] {
			return nil, fmt.Errorf("patch: row %q is upserted twice or also removed", item.ID)
		}
		upserted[item.ID] = i
	}
	if p.At == nil {
		out := make([]ListItem, 0, len(rows)+len(p.Upsert))
		placed := make(map[string]bool, len(p.Upsert))
		for _, row := range rows {
			if gone[row.ID] {
				continue
			}
			if i, ok := upserted[row.ID]; ok {
				row, placed[row.ID] = p.Upsert[i], true
			}
			out = append(out, row)
		}
		for _, item := range p.Upsert {
			if !placed[item.ID] {
				out = append(out, item)
			}
		}
		return out, nil
	}
	kept := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		if _, ok := upserted[row.ID]; !ok && !gone[row.ID] {
			kept = append(kept, row)
		}
	}
	n := len(kept) + len(p.Upsert)
	out := make([]ListItem, 0, n)
	next := 0 // the next upsert
	for i := range n {
		if next < len(p.At) {
			at := p.At[next]
			if at < i || at >= n || next > 0 && at <= p.At[next-1] {
				return nil, fmt.Errorf("patch: index %d (row %q) is out of order or past the %d rows", at, p.Upsert[next].ID, n)
			}
			if at == i {
				out = append(out, p.Upsert[next])
				next++
				continue
			}
		}
		out = append(out, kept[i-next])
	}
	return out, nil
}

// sameCells reports whether rows and items draw the same cells in the
// same order, so a table's measured widths (List.widest) still hold.
// cells falls back to a row's label, so the label counts too.
func sameCells(rows, items []ListItem) bool {
	return slices.EqualFunc(rows, items, func(a, b ListItem) bool {
		return a.Label == b.Label && slices.Equal(a.Cells, b.Cells)
	})
}
