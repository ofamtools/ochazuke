package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The action table is flat: Runtime.Operations maps a name (an operation
// key, noun.verb) to its action, and an alias is one more name pointing at
// the same action. The dot in a name is a convention, not structure: a
// verb may hold one (dirs.base.add is the verb base.add of dirs), because
// a name is looked up whole and the action carries its own noun and verb.
// Nouns stay types and tags: they target (the nearest thing of the
// action's noun on the focus path) and group (the CLI, the palette, ?
// help).

// Alias sources: who declared an alias.
const (
	AliasFramework = "framework" // the framework's own old names (registered inside Builtin)
	AliasApp       = "app"       // an application's NounBuilder.Alias, Renamed or Runtime.AliasAction
	AliasConfig    = "config"    // a row of the user's actions.json
)

// ActionAlias is one extra name of a registered action. Alias is the
// name (noun.verb, its noun canonical), Action the canonical key it runs.
// Noun and Verb are the alias's own noun and verb: the subject side the
// handler is told (Invocation). Since and RemoveAfter mark a deprecated
// alias, an old name kept for one release after a rename (Keep actions):
// its first use in a runtime posts a one-time notice naming the new name.
// Note is free text shown beside it.
type ActionAlias struct {
	Alias       string `json:"alias"`
	Action      string `json:"action"`
	Noun        string `json:"noun,omitempty"`
	Verb        string `json:"verb,omitempty"`
	Since       string `json:"since,omitempty"`
	RemoveAfter string `json:"removeAfter,omitempty"`
	Note        string `json:"note,omitempty"`
	Source      string `json:"source,omitempty"`
}

// Deprecated reports whether the alias is an old name on its way out.
func (a ActionAlias) Deprecated() bool { return a.Since != "" || a.RemoveAfter != "" }

// notice is the one-time notice a deprecated alias posts on first use.
func (a ActionAlias) notice(display func(string) string) string {
	text := fmt.Sprintf("%s is an old name of %s", display(a.Alias), display(a.Action))
	var when []string
	if a.Since != "" {
		when = append(when, "renamed in "+a.Since)
	}
	if a.RemoveAfter != "" {
		when = append(when, "removed after "+a.RemoveAfter)
	}
	if len(when) > 0 {
		text += " (" + strings.Join(when, "; ") + ")"
	}
	return text
}

// Invocation is the name a request ran an action by: the action's own key
// or one of its aliases. Noun is that name's noun (the subject side: for
// project.associate, an alias of pane.associate, it is project), Verb its
// verb. Handlers read it from Ctx.Invoked (application verbs) or
// Runtime.Invoked (interaction handlers).
type Invocation struct {
	Name  string `json:"name"`
	Noun  string `json:"noun"`
	Verb  string `json:"verb"`
	Alias bool   `json:"alias,omitempty"`
}

// actionNames is the runtime's alias table and the invocation of the
// running request: one Runtime field.
type actionNames struct {
	aliases    map[string]ActionAlias // alias name -> alias
	invocation Invocation             // the running request's name
	noticed    map[string]bool        // deprecated aliases whose notice was posted
	problems   []string               // aliases dropped because an action took their name
}

// Invoked is the name the running request named its action by (the
// action's own key unless an alias ran it); the zero Invocation outside a
// request.
func (r *Runtime) Invoked() Invocation { return r.actionNames.invocation }

// AliasAction makes alias another name of the action registered as action
// (an application's permanent alias: show for get, forget for kill, or the
// same operation under another noun, project.associate for
// pane.associate). See AddActionAlias for the rules.
func (r *Runtime) AliasAction(alias, action string) error {
	return r.AddActionAlias(ActionAlias{Alias: alias, Action: action})
}

// AddActionAlias registers an alias. The alias names a registered noun and
// a verb (dirs.base.add: the longest registered noun prefix is the noun,
// the rest the verb); action names a registered action, by its key or by
// another alias (which resolves to its action). An alias may not be the
// name of a registered action, nor of another alias for a different
// action; registering the same alias for the same action again is a
// no-op. Source defaults to app, or framework inside Builtin.
func (r *Runtime) AddActionAlias(a ActionAlias) error {
	if a.Source == "" {
		a.Source = AliasApp
		if r.builtinDepth > 0 {
			a.Source = AliasFramework
		}
	}
	checked, err := r.checkAlias(a)
	if err != nil {
		return err
	}
	if prev, ok := r.actionNames.aliases[checked.Alias]; ok && prev.Action == checked.Action {
		return nil
	}
	if r.actionNames.aliases == nil {
		r.actionNames.aliases = map[string]ActionAlias{}
	}
	r.actionNames.aliases[checked.Alias] = checked
	return nil
}

// checkAlias validates an alias and returns it canonical: its name with
// the noun resolved, Noun and Verb split, Action the canonical key.
func (r *Runtime) checkAlias(a ActionAlias) (ActionAlias, error) {
	a.Alias, a.Action = strings.TrimSpace(a.Alias), strings.TrimSpace(a.Action)
	switch {
	case a.Alias == "" || a.Action == "":
		return a, errors.New("alias and action are both required")
	case strings.ContainsAny(a.Alias, " \t\n"):
		return a, fmt.Errorf("alias %q must be one name without spaces", a.Alias)
	}
	op, ok := r.Operation(a.Action)
	if !ok {
		return a, fmt.Errorf("action %q is not registered", a.Action)
	}
	a.Action = op.Key()
	noun, verb, ok := r.splitNounName(a.Alias)
	if !ok {
		return a, fmt.Errorf("alias %q names no registered noun (an alias is noun.verb)", a.Alias)
	}
	a.Noun, a.Verb, a.Alias = noun, verb, noun+"."+verb
	name := r.DisplayKey(a.Alias)
	if a.Alias == a.Action {
		return a, fmt.Errorf("alias %s names itself", name)
	}
	if _, taken := r.Operations[a.Alias]; taken {
		return a, fmt.Errorf("alias %s would shadow the action %s", name, name)
	}
	if prev, ok := r.actionNames.aliases[a.Alias]; ok && prev.Action != a.Action {
		return a, fmt.Errorf("alias %s is already an alias of %s (%s)", name, r.DisplayKey(prev.Action), prev.Source)
	}
	if r.Nouns[a.Alias] != nil {
		return a, fmt.Errorf("alias %s clashes with the noun %s", name, name)
	}
	return a, nil
}

// splitNounName splits a name into a registered noun (its canonical name)
// and the rest: the longest leading run of dotted words that names a noun
// or a noun alias wins, so dirs.base.add is dirs and base.add unless a
// noun dirs.base exists.
func (r *Runtime) splitNounName(name string) (noun, verb string, ok bool) {
	for i := strings.LastIndex(name, "."); i > 0; i = strings.LastIndex(name[:i], ".") {
		if i == len(name)-1 {
			continue
		}
		if n := r.CanonicalNoun(name[:i]); r.Nouns[n] != nil {
			return n, name[i+1:], true
		}
	}
	return "", "", false
}

// removeAlias drops an alias by name (config reloads, a later Register of
// that name).
func (r *Runtime) removeAlias(name string) { delete(r.actionNames.aliases, name) }

// claimName is called by Register before an action takes name: an alias
// of that name gives way (the canonical action wins) and the drop is
// recorded, never a panic, so a host mounting a newer catalog keeps
// starting.
func (r *Runtime) claimName(name string) {
	a, ok := r.actionNames.aliases[name]
	if !ok {
		return
	}
	r.removeAlias(name)
	r.actionNames.problems = append(r.actionNames.problems, fmt.Sprintf("alias %s of %s dropped: an action of that name is registered", r.DisplayKey(name), r.DisplayKey(a.Action)))
}

// ActionAliasProblems are the aliases dropped because an action registered
// under their name after them.
func (r *Runtime) ActionAliasProblems() []string { return slices.Clone(r.actionNames.problems) }

// ActionAliases lists every alias, sorted by name.
func (r *Runtime) ActionAliases() []ActionAlias {
	out := make([]ActionAlias, 0, len(r.actionNames.aliases))
	for _, a := range r.actionNames.aliases {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

// AliasesOf lists the aliases of an action (by its key or any of its
// names), sorted by name; nil when it has none.
func (r *Runtime) AliasesOf(name string) []ActionAlias {
	op, ok := r.Operation(name)
	if !ok {
		return nil
	}
	var out []ActionAlias
	for _, a := range r.actionNames.aliases {
		if a.Action == op.Key() {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

// aliasNames is the display names of an action's aliases.
func (r *Runtime) aliasNames(key string) []string {
	var out []string
	for _, a := range r.AliasesOf(key) {
		out = append(out, r.DisplayKey(a.Alias))
	}
	return out
}

// aliasLabels is how ? help lists an action's aliases on its row: an
// alias on the action's own noun by its verb (forget), one under another
// noun by its display key (project.associate).
func (r *Runtime) aliasLabels(key string) []string {
	op, ok := r.Operation(key)
	if !ok {
		return nil
	}
	var out []string
	for _, a := range r.AliasesOf(key) {
		if a.Noun == op.Noun {
			out = append(out, a.Verb)
		} else {
			out = append(out, r.DisplayKey(a.Alias))
		}
	}
	return out
}

// ActionAlias is the alias a name is, its noun resolved (false when the
// name is no alias).
func (r *Runtime) ActionAlias(name string) (ActionAlias, bool) {
	a, ok := r.actionNames.aliases[r.CanonicalKey(name)]
	return a, ok
}

// ResolveAction is the action a name runs and how it was named: the
// action's own key (noun aliases resolved: settings.set is
// ochazuke.settings.set) or an alias of it.
func (r *Runtime) ResolveAction(name string) (Operation, Invocation, bool) {
	key := r.CanonicalKey(name)
	if op, ok := r.Operations[key]; ok {
		return op, Invocation{Name: key, Noun: op.Noun, Verb: op.Verb}, true
	}
	if a, ok := r.actionNames.aliases[key]; ok {
		if op, ok := r.Operations[a.Action]; ok {
			return op, Invocation{Name: a.Alias, Noun: a.Noun, Verb: a.Verb, Alias: true}, true
		}
	}
	return Operation{}, Invocation{}, false
}

// invocationOf is the Invocation of a canonical request: its alias when
// it was spelled by one (Request.Via), else its own key.
func (r *Runtime) invocationOf(q Request) (Invocation, *ActionAlias) {
	if q.Via != "" {
		if a, ok := r.actionNames.aliases[q.Via]; ok && a.Action == q.Key() {
			return Invocation{Name: a.Alias, Noun: a.Noun, Verb: a.Verb, Alias: true}, &a
		}
	}
	return Invocation{Name: q.Key(), Noun: q.Noun, Verb: q.Verb}, nil
}

// invoking makes inv the running request's name until restore runs, and
// posts a deprecated alias's one-time notice.
func (r *Runtime) invoking(inv Invocation, a *ActionAlias) (restore func()) {
	if a != nil && a.Deprecated() && !r.actionNames.noticed[a.Alias] {
		if r.actionNames.noticed == nil {
			r.actionNames.noticed = map[string]bool{}
		}
		r.actionNames.noticed[a.Alias] = true
		r.UI().Notify(a.notice(r.DisplayKey))
	}
	before := r.actionNames.invocation
	r.actionNames.invocation = inv
	return func() { r.actionNames.invocation = before }
}

// keyParts is the noun and verb of the action a name runs (so a dotted
// verb splits where it was declared, and an alias reads its action's),
// else SplitKey's last-dot reading.
func (r *Runtime) keyParts(name string) (noun, verb string) {
	if op, ok := r.Operation(name); ok {
		return op.Noun, op.Verb
	}
	return SplitKey(r.CanonicalKey(name))
}

// Alias makes each name another verb of this noun for the last declared
// verb (show for get, forget for kill): a permanent alias. A name may hold
// a dot (base.add). It panics on a clash (AddActionAlias), as Verb does on
// a duplicate.
func (b *NounBuilder) Alias(verbs ...string) *NounBuilder {
	if b.last == "" {
		panic("Alias must follow Verb")
	}
	for _, verb := range verbs {
		if err := b.r.AliasAction(b.n.Name+"."+verb, b.last); err != nil {
			panic(err)
		}
	}
	return b
}

// AliasOn makes noun.verb another name of the last declared verb: one
// operation reachable under both nouns' names (project.associate for
// pane.associate). The handler reads which side named it from
// Ctx.Invoked.
func (b *NounBuilder) AliasOn(noun, verb string) *NounBuilder {
	if b.last == "" {
		panic("AliasOn must follow Verb")
	}
	if err := b.r.AliasAction(noun+"."+verb, b.last); err != nil {
		panic(err)
	}
	return b
}

// Renamed keeps old, the verb's name before a rename, as a deprecated
// alias of the last declared verb: since is the release that renamed it
// and removeAfter the last release that keeps the old name (one release,
// per Keep actions; record it in the CHANGELOG). Its first use posts a
// one-time notice naming the new name.
func (b *NounBuilder) Renamed(old, since, removeAfter string) *NounBuilder {
	if b.last == "" {
		panic("Renamed must follow Verb")
	}
	if err := b.r.AddActionAlias(ActionAlias{Alias: b.n.Name + "." + old, Action: b.last, Since: since, RemoveAfter: removeAfter}); err != nil {
		panic(err)
	}
	return b
}
