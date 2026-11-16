package rulecheck

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

const hiddenKeysHeader = `rulecheck hiddenkeys baseline: table (noun/mode or #instance/mode), key, operation with its arguments, count, reason (tab separated).
A row says: the hidden row on KEY in TABLE takes a key that, at some focus, already did something: without the hidden rows, a
visible row answered KEY there, at once or under a guard. A hidden row is meant to give a verb a key
nobody uses; one that shadows a motion or a standard key changes what that key does (v0.143.0: outline k opened an args form on a
tree's top row). Fix it by removing the row or moving it to a free key.
A row with a reason is a reviewed hidden row that takes the key on purpose; a row without one is debt that only shrinks
(RULECHECK_UPDATE=1 removes and lowers rows, never adds one; adding a row is a reviewed edit).`

// HiddenKeysWidth is the number of key fields of a hiddenkeys row.
const HiddenKeysWidth = 3

// HiddenKeysCoverage is what the hiddenkeys walk saw.
type HiddenKeysCoverage struct {
	// Places and Walks are the places walked and the (place, mode) walks
	// read; Hidden the hidden rows that answered on some walk and shared
	// their key with a visible row there (the only ones compared).
	Places, Walks, Hidden int
}

func (c HiddenKeysCoverage) String() string {
	return fmt.Sprintf("places %d; walks %d; hidden rows sharing a key on a walk %d", c.Places, c.Walks, c.Hidden)
}

// HiddenKeys reports every hidden row of r that changes what a key does.
//
// The invariant: take every hidden row out of a walk at once; any key
// (or key sequence) that then resolves to some row's operation, at once
// or under a guard, must resolve the same with the hidden rows in. A
// hidden row is meant to give a verb a key nothing at that focus uses; a
// finding is a hidden row that answers before a visible row on its key
// (or on a sequence its keys begin, or on a first key its sequence
// starts with). A guarded visible row counts: collection.move on k is
// guarded (it lets the walk go on at a list's edge), so an outline k row
// behind it still changed what k does on a tree's top row (v0.143.0).
// A key that resolved to nothing without the hidden rows (only an open
// pattern such as app's catch-all, or a prefix's cancel row) is free,
// and a hidden row running the same operation the key ran is no change.
// Taking the hidden rows out all at once means two hidden rows on one key
// never report each other.
//
// The places are every place r has (Runtime.AllPlaces: the live
// instances and the recorded places), the extra places given (a recorded
// noun-places file, LoadPlaces), and every noun on its own; the modes are
// every mode the help offers there (Runtime.HelpModes with the pending
// modes, without key-sequence prefixes), so every component kind the
// runtime or the recording holds is walked. Resolution is the runtime's
// own walk (Runtime.HelpWalkAtPlace, HelpWalk.Trace; HelpWalk.With takes
// the rows out). Generated rows (noun defaults) are not hidden rows here:
// they step aside for any declared row by themselves.
//
// keep names the hidden rows that stay in (by a finding's fields): a
// baseline's accepted rows, which take their keys on purpose and so are
// part of what a key did, so a newer hidden row that shadows one of them
// is reported. A kept row is reported (so its baseline row stays
// covered) wherever it answers beside another row on its key; once it is
// gone or its key is its own, its baseline row is stale. Nil keeps none.
//
// Findings have fields table (noun/mode, or #instance/mode), keys and the
// row's operation with its arguments; the message names one place, mode
// and key, and what the key did there before. work counts one unit per
// walk and stops the walk when it returns false.
func HiddenKeys(r *ochazuke.Runtime, places []ochazuke.NounPlace, keep func(Finding) bool, work func(int) bool) ([]Finding, HiddenKeysCoverage) {
	var cov HiddenKeysCoverage
	if keep == nil {
		keep = func(Finding) bool { return false }
	}
	c := hiddenCheck{keep: keep, seen: map[string]bool{}}
	walked := append(r.AllPlaces(), places...)
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		walked = append(walked, ochazuke.NounPlace{Noun: name})
	}
	found := map[string]Finding{}
	seenPlace := map[string]bool{}
	for _, p := range walked {
		if r.Nouns[r.CanonicalNoun(p.Noun)] == nil {
			continue
		}
		pk := strings.Join(p.Chain(), ">") + "\x00" + p.Modal
		if seenPlace[pk] {
			continue
		}
		seenPlace[pk] = true
		cov.Places++
		for _, m := range r.HelpModes(p, true) {
			if m.Payload.Prefix != "" {
				continue
			}
			if !work(1) {
				return sortedFindings(found), cov
			}
			cov.Walks++
			w := r.HelpWalkAtPlace(p, m)
			for _, f := range c.findings(w) {
				if _, dup := found[f.Key()]; !dup {
					f.Message = fmt.Sprintf("hiddenkeys: %s (%s): %s", strings.Join(p.Chain(), " > "), m.Name, f.Message)
					found[f.Key()] = f
				}
			}
		}
	}
	cov.Hidden = len(c.seen)
	return sortedFindings(found), cov
}

func sortedFindings(found map[string]Finding) []Finding {
	out := make([]Finding, 0, len(found))
	for _, k := range slices.Sorted(maps.Keys(found)) {
		out = append(out, found[k])
	}
	return out
}

// hiddenCheck is one HiddenKeys run: the rows it keeps and the hidden
// rows it compared.
type hiddenCheck struct {
	keep func(Finding) bool
	seen map[string]bool
}

// out reports whether a row of table is one the check takes out: hidden,
// declared, on keys, and not kept.
func (c hiddenCheck) out(table string, b ochazuke.Binding) bool {
	return b.Hidden && !b.Generated && b.Keys != "" && !c.keep(Finding{Fields: []string{table, b.Keys, operationText(b)}})
}

// findings compares the walk with and without its hidden rows on every
// key a hidden row that answers there touches.
func (c hiddenCheck) findings(w ochazuke.HelpWalk) []Finding {
	var drop []ochazuke.Binding
	keys := map[string]bool{}
	var out []Finding
	for i, e := range w.Entries {
		table := tableName(w.TableOf(e))
		if !e.Row.Hidden || e.Row.Generated || e.Row.Keys == "" || !e.State.Answering() || !c.sharesKey(w, i) {
			continue
		}
		if !c.out(table, e.Row) {
			f := Finding{Fields: []string{table, e.Row.Keys, operationText(e.Row)}}
			f.Message = fmt.Sprintf("hidden row %s on %q in %s is kept (baseline) and shares its key here", operationText(e.Row), e.Row.Keys, table)
			out = append(out, f)
			continue
		}
		drop = append(drop, e.Row)
		c.seen[table+"\t"+e.Row.Keys+"\t"+operationText(e.Row)] = true
		f := strings.Fields(e.Row.Keys)
		for n := 1; n <= len(f); n++ {
			keys[strings.Join(f[:n], " ")] = true
		}
		for _, o := range w.Entries {
			if o.Row.Keys != "" && strings.HasPrefix(o.Row.Keys, e.Row.Keys+" ") {
				keys[o.Row.Keys] = true
			}
		}
	}
	if len(drop) == 0 {
		return out
	}
	without := c.withoutRows(w, drop)
	reported := map[string]bool{}
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		before := c.effectiveSteps(without.Trace(k), false)
		if len(before) == 0 {
			continue
		}
		for _, s := range c.effectiveSteps(w.Trace(k), true) {
			table := labelTable(s.Table)
			if !c.out(table, s.Row) {
				continue // a visible or kept row: it answered (or let the walk go on) without the hidden ones too
			}
			if slices.ContainsFunc(before, func(b ochazuke.HelpTraceStep) bool { return operationText(b.Row) == operationText(s.Row) }) {
				continue
			}
			f := Finding{Fields: []string{table, s.Row.Keys, operationText(s.Row)}}
			if reported[f.Key()] {
				continue
			}
			reported[f.Key()] = true
			var did []string
			for _, b := range before {
				did = append(did, operationText(b.Row)+" at "+b.Level+" ("+b.Outcome+")")
			}
			f.Message = fmt.Sprintf("hidden row %s on %q in %s takes %q, which did %s without the hidden rows. Remove the row or give it a free key (or add a reviewed row with a reason)", operationText(s.Row), s.Row.Keys, table, k, strings.Join(did, "; then "))
			out = append(out, f)
		}
	}
	return out
}

// sharesKey reports whether a row the check keeps in takes e's keys, a
// sequence they begin, or a key that begins them: the only hidden rows
// whose removal can change a key's resolution.
func (c hiddenCheck) sharesKey(w ochazuke.HelpWalk, i int) bool {
	e := w.Entries[i]
	for j, o := range w.Entries {
		if j == i || o.Row.Keys == "" || isPrefixCancel(o.Row) || c.out(tableName(w.TableOf(o)), o.Row) {
			continue
		}
		if keysRelated(e.Row.Keys, o.Row.Keys) {
			return true
		}
	}
	return false
}

func keysRelated(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+" ") || strings.HasPrefix(b, a+" ")
}

// effectiveSteps are the steps of a trace that do something with the key,
// in resolution order up to the first that answers unguarded: an answer
// (at once, under a guard, or of a first key alone before a sequence),
// and, with hiddenWaits, a hidden row's wait for its longer sequence (a
// hidden "d a" makes d wait where d answered alone). An open pattern (a
// catch-all), a prefix's cancel row and a visible row's wait are not the
// key doing something; steps not reached or stepping aside are left out.
func (c hiddenCheck) effectiveSteps(tr ochazuke.HelpTrace, hiddenWaits bool) []ochazuke.HelpTraceStep {
	var out []ochazuke.HelpTraceStep
	for _, s := range tr.Steps {
		switch {
		case s.Row.Keys == "", isPrefixCancel(s.Row), s.Row.Request.Empty():
			continue
		case s.Hit == ochazuke.TraceLonger && !(hiddenWaits && c.out(labelTable(s.Table), s.Row)):
			continue
		case strings.HasPrefix(s.Outcome, "not reached"), strings.HasPrefix(s.Outcome, "steps aside"):
			continue
		}
		out = append(out, s)
		if s.Outcome == "ANSWERS" {
			break
		}
	}
	return out
}

// withoutRows is the walk with the hidden rows taken out. HelpWalk.With
// drops every row with a dropped row's identity in its table (keys,
// pattern and guard); the visible ones it took with them are put back.
func (c hiddenCheck) withoutRows(w ochazuke.HelpWalk, drop []ochazuke.Binding) ochazuke.HelpWalk {
	var put []ochazuke.Binding
	for _, o := range w.Entries {
		ot := w.TableOf(o)
		if c.out(tableName(ot), o.Row) {
			continue
		}
		for _, d := range drop {
			if d.Noun == ot.Noun && d.Instance == ot.Instance && d.Mode == ot.Mode && d.Keys == o.Row.Keys && d.Pattern == o.Row.Pattern && d.When == o.Row.When {
				put = append(put, o.Row)
				break
			}
		}
	}
	out := w.With(drop, nil, nil)
	if len(put) > 0 {
		out = out.With(nil, put, nil)
	}
	return out
}

func isPrefixCancel(b ochazuke.Binding) bool {
	return strings.HasSuffix(b.Keys, " "+ochazuke.CancelKey) && b.Request.Key() == "ochazuke.mode.leave" && len(b.From) == 0 && b.When == ""
}

// operationText is a row's operation with its arguments: "noun.verb" or
// "noun.verb {json}".
func operationText(b ochazuke.Binding) string {
	if len(b.Request.Args) == 0 {
		return b.Request.Key()
	}
	args, _ := json.Marshal(b.Request.Args)
	return b.Request.Key() + " " + string(args)
}

// labelTable is a trace step's table (WalkTable.Label: "noun mode",
// "instance id mode", "noun any mode") as tableName writes it.
func labelTable(label string) string {
	owner, mode, _ := strings.Cut(label, " ")
	if owner == "instance" {
		var id string
		id, mode, _ = strings.Cut(mode, " ")
		owner = "#" + id
	}
	if mode == "any mode" {
		mode = "any"
	}
	return owner + "/" + mode
}

// tableName is a walk table as a bindings file's context names it:
// noun/mode, #instance/mode, noun/any for the any-mode table.
func tableName(t ochazuke.WalkTable) string {
	owner := t.Noun
	if t.Instance != "" {
		owner = "#" + t.Instance
	}
	mode := t.Mode
	if mode == "" {
		mode = "any"
	}
	return owner + "/" + mode
}

// LoadPlaces reads a recorded noun-places file (the JSON array a
// runtime's placements record writes, Config.NounPlacesPath) for
// Config.Places. A missing file is no places.
func LoadPlaces(path string) ([]ochazuke.NounPlace, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ochazuke.NounPlace
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

func runHiddenKeys(t *testing.T, c *Context) {
	path := c.BaselinePath("hiddenkeys")
	base, err := LoadBaseline(path, HiddenKeysWidth)
	if err != nil {
		t.Fatalf("hiddenkeys: %s: %v", path, err)
	}
	var found []Finding
	for _, r := range c.AllRuntimes() {
		framework := map[string]bool{}
		if !c.FrameworkRows {
			framework = FrameworkRowKeys(r)
		}
		keep := func(f Finding) bool { return base[f.Key()].Count > 0 || framework[f.Key()] }
		f, cov := HiddenKeys(r, c.Places, keep, c.Work)
		c.Over(t)
		for _, x := range f {
			if !framework[x.Key()] {
				found = append(found, x)
			}
		}
		t.Log("hiddenkeys: " + cov.String())
	}
	CheckFindings(t, "hiddenkeys", path, HiddenKeysWidth, hiddenKeysHeader, dedupe(found))
}

// FrameworkRowKeys are the default rows of r that ochazuke's own files
// declare (source ochazuke/...), as hiddenkeys finding keys: table, keys,
// operation. An application's hiddenkeys keeps them (they are part of
// what a key does, and ochazuke checks them against its own baseline)
// and judges its own rows only, unless Config.FrameworkRows is set.
func FrameworkRowKeys(r *ochazuke.Runtime) map[string]bool {
	out := map[string]bool{}
	for _, d := range r.DefaultBindings() {
		if !strings.HasPrefix(d.Source, ochazuke.FrameworkBindingsSource) || d.Keys == "" {
			continue
		}
		owner := d.Noun
		if d.Instance != "" {
			owner = "#" + d.Instance
		}
		mode := d.Mode
		if mode == "" {
			mode = "any"
		}
		out[Finding{Fields: []string{owner + "/" + mode, d.Keys, operationText(d.Binding)}}.Key()] = true
	}
	return out
}

func dedupe(found []Finding) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, f := range found {
		if !seen[f.Key()] {
			seen[f.Key()] = true
			out = append(out, f)
		}
	}
	return out
}
