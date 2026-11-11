package ochazuke

import (
	"cmp"
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
// user's bindings.json (bindings-help.bind, which calls binding.bind).
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
// verbs, which are many: collection.move, input.insert...).
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
	return r.unboundOperations(r.focusChain(focus), r.walk(focus), mode, focus, nil)
}

// unboundVerbsIn is UnboundVerbs in a resolution context: its walk, mode
// and focus, a hypothetical one judging its guards in that interaction and
// reading no cache.
func (r *Runtime) unboundVerbsIn(ctx resolutionContext) []Operation {
	if r.Instances[ctx.Focus] == nil {
		return nil
	}
	return r.unboundOperations(r.focusChain(ctx.Focus), r.walkIn(ctx), ctx.Mode, ctx.Focus, ctx.within())
}

// PlaceUnboundVerbs is UnboundVerbs for a hypothetical focus on place's
// noun (PlaceHelp): the place's chain innermost first, the walk nounWalk
// reads, and no availability judged, as a guarded row is not.
func (r *Runtime) PlaceUnboundVerbs(place NounPlace, mode ModeState) []Operation {
	place = r.canonicalPlace(place)
	chain := slices.Clone(place.Chain())
	slices.Reverse(chain)
	return r.unboundOperations(chain, r.nounWalk(place, mode), mode, "", nil)
}

// unboundOperations is the shared reading: nouns innermost first (cut to
// the scope setting), walk the levels whose rows bind, focus "" for a
// hypothetical walk. A row counts as binding its verb whether or not its
// guard holds now, and whether or not a row earlier on its key shadows it
// (the first row on a key answers; the others are the key's alternatives).
// within is a query's interaction (its guards read it, no cache is read),
// nil for the live one.
func (r *Runtime) unboundOperations(candidates []string, walk []level, mode ModeState, focus string, within *resolutionContext) []Operation {
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
		for _, row := range r.scanRowsWithin(walk, mode, at, within) {
			if row.Request.Empty() || row.Request.IsProp() {
				continue
			}
			q := r.canonicalRequest(row.Request)
			bound[q.Key()] = true
			// An operation that forwards a verb (collection.apply {verb: X})
			// binds X for the nouns of the rows it acts on.
			for name, p := range r.Operations.At(Key(q.Key())).Parameters {
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
		// The memoized index lists a noun's operations; scanning every
		// registered operation here cost a key split each, every frame.
		for _, o := range r.Operations.Lookups().ByPair[noun] {
			op, key := *o, o.Key.Short()
			if bound[key] || op.Builtin && scope != UnboundAll || focus != "" && !r.verbShownWithin(key, focus, mode, within) {
				continue
			}
			verbs = append(verbs, op)
		}
		slices.SortFunc(verbs, func(a, b Operation) int { return strings.Compare(a.Name(), b.Name()) })
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
		q := Request{Noun: op.Noun(), Verb: op.Name()}
		name := r.displayRequest(q)
		desc := op.Description
		if op.Confirm != "" {
			desc = strings.TrimPrefix(desc+" · asks first", " · ")
		}
		out = append(out, HelpRow{Operation: name, Request: name, Description: desc, Noun: op.Noun(), Section: r.DisplayNoun(op.Noun()) + unboundSuffix, Unbound: true})
	}
	return out
}

// withUnboundRows puts each noun's unbound rows by that noun's place in the
// focus chain (nouns, innermost first), the order the help's levels are
// in: after the noun's own rows (motions apart), or, when it answers none,
// before the first row of an outer noun. An outer noun with no keys of its
// own never goes above the focused row's.
func (r *Runtime) withUnboundRows(rows []HelpRow, ops []Operation, chain []string) []HelpRow {
	extra := r.unboundHelpRows(ops)
	// A level's noun may be spelled by its canonical name or its display
	// name; compare by the display name a section shows.
	shown := make([]string, len(chain))
	for i, noun := range chain {
		shown[i] = r.DisplayNoun(noun)
	}
	pos := func(section string) int {
		if i := slices.Index(shown, section); i >= 0 {
			return i
		}
		return len(chain)
	}
	for len(extra) > 0 {
		n := 1
		for n < len(extra) && extra[n].Noun == extra[0].Noun {
			n++
		}
		noun := extra[0].Noun
		at, own := 0, false
		for i, row := range rows {
			if row.Section == r.DisplayNoun(noun) && !row.Unbound {
				at, own = i+1, true
			}
		}
		if !own {
			at = len(rows)
			for i, row := range rows {
				if !row.Unbound && row.Section != MovementSection && pos(row.Section) > pos(r.DisplayNoun(noun)) {
					at = i
					break
				}
			}
		}
		rows = slices.Insert(rows, at, extra[:n]...)
		extra = extra[n:]
	}
	return rows
}

// placeChain is a place's nouns, innermost first.
func (r *Runtime) placeChain(place NounPlace) []string {
	chain := slices.Clone(r.canonicalPlace(place).Chain())
	slices.Reverse(chain)
	return chain
}

// focusChain is the nouns of the instances from focus out to the root,
// innermost first.
func (r *Runtime) focusChain(focus string) []string {
	var out []string
	for _, id := range r.Ancestry(focus) {
		out = append(out, r.Instances[id].Noun)
	}
	return out
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
	// The help closes when the verb is about to run, not before a form
	// opens: esc on the form comes back to the help, on the same row.
	leave := func() error {
		if r.TopModal() == helpID {
			return r.Pop()
		}
		return nil
	}
	execute := func(q Request) (any, error) {
		top := r.TopModal()
		present := r.resultPresenter(q, func(value any) (bool, error) { return r.presentAnswer(q, value, top) })
		value, err := r.ExecuteAt(r.contextOf(focus), q)
		if err == nil {
			err = present(value)
		}
		return value, err
	}
	_, err := r.runOrAsk(focus, q, execute, ask, false, leave)
	return err
}

// helpSource is the history source of a verb run from the help's unbound
// list: a person's, as a key's.
const helpSource = "help"

// runOrAsk runs q for the row at focus through execute, or, when it lacks
// a required argument its parameters' sources do not fill (or always), opens
// the args form for it (MissingOpensFormSetting); the form's run goes
// through execute under the source the request started with, and a run that
// fails later (a job) reopens the form with its values. leave, when given,
// closes what the request was made from (the help) just before it runs. It
// reports whether the form opened. The one place a key's row (runRow) and a
// verb run from the help (RunUnbound) ask or run. held says a thing holds
// the request (a key that answered at that thing's level, level.holds): the
// thing is what it acts for, so no form opens for a subject and none marks
// one required, as before the where-from ask; a missing required argument
// and a handler's Need still ask.
func (r *Runtime) runOrAsk(focus string, q Request, execute func(Request) (any, error), always, held bool, leave func() error) (bool, error) {
	source := r.history.source
	form := q
	if form.Target == "" && focus != r.Focus {
		form.Target = focus
	}
	var run func(Request) error
	// attempt executes q; a verb that says it lacks a value (a
	// MissingParameterError) asks for it in the args form, whose confirm
	// runs the request again through run (askNeed).
	attempt := func(q Request, watch bool) error {
		value, err := execute(q)
		if err != nil {
			if r.askNeed(withArgs(form, q.Args), err, run, held) {
				return nil
			}
			return err
		}
		if !watch {
			return nil
		}
		r.afterFailure(value, func(late error) {
			// The form comes back with what was typed and says why;
			// where it cannot (a modal is open) the error is told.
			defer r.forceSource(source)()
			if r.askNeed(withArgs(form, q.Args), late, run, held) {
				return
			}
			if e, openErr := r.openArgsForm(withArgs(form, q.Args), run, nil, held); openErr == nil && e != nil {
				e.out.err = late.Error()
			} else {
				r.UI().Error("not run: " + late.Error())
			}
		})
		return nil
	}
	run = func(q Request) error {
		if leave != nil {
			if err := leave(); err != nil {
				return err
			}
		}
		defer r.forceSource(source)()
		return attempt(q, true)
	}
	if always {
		if op, ok := r.Operations.Find(Key(form.Key())); !ok || len(askableParameters(op)) == 0 {
			return false, fmt.Errorf("%s has no argument to ask for", r.displayRequest(form))
		}
		if _, err := r.openArgsForm(form, run, nil, held); err != nil {
			return false, fmt.Errorf("cannot open the form for %s: %w", r.displayRequest(form), err)
		}
		return true, nil
	}
	if r.formForMissing(form, run, held) {
		return true, nil
	}
	if leave != nil {
		if err := leave(); err != nil {
			return false, err
		}
	}
	err := attempt(q, false)
	return false, err
}

// afterFailure calls fn with the error when the job a pending answer
// stands for fails.
func (r *Runtime) afterFailure(value any, fn func(error)) {
	ticket, ok := value.(Ticket)
	if !ok {
		return
	}
	job := r.jobs[ticket.Job]
	if job == nil {
		return
	}
	then := job.then
	job.then = func(c Ctx, value any, err error) error {
		var first error
		if then != nil {
			first = then(c, value, err)
		}
		if err != nil {
			fn(err)
		}
		return first
	}
}

// selectedHelpRow is the help row under the selection of the help's tree,
// or of its table's list when the row is an unbound verb.
func (r *Runtime) selectedHelpRow() (HelpRow, bool) {
	if h := r.helpOpen(); h != nil && h.view == HelpTable {
		list := r.List(helpListID)
		if list == nil {
			return HelpRow{}, false
		}
		// The table view: a list whose unbound rows carry their HelpRow.
		item, ok := list.Selected()
		row, isRow := item.Data.(HelpRow)
		return row, ok && isRow
	}
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

// registerUnboundVerbs adds the help's run and bind verbs and binding.bind.
func registerUnboundVerbs(r *Runtime) {
	r.InvalidateBindings()
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
			if ask {
				return row.Request, r.AskUnbound(origin, q)
			}
			return row.Request, r.RunUnbound(origin, q)
		}
	}
	r.Register(Operation{Key: KeyOf(helpID, "run"), Description: "Run the selected verb, bound or not, on what the help opened over, as its key would (it asks first where the key would; a missing argument opens its form)", Result: "operation", Run: helpRun(false)})
	r.Register(Operation{Key: KeyOf(helpID, "args"), Description: "Open the args form for the selected verb, holding what its parameters' sources fill, before running it on what the help opened over", Result: "operation", Run: helpRun(true)})
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
		return found && len(askableParameters(op)) > 0
	})
	r.Register(Operation{Key: KeyOf(helpID, "bind"), Description: "Bind a key to the selected unbound verb: a form asks for the key and writes the row to your bindings.json (binding.bind)", Result: "operation", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		h := r.helpOpen()
		row, ok := r.selectedHelpRow()
		if h == nil || !ok || !row.Unbound {
			return nil, errors.New("select an unbound verb in the help")
		}
		q := Request{Noun: "ochazuke.binding", Verb: "bind", Args: Arguments{"action": row.Request}}
		_, err := r.OpenArgsForm(q, func(q Request) error {
			_, err := r.ExecuteAt(r.Context(), q)
			return err
		})
		return row.Request, err
	}})
	registerHelpCapture(r)
	n := r.Noun(helpID)
	if n.Guards == nil {
		n.Guards = map[string]string{}
	}
	n.Guards["run"] = "help-runnable"
	n.Guards["args"] = "help-has-args"
	n.Guards["bind"] = "help-unbound-row"
	for _, verb := range []string{"bind-start", "bind-key", "bind-finish", "bind-drop", "bind-literal"} {
		n.Guards[verb] = "help-unbound-row"
	}

	r.Register(Operation{Key: "ochazuke.binding.bind", Description: "Bind a key to an action by writing a row to the user's bindings.json (the first change you make to it from the program: a comment in the file is not kept, the old file is copied to bindings.json.bak) and loading it. The row goes in the action's noun's table in the mode; a key that already answers something else needs replace", Parameters: map[string]Parameter{
		"key":     {Type: "string", Required: true, Description: "the key or key sequence, as the bindings file spells it: x, ctrl+x, g n"},
		"action":  {Type: "string", Required: true, Description: "the operation to run, noun.verb"},
		"mode":    {Type: "string", Description: "the mode the key answers in (default: the current one)"},
		"noun":    {Type: "string", Description: "the noun whose table holds the row (default: the action's noun)"},
		"replace": {Type: "boolean", Description: "Take the key from the row that answers it now"},
	}, Positional: []string{"key", "action"}, Result: "binding", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.bindUserKey(StringArg(a, "key", ""), StringArg(a, "action", ""), StringArg(a, "noun", ""), StringArg(a, "mode", ""), a["replace"] == true)
	}})
	if err := r.AddVerbCheck("ochazuke.binding.bind", VerbCheck{Name: "key-taken", Check: keyTaken}); err != nil {
		panic(err.Error())
	}
}

// keyTaken is the check on binding.bind: a key the walk at the focus (or
// the target table) already answers asks "KEY already runs ACTION here; bind
// anyway?" from every route (the help's b, the form, the palette, the CLI
// with --yes); replace skips the question.
func keyTaken(r *Runtime, _ Context, a Arguments) Verdict {
	if a["replace"] == true {
		return Allow()
	}
	keys := strings.Join(strings.Fields(StringArg(a, "key", "")), " ")
	if b, taken := r.bindConflict(keys); taken {
		return AskFirst(r.conflictQuestion(keys, b))
	}
	if op, ok := r.Operation(StringArg(a, "action", "")); ok {
		noun, mode := r.CanonicalNoun(cmp.Or(StringArg(a, "noun", ""), op.Noun())), cmp.Or(StringArg(a, "mode", ""), r.BaseMode())
		if n := r.Nouns[noun]; n != nil {
			for _, b := range n.Bindings[mode] {
				if b.Keys == keys && b.Pattern == "" && !isPrefixCancel(b) && !b.Unbind && b.Request.Key() != op.Key.Short() {
					return AskFirst(keys + " already runs " + r.displayRequest(b.Request) + " here; bind anyway?")
				}
			}
		}
	}
	return Allow()
}

// bindUserKey is binding.bind: it validates the row against the live
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
		noun = op.Noun()
	}
	noun = r.CanonicalNoun(noun)
	if mode == "" {
		mode = r.BaseMode()
	}
	row, err := r.validateRow(Binding{Noun: noun, Mode: mode, Keys: strings.TrimSpace(key), Request: Request{Noun: op.Noun(), Verb: op.Name()}})
	if err != nil {
		return nil, err
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
	r.syncHelp()
	for _, p := range problems {
		if strings.Contains(p, row.Keys) && !strings.HasPrefix(p, "warning:") {
			return nil, fmt.Errorf("written, but not loaded: %s", p)
		}
	}
	r.UI().Notify(row.Keys + " now runs " + r.displayRequest(row.Request) + " (" + BindingsFile + ")")
	return map[string]any{"key": row.Keys, "action": r.displayRequest(row.Request), "context": noun + "/" + mode, "file": path}, nil
}

// bindWalkRows are the rows of the walk at the focus the help (else the
// base focus) is over, in the base mode: what a key pressed there meets.
func (r *Runtime) bindWalkRows() []levelRow {
	focus, mode := r.BaseFocus(), r.BaseMode()
	if origin, at := r.ModalReturn(helpID); r.Instances[origin] != nil {
		base, _ := baseAndPrefix(at)
		focus, mode = origin, base
	}
	if r.Instances[focus] == nil {
		return nil
	}
	return r.scanRows(r.walk(focus), ModeState{Name: mode}, focus)
}

// bindConflict is the row of the walk at the focus the help (else the
// base focus) is over that already answers keys in the base mode: the same
// key, a sequence that starts with it, or a shorter row it would start a
// sequence from. The first such row, nearest the focus first.
func (r *Runtime) bindConflict(keys string) (Binding, bool) {
	keys = strings.Join(strings.Fields(keys), " ")
	if keys == "" {
		return Binding{}, false
	}
	for _, row := range r.bindWalkRows() {
		if row.Pattern != "" || row.Keys == "" || isPrefixCancel(row.Binding) || row.Unbind {
			continue
		}
		if row.Keys == keys || strings.HasPrefix(row.Keys, keys+" ") || strings.HasPrefix(keys, row.Keys+" ") {
			return row.Binding, true
		}
	}
	return Binding{}, false
}

// bindContinues reports whether a row of the walk goes on from keys: keys
// is the start of a sequence that is bound (g, with g J bound), so the
// next key may still belong to the sequence being captured.
func (r *Runtime) bindContinues(keys string) bool {
	keys = strings.Join(strings.Fields(keys), " ")
	for _, row := range r.bindWalkRows() {
		if row.Pattern == "" && !isPrefixCancel(row.Binding) && !row.Unbind && strings.HasPrefix(row.Keys, keys+" ") {
			return true
		}
	}
	return false
}

// conflictQuestion is the question a taken key asks, by how it is taken:
// the same key, a sequence it would cut short (keys alone shadows b), or a
// shorter row that already answers before keys could.
func (r *Runtime) conflictQuestion(keys string, b Binding) string {
	what := r.displayRequest(b.Request)
	switch {
	case b.Keys == keys:
		return keys + " already runs " + what + " here; bind anyway?"
	case strings.HasPrefix(b.Keys, keys+" "):
		return keys + " starts the sequence " + b.Keys + " (" + what + "): binding " + keys + " alone would shadow it; bind anyway?"
	default:
		return b.Keys + " already runs " + what + " here, so " + keys + " would never be reached; bind anyway?"
	}
}

// askableParameters are the parameters a person gives: not the target and
// not a verb's plumbing (Parameter.Plumbing).
func askableParameters(op Operation) []string {
	var out []string
	for name, p := range op.Parameters {
		if name != "target" && !p.Plumbing {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// helpRunnable reports whether a help row stands for one operation a run
// can start: not a row with children (a prefix or a sequence, which fold)
// nor a prefix the help made up.
func helpRunnable(row HelpRow) bool {
	return row.Operation != "" && row.Request != "" && !row.Group && len(row.Steps) == 0 && row.Enters == ""
}
