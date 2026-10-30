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
	AliasStandard  = "standard"  // generated with a standard verb (NOUN.patch for NOUN.set); gives way, unrecorded, to an application verb of its name
)

// ActionAlias is one extra name of a registered action. Alias is the
// name (noun.verb, its noun canonical), Action the canonical key it runs.
// Noun and Verb are the alias's own noun and verb: the subject side the
// handler is told (Invocation). Since and RemoveAfter mark a deprecated
// alias, an old name kept for one release after a rename (Keep actions):
// its first use in a runtime posts a one-time notice naming the new name.
// Note is free text shown beside it. Args are preset arguments the
// alias runs its action with ({"force": true} for w!): a typed or
// bound argument of the same name wins. Presets may leave a required
// parameter out: it is asked for when the alias runs (the args form for a
// key or the palette, NAME is required for the CLI and the API). Bare marks an alias declared
// without a noun (w!, q): its noun is its action's, and the bare name
// alone reaches it in the palette, the CLI and the API (Request{Verb:
// "w!"}); a binding row names it by its full name (restructure.w!).
// Shadows marks a bare alias that may take the name of a verb and yields
// to it by what follows (set for settings.apply beside mode set): typed
// alone, or followed by words its action's rest parameter reads, it is
// the alias; followed by a noun that has the verb of its name, the line
// is that noun's verb (set mode normal is mode set normal). Its name is
// the bare word (set), since NOUN.VERB may be the verb it shadows.
// Palette keeps a bare alias to the command line inside the app: the CLI
// leaves the word to the shell's own commands, so bin help stays the
// shell's help while :help opens the docs viewer, which needs the TUI.
type ActionAlias struct {
	Alias       string    `json:"alias"`
	Action      string    `json:"action"`
	Noun        string    `json:"noun,omitempty"`
	Verb        string    `json:"verb,omitempty"`
	Since       string    `json:"since,omitempty"`
	RemoveAfter string    `json:"removeAfter,omitempty"`
	Note        string    `json:"note,omitempty"`
	Source      string    `json:"source,omitempty"`
	Args        Arguments `json:"args,omitempty"`
	Bare        bool      `json:"bare,omitempty"`
	Shadows     bool      `json:"shadows,omitempty"`
	Palette     bool      `json:"palette,omitempty"`
}

// Name is how the alias is typed: its bare name, else noun.verb.
func (a ActionAlias) Name() string {
	if a.Bare {
		return a.Verb
	}
	return a.Alias
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
	if a.Bare && ok && noun == op.Noun && !strings.Contains(verb, ".") {
		// An alias already made bare (its canonical noun.verb) again.
		a.Alias, ok = verb, false
	}
	switch {
	case ok && !a.Bare && a.Shadows:
		return a, fmt.Errorf("alias %s: shadows is for a bare alias (a name without a dot)", a.Alias)
	case ok && !a.Bare:
	case !strings.Contains(a.Alias, "."):
		// A bare alias (w!, q): its noun is its action's.
		bare := a.Alias
		noun, verb, a.Bare = op.Noun, bare, true
		if prev, ok := r.bareAlias(bare); ok && prev.Alias != bareName(noun, verb, a.Shadows) {
			return a, fmt.Errorf("alias %s is already an alias of %s (%s)", bare, r.DisplayKey(prev.Action), prev.Source)
		}
		if r.Nouns[r.CanonicalNoun(bare)] != nil {
			return a, fmt.Errorf("alias %s clashes with the noun %s", bare, bare)
		}
		if owner, ok := r.verbOwner(bare); ok && !a.Shadows && r.AliasShadow() != AliasShadowAllow {
			return a, fmt.Errorf("alias %s would shadow the verb %s (%s is %s)", bare, r.DisplayKey(owner), AliasShadowSetting, AliasShadowRefuse)
		}
	default:
		return a, fmt.Errorf("alias %q names no registered noun (an alias is noun.verb, or a bare name without a dot)", a.Alias)
	}
	a.Noun, a.Verb, a.Alias = noun, verb, bareName(noun, verb, a.Bare && a.Shadows)
	if len(a.Args) > 0 {
		// Presets are part of a request: what they give is checked here,
		// a required argument they leave out when the alias runs.
		if err := validatePartialArguments(op, a.Args); err != nil {
			return a, fmt.Errorf("alias %s args: %v", a.Name(), err)
		}
	}
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
	if err := r.aliasPropClash(a); err != nil {
		return a, err
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

// bareName is an alias's name in the table: noun.verb, or the bare word
// for an alias that shadows a verb (ActionAlias.Shadows).
func bareName(noun, verb string, shadows bool) string {
	if shadows {
		return verb
	}
	return noun + "." + verb
}

// bareAlias is the bare alias typed as name, false when none is.
func (r *Runtime) bareAlias(name string) (ActionAlias, bool) {
	for _, a := range r.actionNames.aliases {
		if a.Bare && a.Verb == name {
			return a, true
		}
	}
	return ActionAlias{}, false
}

// verbOwner is the first registered action (by key) whose verb is name:
// what a bare alias of that name would shadow as a typed word.
func (r *Runtime) verbOwner(name string) (string, bool) {
	var keys []string
	for key, op := range r.Operations {
		if op.Verb == name {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return "", false
	}
	sort.Strings(keys)
	return keys[0], true
}

// AliasShadowSetting says whether a bare alias may take the name of a
// verb (a typed word): refuse (the default: such an alias row is skipped
// and reported) or allow (the alias loads and, typed alone, wins over the
// verb of that name; the verb stays reachable as noun verb).
const AliasShadowSetting = "aliases.shadow"

// The values of AliasShadowSetting.
const (
	AliasShadowRefuse = "refuse"
	AliasShadowAllow  = "allow"
)

// AliasShadow is the aliases.shadow setting's value (refuse unless set to
// allow).
func (r *Runtime) AliasShadow() string {
	if v, _ := r.settingValue(AliasShadowSetting); v == AliasShadowAllow {
		return AliasShadowAllow
	}
	return AliasShadowRefuse
}

// withAliasArgs fills q's arguments the alias presets and q leaves out
// (Operation.WithPresets: a caller's argument also wins over a preset it
// excludes).
func withAliasArgs(op Operation, q Request, a ActionAlias) Request {
	if len(a.Args) == 0 {
		return q
	}
	q.Args = op.WithPresets(q.Args, a.Args)
	return q
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
	if a.Source == AliasStandard {
		return // a generated alias (patch) gives way to the application's verb
	}
	r.actionNames.problems = append(r.actionNames.problems, fmt.Sprintf("alias %s of %s dropped: an action of that name is registered", r.DisplayKey(name), r.DisplayKey(a.Action)))
}

// claimBareName is called when a noun named name is registered: a bare
// alias of that word gives way (a noun's name is never an alias's, and the
// noun came later: o-inventory's verb noun over the framework's verb
// alias), recorded as claimName records it.
func (r *Runtime) claimBareName(name string) {
	a, ok := r.bareAlias(name)
	if !ok {
		return
	}
	r.removeAlias(a.Alias)
	r.actionNames.problems = append(r.actionNames.problems, fmt.Sprintf("alias %s of %s dropped: a noun of that name is registered", name, r.DisplayKey(a.Action)))
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

// ActionAlias is the alias a name is, its noun resolved, or the bare
// alias a word without a dot is (false when the name is no alias).
func (r *Runtime) ActionAlias(name string) (ActionAlias, bool) {
	if a, ok := r.actionNames.aliases[r.CanonicalKey(name)]; ok {
		return a, true
	}
	if !strings.Contains(name, ".") {
		return r.bareAlias(name)
	}
	return ActionAlias{}, false
}

// ResolveAction is the action a name runs and how it was named: the
// action's own key (noun aliases resolved: settings.set is
// ochazuke.settings.set) or an alias of it.
func (r *Runtime) ResolveAction(name string) (Operation, Invocation, bool) {
	if a, ok := r.bareAlias(strings.TrimPrefix(name, ".")); ok && !strings.Contains(strings.TrimPrefix(name, "."), ".") {
		name = a.Alias
	}
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
