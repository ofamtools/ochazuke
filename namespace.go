package ochazuke

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// FrameworkNamespace is the namespace of the framework's own nouns: every
// noun the framework registers (inside Runtime.Builtin) is named
// ochazuke.<noun> (ochazuke.settings, ochazuke.list, ochazuke.page), with
// the short name (settings) as an alias. The app noun itself keeps its
// name (AppNoun), so its verbs (app.quit, an application's app.state)
// never collide with a framework noun.
const FrameworkNamespace = "ochazuke"

// AppNoun is the noun of the application as a whole (app.quit). It is
// never namespaced.
const AppNoun = "app"

// legacyNamespace is the framework namespace of v0.16.0: app.<noun> is
// read as ochazuke.<noun> for one release (legacyNoun).
const legacyNamespace = AppNoun

// SplitKey splits an operation key on its last dot: the noun before it (a
// noun may be dotted: ochazuke.settings, ai.skills) and the verb after it.
// It is the reading of a name nothing is registered under; a registered
// name splits where it was declared (a verb may hold a dot: dirs.base.add),
// which Runtime.CanonicalKey and the request lookup read from the table. A
// key with no dot is all noun and no verb.
func SplitKey(key string) (noun, verb string) {
	i := strings.LastIndex(key, ".")
	if i < 0 {
		return key, ""
	}
	return key[:i], key[i+1:]
}

// CutKey is SplitKey with ok reporting whether the key held a dot.
func CutKey(key string) (noun, verb string, ok bool) {
	noun, verb = SplitKey(key)
	return noun, verb, noun != key
}

// Namespaced is name under namespace: "ai" and "skills" give "ai.skills";
// an empty namespace is name unchanged.
func Namespaced(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}

// CanonicalNoun is the registered noun a noun name stands for. A noun the
// application registered under that exact name owns it (an app's own
// settings noun shadows the framework's alias); otherwise an alias
// (settings for ochazuke.settings) names its noun; otherwise the name is
// returned unchanged. While framework code registers (Runtime.Builtin) an
// alias wins over an application noun of the same name, so the
// framework's rows and tables always reach its own nouns. The v0.16.0
// spelling app.<noun> of a framework noun is read as ochazuke.<noun>
// (deprecated, for one release) unless a noun of that exact name exists or
// the app noun has a verb of that name (app.state, an application verb,
// shadows the old spelling of ochazuke.state).
func (r *Runtime) CanonicalNoun(name string) string {
	target, aliased := r.aliases[name]
	if !aliased {
		if legacy, ok := r.legacyNoun(name); ok {
			return legacy
		}
		return name
	}
	if r.builtinDepth > 0 || !r.ownsNoun(name) {
		return target
	}
	return name
}

// legacyNoun reads a v0.16.0 framework noun name (app.settings) as its
// canonical name (ochazuke.settings) when that is a framework noun, no noun
// is registered under the old name and the app noun has no verb of the
// short name's first word.
func (r *Runtime) legacyNoun(name string) (string, bool) {
	short, ok := strings.CutPrefix(name, legacyNamespace+".")
	if !ok || short == "" || r.Nouns[name] != nil {
		return "", false
	}
	first, _, _ := strings.Cut(short, ".")
	if _, verb := r.Operations[legacyNamespace+"."+first]; verb {
		return "", false
	}
	canonical := FrameworkNamespace + "." + short
	if r.Nouns[canonical] == nil {
		return "", false
	}
	return canonical, true
}

// ownsNoun reports whether name is registered as a noun of its own: a
// defined noun or the noun of an operation.
func (r *Runtime) ownsNoun(name string) bool {
	return r.Nouns[name] != nil // Register files every operation's noun (ensureNoun)
}

// CanonicalKey is an operation key with its noun resolved (CanonicalNoun):
// settings.set is ochazuke.settings.set unless the application has a settings
// noun of its own. A name is looked up whole first: a registered action or
// action alias keeps its name, and a dotted verb (dirs.base.add) resolves
// where it was declared; otherwise each dot, last first, is tried as the
// noun/verb split, and an unknown name reads its last dot (SplitKey).
func (r *Runtime) CanonicalKey(key string) string {
	if r.knownName(key) {
		return key
	}
	for i := strings.LastIndex(key, "."); i > 0; i = strings.LastIndex(key[:i], ".") {
		if c := r.CanonicalNoun(key[:i]) + key[i:]; c != key && r.knownName(c) {
			return c
		}
	}
	noun, verb := SplitKey(key)
	if verb == "" {
		return key
	}
	return r.CanonicalNoun(noun) + "." + verb
}

// knownName reports whether name is a registered action or action alias.
func (r *Runtime) knownName(name string) bool {
	if _, ok := r.Operations[name]; ok {
		return true
	}
	_, ok := r.actionNames.aliases[name]
	return ok
}

// canonicalRequest is q with its noun resolved and its name looked up in
// the action table: a registered key keeps its action's own noun and verb
// (a dotted verb split where it was declared), and an alias becomes its
// action with Via recording the alias it was spelled by.
func (r *Runtime) canonicalRequest(q Request) Request {
	if q.IsProp() {
		// A prop-shaped request names a prop, not an action: only its
		// noun resolves.
		q.Noun = r.CanonicalNoun(q.Noun)
		return q
	}
	// A bare alias names a request by one word (Request{Verb: "w!"}, or
	// Do("w!"), which reads it as the noun).
	if bare := q.Noun + q.Verb; (q.Noun == "" || q.Verb == "") && bare != "" {
		if a, ok := r.bareAlias(bare); ok {
			if op, ok := r.Operations[a.Action]; ok {
				if q.Via == "" {
					q.Via = a.Alias
				}
				q.Noun, q.Verb = op.Noun, op.Verb
				return withAliasArgs(op, q, a)
			}
		}
	}
	q.Noun = r.CanonicalNoun(q.Noun)
	if q.Verb == "" {
		return q
	}
	key := r.CanonicalKey(q.Key())
	if op, ok := r.Operations[key]; ok {
		q.Noun, q.Verb = op.Noun, op.Verb
		if a, ok := r.actionNames.aliases[q.Via]; ok && a.Action == key {
			q = withAliasArgs(op, q, a)
		}
		return q
	}
	if a, ok := r.actionNames.aliases[key]; ok {
		if op, ok := r.Operations[a.Action]; ok {
			if q.Via == "" {
				q.Via = a.Alias
			}
			q.Noun, q.Verb = op.Noun, op.Verb
			q = withAliasArgs(op, q, a)
		}
	}
	return q
}

// CanonicalRequest is q as the runtime runs it: its noun resolved and an
// alias rewritten to its action, Via keeping the alias (a host building a
// request from words, the CLI shell, calls it before reading the
// operation by q.Key()).
func (r *Runtime) CanonicalRequest(q Request) Request { return r.canonicalRequest(q) }

// Operation is the registered operation a key names, aliases resolved:
// noun aliases (settings.set) and action aliases (an old verb name) alike.
func (r *Runtime) Operation(key string) (Operation, bool) {
	op, _, ok := r.ResolveAction(key)
	return op, ok
}

// Alias makes alias another name of noun: input naming alias (keys, binding
// files, actions.json, the palette, the CLI, Do) reaches noun unless the
// application registers a noun named alias, which then owns the name. The
// framework registers one per framework noun (settings for ochazuke.settings);
// a host mounting an extension's nouns under a namespace (json.doc) gives
// each its bare name (doc) the same way. An alias holding a dot is refused,
// as is one that already names another noun (Unalias it first). See
// AddNounAlias for a deprecated alias.
func (r *Runtime) Alias(alias, noun string) error {
	return r.AddNounAlias(NounAlias{Alias: alias, Noun: noun})
}

// AddNounAlias registers a noun alias with its fields: Since and
// RemoveAfter (either) make it a deprecated name, accepted for now with a
// one-time notice naming the noun (and a bindings check note for a row
// spelled by it), but never completed, listed in the CLI's help or drawn
// (DisplayNoun). Note is free text added to the notice. Shadowed is
// ignored. Registering the same alias again replaces its fields.
func (r *Runtime) AddNounAlias(a NounAlias) error {
	alias, noun := a.Alias, a.Noun
	switch {
	case alias == "" || noun == "":
		return fmt.Errorf("alias and noun must be nonempty")
	case alias == noun:
		return fmt.Errorf("alias %q names itself", alias)
	case strings.Contains(alias, "."):
		return fmt.Errorf("alias %q holds a dot", alias)
	}
	if prev, ok := r.aliases[alias]; ok && prev != noun {
		return fmt.Errorf("alias %q already names %s", alias, prev)
	}
	r.setAlias(alias, noun)
	a.Shadowed = false
	if a.Deprecated() || a.Note != "" {
		if r.actionNames.nouns.meta == nil {
			r.actionNames.nouns.meta = map[string]NounAlias{}
		}
		r.actionNames.nouns.meta[alias] = a
	} else {
		delete(r.actionNames.nouns.meta, alias)
	}
	return nil
}

// Unalias removes a noun alias, so a host can rename a noun's short name
// while it runs (Unalias the old, Alias the new, or AddNounAlias it as
// deprecated). It is an error when alias names no noun.
func (r *Runtime) Unalias(alias string) error {
	if _, ok := r.aliases[alias]; !ok {
		return fmt.Errorf("%q is not a noun alias", alias)
	}
	r.dropAlias(alias)
	return nil
}

// setAlias files alias -> noun in the table and the reverse index.
func (r *Runtime) setAlias(alias, noun string) {
	if r.aliases[alias] == noun {
		return
	}
	r.aliases[alias] = noun
	if r.actionNames.nouns.of == nil {
		r.actionNames.nouns.of = map[string][]string{}
	}
	r.actionNames.nouns.of[noun] = append(r.actionNames.nouns.of[noun], alias)
}

// dropAlias takes alias out of the table, the reverse index and the
// deprecated fields.
func (r *Runtime) dropAlias(alias string) {
	noun := r.aliases[alias]
	delete(r.aliases, alias)
	delete(r.actionNames.nouns.meta, alias)
	delete(r.actionNames.nouns.noticed, alias)
	names := slices.DeleteFunc(r.actionNames.nouns.of[noun], func(a string) bool { return a == alias })
	if len(names) == 0 {
		delete(r.actionNames.nouns.of, noun)
	} else {
		r.actionNames.nouns.of[noun] = names
	}
}

// nounNames holds what the alias table (Runtime.aliases) does not: a
// noun's aliases (the reverse index DisplayNoun reads), the deprecated
// aliases' fields and which of them has posted its notice.
type nounNames struct {
	of      map[string][]string  // noun -> its aliases, in registration order
	meta    map[string]NounAlias // alias -> its fields, for a deprecated alias or one with a note
	noticed map[string]bool      // deprecated aliases whose notice was posted
	// display is the nouns.display value, kept by an observer once read
	// (displayRead).
	display     string
	displayRead bool
}

// Aliases lists the alias names, each with the noun it names, sorted by
// alias; a deprecated alias carries its Since, RemoveAfter and Note.
func (r *Runtime) Aliases() []NounAlias {
	out := make([]NounAlias, 0, len(r.aliases))
	for alias, noun := range r.aliases {
		a := r.nounAlias(alias)
		a.Shadowed = r.CanonicalNoun(alias) != noun
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

// nounAlias is the alias's entry ({alias, noun} with its fields); the zero
// NounAlias when alias is not one.
func (r *Runtime) nounAlias(alias string) NounAlias {
	noun, ok := r.aliases[alias]
	if !ok {
		return NounAlias{}
	}
	a, ok := r.actionNames.nouns.meta[alias]
	if !ok {
		a = NounAlias{Alias: alias}
	}
	a.Noun = noun
	return a
}

// NounAliasesOf lists the aliases that reach noun now (not shadowed by an
// application noun of their name), the live ones shortest first, then the
// deprecated ones.
func (r *Runtime) NounAliasesOf(noun string) []NounAlias {
	noun = r.CanonicalNoun(noun)
	var out []NounAlias
	for _, alias := range r.actionNames.nouns.of[noun] {
		if r.CanonicalNoun(alias) == noun {
			out = append(out, r.nounAlias(alias))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if di, dj := out[i].Deprecated(), out[j].Deprecated(); di != dj {
			return dj
		}
		if len(out[i].Alias) != len(out[j].Alias) {
			return len(out[i].Alias) < len(out[j].Alias)
		}
		return out[i].Alias < out[j].Alias
	})
	return out
}

// NounAlias is one alias: the short name, the noun it names, and whether
// an application noun of that name shadows it (the short name then reaches
// the application's noun and the framework's stays reachable by Noun).
// Since and RemoveAfter mark a deprecated alias (an old name kept for a
// release after a rename, Keep actions); Note is free text its notice adds.
type NounAlias struct {
	Alias       string `json:"alias"`
	Noun        string `json:"noun"`
	Shadowed    bool   `json:"shadowed,omitempty"`
	Since       string `json:"since,omitempty"`
	RemoveAfter string `json:"removeAfter,omitempty"`
	Note        string `json:"note,omitempty"`
}

// Deprecated reports whether the alias is an old name on its way out.
func (a NounAlias) Deprecated() bool { return a.Since != "" || a.RemoveAfter != "" }

// notice is the one-time notice a deprecated noun alias posts on first use.
func (a NounAlias) notice(display func(string) string) string {
	text := fmt.Sprintf("%s is an old name of %s", a.Alias, display(a.Noun))
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
	if a.Note != "" {
		text += ": " + a.Note
	}
	return text
}

// DeprecatedNounAlias is the deprecated alias name stands for, if it is one
// that reaches its noun now.
func (r *Runtime) DeprecatedNounAlias(name string) (NounAlias, bool) {
	a, ok := r.actionNames.nouns.meta[name]
	if !ok || !a.Deprecated() || r.aliases[name] == "" || r.CanonicalNoun(name) != r.aliases[name] {
		return NounAlias{}, false
	}
	a.Noun = r.aliases[name]
	return a, true
}

// NounAliasNotice is the one-time notice of name when it is a deprecated
// noun alias whose notice has not been given yet in this runtime, and
// records it as given; ok is false otherwise. NoteNounAlias posts it; a
// host printing its own warnings (cli.Mount) takes the text.
func (r *Runtime) NounAliasNotice(name string) (text string, ok bool) {
	a, ok := r.DeprecatedNounAlias(name)
	if !ok || r.actionNames.nouns.noticed[name] {
		return "", false
	}
	if r.actionNames.nouns.noticed == nil {
		r.actionNames.nouns.noticed = map[string]bool{}
	}
	r.actionNames.nouns.noticed[name] = true
	return a.notice(r.DisplayNoun), true
}

// NoteNounAlias posts the one-time notice of a deprecated noun alias typed
// as name (a request's noun, a palette or CLI word); nothing for any other
// name.
func (r *Runtime) NoteNounAlias(name string) {
	if text, ok := r.NounAliasNotice(name); ok {
		r.UI().Notify(text)
	}
}

// AliasOf is the alias DisplayNoun draws noun by ("" when it draws the
// noun itself): under nouns.display shortest (the default) the shortest
// live alias that reaches noun now and is shorter than its name; under
// framework only a framework noun's short name (settings for
// ochazuke.settings); under canonical none.
func (r *Runtime) AliasOf(noun string) string {
	switch r.nounsDisplay() {
	case NounsDisplayCanonical:
		return ""
	case NounsDisplayFramework:
		short, ok := strings.CutPrefix(noun, FrameworkNamespace+".")
		if ok && r.aliases[short] == noun && r.CanonicalNoun(short) == noun {
			return short
		}
		return ""
	}
	aliases := r.actionNames.nouns.of[noun]
	if len(aliases) == 0 {
		return ""
	}
	best := ""
	for _, alias := range aliases {
		if a, ok := r.actionNames.nouns.meta[alias]; ok && a.Deprecated() {
			continue
		}
		if r.CanonicalNoun(alias) != noun || len(alias) >= len(noun) {
			continue
		}
		if best == "" || len(alias) < len(best) || len(alias) == len(best) && alias < best {
			best = alias
		}
	}
	return best
}

// NounsDisplaySetting says which name DisplayNoun draws a noun by.
const NounsDisplaySetting = "nouns.display"

// The nouns.display values: shortest (the shortest live alias, any noun),
// framework (only the framework nouns' short names, before v0.57) and
// canonical (every noun by its registered name).
const (
	NounsDisplayShortest  = "shortest"
	NounsDisplayFramework = "framework"
	NounsDisplayCanonical = "canonical"
)

// nounsDisplay is the nouns.display value, read once and then kept by an
// observer: DisplayNoun runs for every hint of every frame.
func (r *Runtime) nounsDisplay() string {
	n := &r.actionNames.nouns
	if !n.displayRead {
		n.displayRead = true
		n.display = r.Settings().String(NounsDisplaySetting)
		r.Settings().Observe(NounsDisplaySetting, func(_ string, v any) { n.display, _ = v.(string) })
	}
	if n.display != "" {
		return n.display
	}
	return NounsDisplayShortest
}

// DisplayNoun is the shortest name that reaches noun: its alias (AliasOf)
// while no application noun shadows it (list for ochazuke.list, doc for an
// extension's json.doc a host aliased doc), else noun itself. The
// footer's hints, the ? help, the palette and the jobs ticker draw it;
// history, bindings.list, operations.list and the catalog print canonical
// names.
func (r *Runtime) DisplayNoun(noun string) string {
	if short := r.AliasOf(noun); short != "" {
		return short
	}
	return noun
}

// DisplayKey is an operation key with its noun as DisplayNoun.
func (r *Runtime) DisplayKey(key string) string {
	noun, verb := SplitKey(key)
	if verb == "" {
		return r.DisplayNoun(key)
	}
	return r.DisplayNoun(noun) + "." + verb
}

// frameworkName is the canonical name of a noun framework code registers:
// ochazuke.<name> for a plain name (app, ochazuke itself and dotted names
// unchanged).
func frameworkName(name string) string {
	if name == AppNoun || name == FrameworkNamespace || strings.Contains(name, ".") {
		return name
	}
	return FrameworkNamespace + "." + name
}

// noteFrameworkNoun records the alias of a noun in the framework
// namespace: ochazuke.settings answers to settings (the first noun to claim a
// short name keeps it).
func (r *Runtime) noteFrameworkNoun(name string) {
	short, ok := strings.CutPrefix(name, FrameworkNamespace+".")
	if !ok || short == "" || strings.Contains(short, ".") {
		return
	}
	if _, taken := r.aliases[short]; !taken {
		r.setAlias(short, name)
	}
}

// checkOverlap refuses a key that is both an operation and a noun: noun x
// with verb y beside noun x.y would make "x y" and "x.y" mean two things.
func (r *Runtime) checkOverlap(key string) error {
	if r.Nouns[key] != nil {
		return fmt.Errorf("operation %s clashes with the noun %s: a verb may not share its name with a noun nested under its noun", key, key)
	}
	return nil
}

// checkNounOverlap refuses a new noun whose name is an operation key.
func (r *Runtime) checkNounOverlap(name string) error {
	if _, ok := r.Operations[name]; ok {
		noun, verb := SplitKey(name)
		return fmt.Errorf("noun %s clashes with the operation %s (verb %s of %s): a noun may not be nested under a noun with a verb of that name", name, name, verb, noun)
	}
	return nil
}

// canonicalPlace is a place with its nouns resolved (a caller may name
// them by alias: NounPlace{Noun: "list"}).
func (r *Runtime) canonicalPlace(p NounPlace) NounPlace {
	p.Noun = r.CanonicalNoun(p.Noun)
	if len(p.Ancestors) > 0 {
		ancestors := make([]string, len(p.Ancestors))
		for i, a := range p.Ancestors {
			ancestors[i] = r.CanonicalNoun(a)
		}
		p.Ancestors = ancestors
	}
	return p
}

// NounWords reads the noun a command line's leading words name and returns
// it (canonical) with the words after it. The noun is the longest run of
// leading words that, joined with dots, names a noun or an alias of one:
// "ochazuke settings list" and "ochazuke.settings list" are ochazuke.settings, "settings
// list" is ochazuke.settings unless the application has a settings noun of its
// own, "app quit" is app. At least one word is left for the verb. ok is
// false when no leading run names a noun.
func (r *Runtime) NounWords(words []string) (noun string, rest []string, ok bool) {
	for n := len(words) - 1; n >= 1; n-- {
		name := r.CanonicalNoun(strings.Join(words[:n], "."))
		if r.Nouns[name] != nil {
			if n == 1 {
				r.NoteNounAlias(words[0]) // a deprecated alias: its one-time notice
			}
			return name, words[n:], true
		}
	}
	return "", words, false
}
