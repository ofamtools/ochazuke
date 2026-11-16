package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The binding edit verbs: ochazuke.binding.rebind, remove, reset and unify
// (and binding.bind, which adds a key), each planned as a value
// (help_edit.go) and written to the user's bindings.json through one
// writer, writeUserBindings, which keeps the old file as bindings.json.bak
// and loads the new one. Every edit notes the file's text before and after
// on the ochazuke.binding-file thing, so history-entry.undo writes the old
// text back: undo is the runtime's own, not a stack of the page's. The
// help page's keys run these verbs (help_page_edit.go); the palette, the
// CLI and MCP run them with explicit arguments.

// bindingFileNoun is the user's bindings.json as a thing: its text, which
// set writes whole and history-entry.undo writes back.
const bindingFileNoun = "ochazuke.binding-file"

// registerBindingEdits declares the bindings file thing and the edit verbs.
func registerBindingEdits(r *Runtime) {
	r.Define(bindingFileNoun).About("Your bindings.json as one thing: its text (set writes it whole and loads it; every key edit is undone by writing the old text back)").Props(
		PropSpec{Name: "text", Multiline: true, Parameter: Parameter{Description: "The file's text; set writes it, the old file kept as bindings.json.bak, and loads it (rows that do not load are reported, never fatal); empty text removes the file"}},
		filePathProp("The user bindings file: $XDG_CONFIG_HOME/<app>/bindings.json, else ~/.config/<app>/bindings.json", "this application has no config directory", func(r *Runtime) string { return r.Config().Path(BindingsFile) }),
	).Store(Store{
		Kind: Singleton, Undo: true,
		Get: func(c Ctx, _ string) (Thing, error) {
			data, _ := os.ReadFile(c.Runtime.Config().Path(BindingsFile))
			return Thing{"text": string(data)}, nil
		},
		Patch: func(c Ctx, _ string, changes Thing) (Thing, error) {
			text, _ := changes["text"].(string)
			if path := c.Runtime.Config().Path(BindingsFile); path != "" {
				problems, err := c.Runtime.writeUserBindings(path, []byte(text))
				if err != nil {
					return nil, err
				}
				c.Runtime.tellProblems(problems)
			}
			return Thing{"text": text}, nil
		},
	})
	row := func(extra map[string]Parameter) map[string]Parameter {
		params := map[string]Parameter{
			"key":      {Type: "string", Required: true, Description: "the row's key or sequence (a key class, such as digit, for a pattern row)"},
			"noun":     {Type: "string", Plumbing: true, Description: "the noun whose table holds the row (default: the first row on the key from the focus)"},
			"instance": {Type: "string", Plumbing: true, Description: "the instance whose table holds the row, in place of a noun"},
			"mode":     {Type: "string", Plumbing: true, Description: "the row's mode (default: the current one; \"\" the any-mode table)"},
			"when":     {Type: "string", Plumbing: true, Description: "the row's guard, part of its identity"},
		}
		for k, v := range extra {
			params[k] = v
		}
		return params
	}
	edit := func(key, desc string, params map[string]Parameter, plan func(r *Runtime, a Arguments) (BindingPlan, bool)) {
		r.Register(Operation{Key: Key(key), Description: desc, Parameters: params, Positional: []string{"key"}, Result: "binding-edit", HistoryKind: HistoryChange,
			Run: func(r *Runtime, _ Context, a Arguments) (any, error) {
				return r.runUserEdit(func(BindingEditor) (BindingPlan, bool) { return plan(r, a) })
			}})
	}
	edit("ochazuke.binding.rebind", "Change a row of the key tables in your bindings.json: its key (to), its arguments, hint, hidden, guard, or the table it sits in; a default left on its old key gets an unbind row. A key that already answers something is taken, and the preview of the help page says what it replaces or shadows",
		row(map[string]Parameter{
			"to":          {Type: "string", Description: "the new key or sequence"},
			"args":        {Type: PropObject, Description: "the arguments the row passes (an empty object drops them)"},
			"hint":        {Type: "string", Description: "the hint the footer shows for it (\"\" drops it)"},
			"hint-hidden": {Type: SettingBoolean, Description: "keep the row off the hint line"},
			"guard":       {Type: "string", Description: "the row's new guard (\"\" drops it)"},
			"to-noun":     {Type: "string", Description: "move the row to this noun's table"},
			"to-mode":     {Type: "string", Description: "move the row to this mode (\"\" the any-mode table)"},
		}), func(r *Runtime, a Arguments) (BindingPlan, bool) {
			old, ok := r.editRow(a)
			if !ok {
				return BindingPlan{}, false
			}
			return r.bindingEditorNow().PlanRebind(old, r.revisionOf(a)), true
		})
	edit("ochazuke.binding.remove", "Take a row off its key in your bindings.json: your own row is deleted, a default gets an unbind row (binding.reset puts it back)", row(nil),
		func(r *Runtime, a Arguments) (BindingPlan, bool) {
			old, ok := r.editRow(a)
			if !ok {
				return BindingPlan{}, false
			}
			return r.bindingEditorNow().PlanRemove(old), true
		})
	edit("ochazuke.binding.reset", "Put the default back on a row's key: every row of your bindings.json with its table, key and guard goes (a replacing row, an unbind row), so the default answers again", row(nil),
		func(r *Runtime, a Arguments) (BindingPlan, bool) {
			old, ok := r.editRow(a)
			if !ok {
				return BindingPlan{}, false
			}
			return r.bindingEditorNow().PlanReset(old), true
		})
	r.Register(Operation{Key: "ochazuke.binding.unify", Description: "One key for a verb on every noun that has it: each noun's table in the mode gets the key (move: the noun's row of the verb leaves its old key), written to your bindings.json as one change",
		Parameters: map[string]Parameter{
			"verb":  {Type: "string", Required: true, Description: "the verb, without its noun: show, archive"},
			"key":   {Type: "string", Required: true, Description: "the key or sequence every noun gets"},
			"mode":  {Type: "string", Description: "the mode (default normal)"},
			"nouns": {Type: SettingList, Description: "only these nouns (default: every noun with the verb)"},
			"move":  {Type: SettingBoolean, Description: "move each noun's row of the verb to the key instead of adding a key beside it"},
		}, Positional: []string{"verb", "key"}, Result: "binding-edit", HistoryKind: HistoryChange,
		Run: func(r *Runtime, _ Context, a Arguments) (any, error) {
			return r.runUserEdit(func(e BindingEditor) (BindingPlan, bool) {
				verb, mode := StringArg(a, "verb", ""), StringArg(a, "mode", "normal")
				rows := r.unifyRows(verb, mode, stringsArg(a, "nouns"))
				return e.PlanUnify(verb, StringArg(a, "key", ""), rows, a["move"] == true), true
			})
		}})
}

// stringsArg is a list argument as strings: a list, or one word.
func stringsArg(a Arguments, name string) []string {
	switch v := a[name].(type) {
	case []string:
		return v
	case []any:
		var out []string
		for _, x := range v {
			out = append(out, fmt.Sprint(x))
		}
		return out
	case string:
		return strings.Fields(v)
	}
	return nil
}

// bindingEditorNow is the editor over the user's file as it stands and the
// default layer.
func (r *Runtime) bindingEditorNow() BindingEditor {
	_, doc, _ := r.readUserBindingsDoc()
	return r.bindingEditor(doc)
}

// bindingEditor is the editor over doc and the default layer.
func (r *Runtime) bindingEditor(doc BindingsDoc) BindingEditor {
	defaults := r.DefaultBindings()
	rows := make([]Binding, len(defaults))
	for i, d := range defaults {
		rows[i] = d.Binding
	}
	return BindingEditor{User: doc, Defaults: rows, Canon: r.CanonicalNoun}
}

// readUserBindingsDoc reads the user's bindings.json: its bytes and rows;
// a missing file is no rows.
func (r *Runtime) readUserBindingsDoc() ([]byte, BindingsDoc, error) {
	old, err := os.ReadFile(r.Config().Path(BindingsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return nil, BindingsDoc{}, err
	}
	doc, _, err := ParseBindings(old)
	return old, doc, err
}

// runUserEdit plans an edit over the user's file and writes it; a plan
// that changes nothing, or a file that cannot be written, is said on the
// status line and nothing is written.
func (r *Runtime) runUserEdit(plan func(BindingEditor) (BindingPlan, bool)) (any, error) {
	path := r.Config().Path(BindingsFile)
	if path == "" {
		r.UI().Info("This application has no config directory: there is no bindings.json to edit.")
		return nil, nil
	}
	old, doc, err := r.readUserBindingsDoc()
	if err != nil {
		return nil, err
	}
	p, ok := plan(r.bindingEditor(doc))
	if !ok {
		return nil, nil
	}
	if !p.Changed {
		r.UI().Info(p.Name + ": your bindings.json already says so; nothing changed.")
		return bindingEditAnswer(p, path, nil), nil
	}
	problems, err := r.applyUserPlan(path, old, p)
	if err != nil {
		return nil, err
	}
	r.UI().Info(p.Name + "." + problemsText(problems))
	return bindingEditAnswer(p, path, problems), nil
}

// bindingEditAnswer is what an edit verb answers.
func bindingEditAnswer(p BindingPlan, path string, problems []string) map[string]any {
	out := map[string]any{"edit": p.Name, "key": p.Key, "action": p.Action, "changed": p.Changed, "file": path}
	if p.How != "" {
		out["how"] = p.How
	}
	if len(problems) > 0 {
		out["problems"] = problems
	}
	return out
}

// problemsText is the load problems as a sentence for the status line.
func problemsText(problems []string) string {
	var bad []string
	for _, p := range problems {
		if !strings.HasPrefix(p, "warning:") {
			bad = append(bad, p)
		}
	}
	if len(bad) == 0 {
		return ""
	}
	return " Not loaded: " + strings.Join(bad, "; ")
}

// tellProblems says the load problems of a write on the status line.
func (r *Runtime) tellProblems(problems []string) {
	if text := problemsText(problems); text != "" {
		r.UI().Info(strings.TrimSpace(text))
	}
}

// applyUserPlan writes a plan's file and notes the change for undo: the
// file's text before and after, on the bindings file thing.
func (r *Runtime) applyUserPlan(path string, old []byte, p BindingPlan) ([]string, error) {
	data, err := FormatBindings(p.Doc)
	if err != nil {
		return nil, err
	}
	problems, err := r.writeUserBindings(path, data)
	if err != nil {
		return nil, err
	}
	r.noteChange(ThingChange{Noun: bindingFileNoun, Key: BindingsFile, Before: map[string]any{"text": string(old)}, After: map[string]any{"text": string(data)}})
	return problems, nil
}

// writeUserBindings is the one writer of the user's bindings.json: the old
// file kept as bindings.json.bak, the new text written atomically (empty
// text removes the file: the state before any edit), the file loaded
// again and an open help refreshed. It returns the load's problems.
func (r *Runtime) writeUserBindings(path string, data []byte) ([]string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 {
		if err := os.WriteFile(path+".bak", old, 0o644); err != nil {
			return nil, err
		}
	}
	if len(data) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else if err := writeFileAtomic(path, data); err != nil {
		return nil, err
	}
	r.RecordWrittenFile(path)
	problems := r.ReloadUserBindings()
	r.syncHelp()
	return problems, nil
}

// editRow is the row an edit verb names: by its table (noun or instance),
// mode, key and guard, the live row with that identity, else the default
// layer's (a row your file unbinds), else the identity alone; with no
// table, the first row on the key the walk at the focus reads. With
// nothing on the key it says so and finds none.
func (r *Runtime) editRow(a Arguments) (Binding, bool) {
	key := normalKeys(StringArg(a, "key", ""))
	mode := r.BaseMode()
	if v, ok := a["mode"].(string); ok {
		mode = v
	}
	want := withKeys(Binding{Noun: r.CanonicalNoun(StringArg(a, "noun", "")), Instance: StringArg(a, "instance", ""), Mode: mode}, key)
	want.When = StringArg(a, "when", "")
	e := BindingEditor{Canon: r.CanonicalNoun}
	if want.Noun == "" && want.Instance == "" {
		for _, row := range r.bindWalkRows() {
			if planKey(row.Binding) == planKey(want) && (want.When == "" || row.When == want.When) {
				return row.Binding.withoutAliases(), true
			}
		}
		r.UI().Info("No row on " + key + " answers from the focus; name its table with noun or instance.")
		return Binding{}, false
	}
	for _, row := range r.storedRows() {
		if e.sameRow(row, want) {
			return row.withoutAliases(), true
		}
	}
	for _, d := range r.DefaultBindings() {
		if e.sameRow(d.Binding, want) {
			return d.Binding.withoutAliases(), true
		}
	}
	return want, true
}

// revisionOf reads rebind's change arguments: each one given is set.
func (r *Runtime) revisionOf(a Arguments) BindingRevision {
	var rev BindingRevision
	if v, ok := a["to"]; ok {
		rev.Keys, rev.SetKeys = fmt.Sprint(v), true
	}
	if v, ok := a["args"]; ok {
		rev.SetArgs = true
		switch v := v.(type) {
		case map[string]any:
			rev.Args = Arguments(v)
		case string:
			_ = json.Unmarshal([]byte(v), &rev.Args)
		}
	}
	if v, ok := a["hint"]; ok {
		rev.Hint, rev.SetHint = fmt.Sprint(v), true
	}
	if v, ok := a["guard"]; ok {
		rev.When, rev.SetWhen = fmt.Sprint(v), true
	}
	if v, ok := a["hint-hidden"]; ok {
		rev.Hidden, rev.SetHidden = v == true, true
	}
	if v, ok := a["to-noun"]; ok && fmt.Sprint(v) != "" {
		rev.Noun, rev.SetTable = r.CanonicalNoun(fmt.Sprint(v)), true
	}
	if v, ok := a["to-mode"]; ok {
		rev.Mode, rev.SetMode = fmt.Sprint(v), true
	}
	return rev
}

// unifyRows are unify's rows: for each noun with the verb (only those
// named, when any are), the noun's first row of the verb in mode, else a
// row naming the noun, mode and request.
func (r *Runtime) unifyRows(verb, mode string, only []string) []Binding {
	want := map[string]bool{}
	for _, n := range only {
		want[r.CanonicalNoun(n)] = true
	}
	var out []Binding
	for _, name := range sortedNouns(r) {
		op, ok := r.Operation(name + "." + verb)
		if !ok || op.Noun() != name || len(want) > 0 && !want[name] {
			continue
		}
		q := Request{Noun: name, Verb: verb}
		row := Binding{Noun: name, Mode: mode, Request: q}
		if n := r.Nouns[name]; n != nil {
			if i := slices.IndexFunc(n.Bindings[mode], func(b Binding) bool { return b.Request.Key() == q.Key() && !b.Unbind }); i >= 0 {
				row = n.Bindings[mode][i]
				row.Noun, row.Mode = name, mode
			}
		}
		out = append(out, row.withoutAliases())
	}
	return out
}

// sortedNouns are the runtime's noun names in order.
func sortedNouns(r *Runtime) []string {
	names := make([]string, 0, len(r.Nouns))
	for name := range r.Nouns {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
