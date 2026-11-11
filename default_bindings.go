package ochazuke

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
)

// frameworkBindings are the framework's own default rows: one file per
// area, loaded where the area registers its verbs (loadFrameworkBindings).
// They are the only place a framework default key is written.
//
//go:embed bindings/*.json
var frameworkBindings embed.FS

// frameworkBindingFiles are the files NewRuntime loads, in order, after the
// framework registered its verbs and guards. app.json (NewModel) and
// palette.json (NewPalette) load with the component that registers their
// verbs; picker.json holds every picker's keys, the path picker's find
// source's among them.
var frameworkBindingFiles = []string{
	"editor.json", "registers.json", // first: they define the pending modes (count_pending) others extend
	"picker.json", "settings-editor.json", "about.json", "nouns.json", "list.json", "table.json", "standard.json", "list-modes.json", "picker-browse.json",
	"tree.json", "timeline.json", "related.json", "form.json", "filter-form.json", "completion.json", "yank-history.json",
	"help.json", "macros.json", "confirm.json",
	"terminal.json", "focus.json", "detail.json", "scroll.json", "panes.json", "history.json", "docs.json", "modal.json", "text-view.json",
}

// builtinSelections are the selection shapes the framework's editor file
// declares (editor.json "modes"): what SelectionShape answers for an input
// no runtime holds (NewInput alone, as in a unit test).
var builtinSelections = sync.OnceValue(func() map[string]string {
	file := frameworkBindingDoc("editor.json")
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
	source := FrameworkBindingsSource + "bindings/" + name
	if err := r.loadDefaultDoc(source, frameworkBindingDoc(name)); err != nil {
		panic(fmt.Sprintf("default bindings: %v", err))
	}
}

// frameworkBindingDocs are the embedded files parsed once per process, by
// file name. The result is shared by every runtime and is immutable: a
// loader copies a row (cloneBinding) before it changes or keeps it.
var frameworkBindingDocs = sync.OnceValue(func() map[string]BindingsDoc {
	entries, err := frameworkBindings.ReadDir("bindings")
	if err != nil {
		panic(err)
	}
	docs := make(map[string]BindingsDoc, len(entries))
	for _, e := range entries {
		data, err := frameworkBindings.ReadFile("bindings/" + e.Name())
		if err != nil {
			panic(err)
		}
		doc, _, err := ParseBindings(data)
		if err != nil {
			panic(fmt.Sprintf("default bindings: %sbindings/%s: %v", FrameworkBindingsSource, e.Name(), err))
		}
		docs[e.Name()] = doc
	}
	return docs
})

// frameworkBindingDoc is the parsed embedded file name; it panics for a
// file that does not exist (a framework bug).
func frameworkBindingDoc(name string) BindingsDoc {
	doc, ok := frameworkBindingDocs()[name]
	if !ok {
		panic("default bindings: no embedded file " + name)
	}
	return doc
}

// cloneBinding is a row sharing no map, slice or pointer with the original,
// so a runtime can keep and change it without touching the parsed copy
// other runtimes read.
func cloneBinding(b Binding) Binding {
	b.Request.Args = deepCloneArgs(b.Request.Args)
	b.From = maps.Clone(b.From)
	b.Aliases = slices.Clone(b.Aliases)
	if b.Present != nil {
		v := *b.Present
		b.Present = &v
	}
	return b
}

func deepCloneArgs(a Arguments) Arguments {
	if a == nil {
		return nil
	}
	return Arguments(cloneValue(map[string]any(a)).(map[string]any))
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = cloneValue(x)
		}
		return out
	case Arguments:
		return deepCloneArgs(t)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = cloneValue(x)
		}
		return out
	}
	return v
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
	return r.loadDefaultDoc(source, file)
}

// loadDefaultDoc installs a parsed file. It treats file as read-only: the
// doc may be the shared parse of an embedded file.
func (r *Runtime) loadDefaultDoc(source string, file BindingsDoc) (err error) {
	for _, name := range slices.Sorted(maps.Keys(file.Modes)) {
		m := file.Modes[name]
		switch {
		case m.Pending:
			err = r.DefineMode(name, true)
		case !r.modeDefined(name):
			err = r.DefineMode(name, false)
		}
		if err == nil && m.Passthrough {
			err = r.DefinePassthrough(name)
		}
		if err == nil && m.Selection != "" {
			err = r.DefineSelection(name, m.Selection)
		}
		if err == nil && m.Extends != "" {
			if m.Extends == name || !r.modeDefined(m.Extends) {
				err = fmt.Errorf("extends unknown mode %q", m.Extends)
			} else {
				spec := r.modes[name]
				spec.Extends = m.Extends
				r.putMode(name, spec)
			}
		}
		if err == nil && m.Outside != "" {
			err = r.DefineMode(name, m.Pending, ModeOutside(m.Outside))
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
		if g.Mode != "" && !r.modeDefined(g.Mode) {
			return fmt.Errorf("%s: %s: unknown mode %q", source, g.label(), g.Mode)
		}
		for _, row := range g.Rows {
			row = cloneBinding(row)
			row.Noun = nouns[i]
			if g.Fallback && bound[fallbackKey(nouns[i], g.Instance, row)] {
				continue
			}
			if err := r.installDefaultRow(source, row, batch); err != nil {
				return fmt.Errorf("%s: %s %q: %w", source, g.label(), row.Keys+row.Pattern, err)
			}
		}
		if g.Instance == "" && len(g.Rows) == 0 {
			// An empty group is a table of the noun's own: the mode's
			// fallback (ModeExtends) does not reach the noun there.
			if n := r.Nouns[nouns[i]]; n != nil && n.Bindings[g.Mode] == nil {
				n.Bindings[g.Mode] = []Binding{}
			}
		}
		if g.Extends != "" {
			if !r.modeDefined(g.Extends) {
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

// SetDefaultBindings replaces the rows tagged with source, then reloads an
// already loaded user bindings file above them. It validates every row
// before changing any table; an error leaves the old rows and user undo log
// intact. source must be nonempty and cannot be SourceGo or SourceGenerated.
// Modes and table inheritance are unchanged; declare those before calling.
// A nil slice removes the source's rows. User rows for removed defaults
// remain in bindings.json and are reapplied with a notice (an unbind has
// nothing to remove until that default returns). Call on the runtime's loop.
func (r *Runtime) SetDefaultBindings(source string, rows []Binding) error {
	r.InvalidateBindings()
	if strings.TrimSpace(source) == "" || source == SourceGo || source == SourceGenerated {
		return fmt.Errorf("default bindings: source %q must be nonempty and not reserved", source)
	}
	normalized := make([]Binding, len(rows))
	for i, row := range rows {
		var err error
		normalized[i], err = r.validateRow(row)
		if err != nil {
			return fmt.Errorf("%s: row %d %q: %w", source, i+1, row.Keys+row.Pattern, err)
		}
		normalized[i].Request.Args = r.typedArgs(normalized[i].Request)
	}
	c := r.Config()
	loaded := c.bindingsLoaded
	r.unapplyUserBindings()
	for _, old := range r.DefaultBindings() {
		if r.defaults.rows[sourceKey(old.Binding)] != source {
			continue
		}
		row := old.Binding
		rest := withPrefixCancels(removeRows(r.tableRows(row), row))
		if row.Instance != "" {
			r.Instances[row.Instance].Bindings = rest
		} else {
			r.Nouns[row.Noun].Bindings[row.Mode] = rest
		}
		key := sourceKey(row)
		delete(r.defaults.rows, key)
		if r.defaults.removed == nil {
			r.defaults.removed = map[string]string{}
		}
		r.defaults.removed[key] = source
		if r.rowTouchesPending(row) {
			r.CancelPendingBinding()
		}
	}
	batch := rowBatch{}
	for _, row := range normalized {
		r.placeDefaultRow(source, row, batch)
	}
	if loaded {
		r.ReloadUserBindings()
	}
	r.syncHelp()
	return nil
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
	r.placeDefaultRow(source, row, batch)
	return nil
}

// placeDefaultRow installs an already validated row; replacement validates
// its whole batch before removing the previous source or the user layer.
func (r *Runtime) placeDefaultRow(source string, row Binding, batch rowBatch) {
	r.InvalidateBindings()
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
		delete(r.defaults.removed, sourceKey(row))
	}
	if r.rowTouchesPending(row) {
		r.CancelPendingBinding()
	}
}

// typedArgs gives a file row's arguments the types its operation's
// parameters declare: an integer parameter's whole number is an int, as
// in a row built with Do. An object stays the decoded map (mode.enter's
// request reads either form).
func (r *Runtime) typedArgs(q Request) Arguments {
	if len(q.Args) == 0 {
		return q.Args
	}
	op := r.Operations.At(Key(q.Key()))
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
	// removed remembers identities removed by SetDefaultBindings so a
	// user's surviving override or unbind is reported on later reloads.
	removed map[string]string
}

// sourceKey is a stored row's identity across tables: its table, mode,
// keys or pattern and identity guard.
func sourceKey(row Binding) string {
	owner := "noun " + row.Noun
	if row.Instance != "" {
		owner = "instance " + row.Instance
	}
	return owner + "\x00" + row.Mode + "\x00" + row.Keys + "\x00" + row.Pattern + "\x00" + row.When
}

// forgetDefaultSource drops the file source of the row with row's identity:
// a row bound in Go took its place.
func (r *Runtime) forgetDefaultSource(row Binding) {
	delete(r.defaults.rows, sourceKey(row))
	delete(r.defaults.removed, sourceKey(row))
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
	return r.defaultRows(func(string, string) bool { return true }, true)
}

// RowsOf are the default rows of one noun's table in mode, as
// DefaultBindings lists them, without building the other tables. An unknown
// noun or mode has none.
func (r *Runtime) RowsOf(noun, mode string) []DefaultBinding {
	return r.defaultRows(func(n, m string) bool { return n == noun && m == mode }, false)
}

// defaultRows is DefaultBindings for the noun tables want accepts; all also
// lists the instance tables and keeps a table a user row created.
func (r *Runtime) defaultRows(want func(noun, mode string) bool, all bool) []DefaultBinding {
	nouns := map[string]map[string][]Binding{}
	for name, n := range r.Nouns {
		tables := map[string][]Binding{}
		for mode, rows := range n.Bindings {
			if want(name, mode) {
				tables[mode] = slices.Clone(rows)
			}
		}
		if all || len(tables) > 0 {
			nouns[name] = tables
		}
	}
	instances := map[string][]Binding{}
	if all {
		for id, inst := range r.Instances {
			if len(inst.Bindings) > 0 {
				instances[id] = slices.Clone(inst.Bindings)
			}
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
				if _, has := tables[a.row.Mode]; has || all {
					tables[a.row.Mode] = restore(tables[a.row.Mode])
				}
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
	doc := BindingsDoc{Modes: r.bindingModes()}
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
		if pending := file.Modes[name].Pending; pending != r.modes[name].Pending {
			report("mode %q: the file says pending %v, the runtime %v", name, pending, r.modes[name].Pending)
		}
	}
	known := func(mode string) bool { return mode == "" || r.modeDefined(mode) }
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
				if mode, _ := normalized.Request.Args["mode"].(string); !r.modes[mode].Pending {
					report("%s %q: mode.enter into %q, which is not a pending mode", g.label(), key, mode)
				}
			}
			placed = append(placed, normalized)
		}
	}
	for _, line := range r.checkFileReachability(placed) {
		report("%s", line)
	}
	return problems
}
