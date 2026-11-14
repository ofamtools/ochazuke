package rulecheck

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

const deadKeysHeader = `rulecheck deadkeys baseline: list, row noun, mode, key, sibling noun, count, reason (tab separated).
A row says: on ROW-NOUN rows of the LIST (its instance id), in MODE, KEY does nothing of the row's own (it falls to the list, page or app),
though a SIBLING-NOUN row beside it answers KEY. Count is 1. Fix it with the row noun's own row on KEY
or a key redirect (the unbound pattern, ochazuke.key.redirect {to: parent}).
A row with a reason is a reviewed difference that stays ("panes kill, items archive"); a row without one is debt that only shrinks
(RULECHECK_UPDATE=1 removes and lowers rows, never adds one; adding a row is a reviewed edit).`

// DeadKeysWidth is the number of key fields of a deadkeys row.
const DeadKeysWidth = 5

// DeadKeysCoverage is what the deadkeys walk saw: lists by their rows.
type DeadKeysCoverage struct {
	// WithRows are the lists holding rows; Mixed the ones whose rows
	// have two or more nouns (the only ones compared).
	WithRows, Mixed int
	// Empty are the lists with no rows in the fixture: not checked.
	Empty []string
}

func (c DeadKeysCoverage) String() string {
	empty := c.Empty
	more := ""
	if len(empty) > 12 {
		more = fmt.Sprintf(", … %d more", len(empty)-12)
		empty = empty[:12]
	}
	return fmt.Sprintf("lists with rows %d; with 2+ row nouns %d; empty %d (not checked): %s%s", c.WithRows, c.Mixed, len(c.Empty), strings.Join(empty, ", "), more)
}

// DeadKeys walks every list with rows in r and reports, for each pair of
// row nouns side by side in one list, the keys one answers at its own row
// level (Runtime.RowLevelKeys: its rows or its key redirect's) that the
// other does not: findings with fields list (its instance id: every
// list's noun is the collection noun, so the id is what names it), row
// noun, mode, key, sibling noun. It takes one representative row per noun
// (and every row carrying rows of its own instance), so its cost grows
// with lists times nouns times modes, never with the data. The modes are
// the rows' declared modes and the base modes their tables name;
// pending, passthrough and typing modes are left out. work counts one
// unit per RowLevelKeys and stops the walk when it returns false.
func DeadKeys(r *ochazuke.Runtime, work func(int) bool) ([]Finding, DeadKeysCoverage) {
	var found []Finding
	var cov DeadKeysCoverage
	for _, id := range slices.Sorted(maps.Keys(r.Instances)) {
		inst := r.Instances[id]
		list, ok := inst.Component.(*ochazuke.Collection)
		if !ok {
			continue
		}
		reps := representatives(r, list.Items())
		if len(reps) == 0 {
			cov.Empty = append(cov.Empty, id)
			continue
		}
		cov.WithRows++
		nouns := map[string]bool{}
		for _, rep := range reps {
			nouns[rep.noun] = true
		}
		if len(nouns) < 2 {
			continue
		}
		cov.Mixed++
		for _, mode := range rowModes(r, reps) {
			keys := make([]map[string]ochazuke.Request, len(reps))
			for i, rep := range reps {
				if !work(1) {
					return found, cov
				}
				keys[i] = r.RowLevelKeys(rep.id, ochazuke.ModeState{Name: mode})
			}
			seen := map[string]bool{}
			for b, rb := range reps {
				for a, ra := range reps {
					if ra.noun == rb.noun {
						continue
					}
					for _, key := range slices.Sorted(maps.Keys(keys[a])) {
						if answers(keys[b], key) {
							continue
						}
						f := Finding{Fields: []string{id, rb.noun, mode, key, ra.noun}}
						if seen[f.Key()] {
							continue
						}
						seen[f.Key()] = true
						f.Message = fmt.Sprintf("deadkeys: list %s: on %s rows in %s, %q does nothing of the row's own; the %s row beside it answers it with %s. Bind it on %s, add a key redirect (unbound → ochazuke.key.redirect {to: parent}), or add a reviewed row with a reason",
							id, rb.noun, mode, key, ra.noun, keys[a][key].Key(), rb.noun)
						found = append(found, f)
					}
				}
			}
		}
	}
	return found, cov
}

// answers reports whether a row's level keys answer key: its own row on
// it, an any row, a row on a sequence key begins (y of y y: the key
// waits for the rest there), or a row on a key that begins key (y y esc
// never gets past a row's y).
func answers(keys map[string]ochazuke.Request, key string) bool {
	if _, ok := keys[key]; ok {
		return true
	}
	if _, all := keys[ochazuke.Any]; all {
		return true
	}
	for k := range keys {
		if strings.HasPrefix(k, key+" ") || strings.HasPrefix(key, k+" ") {
			return true
		}
	}
	return false
}

type rep struct{ id, noun string }

// representatives are the first row of each noun, then every other row
// whose instance carries rows of its own, in list order.
func representatives(r *ochazuke.Runtime, items []ochazuke.ListItem) []rep {
	var out []rep
	first := map[string]bool{}
	for _, it := range items {
		inst := r.Instances[it.ID]
		if inst == nil {
			continue
		}
		if !first[inst.Noun] || len(inst.Bindings) > 0 {
			first[inst.Noun] = true
			out = append(out, rep{id: it.ID, noun: inst.Noun})
		}
	}
	return out
}

// rowModes are the modes the representatives' rows answer in: each
// row's declared mode (Runtime.DeclaredMode: its pin, else normal) first,
// then the other base modes their noun and instance tables name; pending,
// passthrough and typing modes and the any-mode table ("", read in every
// mode walked) are left out.
func rowModes(r *ochazuke.Runtime, reps []rep) []string {
	var out []string
	set := map[string]bool{}
	for _, rp := range reps {
		if m := r.DeclaredMode(rp.id); !slices.Contains(out, m) {
			out = append(out, m)
		}
		if n := r.Nouns[rp.noun]; n != nil {
			for mode := range n.Bindings {
				set[mode] = true
			}
		}
		for _, b := range r.Instances[rp.id].Bindings {
			set[b.Mode] = true
		}
	}
	for _, mode := range slices.Sorted(maps.Keys(set)) {
		if mode == "" || slices.Contains(out, mode) {
			continue
		}
		spec, ok := r.ModeTraits(mode)
		if !ok || spec.Pending || spec.Passthrough || spec.Typing {
			continue
		}
		out = append(out, mode)
	}
	return out
}

func runDeadKeys(t *testing.T, c *Context) {
	var found []Finding
	var cov []string
	for _, r := range c.AllRuntimes() {
		f, cv := DeadKeys(r, c.Work)
		c.Over(t)
		found = append(found, f...)
		cov = append(cov, cv.String())
	}
	for _, line := range cov {
		t.Log("deadkeys: " + line)
	}
	CheckFindings(t, "deadkeys", c.BaselinePath("deadkeys"), DeadKeysWidth, deadKeysHeader, found)
}
