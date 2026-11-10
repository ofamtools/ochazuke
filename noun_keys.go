package ochazuke

import (
	_ "embed"
	"maps"
	"slices"
	"strings"
	"sync"
)

// The generated default key block (noun scaffold e): every stored noun
// gets, in normal mode on its own table, one row per template row of
// bindings/generated/noun-defaults.json whose verb it has (registered and
// available, plus the role the row names). The rows are ordinary binding
// rows marked Binding.Generated: listed by bindings list, ? help and
// bindings defaults (source NounDefaultsSource), replaced by a user row of
// the same identity, and they step aside wherever a declared row (any
// row not generated, whatever its guard: an application's, a tool's, the
// user's) on the focus's walk binds the same keys or a prefix of them
// (stepsAside). Setting keys.noun-defaults off generates none.

// NounDefaultsSetting turns the generated key block on (default) or off.
const NounDefaultsSetting = "keys.noun-defaults"

// nounKeysMode is the mode the generated rows bind in.
var nounKeysMode = "normal"

// NounDefaultsSource is the generated rows' source in bindings defaults.
const NounDefaultsSource = FrameworkBindingsSource + "bindings/generated/noun-defaults.json"

//go:embed bindings/generated/noun-defaults.json
var nounDefaultsFile []byte

// NounKeyRow is one template row: the key, the noun verb it needs (and
// runs, unless Action names another operation), its arguments, the role a
// writable prop of the noun must have, and the hint.
type NounKeyRow struct {
	Key    string    `json:"key"`
	Verb   string    `json:"verb"`
	Action string    `json:"action,omitempty"`
	Args   Arguments `json:"args,omitempty"`
	Role   string    `json:"role,omitempty"`
	Hint   string    `json:"hint,omitempty"`
	Hidden bool      `json:"hidden,omitempty"`
}

// NounKeyRows are the template rows, parsed once.
var NounKeyRows = sync.OnceValue(func() []NounKeyRow {
	var doc struct {
		Rows []NounKeyRow `json:"rows"`
	}
	if err := DecodeJSONC(nounDefaultsFile, &doc); err != nil {
		panic("noun-defaults.json: " + err.Error())
	}
	return doc.Rows
})

// ensureNounKeys (re)generates the noun's key block: its generated rows
// are dropped, then made again from the template for the verbs it has now.
func (r *Runtime) ensureNounKeys(n *Noun) {
	if n == nil || !r.modeDefined(nounKeysMode) {
		return // before the framework's modes load (ensureAllNounKeys runs then)
	}
	mode := nounKeysMode
	rows := n.Bindings[mode]
	if slices.ContainsFunc(rows, func(b Binding) bool { return b.Generated }) {
		n.Bindings[mode] = slices.DeleteFunc(slices.Clone(rows), func(b Binding) bool { return b.Generated })
		r.InvalidateBindings()
	}
	if n.Store == nil || n.NoDefaultKeys || !keyedKind(r.NounKind(n.Name)) || n.Pin != "" || !r.helpFlag(NounDefaultsSetting, true) {
		// Only things listed as rows (records, facts) get the block: live
		// instances (pages, lists) and singletons keep their components'
		// keys, and so does a noun whose instances pin a mode (an input's,
		// the picker's search: it takes text).
		return
	}
	for _, t := range NounKeyRows() {
		row, ok := r.nounKeyRow(n, t)
		if !ok {
			continue
		}
		if slices.ContainsFunc(n.Bindings[mode], func(b Binding) bool { return !b.Unbind && b.Keys == row.Keys }) {
			continue // the noun's own row on the key wins
		}
		if r.userUnbound(row) {
			continue // the user's bindings.json unbound it
		}
		_ = r.installDefaultRow(NounDefaultsSource, row, rowBatch{})
	}
}

// nounKeyRow is the template row for noun n, false when n lacks its verb
// (unregistered or unavailable) or its role.
func (r *Runtime) nounKeyRow(n *Noun, t NounKeyRow) (Binding, bool) {
	op, ok := r.Operations.Find(Key(n.Name + "." + t.Verb))
	if !ok || op.Unavailable != "" {
		return Binding{}, false
	}
	if row := standardRow(t.Verb); op.Standard == "" && (row == nil || row.optional) {
		// The noun's own verb of a free verb's name may mean something
		// else (an outline's show): only a core verb it replaces (its own
		// archive, delete, create, edit) keeps the key.
		return Binding{}, false
	}
	if t.Role != "" && writableRole(n, t.Role) == nil {
		return Binding{}, false
	}
	q := Request{Noun: n.Name, Verb: t.Verb, Args: cloneArgs(t.Args)}
	if t.Action != "" {
		action, ok := r.Operation(t.Action)
		if !ok {
			return Binding{}, false
		}
		q = Request{Noun: action.Noun(), Verb: action.Name(), Args: cloneArgs(t.Args)}
	}
	return Binding{Mode: nounKeysMode, Noun: n.Name, Keys: t.Key, Request: q, Hint: t.Hint, Hidden: t.Hidden, Generated: true}, true
}

// ensureAllNounKeys generates every stored noun's key block (after the
// framework's binding files load, and when keys.noun-defaults changes).
func (r *Runtime) ensureAllNounKeys() {
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		r.ensureNounKeys(r.Nouns[name])
	}
}

// userUnbound reports whether the user's bindings.json holds a row on
// the generated row's noun, mode and keys (an unbind, or a row a guard
// keeps from replacing it): the user's word on the key wins.
func (r *Runtime) userUnbound(row Binding) bool {
	cfg := r.Config()
	if cfg == nil {
		return false
	}
	for _, a := range cfg.applied {
		if a.row.Noun == row.Noun && a.row.Instance == "" && a.row.Mode == row.Mode && a.row.Keys == row.Keys {
			return true
		}
	}
	return false
}

// declaredKeys are the keys a declared (not generated) row binds on the
// levels in mode, what a generated row steps aside for: a row of the
// user's bindings.json or of the application (a tool's, bound in Go or
// its own files) whatever its guard, so its word on a key always wins;
// and a row of the framework's own files only where it would answer at
// the focus now (its guard and its action's availability hold: a table's
// cell-mode y off cell mode does not take y y from the thing; the
// framework keeps its files and the block apart). Prefix cancels are
// left out.
func (r *Runtime) declaredKeys(levels []level, mode ModeState, focus string, exclusive bool) map[string]bool {
	out := map[string]bool{}
	user := r.userRowKeys()
	base, _ := baseAndPrefix(mode)
	add := func(lv level, rows []Binding) {
		for _, b := range rows {
			if b.Keys == "" || b.Unbind || b.Generated || isPrefixCancel(b) || out[b.Keys] {
				continue
			}
			if user[sourceKey(b)] || focus == "" || !r.frameworkRow(b) ||
				r.rowGuardHolds(b.When, KeyEvent{Mode: mode, Focus: focus}) && r.availableWithin(b.Request, r.rowInstance(lv, b.Request, focus), mode, nil) {
				out[b.Keys] = true
			}
		}
	}
	for _, lv := range levels {
		if lv.inst != nil {
			add(lv, rowsForMode(lv.inst.Bindings, base, exclusive))
		}
		if lv.noun != nil {
			for _, rows := range r.nounTables(lv.noun, base) {
				add(lv, rows)
			}
			if !exclusive {
				add(lv, lv.noun.Bindings[""])
			}
		}
	}
	return out
}

// userRowKeys are the identities (sourceKey) of the rows the user's
// bindings.json installed.
func (r *Runtime) userRowKeys() map[string]bool {
	out := map[string]bool{}
	if cfg := r.Config(); cfg != nil {
		for _, a := range cfg.applied {
			out[sourceKey(a.row)] = true
		}
	}
	return out
}

// stepsAside reports whether a generated row on keys gives way to the
// declared keys: the same keys, or a declared prefix of them (a tool's d
// keeps answering at once over a generated d a).
func stepsAside(keys string, declared map[string]bool) bool {
	if declared[keys] {
		return true
	}
	words := strings.Fields(keys)
	for i := 1; i < len(words); i++ {
		if declared[strings.Join(words[:i], " ")] {
			return true
		}
	}
	return false
}

// withDeclared marks each level with the walk's declared keys in mode.
func (r *Runtime) withDeclared(levels []level, mode ModeState, focus string, exclusive bool) []level {
	declared := r.declaredKeys(levels, mode, focus, exclusive)
	out := slices.Clone(levels)
	for i := range out {
		out[i].declared = &declared
	}
	return out
}

func registerNounKeys(r *Runtime) {
	r.Settings().Observe(NounDefaultsSetting, func(string, any) { r.ensureAllNounKeys() })
}

// skips reports whether the level's lookup passes over row: an unbind, a
// row of the other pass, or a generated row stepping aside.
func (lv level) skips(row *Binding) bool {
	if row.Unbind || row.Capture != lv.capture {
		return true
	}
	return lv.declared != nil && generatedAside(*row, *lv.declared)
}

// generatedAside reports whether b is a generated row stepping aside for
// the declared keys.
func generatedAside(b Binding, declared map[string]bool) bool {
	return b.Generated && stepsAside(b.Keys, declared)
}

// keyedKinds are the noun kinds that get the generated key block: records,
// the things a person makes and keeps (facts are read through their
// verbs; their components keep their own keys).
var keyedKinds = []string{NounKindRecord}

func keyedKind(kind string) bool { return slices.Contains(keyedKinds, kind) }

// frameworkRow reports whether a row came from one of the framework's
// embedded binding files.
func (r *Runtime) frameworkRow(b Binding) bool {
	source, ok := r.defaults.rows[sourceKey(b)]
	return ok && strings.HasPrefix(source, FrameworkBindingsSource)
}
