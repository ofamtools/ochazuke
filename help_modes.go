package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// HelpStep is one key of a sequence as the help's steps show it: the key,
// and what it does at that point in the sequence: the pending mode it
// enters (Mode), waiting for the next key, or the final operation.
type HelpStep struct {
	Key         string `json:"key"`
	Operation   string `json:"operation,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Description string `json:"description"`
}

// helpModeDepth is how many pending modes the help expands inside one
// another: a mode's row that enters a further mode lists that mode's rows
// once more (one level of recursion), and deeper (a cycle: a mode whose
// row enters itself) the row shows as mode.enter.
const helpModeDepth = 2

// guardedOffIn is guardedOff judged in mode: a pending mode's rows are
// judged in the mode they answer in.
func (r *Runtime) guardedOffIn(b Binding, focus string, mode ModeState) bool {
	if b.When == "" {
		return false
	}
	check := r.guard(b.When)
	return check == nil || !check(r, KeyEvent{Mode: mode, Focus: focus})
}

// helpEnters reports the pending mode a row's key enters and the help rows
// the walk answers with there, when the help lists them after the row: a
// row on literal keys whose request is mode.enter into a pending mode,
// with no Help of its own (the register, macro and count rows say in
// words what their mode takes) and no operator (an operator's mode is
// every motion), within helpModeDepth, and with at least one row shown
// there. sub is nil otherwise.
func (r *Runtime) helpEnters(w helpWalk, row levelRow, depth int) (ModeState, []HelpRow) {
	b := row.Binding
	if w.enter == nil || depth >= helpModeDepth || b.Keys == "" || b.Help != "" || b.Request.Key() != "ochazuke.mode.enter" {
		return ModeState{}, nil
	}
	name := StringArg(b.Request.Args, "mode", "")
	if !r.pendingModes[name] || StringArg(b.Request.Args, "op", "") != "" {
		return ModeState{}, nil
	}
	base, _ := baseAndPrefix(w.mode)
	if r.pendingModes[base] && w.mode.Payload.Return != "" {
		base = w.mode.Payload.Return
	}
	mode := ModeState{Name: name, Payload: Payload{Prefix: StringArg(b.Request.Args, "prefix", ""), Return: base}}
	inner := w
	inner.mode = mode
	sub := r.helpRows(inner, w.enter(mode), depth+1)
	if len(sub) == 0 {
		return ModeState{}, nil
	}
	return mode, sub
}

// enteringRows are an entering row and the mode's rows after it: the row
// itself runs mode.enter (its Hint, a footer summary, gives way to it) and
// says which mode its key enters; each of the mode's rows is the whole
// sequence (the entering key, then the mode's key), its steps the entering
// step then the mode row's own. helpGroups lists them Under the key.
func (r *Runtime) enteringRows(help HelpRow, row levelRow, mode ModeState, sub []HelpRow, exact map[string]levelRow) []HelpRow {
	help.Operation = r.DisplayKey(row.Request.Key())
	if row.guard != "" {
		help.Operation += " ?"
	}
	help.Enters = mode.Name
	what := "enters " + mode.Name + ": " + helpWaits
	if fallback, err := requestArg(row.Request.Args, "request"); err == nil && fallback != nil && fallback.Verb != "" {
		what += "; alone, after the timeout, " + r.DisplayKey(fallback.Key())
		if keys := StringArg(fallback.Args, "keys", ""); keys != "" {
			what += " " + keys
		}
	}
	if row.guard != "" {
		what = "when " + row.guard + " · " + what
	}
	help.Description = what
	help.Hint = ""
	enter := HelpStep{Key: help.Final, Operation: help.Operation, Mode: mode.Name, Description: "enters " + mode.Name + ": " + helpWaits}
	help.Steps = r.helpSteps(help, exact, enter)
	lead := help.Steps
	if lead == nil {
		lead = []HelpStep{enter}
	}
	out := []HelpRow{help}
	for _, s := range sub {
		own := s.Steps
		if own == nil {
			own = []HelpStep{{Key: s.Final, Operation: s.Operation, Mode: s.Enters, Description: s.Description}}
			if s.Enters != "" {
				own[0].Description = "enters " + s.Enters + ": " + helpWaits
			}
		}
		s.Steps = append(append([]HelpStep{}, lead...), own...)
		s.Key = help.Key + " " + s.Key
		s.Prefix, s.Final = SplitKeys(s.Key)
		s.Section = help.Section
		out = append(out, s)
	}
	return out
}

// helpSteps are a row's keys one by one, last the given final step: each
// key before it waits for the next (and when a row is bound on the keys
// so far, says what they run alone, after the timeout). A single key has
// no steps (nil).
func (r *Runtime) helpSteps(help HelpRow, exact map[string]levelRow, final HelpStep) []HelpStep {
	parts := strings.Fields(help.Prefix)
	if len(parts) == 0 {
		return nil
	}
	steps := make([]HelpStep, 0, len(parts)+1)
	for i, key := range parts {
		so := strings.Join(parts[:i+1], " ")
		what := fmt.Sprintf("%s (%s …)", helpWaits, so)
		if row, ok := exact[so]; ok && row.Request.Verb != "" {
			what += "; alone, after the timeout, " + r.DisplayKey(row.Request.Key())
		}
		steps = append(steps, HelpStep{Key: key, Description: what})
	}
	return append(steps, final)
}

// helpWaits is what a prefix does: a key that enters a pending mode and
// the first key of a sequence read the same.
const helpWaits = "waits for the next key"

// helpGroups gives every prefix a row (R6.10): inside each section, a key
// that starts longer sequences (g of g c, ctrl+a entering leader_pending,
// g c of g c x) is a Group row and each row is listed Under the longest
// such prefix of its key, so a sequence sits under its prefix row, which
// sits under its own: one level per key. A prefix bound to nothing alone
// gets a row of its own where its first sequence answers: no operation,
// "waits for the next key (N keys)". A row on the bare prefix keeps its
// operation and opens onto the sequences too; an entering row counts its
// keys. N counts the keys under it that run something: the sequences and
// the prefixes bound to an operation of their own, never a cancel.
func helpGroups(rows []HelpRow) []HelpRow {
	rows = cancelsWithSequences(rows)
	out := make([]HelpRow, 0, len(rows))
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].Section == rows[start].Section {
			end++
		}
		out = append(out, sectionGroups(rows[start:end])...)
		start = end
	}
	return out
}

// sectionGroups is helpGroups for one section's rows.
func sectionGroups(rows []HelpRow) []HelpRow {
	keys := map[string]bool{}
	group := map[string]bool{}
	for _, row := range rows {
		keys[row.Key] = true
		parts := strings.Fields(row.Key)
		for i := 1; i < len(parts); i++ {
			group[strings.Join(parts[:i], " ")] = true
		}
	}
	var out []HelpRow
	made := map[string]bool{}
	for _, row := range rows {
		parts := strings.Fields(row.Key)
		for i := 1; i < len(parts); i++ {
			prefix := strings.Join(parts[:i], " ")
			if keys[prefix] || made[prefix] {
				continue
			}
			made[prefix] = true
			out = append(out, prefixRow(prefix, row))
		}
		out = append(out, row)
	}
	count := map[string]int{}
	for i := range out {
		row := &out[i]
		row.Group = group[row.Key]
		row.Under = ""
		parts := strings.Fields(row.Key)
		for j := len(parts) - 1; j >= 1; j-- {
			if prefix := strings.Join(parts[:j], " "); group[prefix] {
				if row.Under == "" {
					row.Under = prefix
				}
				// A key counts when it runs something: a sequence, or a
				// prefix bound to an operation of its own (g c beside g c x).
				if !row.cancel && (!group[row.Key] || row.Operation != "" && row.Enters == "") {
					count[prefix]++
				}
			}
		}
	}
	for i := range out {
		row := &out[i]
		if !row.Group || !strings.Contains(row.Description, helpWaits) || row.Operation != "" && row.Enters == "" {
			continue
		}
		n := count[row.Key]
		word := "keys"
		if n == 1 {
			word = "key"
		}
		row.Description = strings.Replace(row.Description, helpWaits, fmt.Sprintf("%s (%d %s)", helpWaits, n, word), 1)
	}
	return out
}

// prefixRow is the row a prefix bound to nothing alone gets (helpGroups),
// placed where first lists a sequence on it: no operation, helpWaits, which
// helpGroups follows with the count of keys.
func prefixRow(prefix string, first HelpRow) HelpRow {
	p, f := SplitKeys(prefix)
	return HelpRow{Key: prefix, Prefix: p, Final: f, Description: helpWaits, Noun: first.Noun, Section: first.Section}
}

// cancelsWithSequences moves a generated prefix cancel (g esc) into the
// section that lists sequences on its prefix when its own lists none (the
// list's g esc beside g g, a motion, so under movement), the first such
// section; a cancel no section needs is left out.
func cancelsWithSequences(rows []HelpRow) []HelpRow {
	has := map[string]bool{} // section + prefix of a sequence that is no cancel
	var sections []string
	for _, row := range rows {
		if !slices.Contains(sections, row.Section) {
			sections = append(sections, row.Section)
		}
		if row.cancel {
			continue
		}
		parts := strings.Fields(row.Key)
		for i := 1; i < len(parts); i++ {
			has[row.Section+"\x00"+strings.Join(parts[:i], " ")] = true
		}
	}
	out := make([]HelpRow, 0, len(rows))
	moved := map[string][]HelpRow{}
	for _, row := range rows {
		if !row.cancel || has[row.Section+"\x00"+row.Prefix] {
			out = append(out, row)
			continue
		}
		for _, section := range sections {
			if has[section+"\x00"+row.Prefix] {
				row.Section = section
				moved[section] = append(moved[section], row)
				break
			}
		}
	}
	if len(moved) == 0 {
		return out
	}
	var again []HelpRow
	for i, row := range out {
		again = append(again, row)
		if i+1 == len(out) || out[i+1].Section != row.Section {
			again = append(again, moved[row.Section]...)
		}
	}
	return again
}
