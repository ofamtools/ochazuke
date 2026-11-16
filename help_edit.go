package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// Editing the user's bindings.json from the binding help page (stage 2),
// as values: what an edit writes to the file, which rows it takes out of
// and puts into the live tables, what that does to a walk (the preview),
// and the file against its start (the diff). Nothing here reads or writes
// a file or the runtime; binding_edit.go runs the plans and help_page_edit.go
// draws them. Ported from the binding help prototype's edits.js and the
// binding editor's merge rules (edit-model.js): a row's identity is its
// table, mode, keys or pattern and guard; a user row with a default's
// identity replaces it in place; a default leaves only by an unbind row.

// BindingEditor holds what a plan reads: the user's file as it stands, the
// default layer's rows (every table without the user's file; a plan reads
// only the identities it touches) and the noun spelling the runtime reads
// (CanonicalNoun), so a file row naming a noun by an alias still matches.
type BindingEditor struct {
	User     BindingsDoc
	Defaults []Binding
	Canon    func(string) string
}

// BindingPlan is one edit, planned: the user's file after it, what it is
// called, the key and operation it is about, and the rows it takes out of
// (Drop) and puts into (Put, by identity) the live tables. How says what a
// removal did: "unbound" (an unbind row for a default) or "deleted" (the
// user's own row went).
type BindingPlan struct {
	Doc    BindingsDoc
	Name   string
	How    string
	Key    string
	Action string
	Drop   []Binding
	Put    []Binding
	// Changed reports whether the file changes at all.
	Changed bool
}

// BindingRevision are the fields a rebind may change besides the keys,
// each applied only when its Set flag is true: the arguments, the hint,
// hidden, the guard (when), and the table (a noun or an instance) and mode
// it sits in.
type BindingRevision struct {
	Keys                                                    string
	Args                                                    Arguments
	Hint, When, Noun, Instance, Mode                        string
	Hidden                                                  bool
	SetKeys, SetArgs, SetHint, SetWhen, SetHidden, SetTable bool
	SetMode                                                 bool
}

// canon is the editor's noun spelling, the name itself without one.
func (e BindingEditor) canon(noun string) string {
	if e.Canon == nil || noun == "" {
		return noun
	}
	return e.Canon(noun)
}

// sameTable reports whether two rows sit in one table: the same noun (as
// the runtime reads it) or instance, and mode.
func (e BindingEditor) sameTable(a, b Binding) bool {
	if a.Instance != "" || b.Instance != "" {
		return a.Instance == b.Instance && a.Mode == b.Mode
	}
	return e.canon(a.Noun) == e.canon(b.Noun) && a.Mode == b.Mode
}

// sameRow reports whether two rows have one identity: table, keys or
// pattern, and guard.
func (e BindingEditor) sameRow(a, b Binding) bool {
	return e.sameTable(a, b) && a.Keys == b.Keys && a.Pattern == b.Pattern && a.When == b.When
}

// hasDefault reports whether the default layer has a row with row's
// identity: taking it out of the live tables needs an unbind row.
func (e BindingEditor) hasDefault(row Binding) bool {
	return slices.ContainsFunc(e.Defaults, func(d Binding) bool { return e.sameRow(d.withoutAliases(), row) })
}

// defaultsOf are the default layer's rows with row's identity.
func (e BindingEditor) defaultsOf(row Binding) []Binding {
	var out []Binding
	for _, d := range e.Defaults {
		if e.sameRow(d, row) {
			out = append(out, d.withoutAliases())
		}
	}
	return out
}

// withoutAliases is the row as a file holds it: no listing aliases, no
// generated mark.
func (b Binding) withoutAliases() Binding {
	b.Aliases, b.Generated = nil, false
	return b
}

// fileEdit is a user file being changed: its groups, rows taken out by
// identity and rows put at the end of their table's group.
type fileEdit struct {
	e   BindingEditor
	doc BindingsDoc
}

// edit starts a change of a copy of the user's file.
func (e BindingEditor) edit() *fileEdit {
	doc := BindingsDoc{Modes: e.User.Modes}
	for _, g := range e.User.Groups {
		g.Rows = slices.Clone(g.Rows)
		doc.Groups = append(doc.Groups, g)
	}
	return &fileEdit{e: e, doc: doc}
}

// remove takes every row with row's identity out of the file, unbind rows
// included.
func (f *fileEdit) remove(row Binding) {
	for i := range f.doc.Groups {
		f.doc.Groups[i].Rows = slices.DeleteFunc(f.doc.Groups[i].Rows, func(b Binding) bool { return f.e.sameRow(b, row) })
	}
}

// add puts row last in its table's group, a new group at the end when the
// file has none for it.
func (f *fileEdit) add(row Binding) {
	row = row.withoutAliases()
	for i, g := range f.doc.Groups {
		if f.e.sameTable(Binding{Noun: g.Noun, Instance: g.Instance, Mode: g.Mode}, row) {
			row.Noun, row.Instance, row.Mode = g.Noun, g.Instance, g.Mode
			f.doc.Groups[i].Rows = append(f.doc.Groups[i].Rows, row)
			return
		}
	}
	f.doc.Groups = append(f.doc.Groups, BindingGroup{Noun: row.Noun, Instance: row.Instance, Mode: row.Mode, Rows: []Binding{row}})
}

// unbind adds the unbind row of row's identity.
func (f *fileEdit) unbind(row Binding) {
	off := Binding{Noun: row.Noun, Instance: row.Instance, Mode: row.Mode, Keys: row.Keys, Pattern: row.Pattern, Unbind: true}
	off.When = row.When
	f.add(off)
}

// replace puts next where old was: one identity is one row in place; a new
// identity takes old out (an unbind row when a default has old's identity)
// and next in, replacing any row with next's identity.
func (f *fileEdit) replace(old, next Binding) {
	f.remove(old)
	if !f.e.sameRow(old, next) && f.e.hasDefault(old) {
		f.unbind(old)
	}
	f.remove(next)
	f.add(next)
}

// done is the plan: the file after, empty groups dropped, and whether it
// differs from the file before.
func (f *fileEdit) done(p BindingPlan) BindingPlan {
	f.doc.Groups = slices.DeleteFunc(f.doc.Groups, func(g BindingGroup) bool { return len(g.Rows) == 0 })
	p.Doc = f.doc
	before, _ := FormatBindings(f.e.User)
	after, _ := FormatBindings(f.doc)
	p.Changed = string(before) != string(after)
	return p
}

// normalKeys is a key or sequence as the file spells it: single spaces.
func normalKeys(keys string) string { return strings.Join(strings.Fields(keys), " ") }

// withKeys is row on keys: a pattern for a key class, else keys.
func withKeys(row Binding, keys string) Binding {
	keys = normalKeys(keys)
	row.Keys, row.Pattern = keys, ""
	if isPattern(keys) {
		row.Keys, row.Pattern = "", keys
	}
	return row
}

// planKey is how a plan and a preview name a row's keys: its keys or
// pattern.
func planKey(row Binding) string { return row.Keys + row.Pattern }

// PlanRebind plans a rebind: row (a live row) with the revision's changes,
// its keys first. A default left on a new identity gets an unbind row; a
// row on the new identity is replaced.
func (e BindingEditor) PlanRebind(row Binding, rev BindingRevision) BindingPlan {
	row = row.withoutAliases()
	next := rev.apply(row)
	f := e.edit()
	f.replace(row, next)
	var changes []string
	if planKey(next) != planKey(row) {
		changes = append(changes, planKey(row)+" to "+planKey(next))
	}
	changes = append(changes, revisedFields(row, next)...)
	name := "rebind " + row.Request.Key() + " " + strings.Join(changes, ", ")
	return f.done(BindingPlan{Name: strings.TrimSpace(name), Key: planKey(next), Action: row.Request.Key(), Drop: []Binding{row}, Put: []Binding{next}})
}

// apply is row with the revision's fields.
func (rev BindingRevision) apply(row Binding) Binding {
	next := row
	if rev.SetKeys {
		next = withKeys(next, rev.Keys)
	}
	if rev.SetArgs {
		next.Request.Args = cloneArgs(rev.Args)
		if len(next.Request.Args) == 0 {
			next.Request.Args = nil
		}
	}
	if rev.SetHint {
		next.Hint = rev.Hint
	}
	if rev.SetWhen {
		next.When = rev.When
	}
	if rev.SetHidden {
		next.Hidden = rev.Hidden
	}
	if rev.SetTable {
		next.Noun, next.Instance = rev.Noun, rev.Instance
		if rev.Instance != "" {
			next.Noun = ""
		}
	}
	if rev.SetMode {
		next.Mode = rev.Mode
	}
	return next
}

// revisedFields names the fields next changes on row besides the keys.
func revisedFields(row, next Binding) []string {
	var out []string
	for _, f := range []struct {
		changed bool
		name    string
	}{
		{fmt.Sprint(row.Request.Args) != fmt.Sprint(next.Request.Args), "args"},
		{row.Hint != next.Hint, "hint"},
		{row.When != next.When, "guard"},
		{row.Hidden != next.Hidden, "hidden"},
		{row.Noun != next.Noun || row.Instance != next.Instance, "table"},
		{row.Mode != next.Mode, "mode"},
	} {
		if f.changed {
			out = append(out, f.name)
		}
	}
	return out
}

// PlanAdd plans another key for a request: a row on keys in at's table
// (its noun or instance and mode) running q. A row on that identity is
// replaced.
func (e BindingEditor) PlanAdd(at Binding, q Request, keys string) BindingPlan {
	next := withKeys(Binding{Noun: at.Noun, Instance: at.Instance, Mode: at.Mode, Request: q}, keys)
	if next.Instance != "" {
		next.Noun = ""
	}
	if len(next.Request.Args) == 0 {
		next.Request.Args = nil
	}
	f := e.edit()
	f.remove(next)
	f.add(next)
	return f.done(BindingPlan{Name: "add " + planKey(next) + " to " + q.Key(), Key: planKey(next), Action: q.Key(), Put: []Binding{next}})
}

// PlanRemove plans taking a row off its key: the user's own row is
// deleted; a default (or a user row over one) gets an unbind row.
func (e BindingEditor) PlanRemove(row Binding) BindingPlan {
	row = row.withoutAliases()
	f := e.edit()
	f.remove(row)
	how := "deleted"
	if e.hasDefault(row) {
		f.unbind(row)
		how = "unbound"
	}
	return f.done(BindingPlan{Name: "remove " + planKey(row) + " from " + row.Request.Key(), How: how, Key: planKey(row), Action: row.Request.Key(), Drop: []Binding{row}})
}

// PlanReset plans putting the default back on a row's identity: every row
// of the user's file with it (a replacing row, an unbind row) goes, and the
// default layer's rows answer again. With no default under it the user's
// row goes and the key is free.
func (e BindingEditor) PlanReset(row Binding) BindingPlan {
	row = row.withoutAliases()
	f := e.edit()
	f.remove(row)
	put := e.defaultsOf(row)
	how := "restored"
	if len(put) == 0 {
		how = "deleted"
	}
	return f.done(BindingPlan{Name: "reset " + planKey(row) + " in " + tableLabel(row), How: how, Key: planKey(row), Action: row.Request.Key(), Drop: []Binding{row}, Put: put})
}

// PlanUnify plans one key for a verb on many nouns: each row in rows
// (one per noun: a live row of the verb, or a row naming only the noun,
// mode and request when the noun has none) gets keys, moved off its old
// key when move is set and it has one, else added beside it. One plan, so
// one undo takes it all back.
func (e BindingEditor) PlanUnify(verb, keys string, rows []Binding, move bool) BindingPlan {
	f := e.edit()
	p := BindingPlan{Key: normalKeys(keys), Action: "*." + verb}
	for _, row := range rows {
		row = row.withoutAliases()
		next := withKeys(row, keys)
		if move && planKey(row) != "" {
			f.replace(row, next)
			p.Drop = append(p.Drop, row)
		} else {
			next.Hint, next.When, next.Hidden, next.Help = "", "", false, ""
			f.remove(next)
			f.add(next)
		}
		p.Put = append(p.Put, next)
	}
	how := "add"
	if move {
		how = "move"
	}
	p.Name = fmt.Sprintf("%s %s to %s on %d nouns", how, p.Key, verb, len(rows))
	return f.done(p)
}

// BindingDiffLine is one row of the user's file that an edit added,
// changed or removed: its table, the row as the file writes it and, for a
// changed row, what it was.
type BindingDiffLine struct {
	Table string `json:"table"`
	Text  string `json:"text"`
	Was   string `json:"was,omitempty"`
	// Rows are the rows of the line, their table filled in.
	Rows []Binding `json:"-"`
}

// BindingDiff is the user's file against its start: rows added, changed
// (same identity, other content) and removed.
type BindingDiff struct {
	Added   []BindingDiffLine `json:"added"`
	Changed []BindingDiffLine `json:"changed"`
	Removed []BindingDiffLine `json:"removed"`
}

// Empty reports whether the file is as it started.
func (d BindingDiff) Empty() bool {
	return len(d.Added)+len(d.Changed)+len(d.Removed) == 0
}

// Touched are the identities (sourceKey, nouns as canon reads them) of
// the rows added or changed: the rows the page marks changed.
func (d BindingDiff) Touched(canon func(string) string) map[string]bool {
	out := map[string]bool{}
	for _, l := range append(slices.Clone(d.Added), d.Changed...) {
		for _, b := range l.Rows {
			if canon != nil && b.Noun != "" {
				b.Noun = canon(b.Noun)
			}
			out[sourceKey(b)] = true
		}
	}
	return out
}

// Lines are the diff as the file view writes it: + added, ~ changed (with
// what it was), - removed.
func (d BindingDiff) Lines() []string {
	if d.Empty() {
		return []string{"No change against the start."}
	}
	var out []string
	for _, l := range d.Added {
		out = append(out, "+ "+l.Table+"  "+l.Text)
	}
	for _, l := range d.Changed {
		out = append(out, "~ "+l.Table+"  "+l.Text+"  (was "+l.Was+")")
	}
	for _, l := range d.Removed {
		out = append(out, "- "+l.Table+"  "+l.Text)
	}
	return out
}

// DiffBindings compares two user files row by row, by identity (an unbind
// row is an identity of its own).
func DiffBindings(start, now BindingsDoc) BindingDiff {
	type side struct {
		order []string
		rows  map[string][]Binding
	}
	group := func(doc BindingsDoc) side {
		s := side{rows: map[string][]Binding{}}
		for _, g := range doc.Groups {
			for _, b := range g.Rows {
				b.Noun, b.Instance, b.Mode = g.Noun, g.Instance, g.Mode
				id := sourceKey(b) + fmt.Sprint(b.Unbind)
				if _, ok := s.rows[id]; !ok {
					s.order = append(s.order, id)
				}
				s.rows[id] = append(s.rows[id], b)
			}
		}
		return s
	}
	text := func(rows []Binding) string {
		parts := make([]string, len(rows))
		for i, b := range rows {
			data, _ := formatBindingLine(b)
			parts[i] = string(data)
		}
		return strings.Join(parts, " ")
	}
	a, b := group(start), group(now)
	var d BindingDiff
	for _, id := range b.order {
		rows := b.rows[id]
		line := BindingDiffLine{Table: tableLabel(rows[0]), Text: text(rows), Rows: rows}
		switch was, ok := a.rows[id]; {
		case !ok:
			d.Added = append(d.Added, line)
		case text(was) != line.Text:
			line.Was = text(was)
			d.Changed = append(d.Changed, line)
		}
	}
	for _, id := range a.order {
		if _, ok := b.rows[id]; !ok {
			rows := a.rows[id]
			d.Removed = append(d.Removed, BindingDiffLine{Table: tableLabel(rows[0]), Text: text(rows), Rows: rows})
		}
	}
	return d
}

// With is the walk as it would be after a plan: drop's rows out of the
// tables that hold them, put's rows in their tables by identity (in place
// of a row with the same identity, else last; a table the walk's level
// lacks in the walk's mode or any mode is made), then every entry judged
// again. guard names a new row's guard (nil: its own when); the rows the
// walk had keep theirs.
func (w HelpWalk) With(drop, put []Binding, guard func(Binding) string) HelpWalk {
	out := w
	out.Levels = make([]HelpLevel, len(w.Levels))
	for i, l := range w.Levels {
		l.Tables = slices.Clone(l.Tables)
		for t := range l.Tables {
			l.Tables[t].Rows = slices.Clone(l.Tables[t].Rows)
		}
		out.Levels[i] = l
	}
	e := BindingEditor{}
	for _, d := range drop {
		for i := range out.Levels {
			for t, table := range out.Levels[i].Tables {
				out.Levels[i].Tables[t].Rows = slices.DeleteFunc(table.Rows, func(b Binding) bool { return e.sameRow(b, d) })
			}
		}
	}
	for _, p := range put {
		out.placeRow(p)
	}
	guards := map[string]string{}
	for _, en := range w.Entries {
		guards[sourceKey(en.Row)+"\x00"+en.Row.Request.Key()] = en.Guard
	}
	guardOf := func(b Binding) string {
		if g, ok := guards[sourceKey(b)+"\x00"+b.Request.Key()]; ok {
			return g
		}
		if guard != nil {
			return guard(b)
		}
		return b.When
	}
	out.Declared = map[string]bool{}
	for k, v := range w.Declared {
		out.Declared[k] = v
	}
	for _, d := range drop {
		delete(out.Declared, d.Keys)
	}
	for _, p := range put {
		if p.Keys != "" {
			out.Declared[p.Keys] = true
		}
	}
	out.collect(guardOf)
	return out
}

// placeRow puts a row into the walk's table for it: by identity in place,
// else last; a level that owns the row's noun or instance gets the table
// when the row's mode is the walk's or any mode. A row no level owns is
// off the walk and changes nothing.
func (w *HelpWalk) placeRow(row Binding) {
	e := BindingEditor{}
	for i, l := range w.Levels {
		owns := row.Instance != "" && row.Instance == l.Instance || row.Instance == "" && row.Noun == l.Noun
		if !owns {
			continue
		}
		for t, table := range l.Tables {
			if (table.Instance != "") != (row.Instance != "") || table.Mode != row.Mode {
				continue
			}
			rows := table.Rows
			if j := slices.IndexFunc(rows, func(b Binding) bool { return e.sameRow(b, row) }); j >= 0 {
				rows[j] = row
			} else {
				rows = append(rows, row)
			}
			w.Levels[i].Tables[t].Rows = rows
			return
		}
		if row.Mode != w.Mode && row.Mode != "" {
			return
		}
		table := WalkTable{Kind: TableMode, Noun: row.Noun, Instance: row.Instance, Mode: row.Mode, Rows: []Binding{row}}
		switch {
		case row.Instance != "":
			table.Kind = TableInstance
			w.Levels[i].Tables = append([]WalkTable{table}, l.Tables...)
		case row.Mode == "":
			table.Kind = TableAnyMode
			w.Levels[i].Tables = append(l.Tables, table)
		default:
			at := 0
			for at < len(l.Tables) && l.Tables[at].Instance != "" {
				at++
			}
			w.Levels[i].Tables = slices.Insert(l.Tables, at, table)
		}
		return
	}
}

// PreviewLine is one line of an edit's preview, with the tone it is drawn
// in: "" plain, or a status (answers, maybe, shadowed), "nokey" or "muted".
type PreviewLine struct {
	Tone string `json:"tone,omitempty"`
	Text string `json:"text"`
}

// Preview tones besides the row statuses.
const (
	PreviewNoKey = "nokey"
	PreviewMuted = "muted"
)

// BindingPreview is what a plan does from the walk's focus: the lines the
// page shows before it applies, and the operations it leaves with no key
// from here (Orphans).
type BindingPreview struct {
	Lines   []PreviewLine `json:"lines"`
	Orphans []string      `json:"orphans,omitempty"`
}

// Texts are the preview's lines without their tones.
func (p BindingPreview) Texts() []string {
	out := make([]string, len(p.Lines))
	for i, l := range p.Lines {
		out[i] = l.Text
	}
	return out
}

// Preview is what plan does from the walk's focus, read off the walk
// before and the walk after (With): what its key runs now and after, the
// rows it replaces (same table, key and guard), the rows it shadows (and
// whether they were shadowed already), and the keys each operation that
// loses one keeps. Nothing is refused: a key taken by a nearer row is
// said to be taken, and the edit stays possible. display names a
// request (nil: its key).
func (w HelpWalk) Preview(plan BindingPlan, guard func(Binding) string, display func(Request) string) BindingPreview {
	if display == nil {
		display = func(q Request) string { return q.Key() }
	}
	after := w.With(plan.Drop, plan.Put, guard)
	var out BindingPreview
	add := func(tone, text string) { out.Lines = append(out.Lines, PreviewLine{tone, text}) }
	key := plan.Key
	if key == "" {
		return out
	}
	runs := func(v HelpWalk) string { return v.runsText(key, display) }
	add("", fmt.Sprintf("From %s in %s, %s runs now: %s", w.Place.Noun, w.Mode, key, runs(w)))
	tone := string(StatusMaybe)
	if first := after.firstOn(key); first >= 0 && after.Entries[first].Row.Request.Key() == plan.Action {
		tone = string(StatusAnswers)
	}
	add(tone, fmt.Sprintf("After this change, %s runs: %s", key, runs(after)))
	var hit []string
	e := BindingEditor{}
	for _, p := range plan.Put {
		for _, en := range w.Entries {
			if e.sameRow(en.Row, p) && en.Row.Request.Key() != p.Request.Key() {
				add(string(StatusShadowed), fmt.Sprintf("Replaces %s on %s in %s (same table, key and guard).", display(en.Row.Request), planKey(p), tableLabel(p)))
				hit = append(hit, en.Row.Request.Key())
			}
		}
		hit = append(hit, after.shadowLines(w, p, display, add)...)
	}
	slices.Sort(hit)
	for _, action := range slices.Compact(hit) {
		keys, here := after.keysOf(action)
		switch {
		case len(keys) == 0:
			add(PreviewNoKey, action+" keeps no key on this walk")
			out.Orphans = append(out.Orphans, action)
		case len(here) == 0:
			add("", action+" keeps "+strings.Join(keys, ", ")+" (none answers from this focus)")
			out.Orphans = append(out.Orphans, action)
		default:
			add("", action+" keeps "+strings.Join(keys, ", ")+" (from here: "+strings.Join(here, ", ")+")")
		}
	}
	if len(hit) == 0 && len(plan.Put) > 0 {
		add(string(StatusAnswers), "Nothing else loses "+key+".")
	}
	return out
}

// firstOn is the entry that takes key first on the walk (its own keys or
// pattern; a generated row that steps aside is passed over), guarded or
// not; -1 for none.
func (w HelpWalk) firstOn(key string) int {
	tokens := strings.Fields(key)
	for i, en := range w.Entries {
		if en.State == StatusStepsAside {
			continue
		}
		if hit := traceHit(en.Row, key, tokens); hit == TraceExact || hit == TracePattern {
			return i
		}
	}
	return -1
}

// runsText is what key runs on the walk, as the preview says it: the row
// that takes it first and where, with the guard it answers under, and
// whether the key also starts sequences (so a press waits).
func (w HelpWalk) runsText(key string, display func(Request) string) string {
	text := "nothing"
	if i := w.firstOn(key); i >= 0 {
		en := w.Entries[i]
		text = display(en.Row.Request) + " (at " + w.LevelLabel(en.Level) + ")"
		if en.Guard != "" {
			text += " when " + en.Guard + " holds"
		}
	}
	longer := 0
	for _, en := range w.Entries {
		if en.State != StatusStepsAside && en.Row.Keys != "" && strings.HasPrefix(en.Row.Keys, key+" ") {
			longer++
		}
	}
	if longer > 0 {
		text += fmt.Sprintf("; it also starts %d sequences, so a press waits for the next key", longer)
	}
	return text
}

// shadowLines adds the lines about a put row on the walk after (w is the
// walk before): taken first by a nearer row, the rows it shadows now, or
// off this walk. It returns the operations it newly shadows.
func (w HelpWalk) shadowLines(before HelpWalk, p Binding, display func(Request) string, add func(tone, text string)) []string {
	e := BindingEditor{}
	mine := slices.IndexFunc(w.Entries, func(en HelpEntry) bool { return e.sameRow(en.Row, p) && en.Row.Request.Key() == p.Request.Key() })
	if mine < 0 {
		add(PreviewMuted, "The row is not on this walk; it answers where its table is read.")
		return nil
	}
	if s := w.Entries[mine].State; s == StatusShadowed || s == StatusMaybe {
		by := w.Entries[w.Entries[mine].By]
		guard := ""
		if by.Guard != "" {
			guard = " when " + by.Guard
		}
		add(string(StatusMaybe), fmt.Sprintf("A nearer row answers %s first: %s at %s%s. Your row is kept and answers where that one does not.", planKey(p), display(by.Row.Request), w.LevelLabel(by.Level), guard))
	}
	var hit []string
	for _, en := range w.Entries {
		if en.By != mine {
			continue
		}
		was := slices.IndexFunc(before.Entries, func(o HelpEntry) bool {
			return o.Row.same(en.Row) && o.Row.Request.Key() == en.Row.Request.Key() && o.Row.Noun == en.Row.Noun && o.Row.Instance == en.Row.Instance
		})
		already := was >= 0 && before.Entries[was].State.Hidden()
		verb := "Shadows: "
		if en.State == StatusStepsAside {
			verb = "Makes step aside: "
		}
		text := fmt.Sprintf("%s%s (%s) at %s", verb, display(en.Row.Request), planKey(en.Row), w.LevelLabel(en.Level))
		if already {
			add(PreviewMuted, text+" (it was shadowed before too)")
			continue
		}
		add(string(StatusShadowed), text)
		hit = append(hit, en.Row.Request.Key())
	}
	return hit
}

// keysOf are the keys of every row on the walk that runs action, and the
// ones of them that answer from the walk's focus.
func (w HelpWalk) keysOf(action string) (keys, here []string) {
	for _, en := range w.Entries {
		if en.Row.Request.Key() != action {
			continue
		}
		k := planKey(en.Row)
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
		if en.State.Answering() && !slices.Contains(here, k) {
			here = append(here, k)
		}
	}
	return keys, here
}
