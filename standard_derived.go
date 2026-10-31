package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The derived standard verbs (lane S4): rows of the standard verb table
// read from a prop's role or from the store's handlers, each running the
// standard set or create, so history, watch, availability and the store
// are those verbs'.
//
//   - rename writes the label prop (RoleLabel): its argument is name, or
//     to when the noun's key prop is named name (the key's name is
//     reserved on the noun's verbs). The args form opens holding the old
//     name, selected (rename.select); an empty label opens empty with its
//     Fallback as the placeholder (Parameter.Edits).
//   - move writes the owner reference (RoleOwner): to, one of the
//     reference noun's things.
//   - duplicate creates a copy of the thing: every prop create writes,
//     read from the thing, with prop=value overrides.
//   - archive and restore set the archive prop (RoleArchive) true and false.
//
// And the two behaviour settings of create and delete:
//
//   - create.from-focus (default on): create fills an omitted reference
//     prop declared FromFocus from the focused thing of its noun, and says
//     so (createFill; the palette slot and the args form show it first).
//   - delete.archive (default off): on a noun with an archive prop, delete
//     archives a live thing and deletes an archived one for real
//     (archiveOnDelete). The confirm says which (deleteConfirm): the
//     archive asks DefaultArchiveConfirm, or nothing with
//     delete.archive-confirm off; the real delete asks the delete prompt.

func init() {
	standardVerbs = append(standardVerbs, derivedVerbs...)
	for _, v := range standardVerbs {
		if v.FocusSetting != "" {
			focusSettings[v.Verb] = v.FocusSetting
		}
	}
}

// focusSettings are the rows' FocusSetting by verb (read without the
// table, which refers to the handlers that read it).
var focusSettings = map[string]string{}

var derivedVerbs = []standardVerb{
	{Verb: "rename", Description: "Rename a thing: write its label prop (name; to when the key prop is named name); the form opens on the old name",
		Kinds: []string{Collection, Singleton}, Needs: func(n *Noun) bool { return n.Store.Patch != nil && writableRole(n, RoleLabel) != nil },
		Subject: true, Repeatable: true, HistoryKind: HistoryChange, Positional: func(n *Noun) []string { return []string{renameArg(n)} },
		Params: renameParams, Writes: roleWrites(RoleLabel), Run: runRename},
	{Verb: "move", Description: "Move a thing to another owner: write its owner reference (to)",
		Kinds: []string{Collection}, Needs: func(n *Noun) bool { return n.Store.Patch != nil && writableRole(n, RoleOwner) != nil },
		Subject: true, Many: true, Repeatable: true, HistoryKind: HistoryChange, Positional: func(*Noun) []string { return []string{"to"} },
		Params: moveParams, Writes: roleWrites(RoleOwner), Run: runMove},
	{Verb: "duplicate", Description: "Create a copy of a thing: every prop create writes, from the thing, with prop=value overrides (pairs or changes); answers the new thing",
		Kinds: []string{Collection}, Needs: func(n *Noun) bool { return n.Store.Get != nil && n.Store.Create != nil && len(createProps(n)) > 0 },
		Subject: true, HistoryKind: HistoryChange, Positional: func(*Noun) []string { return []string{"pairs"} },
		Params: duplicateParams, Writes: duplicateWrites, Run: runDuplicate},
	{Verb: "archive", Destructive: true, Description: "Archive a thing: set its archive prop",
		Kinds: []string{Collection}, Needs: func(n *Noun) bool { return n.Store.Patch != nil && writableRole(n, RoleArchive) != nil },
		Subject: true, Many: true, Repeatable: true, HistoryKind: HistoryChange,
		Writes: roleWrites(RoleArchive), Run: archiveRun(true)},
	{Verb: "restore", Description: "Restore an archived thing: clear its archive prop",
		Kinds: []string{Collection}, Needs: func(n *Noun) bool { return n.Store.Patch != nil && writableRole(n, RoleArchive) != nil },
		Subject: true, Many: true, Repeatable: true, HistoryKind: HistoryChange,
		Writes: roleWrites(RoleArchive), Run: archiveRun(false)},
}

// writableRole is the noun's prop with that role when set may write it.
func writableRole(n *Noun, role string) *PropSpec {
	if p := n.roleProp(role); p != nil && p.writable() {
		return p
	}
	return nil
}

// roleWrites is the Writes of a row that writes the prop with that role.
func roleWrites(role string) func(n *Noun, a Arguments) ([]string, error) {
	return func(n *Noun, _ Arguments) ([]string, error) {
		if p := n.roleProp(role); p != nil {
			return []string{p.Name}, nil
		}
		return nil, nil
	}
}

// renameArg is rename's argument: name, or to when the key prop takes
// name (the key's name means the subject on every verb of the noun).
func renameArg(n *Noun) string {
	if key := n.keyProp(); key != nil && key.Name == "name" {
		return "to"
	}
	return "name"
}

func renameParams(n *Noun) map[string]Parameter {
	label := n.roleProp(RoleLabel)
	param := propParameter(*label)
	param.Required, param.Edits = true, label.Name
	param.Description = fmt.Sprintf("The new %s (an empty one is allowed)", label.Name)
	if label.Fallback != "" {
		param.Description += fmt.Sprintf("; empty shows the %s", label.Fallback)
	}
	return map[string]Parameter{renameArg(n): param}
}

func moveParams(n *Noun) map[string]Parameter {
	owner := n.roleProp(RoleOwner)
	param := propParameter(*owner)
	param.Required = true
	param.Description = fmt.Sprintf("The %s to move to (written to %s)", owner.Noun, owner.Name)
	return map[string]Parameter{"to": param}
}

func duplicateParams(n *Noun) map[string]Parameter {
	return map[string]Parameter{
		"pairs":   {Type: "string", Rest: true, Description: "prop=value overrides, written in place of the thing's values"},
		"changes": {Type: "object", Description: "Overrides as an object of prop to value"},
	}
}

// setVia runs the standard set on c's subject with prop written to value:
// the derived verbs are set under another name. Only the subject's key
// goes through from the request's arguments.
func (r *Runtime) setVia(n *Noun, c Ctx, prop string, value any) (any, error) {
	args := Arguments{"prop": prop, "value": value}
	if key := n.keyProp(); key != nil {
		if v, ok := c.Args[key.Name]; ok {
			args[key.Name] = v
		}
	}
	c.Args = args
	return runSet(r, n, c)
}

func runRename(r *Runtime, n *Noun, c Ctx) (any, error) {
	arg := renameArg(n)
	v, ok := c.Args[arg]
	if !ok {
		return nil, &ArgumentError{fmt.Errorf("%s is required", arg)}
	}
	return r.setVia(n, c, n.roleProp(RoleLabel).Name, fmt.Sprint(v))
}

func runMove(r *Runtime, n *Noun, c Ctx) (any, error) {
	v, ok := c.Args["to"]
	if !ok {
		return nil, &ArgumentError{errors.New("to is required")}
	}
	return r.setVia(n, c, n.roleProp(RoleOwner).Name, fmt.Sprint(v))
}

func archiveRun(on bool) func(r *Runtime, n *Noun, c Ctx) (any, error) {
	return func(r *Runtime, n *Noun, c Ctx) (any, error) {
		return r.setVia(n, c, n.roleProp(RoleArchive).Name, on)
	}
}

// duplicateOverrides reads duplicate's overrides: prop=value words (pairs)
// or a changes object, never both; each a prop create writes, typed by it.
func duplicateOverrides(n *Noun, a Arguments) (map[string]any, []string, error) {
	pairs := StringArg(a, "pairs", "")
	obj, hasChanges := a["changes"]
	raw := map[string]any{}
	var order []string
	switch {
	case hasChanges && pairs != "":
		return nil, nil, &ArgumentError{errors.New("duplicate takes pairs or changes, not both")}
	case hasChanges:
		m, err := propValue(PropSpec{Name: "changes", Parameter: Parameter{Type: PropObject}}, obj)
		if err != nil {
			return nil, nil, &ArgumentError{fmt.Errorf("changes %w", err)}
		}
		raw = m.(map[string]any)
		order = slices.Sorted(maps.Keys(raw))
		slices.SortStableFunc(order, func(x, y string) int { return propIndex(n, x) - propIndex(n, y) })
	default:
		for _, w := range splitPairs(pairs) {
			name, v, ok := strings.Cut(w, "=")
			if !ok {
				return nil, nil, &ArgumentError{fmt.Errorf("%q is not prop=value", w)}
			}
			if _, dup := raw[name]; dup {
				return nil, nil, &ArgumentError{fmt.Errorf("%s is written twice", name)}
			}
			raw[name] = unquoteWord(v)
			order = append(order, name)
		}
	}
	out := map[string]any{}
	for _, name := range order {
		p := n.prop(name)
		switch {
		case p == nil:
			return nil, nil, &ArgumentError{fmt.Errorf("%s has no prop %q", n.Name, name)}
		case p.ReadOnly || p.Key:
			return nil, nil, &ArgumentError{fmt.Errorf("%s.%s is read-only", n.Name, name)}
		}
		typed, err := propValue(*p, raw[name])
		if err != nil {
			return nil, nil, &ArgumentError{fmt.Errorf("%s %w", name, err)}
		}
		if _, exists := out[p.Name]; exists {
			return nil, nil, &ArgumentError{fmt.Errorf("%s is written twice", p.Name)}
		}
		out[p.Name] = typed
	}
	for i, name := range order {
		order[i] = n.prop(name).Name
	}
	return out, order, nil
}

func duplicateWrites(n *Noun, a Arguments) ([]string, error) {
	_, order, err := duplicateOverrides(n, a)
	return order, err
}

func runDuplicate(r *Runtime, n *Noun, c Ctx) (any, error) {
	overrides, order, err := duplicateOverrides(n, c.Args)
	if err != nil {
		return nil, err
	}
	if name := r.unshownProp(n, c, order); name != "" {
		return nil, fmt.Errorf("%s.%s is not available here", r.DisplayNoun(n.Name), name)
	}
	key, err := r.subjectKey(n, c)
	if err != nil {
		return nil, err
	}
	t, err := n.Store.Get(c, key)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("no %s %q", r.DisplayNoun(n.Name), key)
	}
	values := Thing{}
	for _, p := range createProps(n) {
		if v, ok := overrides[p.Name]; ok {
			values[p.Name] = v
			continue
		}
		if v, ok := t[p.Name]; ok && v != nil {
			typed, err := propValue(p, v)
			if err != nil {
				typed = v // the store's own form: create judges it
			}
			values[p.Name] = typed
		}
	}
	made, err := n.Store.Create(c, values)
	if err != nil {
		return nil, err
	}
	if made == nil {
		made = values
	}
	return thingRecord(n, made, nil), nil
}

// createPositional is create's words: the label prop, then the props
// filled from the focus (FromFocus), so the slot that shows the fill comes
// next on the line (task create "Buy milk" ⟨project · focus: Home⟩), then
// the other required props in declared order, then the pairs rest. A
// rest parameter takes the line's last words, so every required one
// comes before it (checkRestParameter).
func createPositional(n *Noun) []string {
	out := labelFirst(n)
	for _, p := range createProps(n) {
		if p.FromFocus && p.Role != RoleLabel {
			out = append(out, p.Name)
		}
	}
	for _, p := range createProps(n) {
		if p.Required && !slices.Contains(out, p.Name) {
			out = append(out, p.Name)
		}
	}
	return append(out, "pairs") // the rest: prop=value words
}

// The focus fill: a parameter that declares Focus is filled from the
// focused thing of its noun when the request omits it. focusValue is the
// one reading of it, for the palette slot, the args form and create.

// focusThing is a thing the focus holds: its key (the key prop in the
// instance's Data, else the instance id) and how it is shown.
type focusThing struct {
	Key, Label string
}

// instanceThing reads an instance as the thing it stands for.
func (r *Runtime) instanceThing(inst *Instance) focusThing {
	key := inst.ID
	if n := r.Nouns[inst.Noun]; n != nil {
		if kp := n.keyProp(); kp != nil {
			if v := dataField(inst.Data, kp.Name); !emptyProp(v) {
				key = fmt.Sprint(v)
			}
		}
	}
	label := r.thingLabel(inst)
	if label == "" {
		label = inst.Label
	}
	if label == "" {
		label = key
	}
	return focusThing{Key: key, Label: label}
}

// focusValue is the thing op's parameter name is filled with at focus
// when the request omits it: the nearest instance of the parameter's noun
// on the focus path, when the parameter declares Focus and the row's
// FocusSetting (if any) is on for a value that is not the subject.
func (r *Runtime) focusValue(op Operation, name, focus string) (focusThing, bool) {
	param, ok := op.Parameters[name]
	if !ok || !param.Focus || param.Noun == "" || focus == "" {
		return focusThing{}, false
	}
	if setting := focusSettings[op.Standard]; setting != "" && param.Noun != op.Noun && !r.helpFlag(setting, true) {
		return focusThing{}, false
	}
	id := r.nounInstance(r.CanonicalNoun(param.Noun), focus)
	if id == "" {
		return focusThing{}, false
	}
	return r.instanceThing(r.Instances[id]), true
}

// asking records the focus a request was made at while it runs.
func (r *Runtime) asking(focus string) (restore func()) {
	before := r.askedAt
	r.askedAt = focus
	return func() { r.askedAt = before }
}

// focusFills are the props create filled from the focus.
type focusFills []string

// notify names the fills in one notice, so a fill is never silent.
func (f focusFills) notify(c Ctx) {
	if len(f) > 0 {
		c.Notify(strings.Join(f, ", ") + " (from the focus)")
	}
}

// createFill fills values' omitted FromFocus props from the focus the
// request was made at (focusValue), and answers what it filled, each as
// "prop label".
func (r *Runtime) createFill(n *Noun, c Ctx, values Thing) focusFills {
	op, ok := r.Operations[n.Name+".create"]
	if !ok || op.Standard != "create" {
		return nil
	}
	focus := r.askedAt
	if focus == "" {
		focus = focusOf(c.Context)
	}
	var out focusFills
	for _, p := range createProps(n) {
		if !p.FromFocus {
			continue
		}
		if _, given := c.Args[p.Name]; given {
			continue
		}
		if t, ok := r.focusValue(op, p.Name, focus); ok {
			values[p.Name] = t.Key
			out = append(out, p.Name+" "+t.Label)
		}
	}
	return out
}

// archiveTarget is the archive prop and the thing when delete under
// delete.archive will archive key (a live thing of a noun with a writable
// archive prop, the setting on); nil when delete will delete it.
func (r *Runtime) archiveTarget(n *Noun, c Ctx, key string) (*PropSpec, Thing, error) {
	p := writableRole(n, RoleArchive)
	if p == nil || n.Store.Patch == nil || n.Store.Get == nil || !r.helpFlag(DeleteArchiveSetting, false) {
		return nil, nil, nil
	}
	t, err := n.Store.Get(c, key)
	if err != nil {
		return nil, nil, err
	}
	if v, err := propValue(*p, t[p.Name]); err == nil && v == true {
		return nil, nil, nil // archived already: delete it for real
	}
	return p, t, nil
}

// archiveOnDelete is delete under delete.archive: on a noun with a
// writable archive prop, a live thing is archived (ok, the answer
// {archived: key}); an archived one, or any thing with the setting off or
// on a noun without the role, is left to the real delete (not ok).
func (r *Runtime) archiveOnDelete(n *Noun, c Ctx, key string) (any, bool, error) {
	p, t, err := r.archiveTarget(n, c, key)
	if err != nil {
		return nil, true, err
	}
	if p == nil {
		return nil, false, nil
	}
	if _, err := n.Store.Patch(c, key, Thing{p.Name: true}); err != nil {
		return nil, true, err
	}
	label := n.displayLabel(func(name string) any { return t[name] })
	if label == "" {
		label = key
	}
	c.Notify(fmt.Sprintf("archived %s %s: delete it again to delete it for good (delete.archive)", r.DisplayNoun(n.Name), label))
	return map[string]any{"archived": key}, true, nil
}

// deleteConfirm is the standard delete's prompt for one request: prompt
// (the delete's) when any thing it acts on will be deleted, else the
// archive's, DefaultArchiveConfirm, or nothing with
// delete.archive-confirm off. A thing the store cannot read asks the
// delete prompt.
func deleteConfirm(r *Runtime, n *Noun, c Context, q Request, prompt string) string {
	insts := []*Instance{r.Instances[focusOf(c)]}
	if len(q.Subjects) > 1 {
		insts = insts[:0]
		for _, id := range q.Subjects {
			insts = append(insts, r.Instances[id])
		}
	}
	for _, inst := range insts {
		ctx := Ctx{actor: r.Actor(), Runtime: r, Context: c, Args: q.Args, Instance: inst}
		key, err := r.subjectKey(n, ctx)
		if err != nil {
			return prompt
		}
		if p, _, err := r.archiveTarget(n, ctx, key); p == nil || err != nil {
			return prompt
		}
	}
	if !r.helpFlag(DeleteArchiveConfirmSetting, true) {
		return ""
	}
	return DefaultArchiveConfirm
}

// propAliasClash refuses a prop that an alias of its noun is already
// named like, unless that alias is the prop's own set (2.5: task.status
// as task.set {prop: status}).
func (r *Runtime) propAliasClash(n *Noun, prop string) error {
	a, ok := r.actionNames.aliases[n.Name+"."+prop]
	if !ok || r.propSetAlias(n.Name, prop, a) {
		return nil
	}
	return fmt.Errorf("prop %s clashes with the alias %s of %s: a word after a noun is a verb or a prop, never both", prop, r.DisplayKey(a.Alias), r.DisplayKey(a.Action))
}

// propSetAlias reports whether an alias named noun.prop is that prop's
// set: its action noun's standard set with the preset prop the alias's
// own name. Only that alias may share a prop's name.
func (r *Runtime) propSetAlias(noun, prop string, a ActionAlias) bool {
	op, ok := r.Operations[a.Action]
	return ok && op.Noun == noun && op.Standard == "set" && fmt.Sprint(a.Args["prop"]) == prop
}

// aliasPropClash is the clash of an alias named after a prop of its noun
// that is not the prop's set (propSetAlias), nil for none.
func (r *Runtime) aliasPropClash(a ActionAlias) error {
	n := r.Nouns[a.Noun]
	if a.Bare || n.prop(a.Verb) == nil || r.propSetAlias(a.Noun, a.Verb, a) {
		return nil
	}
	return fmt.Errorf("alias %s clashes with the prop %s of %s: a word after a noun is a verb or a prop, never both (an alias of %s.set with prop %s may share it)", r.DisplayKey(a.Alias), a.Verb, r.DisplayNoun(a.Noun), r.DisplayNoun(a.Noun), a.Verb)
}

// StandardShadows lists, as info lines, every verb the application
// registered under a standard verb's name on a noun whose store would
// generate it: the named verb wins, and the standard one is not offered.
func (r *Runtime) StandardShadows() []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		n := r.Nouns[name]
		if n.Store == nil {
			continue
		}
		for _, v := range standardVerbs {
			op, ok := r.Operations[name+"."+v.Verb]
			if ok && op.Standard == "" && v.fits(n) {
				out = append(out, fmt.Sprintf("info: %s is a named verb: it shadows the standard %s of %s", r.DisplayKey(op.Key()), v.Verb, r.DisplayNoun(name)))
			}
		}
	}
	return out
}
