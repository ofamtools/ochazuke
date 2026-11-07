package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The role-derived row (archive, in the core table, core_verbs.go) reads
// its prop from a role and runs the standard set, so history, watch,
// availability and the store are set's: archive sets the archive prop
// (RoleArchive) true. Un-archiving is set {props: {"@archive": false}}.
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

// setVia runs the standard set on c's subject with prop written to value:
// a derived verb is set under another name. Only the subject's key goes
// through from the request's arguments.
func (r *Runtime) setVia(n *Noun, c Ctx, prop string, value any) (any, error) {
	args := Arguments{"props": map[string]any{prop: value}}
	if key := n.keyProp(); key != nil {
		if v, ok := c.Args[key.Name]; ok {
			args[key.Name] = v
		}
	}
	c.Args = args
	return runSet(r, n, c)
}

// runArchive sets the archive prop true.
func runArchive(r *Runtime, n *Noun, c Ctx) (any, error) {
	return r.setVia(n, c, n.roleProp(RoleArchive).Name, true)
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
	if !ok || !param.Focus || param.Required || param.Noun == "" || focus == "" {
		return focusThing{}, false
	}
	if row := standardRow(op.Standard); row != nil && row.FocusSetting != "" && param.Noun != op.Noun && !r.helpFlag(row.FocusSetting, true) {
		return focusThing{}, false
	}
	id := r.nounInstance(r.CanonicalNoun(param.Noun), focus)
	if personSource(r.history.source) {
		id, _ = r.nearestOf(param.Noun, focus)
	}
	if id == "" {
		return focusThing{}, false
	}
	return r.instanceThing(r.Instances[id]), true
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
// request was made at (focusValue, with the parameters of the standard
// verb running: Ctx.standard), and answers what it filled, each as "prop
// label".
func (r *Runtime) createFill(n *Noun, c Ctx, values Thing) focusFills {
	op, ok := r.Operations[c.standard]
	if !ok {
		return nil
	}
	focus := ""
	if rc := requestOf(c.Context); rc != nil {
		focus = rc.AskedAt
	}
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
// write: its action is a standard verb of the noun that writes props
// (WritesProps: set) whose presets write that prop alone, named either
// way set takes one: prop (`{prop: read, value: true}`) or props
// (`{props: {read: true}}`). Only that alias may share a prop's name.
func (r *Runtime) propSetAlias(noun, prop string, a ActionAlias) bool {
	op, ok := r.Operations[a.Action]
	if !ok || op.Noun != noun {
		return false
	}
	row := standardRow(op.Standard)
	return row != nil && row.WritesProps && slices.Equal(presetWrites(a.Args), []string{prop})
}

// presetWrites are the props an alias's preset arguments write, by
// either of set's forms: the prop argument, the keys of the props object.
func presetWrites(args Arguments) []string {
	var out []string
	if p, ok := args["prop"].(string); ok && p != "" {
		out = append(out, p)
	}
	props, _ := args["props"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(props)) {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
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
// A verb declared to shadow it on purpose (Operation.ShadowsStandard) is
// not listed.
func (r *Runtime) StandardShadows() []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		n := r.Nouns[name]
		if n.Store == nil {
			continue
		}
		for _, v := range standardTable() {
			op, ok := r.Operations[name+"."+v.Verb]
			if ok && op.Standard == "" && !op.ShadowsStandard && v.unmet(r, n) == "" {
				out = append(out, fmt.Sprintf("info: %s is a named verb: it shadows the standard %s of %s", r.DisplayKey(op.Key()), v.Verb, r.DisplayNoun(name)))
			}
		}
	}
	return out
}
