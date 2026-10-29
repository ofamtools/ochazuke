package ochazuke

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"sync"
)

// frameworkBindings are the framework's own default rows: one file per
// area, loaded where the area registers its verbs (loadFrameworkBindings).
// They are the only place a framework default key is written.
//
//go:embed bindings/*.json
var frameworkBindings embed.FS

// frameworkBindingFiles are the files NewRuntime loads, in order, after the
// framework registered its verbs and guards. app.json (NewModel),
// palette.json (NewPalette) and picker.json (NewPicker) load with the
// component that registers their verbs.
var frameworkBindingFiles = []string{
	"editor.json", "registers.json", // first: they define the pending modes (count_pending) others extend
	"chooser.json", "settings-editor.json", "list.json", "table.json", "list-modes.json",
	"tree.json", "timeline.json", "form.json", "completion.json", "yank-history.json",
	"help.json", "macros.json", "confirm.json",
	"terminal.json", "focus.json", "detail.json", "panes.json", "history.json", "modal.json",
}

// builtinSelections are the selection shapes the framework's editor file
// declares (editor.json "modes"): what SelectionShape answers for an input
// no runtime holds (NewInput alone, as in a unit test).
var builtinSelections = sync.OnceValue(func() map[string]string {
	data, err := frameworkBindings.ReadFile("bindings/editor.json")
	if err != nil {
		panic(err)
	}
	file, _, err := ParseBindings(data)
	if err != nil {
		panic(err)
	}
	out := map[string]string{}
	for name, m := range file.Modes {
		if m.Selection != "" {
			out[name] = m.Selection
		}
	}
	return out
})

// FrameworkBindingsSource is the source prefix of the framework's embedded
// files in bindings defaults ("ochazuke/bindings/list.json").
const FrameworkBindingsSource = "ochazuke/"

// loadFrameworkBindings installs one embedded framework file; a file that
// does not load is a framework bug, so it panics at startup (every test
// that builds a runtime catches it).
func (r *Runtime) loadFrameworkBindings(name string) {
	data, err := frameworkBindings.ReadFile("bindings/" + name)
	if err != nil {
		panic(err)
	}
	r.MustLoadDefaultBindings(FrameworkBindingsSource+"bindings/"+name, data)
}

// LoadDefaultBindings installs a default bindings file (the grouped
// format, ParseBindings in bindings_file.go) into the default
// layer: the noun and instance tables, below the user's bindings.json,
// exactly where the same rows bound in Go would sit. Modes come first,
// then each group's rows in file order (a row replaces the row with its
// identity in place, a guarded row goes before the unguarded row on its
// keys, a sequence gets its prefix cancel), then the group's extends.
//
// Unlike a user file, an embedded default is checked strictly: an unknown
// operation, argument, source, guard, mode or instance is an error naming
// source, the table and the key, and the rows after it are not installed.
// An application loads its files at setup, after it registered the verbs
// and guards they name and before Model.Init applies the user's file;
// MustLoadDefaultBindings panics instead, so a test that builds the app
// catches a bad row. source tags the rows in bindings defaults ("mtm/
// bindings/app.json").
func (r *Runtime) LoadDefaultBindings(source string, data []byte) error {
	file, _, err := ParseBindings(data)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	for _, name := range slices.Sorted(maps.Keys(file.Modes)) {
		m := file.Modes[name]
		switch {
		case m.Pending:
			err = r.DefineMode(name, true)
		case !r.Modes[name] && !r.pendingModes[name]:
			err = r.DefineMode(name, false)
		}
		if err == nil && m.Passthrough {
			err = r.DefinePassthrough(name)
		}
		if err == nil && m.Selection != "" {
			err = r.DefineSelection(name, m.Selection)
		}
		if err != nil {
			return fmt.Errorf("%s: mode %q: %w", source, name, err)
		}
	}
	// Resolve each group's table, and for a fallback group the keys its
	// table bound before the file.
	nouns := make([]string, len(file.Groups))
	bound := map[string]bool{}
	for i, g := range file.Groups {
		if g.Instance != "" {
			if r.Instances[g.Instance] == nil {
				return fmt.Errorf("%s: %s: unknown instance %q", source, g.label(), g.Instance)
			}
		} else {
			nouns[i] = r.Define(g.Noun).Noun().Name
		}
		if g.Fallback {
			for _, b := range r.tableRows(Binding{Noun: nouns[i], Instance: g.Instance, Mode: g.Mode}) {
				if b.Mode == g.Mode {
					bound[fallbackKey(nouns[i], g.Instance, b)] = true
				}
			}
		}
	}
	// The file is one batch: a key repeated in a table is an alternative
	// after the first, not a replacement.
	batch := rowBatch{}
	for i, g := range file.Groups {
		if g.Mode != "" && !r.Modes[g.Mode] && !r.pendingModes[g.Mode] {
			return fmt.Errorf("%s: %s: unknown mode %q", source, g.label(), g.Mode)
		}
		for _, row := range g.Rows {
			row.Noun = nouns[i]
			if g.Fallback && bound[fallbackKey(nouns[i], g.Instance, row)] {
				continue
			}
			if err := r.installDefaultRow(source, row, batch); err != nil {
				return fmt.Errorf("%s: %s %q: %w", source, g.label(), row.Keys+row.Pattern, err)
			}
		}
		if g.Extends != "" {
			if !r.Modes[g.Extends] && !r.pendingModes[g.Extends] {
				return fmt.Errorf("%s: %s: extends unknown mode %q", source, g.label(), g.Extends)
			}
			r.Define(nouns[i]).Mode(g.Mode).Extends(g.Extends)
			r.defaults.extends[nouns[i]+"/"+g.Mode] = source
		}
	}
	return nil
}

// MustLoadDefaultBindings is LoadDefaultBindings that panics on an error:
// embedded defaults are code, and a bad row fails at startup.
func (r *Runtime) MustLoadDefaultBindings(source string, data []byte) {
	if err := r.LoadDefaultBindings(source, data); err != nil {
		panic(fmt.Sprintf("default bindings: %v", err))
	}
}

// LoadDefaultBindingsFS installs every file of fsys matching the patterns
// (fs.Glob), each pattern's files in name order, each tagged with its path.
// A pattern that matches nothing is an error.
func (r *Runtime) LoadDefaultBindingsFS(fsys fs.FS, patterns ...string) error {
	for _, pattern := range patterns {
		names, err := fs.Glob(fsys, pattern)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return fmt.Errorf("default bindings: no file matches %q", pattern)
		}
		slices.Sort(names)
		for _, name := range names {
			data, err := fs.ReadFile(fsys, name)
			if err != nil {
				return err
			}
			if err := r.LoadDefaultBindings(path.Clean(name), data); err != nil {
				return err
			}
		}
	}
	return nil
}

func fallbackKey(noun, instance string, b Binding) string {
	return noun + "\x00" + instance + "\x00" + b.Mode + "\x00" + b.Keys + "\x00" + b.Pattern
}

// installDefaultRow validates and places one default row, recording its
// source.
func (r *Runtime) installDefaultRow(source string, row Binding, batch rowBatch) error {
	row, err := r.validateRow(row)
	if err != nil {
		return err
	}
	row.Request.Args = r.typedArgs(row.Request)
	row = withImplicitGuard(row)
	if row.Instance != "" {
		inst := r.Instances[row.Instance]
		inst.Bindings = withPrefixCancels(bindRow(inst.Bindings, row, batch))
	} else {
		noun := r.Nouns[row.Noun]
		noun.Bindings[row.Mode] = withPrefixCancels(bindRow(noun.Bindings[row.Mode], row, batch))
	}
	if row.Unbind {
		delete(r.defaults.rows, sourceKey(row))
	} else {
		r.defaults.rows[sourceKey(row)] = source
	}
	if r.rowTouchesPending(row) {
		r.CancelPendingBinding()
	}
	return nil
}

// typedArgs gives a file row's arguments the types its operation's
// parameters declare: an integer parameter's whole number is an int, as
// in a row built with Do. An object stays the decoded map (mode.enter's
// request reads either form).
func (r *Runtime) typedArgs(q Request) Arguments {
	if len(q.Args) == 0 {
		return q.Args
	}
	op := r.Operations[q.Key()]
	out := maps.Clone(q.Args)
	for name, v := range out {
		if n, ok := v.(float64); ok && op.Parameters[name].Type == "integer" && n == float64(int(n)) {
			out[name] = int(n)
		}
	}
	return out
}

// defaultSources is the registry behind bindings defaults: the file each
// default row came from, by sourceKey, and each noun mode's extends, by
// "noun/mode". A row or extends bound in Go has no entry.
type defaultSources struct {
	rows    map[string]string
	extends map[string]string
}

// sourceKey is a stored row's identity across tables: its table, mode,
// keys or pattern and identity guard.
func sourceKey(row Binding) string {
	owner := "noun " + row.Noun
	if row.Instance != "" {
		owner = "instance " + row.Instance
	}
	return owner + "\x00" + row.Mode + "\x00" + row.Keys + "\x00" + row.Pattern + "\x00" + row.identityGuard()
}

// forgetDefaultSource drops the file source of the row with row's identity:
// a row bound in Go took its place.
func (r *Runtime) forgetDefaultSource(row Binding) {
	delete(r.defaults.rows, sourceKey(row))
}

// Source labels of bindings defaults for rows no file declared.
const (
	// SourceGo is a default row bound in Go (Bind, ModeBuilder.Bind): an
	// application's own, or one computed at runtime (an instance's rows).
	SourceGo = "go"
	// SourceGenerated is a key sequence's prefix cancel ("g esc"), which
	// the framework adds to every table with a sequence.
	SourceGenerated = "generated"
)

// DefaultBinding is one row of the default layer and where it is declared:
// a file's source, SourceGo or SourceGenerated.
type DefaultBinding struct {
	Binding
	Source string `json:"source"`
}

// DefaultBindings are the rows of the default layer, every table as it
// stands without the user's bindings.json (the rows it replaced back in
// place, the rows it added left out), each with its source: nouns by name,
// each noun's modes by name, then instances by id, rows in table order.
func (r *Runtime) DefaultBindings() []DefaultBinding {
	nouns := map[string]map[string][]Binding{}
	for name, n := range r.Nouns {
		tables := map[string][]Binding{}
		for mode, rows := range n.Bindings {
			tables[mode] = slices.Clone(rows)
		}
		nouns[name] = tables
	}
	instances := map[string][]Binding{}
	for id, inst := range r.Instances {
		if len(inst.Bindings) > 0 {
			instances[id] = slices.Clone(inst.Bindings)
		}
	}
	if c := r.Config(); c != nil {
		for i := len(c.applied) - 1; i >= 0; i-- {
			a := c.applied[i]
			restore := func(rows []Binding) []Binding {
				rows = removeRows(rows, a.row)
				for k, b := range a.replaced {
					rows = insertRow(rows, a.at+k, b)
				}
				return withPrefixCancels(rows)
			}
			if a.row.Instance != "" {
				if rows, ok := instances[a.row.Instance]; ok {
					instances[a.row.Instance] = restore(rows)
				}
			} else if tables, ok := nouns[a.row.Noun]; ok {
				tables[a.row.Mode] = restore(tables[a.row.Mode])
			}
		}
	}
	var out []DefaultBinding
	add := func(row Binding) {
		source, ok := r.defaults.rows[sourceKey(row)]
		switch {
		case ok:
		case isPrefixCancel(row):
			source = SourceGenerated
		default:
			source = SourceGo
		}
		out = append(out, DefaultBinding{Binding: row, Source: source})
	}
	for _, name := range slices.Sorted(maps.Keys(nouns)) {
		for _, mode := range slices.Sorted(maps.Keys(nouns[name])) {
			for _, row := range nouns[name][mode] {
				row.Noun, row.Instance, row.Mode = name, "", mode
				add(row)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(instances)) {
		for _, row := range instances[id] {
			row.Noun, row.Instance = "", id
			add(row)
		}
	}
	return out
}

// defaultBindingsDoc is the default layer as a bindings file: the pending
// and passthrough modes, then each table's rows grouped by source (a table
// whose rows come from two places is two groups, in table order), each
// group tagged with it, the table's extends on its groups; a noun's mode
// that only extends another is a group of no rows.
func (r *Runtime) defaultBindingsDoc() BindingsDoc {
	doc := BindingsDoc{Modes: r.BindingsDoc().Modes}
	extended := map[string]bool{}
	for _, row := range r.DefaultBindings() {
		last := len(doc.Groups) - 1
		if last >= 0 && doc.Groups[last].Noun == row.Noun && doc.Groups[last].Instance == row.Instance && doc.Groups[last].Mode == row.Mode && doc.Groups[last].Source == row.Source {
			doc.Groups[last].Rows = append(doc.Groups[last].Rows, row.Binding)
			continue
		}
		g := BindingGroup{Noun: row.Noun, Instance: row.Instance, Mode: row.Mode, Source: row.Source, Rows: []Binding{row.Binding}}
		if row.Instance == "" {
			extended[row.Noun+"/"+row.Mode] = true
			g.Extends = r.Nouns[row.Noun].Extends[row.Mode]
		}
		doc.Groups = append(doc.Groups, g)
	}
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		for _, mode := range slices.Sorted(maps.Keys(r.Nouns[name].Extends)) {
			if !extended[name+"/"+mode] {
				source := r.defaults.extends[name+"/"+mode]
				if source == "" {
					source = SourceGo
				}
				doc.Groups = append(doc.Groups, BindingGroup{Noun: name, Mode: mode, Extends: r.Nouns[name].Extends[mode], Source: source})
			}
		}
	}
	return doc
}

// moveDefaultSource carries a row's file source to the keys the terminal
// leader moved it to (SetTerminalLeader): the row is still the file's.
func (r *Runtime) moveDefaultSource(noun string, b Binding, keys string) {
	b.Noun, b.Instance = noun, ""
	source, ok := r.defaults.rows[sourceKey(b)]
	if !ok {
		return
	}
	delete(r.defaults.rows, sourceKey(b))
	b.Keys = keys
	r.defaults.rows[sourceKey(b)] = source
}

// CheckDefaultBindings reports what is wrong with a default bindings file
// on this runtime, every problem at once: a file that does not parse, a
// table whose noun, instance or mode is unknown, an extends to an unknown
// mode, a row that does not validate (an unknown operation, argument,
// source or guard), a mode.enter into a mode that is not pending, and two
// rows that clash (the same key and guard in one table: the second would
// replace the first). It is the rule check for embedded defaults: an
// application runs it in a test on its built runtime, after its files
// loaded, for each file it embeds.
func (r *Runtime) CheckDefaultBindings(source string, data []byte) []string {
	file, _, err := ParseBindings(data)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", source, err)}
	}
	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, source+": "+fmt.Sprintf(format, args...))
	}
	for _, name := range slices.Sorted(maps.Keys(file.Modes)) {
		if pending := file.Modes[name].Pending; pending != r.pendingModes[name] {
			report("mode %q: the file says pending %v, the runtime %v", name, pending, r.pendingModes[name])
		}
	}
	known := func(mode string) bool { return mode == "" || r.Modes[mode] || r.pendingModes[mode] }
	var placed []Binding
	for _, g := range file.Groups {
		noun := ""
		if g.Instance != "" {
			if r.Instances[g.Instance] == nil {
				report("%s: unknown instance %q", g.label(), g.Instance)
				continue
			}
		} else if n := r.Noun(g.Noun); n == nil {
			report("%s: unknown noun %q", g.label(), g.Noun)
			continue
		} else {
			noun = n.Name
		}
		if !known(g.Mode) {
			report("%s: unknown mode %q", g.label(), g.Mode)
			continue
		}
		if g.Extends != "" && !known(g.Extends) {
			report("%s: extends unknown mode %q", g.label(), g.Extends)
		}
		for _, row := range g.Rows {
			row.Noun = noun
			key := row.Keys + row.Pattern
			normalized, err := r.validateRow(row)
			if err != nil {
				report("%s %q: %v", g.label(), key, err)
				continue
			}
			if normalized.Request.Key() == "ochazuke.mode.enter" {
				if mode, _ := normalized.Request.Args["mode"].(string); !r.pendingModes[mode] {
					report("%s %q: mode.enter into %q, which is not a pending mode", g.label(), key, mode)
				}
			}
			placed = append(placed, withImplicitGuard(normalized))
		}
	}
	for _, line := range r.checkFileReachability(placed) {
		report("%s", line)
	}
	return problems
}
