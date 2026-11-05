package ochazuke

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Unbound verbs. The ? help lists the keys the focus answers to; a verb its
// noun has that no row binds has no key to list, and was therefore out of
// reach of anyone who did not know to open the palette. The help lists
// every such verb too, in a section "<Noun> · unbound" with the key column
// blank (HelpRow.Unbound). From there enter runs the verb as a key would
// (bindings-help.run: the same availability, rights and confirm, a missing
// argument opens its form) and b asks for a key and writes the row to the
// user's bindings.json (bindings-help.bind, which calls bindings.bind).
//
// The list is read from the catalog's operations and the walk's rows alone:
// a verb is unbound when its noun is on the focus's path, the verb is
// available there (VerbShown), and no row of the walk requests it in the
// mode. Mounted extension verbs are operations of the host runtime like any
// other, so they list the same way.

// The settings of the unbound listing, rows of settings/ochazuke.json
// (section help): whether the help lists unbound verbs at all (default
// on), and whose verbs: focus (the focused instance's noun, the default),
// chain (every noun on the focus chain) or all (also the framework's own
// verbs, which are many: list.move, input.insert...).
const (
	UnboundVerbsSetting = "help.unbound-verbs"
	UnboundScopeSetting = "help.unbound-scope"
)

// Unbound scopes (UnboundScopeSetting).
const (
	UnboundFocus = "focus"
	UnboundChain = "chain"
	UnboundAll   = "all"
)

// unboundSuffix follows a noun's label in the section of its unbound verbs.
const unboundSuffix = " · unbound"

// UnboundVerbs are the operations the focus's noun (and, by the scope
// setting, the nouns of its path) has that no row of the focus's walk binds
// in mode, available at the focus: each noun in path order (the focused
// instance first), its verbs by name. Nothing while help.unbound-verbs is
// off.
func (r *Runtime) UnboundVerbs(focus string, mode ModeState) []Operation {
	if r.Instances[focus] == nil {
		return nil
	}
	var nouns []string
	for _, id := range r.Ancestry(focus) {
		nouns = append(nouns, r.Instances[id].Noun)
	}
	return r.unboundOperations(nouns, r.walk(focus), mode, focus)
}

// PlaceUnboundVerbs is UnboundVerbs for a hypothetical focus on place's
// noun (PlaceHelp): the place's chain innermost first, the walk nounWalk
// reads, and no availability judged, as a guarded row is not.
func (r *Runtime) PlaceUnboundVerbs(place NounPlace, mode ModeState) []Operation {
	place = r.canonicalPlace(place)
	chain := slices.Clone(place.Chain())
	slices.Reverse(chain)
	return r.unboundOperations(chain, r.nounWalk(place, mode), mode, "")
}

// unboundOperations is the shared reading: nouns innermost first (cut to
// the scope setting), walk the levels whose rows bind, focus "" for a
// hypothetical walk. A row counts as binding its verb whether or not its
// guard holds now, and whether or not a row earlier on its key shadows it
// (the first row on a key answers; the others are the key's alternatives).
func (r *Runtime) unboundOperations(candidates []string, walk []level, mode ModeState, focus string) []Operation {
	if !r.helpFlag(UnboundVerbsSetting, true) {
		return nil
	}
	scope := r.Settings().String(UnboundScopeSetting)
	var nouns []string
	for _, noun := range candidates {
		if noun == "" || noun == AppNoun || slices.Contains(nouns, noun) {
			continue
		}
		nouns = append(nouns, noun)
		if scope != UnboundChain && scope != UnboundAll {
			break
		}
	}
	bound := map[string]bool{}
	for _, at := range []string{focus, ""} {
		for _, row := range r.scanRows(walk, mode, at, false) {
			if !row.Request.Empty() && !row.Request.IsProp() {
				bound[r.canonicalRequest(row.Request).Key()] = true
			}
		}
	}
	var out []Operation
	for _, noun := range nouns {
		var verbs []Operation
		for key, op := range r.Operations {
			if op.Noun != noun || bound[key] || op.Builtin && scope != UnboundAll || focus != "" && !r.VerbShown(key, focus, mode) {
				continue
			}
			verbs = append(verbs, op)
		}
		slices.SortFunc(verbs, func(a, b Operation) int { return strings.Compare(a.Verb, b.Verb) })
		out = append(out, verbs...)
	}
	return out
}

// unboundHelpRows are the help rows of UnboundVerbs: no key, the operation
// as the help names one, its description (marked when it asks first), the
// noun's section with " · unbound".
func (r *Runtime) unboundHelpRows(ops []Operation) []HelpRow {
	var out []HelpRow
	for _, op := range ops {
		q := Request{Noun: op.Noun, Verb: op.Verb}
		name := r.displayRequest(q)
		desc := op.Description
		if op.Confirm != "" {
			desc = strings.TrimPrefix(desc+" · asks first", " · ")
		}
		out = append(out, HelpRow{Operation: name, Request: name, Description: desc, Noun: op.Noun, Section: r.DisplayNoun(op.Noun) + unboundSuffix, Unbound: true})
	}
	return out
}

// withUnboundRows puts each noun's unbound rows after that noun's own
// rows (the last row it answers with, motions apart), or first when it
// answers none, so a focused row's verbs stay at the top.
func (r *Runtime) withUnboundRows(rows []HelpRow, ops []Operation) []HelpRow {
	extra := r.unboundHelpRows(ops)
	for len(extra) > 0 {
		n := 1
		for n < len(extra) && extra[n].Noun == extra[0].Noun {
			n++
		}
		at := 0
		for i, row := range rows {
			if row.Noun == extra[0].Noun && row.Section != MovementSection && !row.Unbound {
				at = i + 1
			}
		}
		rows = slices.Insert(rows, at, extra[:n]...)
		extra = extra[n:]
	}
	return rows
}

// RunUnbound runs a request from focus as a key would: the same
// availability, agent rights and confirm (ExecuteAt), a missing required
// argument opening its form (MissingOpensFormSetting), a text answer shown
// as a key's is (keys.text-results). It is what enter does on an unbound
// help row.
func (r *Runtime) RunUnbound(focus string, q Request) error {
	if r.Instances[focus] == nil {
		return fmt.Errorf("unknown instance %q", focus)
	}
	defer r.forceSource(helpSource)()
	execute := func(q Request) error {
		present := r.keyResultPresenter(q)
		value, err := r.ExecuteAt(r.contextOf(focus), q)
		if err == nil && present != nil {
			err = present(value)
		}
		return err
	}
	_, err := r.runOrAsk(focus, q, execute)
	return err
}

// helpSource is the history source of a verb run from the help's unbound
// list: a person's, as a key's.
const helpSource = "help"

// runOrAsk runs q for the row at focus through execute, or, when it lacks
// a required argument its parameters' sources do not fill, opens the args
// form for it (MissingOpensFormSetting); the form's run goes through
// execute under the source the request started with. It reports whether the
// form opened. The one place a key's row (runRow) and an unbound verb
// (RunUnbound) ask or run.
func (r *Runtime) runOrAsk(focus string, q Request, execute func(Request) error) (bool, error) {
	source := r.history.source
	form := q
	if form.Target == "" && focus != r.Focus {
		form.Target = focus
	}
	if r.formForMissing(form, func(q Request) error {
		defer r.forceSource(source)()
		return execute(q)
	}) {
		return true, nil
	}
	return false, execute(q)
}

// selectedHelpRow is the help row under the help tree's selection.
func (r *Runtime) selectedHelpRow() (HelpRow, bool) {
	tree := r.Tree(helpTreeID)
	if tree == nil {
		return HelpRow{}, false
	}
	node, ok := tree.Selected()
	if !ok {
		return HelpRow{}, false
	}
	row, ok := node.Data.(HelpRow)
	return row, ok
}

// registerUnboundVerbs adds the help's run and bind verbs and bindings.bind.
func registerUnboundVerbs(r *Runtime) {
	r.DefineGuard("help-unbound-row", func(r *Runtime, _ KeyEvent) bool {
		row, ok := r.selectedHelpRow()
		return ok && row.Unbound
	})
	r.Register(Operation{Verb: "run", Noun: helpID, Description: "Run the selected unbound verb on what the help opened over, as its key would (it asks first where the key would; a missing argument opens its form)", Result: "operation", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		h := r.helpOpen()
		row, ok := r.selectedHelpRow()
		if h == nil || !ok || !row.Unbound {
			return nil, errors.New("select an unbound verb in the help")
		}
		origin, _ := r.ModalReturn(helpID)
		noun, verb := r.keyParts(row.Request)
		if err := r.Pop(); err != nil {
			return nil, err
		}
		return row.Request, r.RunUnbound(origin, Request{Noun: noun, Verb: verb})
	}})
	r.Register(Operation{Verb: "bind", Noun: helpID, Description: "Bind a key to the selected unbound verb: a form asks for the key and writes the row to your bindings.json (bindings.bind)", Result: "operation", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		h := r.helpOpen()
		row, ok := r.selectedHelpRow()
		if h == nil || !ok || !row.Unbound {
			return nil, errors.New("select an unbound verb in the help")
		}
		q := Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"action": row.Request}}
		_, err := r.OpenArgsForm(q, func(q Request) error {
			_, err := r.ExecuteAt(r.Context(), q)
			return err
		})
		return row.Request, err
	}})
	n := r.Noun(helpID)
	if n.Guards == nil {
		n.Guards = map[string]string{}
	}
	n.Guards["run"] = "help-unbound-row"
	n.Guards["bind"] = "help-unbound-row"

	r.Register(Operation{Verb: "bind", Noun: "ochazuke.bindings", Description: "Bind a key to an action by writing a row to the user's bindings.json (the first change you make to it from the program: a comment in the file is not kept, the old file is copied to bindings.json.bak) and loading it. The row goes in the action's noun's table in the mode; a key that already answers something else needs replace", Parameters: map[string]Parameter{
		"key":     {Type: "string", Required: true, Description: "the key or key sequence, as the bindings file spells it: x, ctrl+x, g n"},
		"action":  {Type: "string", Required: true, Description: "the operation to run, noun.verb"},
		"mode":    {Type: "string", Description: "the mode the key answers in (default: the current one)"},
		"noun":    {Type: "string", Description: "the noun whose table holds the row (default: the action's noun)"},
		"replace": {Type: "boolean", Description: "Take the key from the row that answers it now"},
	}, Positional: []string{"key", "action"}, Result: "binding", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.bindUserKey(StringArg(a, "key", ""), StringArg(a, "action", ""), StringArg(a, "noun", ""), StringArg(a, "mode", ""), a["replace"] == true)
	}})
}

// bindUserKey is bindings.bind: it validates the row against the live
// tables, adds it to the user's bindings file (the file kept, as a grouped
// file, with the row last in its table), loads the file again and refreshes
// an open help.
func (r *Runtime) bindUserKey(key, action, noun, mode string, replace bool) (any, error) {
	path := r.Config().Path(BindingsFile)
	if path == "" {
		return nil, errors.New("this application has no config directory: nothing to write the binding to")
	}
	op, ok := r.Operation(action)
	if !ok {
		return nil, fmt.Errorf("unknown operation %q", action)
	}
	if noun == "" {
		noun = op.Noun
	}
	noun = r.CanonicalNoun(noun)
	if mode == "" {
		mode = r.BaseMode()
	}
	row, err := r.validateRow(Binding{Noun: noun, Mode: mode, Keys: strings.TrimSpace(key), Request: Request{Noun: op.Noun, Verb: op.Verb}})
	if err != nil {
		return nil, err
	}
	for _, b := range r.tableRows(row) {
		if b.same(row) && !isPrefixCancel(b) && !b.Unbind && b.Request.Key() != row.Request.Key() && !replace {
			return nil, fmt.Errorf("%s is bound to %s in %s/%s; pass replace to take it", row.Keys, r.displayRequest(b.Request), noun, mode)
		}
	}
	var doc BindingsDoc
	old, err := os.ReadFile(path)
	switch {
	case err == nil:
		if doc, _, err = ParseBindings(old); err != nil {
			return nil, fmt.Errorf("%s does not parse, so it is left alone: %w", BindingsFile, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	line := row
	line.Noun, line.Instance, line.Mode = noun, "", mode
	placed := false
	for i, g := range doc.Groups {
		if g.Instance != "" || g.Noun != noun || g.Mode != mode {
			continue
		}
		for j, b := range g.Rows {
			if b.same(line) {
				doc.Groups[i].Rows[j] = line
				placed = true
				break
			}
		}
		if !placed {
			doc.Groups[i].Rows = append(doc.Groups[i].Rows, line)
			placed = true
		}
		break
	}
	if !placed {
		doc.Groups = append(doc.Groups, BindingGroup{Noun: noun, Mode: mode, Rows: []Binding{line}})
	}
	data, err := FormatBindings(doc)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if len(old) > 0 {
		if err := os.WriteFile(path+".bak", old, 0o644); err != nil {
			return nil, err
		}
	}
	if err := writeFileAtomic(path, data); err != nil {
		return nil, err
	}
	r.RecordWrittenFile(path)
	problems := r.ReloadUserBindings()
	if h := r.helpOpen(); h != nil {
		if err := h.refreshBindings(); err != nil {
			r.UI().Error("refresh binding help: " + err.Error())
		}
	}
	for _, p := range problems {
		if strings.Contains(p, row.Keys) && !strings.HasPrefix(p, "warning:") {
			return nil, fmt.Errorf("written, but not loaded: %s", p)
		}
	}
	return map[string]any{"key": row.Keys, "action": r.displayRequest(row.Request), "context": noun + "/" + mode, "file": path}, nil
}
