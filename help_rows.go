package ochazuke

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The binding help page's rows (help_page.go places them): every binding
// row of the program and every verb the walk leaves without a key, each
// with its status from the walk's focus, where it is defined and the
// fields the query reads. Values: no drawing, no writes.

// Source classes of HelpPageRow.Class: where a row comes from, as source:
// terms name them.
const (
	SourceUser    = "user"    // the user's bindings.json
	SourceLibrary = "library" // one of the framework's embedded files
	SourceApp     = "app"     // the application: its files or Go
)

// HelpPageRow is one line of the page's list: a binding row, or a verb
// with no key. The fields are what the columns show and the query reads.
type HelpPageRow struct {
	// Unbound marks a verb with no key (Row is then zero).
	Unbound bool   `json:"unbound,omitempty"`
	ID      string `json:"id"`
	// Row is the binding; zero for a verb with no key.
	Row       Binding   `json:"row,omitzero"`
	Key       string    `json:"key"`
	Operation string    `json:"operation"`
	Verb      string    `json:"verb"`
	Noun      string    `json:"noun"`
	Args      string    `json:"args,omitempty"`
	Mode      string    `json:"mode"`
	Source    string    `json:"source"`
	Class     string    `json:"class"`
	Guard     string    `json:"guard,omitempty"`
	Help      string    `json:"help,omitempty"`
	Hidden    bool      `json:"hidden,omitempty"`
	Level     int       `json:"level"`
	LevelName string    `json:"levelName"`
	Table     string    `json:"table"`
	State     RowStatus `json:"status"`
	// By is the row that keeps this one from answering: its key, operation
	// and level; nil when none does.
	By    *HelpBy `json:"by,omitempty"`
	Order int     `json:"order"`
}

// HelpBy is the row a shadowed, maybe or stepping-aside row gives way to.
type HelpBy struct {
	Key       string `json:"key"`
	Operation string `json:"operation"`
	Level     string `json:"level"`
	Guard     string `json:"guard,omitempty"`
	Capture   bool   `json:"capture,omitempty"`
}

// Status is the row's status from the walk's focus.
func (v HelpPageRow) Status() RowStatus { return v.State }

// StatusText is the status column: the status, and for one a nearer row
// decides, that row's level ("shadowed (pane)") or its keys ("steps aside
// (y)") or guard ("maybe (has-rows)").
func (v HelpPageRow) StatusText() string {
	s := string(v.State)
	if v.By == nil {
		return s
	}
	switch v.State {
	case StatusShadowed:
		return s + " (" + v.By.Level + ")"
	case StatusStepsAside:
		return s + " (" + v.By.Key + ")"
	case StatusMaybe:
		return s + " (" + cmp.Or(v.By.Guard, v.By.Key) + ")"
	}
	return s
}

// OnWalk reports whether the row belongs to a level of the walk.
func (v HelpPageRow) OnWalk() bool { return v.Level >= 0 }

// Defined is where the row is declared, as far as the runtime knows: the
// file, the user's bindings.json, generated, or Go; for a verb with no
// key, the noun it is a verb of.
func (v HelpPageRow) Defined() string {
	switch {
	case v.Unbound:
		return "verb of " + v.Noun
	case v.Source == SourceUser:
		return "your bindings.json"
	case v.Source == SourceGo:
		return "Go (" + v.Class + ")"
	}
	return v.Source
}

// Where is the row's definition in sentences, the page's detail lines.
func (v HelpPageRow) Where() []string {
	if v.Unbound {
		return []string{v.Operation + " has no key from this focus.", "It is a verb of " + v.Noun + "; the palette and the CLI run it, and y y copies a bindings.json row for it."}
	}
	lines := []string{fmt.Sprintf("%s runs %s, in the table %s.", v.Key, v.Operation, v.Table)}
	switch v.Source {
	case SourceUser:
		lines = append(lines, "Defined in your bindings.json (the user layer).")
	case SourceGenerated:
		lines = append(lines, "Generated: a prefix cancel or a noun's default key block; a declared row on its keys takes its place.")
	case SourceGo:
		lines = append(lines, "Bound in Go by the "+v.Class+" (no file declares it).")
	default:
		lines = append(lines, "Defined in "+v.Source+".")
	}
	status := "From this focus it " + v.StatusText() + "."
	if v.By != nil {
		status += " Decided by " + v.By.Key + " → " + v.By.Operation + " at " + v.By.Level + "."
	}
	return append(lines, status)
}

// BindingSource is where a stored row comes from, as the runtime records
// it when rows load: SourceUser for a row the user's bindings.json put
// there, the file a default row came from, SourceGenerated for a generated
// row (a noun's default block, a prefix cancel), else SourceGo.
func (r *Runtime) BindingSource(b Binding) string {
	if r.userRowKeys()[sourceKey(b)] {
		return SourceUser
	}
	return r.defaultSource(b)
}

// defaultSource is BindingSource for the default layer.
func (r *Runtime) defaultSource(b Binding) string {
	if source, ok := r.defaults.rows[sourceKey(b)]; ok {
		return source
	}
	if b.Generated || isPrefixCancel(b) {
		return SourceGenerated
	}
	return SourceGo
}

// sourceClass is a source's class: user, generated, library (one of the
// framework's files, or a framework operation bound in Go) or app.
func (r *Runtime) sourceClass(source string, b Binding) string {
	switch {
	case source == SourceUser:
		return SourceUser
	case source == SourceGenerated:
		return SourceGenerated
	case strings.HasPrefix(source, FrameworkBindingsSource):
		return SourceLibrary
	case source == SourceGo && r.Operations.At(Key(r.CanonicalKey(requestOperation(b.Request)))).Builtin:
		return SourceLibrary
	}
	return SourceApp
}

// HelpRows are the page's rows for a walk: the walk's entries in
// resolution order, then every other stored row (other mode for a row of a
// walk level's table, off walk for the rest), then the verbs the walk
// leaves without a key.
func (r *Runtime) HelpRows(w HelpWalk) []HelpPageRow {
	var out []HelpPageRow
	onWalk := map[string]bool{}
	by := func(i int) *HelpBy {
		if i < 0 {
			return nil
		}
		o := w.Entries[i]
		return &HelpBy{Key: rowKey(o.Row), Operation: r.displayRequest(o.Row.Request), Level: w.LevelLabel(o.Level), Guard: o.Guard, Capture: o.Row.Capture}
	}
	for _, e := range w.Entries {
		onWalk[sourceKey(e.Row)+"\x00"+fmt.Sprint(e.Capture)] = true
		v := r.helpPageRow(e.Row, len(out))
		v.Level, v.LevelName, v.Table, v.State, v.By = e.Level, w.LevelLabel(e.Level), w.TableOf(e).Label(), e.Status(), by(e.By)
		out = append(out, v)
	}
	for _, b := range r.storedRows() {
		if onWalk[sourceKey(b)+"\x00"+fmt.Sprint(b.Capture)] {
			continue
		}
		v := r.helpPageRow(b, len(out))
		v.Level = w.LevelOf(b)
		v.LevelName = w.LevelLabel(v.Level)
		v.Table = WalkTable{Noun: b.Noun, Instance: b.Instance, Mode: b.Mode}.Label()
		v.State = StatusOffWalk
		if v.Level >= 0 {
			v.State = StatusOtherMode
		}
		out = append(out, v)
	}
	for _, op := range w.Unbound {
		noun := op.Noun()
		level := slices.IndexFunc(w.Levels, func(l HelpLevel) bool { return l.Noun == noun })
		out = append(out, HelpPageRow{Unbound: true, ID: "verb:" + op.Key.Short(), Key: "(no key)", Operation: r.displayRequest(Request{Noun: noun, Verb: op.Name()}),
			Verb: op.Name(), Noun: noun, Help: op.Description, Level: level, LevelName: w.LevelLabel(level), Table: noun + " verbs", State: StatusNoKey, Order: len(out)})
	}
	return out
}

// storedRows are every stored row of every table, nouns then instances
// (AllBindings without its sort and aliases), unbind rows left out.
func (r *Runtime) storedRows() []Binding {
	var out []Binding
	for _, b := range r.AllBindings() {
		if !b.Unbind {
			b.Aliases = nil
			out = append(out, b)
		}
	}
	return out
}

// helpPageRow is a binding's page row before the walk places it.
func (r *Runtime) helpPageRow(b Binding, order int) HelpPageRow {
	source := r.BindingSource(b)
	owner := b.Noun
	if b.Instance != "" {
		owner = "instance " + b.Instance
	}
	args := ""
	if len(b.Request.Args) > 0 {
		data, _ := json.Marshal(b.Request.Args)
		args = string(data)
	}
	help := cmp.Or(b.Help, r.Operations.At(Key(r.CanonicalKey(requestOperation(b.Request)))).Description)
	guard := r.rowGuardText(b)
	return HelpPageRow{ID: "row:" + sourceKey(b) + "\x00" + fmt.Sprint(b.Capture), Row: b, Key: rowKey(b),
		Operation: r.displayRequest(b.Request), Verb: b.Request.Verb, Noun: owner, Args: args, Mode: modeLabel(b.Mode),
		Source: source, Class: r.sourceClass(source, b), Guard: guard, Help: help, Hidden: b.Hidden, Order: order}
}

// BindingsLine is a row as a bindings.json snippet the user's file takes:
// the row itself, or for a verb with no key a row on key (a free key the
// caller chose) in normal mode.
func (v HelpPageRow) BindingsLine(key string) (string, error) {
	row := v.Row
	if v.Unbound {
		noun, _ := strings.CutSuffix(v.Operation, "."+v.Verb)
		row = Binding{Noun: v.Noun, Mode: "normal", Keys: key, Request: Request{Noun: noun, Verb: v.Verb}}
	}
	row.Aliases = nil
	data, err := FormatBindings(BindingsDoc{Groups: GroupBindings([]Binding{row})})
	return strings.TrimSpace(string(data)), err
}

// FreeKeys are the plain letters no row on the walk starts with, the keys
// a verb with no key could take here.
func (w HelpWalk) FreeKeys() []string {
	taken := map[string]bool{}
	for _, e := range w.Entries {
		if e.State == StatusStepsAside || e.Row.Keys == "" {
			continue
		}
		first, _, _ := strings.Cut(e.Row.Keys, " ")
		taken[first] = true
	}
	var out []string
	for _, c := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		if !taken[string(c)] {
			out = append(out, string(c))
		}
	}
	return out
}
