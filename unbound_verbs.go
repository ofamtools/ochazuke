package ochazuke

import (
	"errors"
	"fmt"
	"maps"
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
			if row.Request.Empty() || row.Request.IsProp() {
				continue
			}
			q := r.canonicalRequest(row.Request)
			bound[q.Key()] = true
			// An operation that forwards a verb (list.apply {verb: X})
			// binds X for the nouns of the rows it acts on.
			for name, p := range r.Operations[q.Key()].Parameters {
				if verb, ok := q.Args[name].(string); ok && p.Forwards && verb != "" {
					for _, noun := range nouns {
						bound[noun+"."+verb] = true
					}
				}
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
// as a key's is (keys.text-results) and any other data answer shown too
// (results.show-data). It is what enter does on a help row.
func (r *Runtime) RunUnbound(focus string, q Request) error { return r.runFromHelp(focus, q, false) }

// AskUnbound is RunUnbound that always opens the args form first, holding
// what the request and the parameters' sources fill, for a verb with
// parameters (a in the help).
func (r *Runtime) AskUnbound(focus string, q Request) error { return r.runFromHelp(focus, q, true) }

func (r *Runtime) runFromHelp(focus string, q Request, ask bool) error {
	if r.Instances[focus] == nil {
		return fmt.Errorf("unknown instance %q", focus)
	}
	defer r.forceSource(helpSource)()
	execute := func(q Request) error {
		present := r.keyResultPresenter(q)
		top := r.TopModal()
		value, err := r.ExecuteAt(r.contextOf(focus), q)
		switch {
		case err != nil:
		case present != nil:
			err = present(value)
		case r.TopModal() == top:
			err = r.presentData(q, value)
		}
		return err
	}
	_, err := r.runOrAsk(focus, q, execute, ask)
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
func (r *Runtime) runOrAsk(focus string, q Request, execute func(Request) error, always bool) (bool, error) {
	source := r.history.source
	form := q
	if form.Target == "" && focus != r.Focus {
		form.Target = focus
	}
	run := func(q Request) error {
		defer r.forceSource(source)()
		return execute(q)
	}
	if always {
		if op, ok := r.Operations[form.Key()]; ok && len(op.Parameters) > 0 {
			if _, err := r.OpenArgsForm(form, run); err == nil {
				return true, nil
			}
		}
	}
	if r.formForMissing(form, run) {
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
	// run and args act on the selected row of the help, bound or not: one
	// rule for enter on any operation row (a row with children folds
	// instead, help-foldable).
	helpRun := func(ask bool) func(*Runtime, Context, Arguments) (any, error) {
		return func(r *Runtime, _ Context, _ Arguments) (any, error) {
			row, ok := r.selectedHelpRow()
			if r.helpOpen() == nil || !ok || !helpRunnable(row) {
				return nil, errors.New("select a verb in the help")
			}
			origin, _ := r.ModalReturn(helpID)
			noun, verb := r.keyParts(row.Request)
			q := Request{Noun: noun, Verb: verb}
			if !row.Unbound {
				q.Args = cloneArgs(row.Args)
			}
			if err := r.Pop(); err != nil {
				return nil, err
			}
			if ask {
				return row.Request, r.AskUnbound(origin, q)
			}
			return row.Request, r.RunUnbound(origin, q)
		}
	}
	r.Register(Operation{Verb: "run", Noun: helpID, Description: "Run the selected verb, bound or not, on what the help opened over, as its key would (it asks first where the key would; a missing argument opens its form)", Result: "operation", Run: helpRun(false)})
	r.Register(Operation{Verb: "args", Noun: helpID, Description: "Open the args form for the selected verb, holding what its parameters' sources fill, before running it on what the help opened over", Result: "operation", Run: helpRun(true)})
	r.DefineGuard("help-runnable", func(r *Runtime, _ KeyEvent) bool {
		row, ok := r.selectedHelpRow()
		return ok && helpRunnable(row)
	})
	r.DefineGuard("help-has-args", func(r *Runtime, _ KeyEvent) bool {
		row, ok := r.selectedHelpRow()
		if !ok || !helpRunnable(row) {
			return false
		}
		noun, verb := r.keyParts(row.Request)
		op, found := r.Operation(noun + "." + verb)
		return found && slices.ContainsFunc(slices.Collect(maps.Keys(op.Parameters)), func(name string) bool { return name != "target" })
	})
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
	r.Register(Operation{Verb: "bind-key", Noun: helpID, Description: "Bind the key just pressed to the selected unbound verb (b waits for it; esc cancels): writes the row to your bindings.json; a key that already runs something asks first", Parameters: map[string]Parameter{
		"key": {Type: "string", Required: true, Description: "the key, as the bindings file spells it"},
	}, Result: "binding", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		row, ok := r.selectedHelpRow()
		if r.helpOpen() == nil || !ok || !row.Unbound {
			return nil, errors.New("select an unbound verb in the help")
		}
		keys := StringArg(a, "key", "")
		q := Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": keys, "action": row.Request}}
		if b, conflict := r.bindConflict(keys); conflict {
			q.Args["replace"] = true
			return r.openConfirm(c, q, Operation{Noun: "ochazuke.bindings", Verb: "bind", Confirm: (&BindConflictError{Keys: keys, Action: r.displayRequest(b.Request)}).Error()})
		}
		return r.Execute(q)
	}})
	n := r.Noun(helpID)
	if n.Guards == nil {
		n.Guards = map[string]string{}
	}
	n.Guards["run"] = "help-runnable"
	n.Guards["args"] = "help-has-args"
	n.Guards["bind"] = "help-unbound-row"
	n.Guards["bind-key"] = "help-unbound-row"

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
	if !replace {
		if b, conflict := r.bindConflict(row.Keys); conflict {
			return nil, &BindConflictError{Keys: row.Keys, Action: r.displayRequest(b.Request)}
		}
		for _, b := range r.tableRows(row) {
			if b.same(row) && !isPrefixCancel(b) && !b.Unbind && b.Request.Key() != row.Request.Key() {
				return nil, &BindConflictError{Keys: row.Keys, Action: r.displayRequest(b.Request)}
			}
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
	r.UI().Notify(row.Keys + " now runs " + r.displayRequest(row.Request) + " (" + BindingsFile + ")")
	return map[string]any{"key": row.Keys, "action": r.displayRequest(row.Request), "context": noun + "/" + mode, "file": path}, nil
}

// BindConflictError says a key already runs something where it is being
// bound, in one plain sentence that is also the question to ask.
type BindConflictError struct{ Keys, Action string }

func (e *BindConflictError) Error() string {
	return e.Keys + " already runs " + e.Action + " here; bind anyway?"
}

// bindConflict is the row of the walk at the focus the help (else the
// base focus) is over that already answers keys in the base mode: the same
// key, a sequence that starts with it, or a shorter row it would start a
// sequence from. The first such row, nearest the focus first.
func (r *Runtime) bindConflict(keys string) (Binding, bool) {
	keys = strings.Join(strings.Fields(keys), " ")
	focus, mode := r.BaseFocus(), r.BaseMode()
	if origin, at := r.ModalReturn(helpID); r.Instances[origin] != nil {
		base, _ := baseAndPrefix(at)
		focus, mode = origin, base
	}
	if keys == "" || r.Instances[focus] == nil {
		return Binding{}, false
	}
	for _, row := range r.scanRows(r.walk(focus), ModeState{Name: mode}, focus, false) {
		if row.Pattern != "" || row.Keys == "" || isPrefixCancel(row.Binding) || row.Unbind {
			continue
		}
		if row.Keys == keys || strings.HasPrefix(row.Keys, keys+" ") || strings.HasPrefix(keys, row.Keys+" ") {
			return row.Binding, true
		}
	}
	return Binding{}, false
}

// helpRunnable reports whether a help row stands for one operation a run
// can start: not a row with children (a prefix or a sequence, which fold)
// nor a prefix the help made up.
func helpRunnable(row HelpRow) bool {
	return row.Operation != "" && row.Request != "" && !row.Group && len(row.Steps) == 0 && row.Enters == ""
}
