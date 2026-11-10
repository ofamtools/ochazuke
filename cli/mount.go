package cli

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"sort"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// Mount maps the runtime registry to oden command groups and operation metadata.
// A dotted noun nests (ochazuke.settings is the group settings in the group ochazuke);
// a framework alias (settings) is a top-level alias too while no app command
// or noun of that name shadows it; a dotted first word (ochazuke.settings) is read as the
// groups it nests in. A namespace that is no noun (mcp of an extension's
// mcp.item) is the group NS. with NS as its alias, so an app command named
// NS owns the word whether it is added before or after Mount.
// Oden owns command resolution and help; the shell owns operation execution.
// The catalog command follows the nouns so a noun named catalog wins.
// extra also declares empty command groups before their operations are registered.
// Mount chains the application's Default after verb-first words
// (VerbFirstDefault); an application that sets Default later chains it itself.
func Mount(app *oden.App, sh *Shell, extra ...string) {
	sh.mountSetup(app)
	// The user's actions.json verbs are commands too, so oden's help and
	// dispatch know them.
	for _, problem := range sh.Runtime.EnsureUserActions() {
		fmt.Fprintln(sh.stderr(), "warning:", problem)
	}
	r := sh.Runtime
	// spelled: the leading words were spelled from a noun's name here (a
	// dotted word, an alias), so a namespace among them is its group NS.
	// even when an application command owns the word NS.
	spelled := false
	// A bare action alias (bin w!) is its noun's words and its verb. One
	// that shadows a verb reads by the word after it (ReadBareAlias: bin
	// set mode normal is bin mode set normal), and one followed by more
	// aliases of its action (bin verbose set opt?) is read by the shell
	// whole: a command of its own name runs the words as typed.
	if len(app.Args) > 0 && sh.bareAlias(app.Args[0]) {
		a, _ := r.ActionAlias(app.Args[0])
		reading, _, err := r.ReadBareAlias(r.Context(), app.Args, nil)
		switch {
		case err == nil && reading.Noun != "":
			noun := r.CanonicalNoun(reading.Noun)
			app.Args = append(append(strings.Split(noun, "."), reading.Alias.ShadowedVerb()), app.Args[2:]...)
			spelled = true
		case err != nil || a.ShadowedVerb() != "" || reading.Skip > 1:
			name := app.Args[0]
			app.Add(oden.Command{Name: name, Summary: "alias of " + r.DisplayKey(a.Action), Source: a.Alias,
				Run: func(ctx context.Context, args []string) error {
					return sh.Run(ctx, append([]string{name}, args...))
				}})
		default:
			app.Args = append(append(strings.Split(a.Noun, "."), a.Verb), app.Args[1:]...)
			spelled = true
		}
	}
	// A deprecated noun alias (an extension noun's old name, AddNounAlias
	// with Since or RemoveAfter) has no group, so help does not list it:
	// the word is read as its noun's groups, with its one-time notice.
	if len(app.Args) > 0 {
		if a, ok := r.DeprecatedNounAlias(app.Args[0]); ok {
			if text, ok := r.NounAliasNotice(a.Alias); ok {
				fmt.Fprintln(sh.stderr(), "notice:", text)
			}
			app.Args = deprecatedNounWords(r, a, app.Args[1:])
			spelled = true
		}
	}
	// A dotted noun is one word too (bin ochazuke.settings list): the word is
	// read as the groups it nests in before oden resolves it.
	if len(app.Args) > 0 && strings.Contains(app.Args[0], ".") {
		if noun := r.CanonicalNoun(app.Args[0]); r.Nouns[noun] != nil {
			app.Args = append(strings.Split(noun, "."), app.Args[1:]...)
			spelled = true
		}
	}
	// The v0.16.0 spelling of a framework noun (bin app settings list) is
	// read as ochazuke settings list for one release, unless app has a verb
	// of that name (CanonicalNoun).
	if len(app.Args) > 1 && app.Args[0] == ochazuke.AppNoun {
		if old := ochazuke.AppNoun + "." + app.Args[1]; r.Nouns[old] == nil {
			if noun := r.CanonicalNoun(old); noun != old && r.Nouns[noun] != nil {
				app.Args = append(strings.Split(noun, "."), app.Args[2:]...)
				spelled = true
			}
		}
	}
	// NOUN PROP [VALUE] (plan 2.6): a word after a noun that reads as one of
	// its props rather than a verb (Runtime.ReadNounWord, as the palette
	// reads it) is a command for this run only, by the prop's whole name,
	// so help still lists the verbs alone.
	// A noun's words with no action word, or only flags, run its default
	// action (Noun.DefaultAction: review --web is review page --web).
	app.Args = sh.defaultActionWords(app.Args)
	propNoun, prop := "", ""
	if noun, rest, ok := r.NounWords(app.Args); ok && len(rest) > 0 {
		if _, p, err := sh.nounWord(noun, rest[0]); err == nil && p != "" {
			propNoun, prop = noun, p
			head := app.Args[:len(app.Args)-len(rest)]
			app.Args = append(append(append([]string{}, head...), p), rest[1:]...)
		}
	}

	root := &nounGroup{}
	for _, noun := range extra {
		root.at(noun).noun = true
	}
	for noun := range r.Nouns {
		root.at(noun).noun = true
	}
	for _, op := range r.Catalog() {
		parameters := make([]oden.Parameter, 0, len(op.Parameters))
		for _, name := range op.ParameterOrder() {
			p := op.Parameters[name]
			parameters = append(parameters, oden.Parameter{
				Name: name, Type: p.Type, Description: p.Description,
				Required: p.Required, Choices: p.Enum, Source: r.ChoicesSource(p), Free: p.Free,
			})
		}
		details := verbDetails(op)
		noun, verb := op.Noun(), op.Name()
		// An alias on the same noun is an oden alias of the verb's command
		// (help lists it beside the name, once); an alias under another
		// noun is a command in that noun's group naming the action it
		// runs (project associate, alias of pane associate).
		var aliases []string
		for _, a := range commandAliases(r, op.Key.Short()) {
			// An alias with preset arguments is a command of its own, so
			// it runs by its name (and its args) and its presets are
			// not required words.
			if a.Noun == noun && len(a.Args) == 0 {
				aliases = append(aliases, a.Verb)
				continue
			}
			aliasParams := parameters
			if len(a.Args) > 0 {
				aliasParams = make([]oden.Parameter, len(parameters))
				for i, p := range parameters {
					if _, preset := a.Args[p.Name]; preset {
						p.Required = false
					}
					aliasParams[i] = p
				}
			}
			aliasNoun, aliasVerb := a.Noun, a.Verb
			ag := root.at(aliasNoun)
			ag.noun = true
			ag.verbs = append(ag.verbs, oden.Command{
				Name: aliasVerb, Summary: "alias of " + r.DisplayKey(op.Key.Short()) + ": " + op.Description, Source: a.Alias,
				Usage:      "[ARG...] [--target ID] [--json ...] [--ARG VALUE] [--format F] [--yes]",
				Parameters: aliasParams, Details: details + "\nAlias of: " + op.Key.Short() + presetDetails(a),
				Run: func(ctx context.Context, args []string) error {
					return sh.Run(ctx, append([]string{aliasNoun, aliasVerb}, args...))
				},
			})
		}
		if len(aliases) > 0 {
			details += "\nAliases: " + strings.Join(aliases, ", ")
		}
		g := root.at(noun)
		g.noun = true
		g.markStandard(verb, op)
		g.verbs = append(g.verbs, oden.Command{
			Name: verb, Aliases: aliases, Summary: op.Description, Source: op.Key.Short(),
			Usage:      "[ARG...] [--target ID] [--json ...] [--ARG VALUE] [--format F] [--yes]",
			Parameters: parameters, Details: details,
			Run: func(ctx context.Context, args []string) error {
				return sh.Run(ctx, append([]string{noun, verb}, args...))
			},
		})
	}
	if prop != "" {
		noun := propNoun
		g := root.at(noun)
		g.verbs = append(g.verbs, oden.Command{
			Name: prop, Summary: "the " + prop + " prop: read it, or write it with a value", Source: noun + "." + prop,
			Usage: "[VALUE] [--toggle] [--KEY KEY] [--target ID] [--format F] [--yes]",
			Run: func(ctx context.Context, args []string) error {
				return sh.Run(ctx, append([]string{noun, prop}, args...))
			},
		})
	}
	// Each dotted noun nests: ochazuke.settings is the group settings inside
	// the group ochazuke (bin ochazuke settings list).
	//
	// A namespace that is no noun (mcp of an extension's mcp.item) is the
	// group NS. with its word NS as an alias: an alias loses to a command
	// name in either registration order, so an application's own command
	// of that word (mtm mcp) owns it, as it owns a framework short name,
	// and NS.NOUN (read as NS. NOUN) still reaches the namespace's nouns.
	var topNames []string
	top := map[string]*nounGroup{}
	shortNames := map[string][]string{}
	words := map[string]bool{} // the namespace words taken as aliases
	for _, name := range root.names() {
		g, canonical := root.children[name], name
		if !g.noun {
			canonical = name + NamespaceSuffix
			shortNames[canonical] = []string{name}
			words[name] = true
		}
		topNames, top[canonical] = append(topNames, canonical), g
	}
	// A verb of a namespace's nouns after the namespace (mtm lane state) is
	// the verb on the noun named like the namespace, else the one the verb
	// belongs to (cli.verb-first).
	app.Args, spelled = sh.groupVerbFirst(top, app.Args, spelled)
	if spelled && len(app.Args) > 0 {
		if g := top[app.Args[0]+NamespaceSuffix]; g != nil && !g.noun {
			app.Args[0] += NamespaceSuffix
		}
	}
	// Use oden aliases, not commands named after aliases: a command name
	// wins over an alias in either registration order. Keep a canonical
	// top-level group too, so help identifies what each shortcut reaches.
	for _, a := range r.Aliases() {
		if a.Deprecated() {
			continue
		}
		if g := root.find(a.Noun); g != nil {
			if top[a.Noun] == nil {
				topNames, top[a.Noun] = append(topNames, a.Noun), g
			}
			if !a.Shadowed && top[a.Alias] == nil && !words[a.Alias] {
				shortNames[a.Noun] = append(shortNames[a.Noun], a.Alias)
			}
		}
	}
	// A noun's standard verbs are one line of its help (owner decision
	// 24, setting cli.standard-verbs): when these words ask for a group's
	// listing, that group lists its named verbs and the line names the
	// rest. Only the listing changes; every verb still runs.
	line := r.Settings().String(ochazuke.CLIStandardVerbsSetting) != ochazuke.CLIStandardVerbsList
	stdLine := ""
	if line {
		if g, words := listedGroup(top, topNames, shortNames, sh.listedArgs(app), line); g != nil && len(g.std) > 0 {
			g.listing = true
			stdLine = g.standardLine(strings.Join(append([]string{app.Name}, words...), " "))
		}
	}
	for _, name := range topNames {
		command := top[name].command(name, line)
		command.Aliases = shortNames[name]
		markNamespace(&command, name)
		sh.addGroup(app, command)
	}
	app.HelpText = `CLI execution:
  Each call uses a fresh local runtime, not an already-running TUI.
  The framework's nouns are ochazuke.<noun> (ochazuke settings list, ochazuke.settings list);
  a short name (settings list) reaches one unless the tool has a command or noun of its own.
  A namespace NS (an extension's nouns NS.NOUN) is the group NS. with NS as its alias:
  a command of the tool's own named NS owns the word, and NS.NOUN still reaches the noun.
  --target selects an instance; some operations require an existing instance.
  --yes (or -y) accepts confirmation; otherwise the CLI asks on stdin.
  Supply prompt values as arguments: non-confirmation UI prompts need the TUI.
  Bare arguments fill parameters in the order shown; named flags take precedence.
  Boolean parameters use named flags unless explicitly declared positional.
  Results are JSON; --format table|markdown|toon prints a list's rows as a
  table from the noun's columns (table cuts to the width on a terminal).
  Scheduled work settles before the command exits.
  Interaction verbs may require UI state and can have no lasting CLI effect.`
	if stdLine != "" {
		app.HelpText = stdLine + "\n\n" + app.HelpText
	}
	app.HelpBrief = briefHelp(app.Name, stdLine)
	app.Add(sh.shellCommands()...)
	// A word that names no command but is a verb of the tool's nouns runs
	// it (BIN add, and BIN state over the builtin when a noun has one):
	// oden's Claims and Default, over the tool's own (cli.verb-first).
	app.Args = sh.readerWords(app, topNames)
	app.Claims = sh.VerbFirstClaims(app)
	app.Default = sh.VerbFirstDefault(app, app.Default)
	sh.catalogOptions = append(sh.catalogOptions, func(doc *CatalogDoc) {
		doc.Commands = OdenCommands(app).Commands()
		for i := range doc.Commands {
			doc.Commands[i].Help = ""
		}
	})
}

// briefHelp is the CLI notes of default help; --all has the full text.
func briefHelp(bin, stdLine string) string {
	brief := `CLI execution: each call uses a fresh local runtime; results are JSON (--format table|markdown|toon for a list's rows).
  --target selects an instance; --yes (or -y) accepts confirmation.
  Run '` + bin + ` help --all' for every command and the full notes.`
	if stdLine != "" {
		brief = stdLine + "\n\n" + brief
	}
	return brief
}

// markNamespace says which namespace the top-level command name belongs to,
// from the name alone: a dotted noun (ochazuke.about) is a member of its
// first part, and the group NS. of a namespace that is no noun is that
// namespace's head. A part that starts with "__" is internal (a state or
// widget noun), hidden from default help. A name of one part is the tool's
// own and keeps no namespace (oden.Command.Namespace).
func markNamespace(c *oden.Command, name string) {
	if ns, ok := strings.CutSuffix(name, NamespaceSuffix); ok && !strings.Contains(ns, ".") {
		c.Namespace, c.NamespaceHead = ns, true
	} else if ns, _, ok := strings.Cut(name, "."); ok {
		c.Namespace = ns
	}
	for part := range strings.SplitSeq(name, ".") {
		if strings.HasPrefix(part, "__") {
			c.Hidden = true
		}
	}
}

// NamespaceSuffix ends the CLI group name of a namespace that is no noun
// (mcp. for an extension's mcp.item); the namespace's word is its alias,
// so an application command of that word owns it (Mount).
const NamespaceSuffix = "."

// shellCommands is the reserved command table. A real app noun wins; a
// framework short-name alias cannot hide an existing shell command.
func (sh *Shell) shellCommands() []oden.Command {
	catalog := oden.Command{
		Name:    "catalog",
		Summary: "print the nouns, verbs, bindings and pages as JSON; catalog inspect writes them as an HTML page",
		Source:  "cli",
		Usage:   "[inspect [--out FILE] [--open] [--bin PATH]... [--catalog FILE.json]... [--annotations FILE.json | --audit FILE.md]]",
		Details: "catalog prints the catalog JSON a host reads.\ncatalog inspect writes a self-contained HTML inspector of this tool's nouns, verbs,\neffective key rows, user actions and the framework nouns, plus any --bin tools'\nand --catalog files' catalogs, and prints its path (catalog inspect --help).",
		Run:     sh.catalogCommand,
	}
	return []oden.Command{catalog, {Name: "complete", Summary: "complete a CLI word using the current argument values", Usage: "-- NOUN VERB [ARG...] WORD", Source: "cli", Run: sh.completeCommand}, {Name: "completion", Summary: "print a bash or zsh completion script", Usage: "bash|zsh", Source: "cli", Run: sh.completionCommand}}
}

// version is the shell's Version, else the main module's build version.
func (s *Shell) version() string {
	if s.Version != "" {
		return s.Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return ""
}

// nounGroup is one level of the command tree Mount builds: a noun's verbs
// and the groups of the nouns nested under it (ochazuke.settings under ochazuke).
type nounGroup struct {
	noun     bool // a registered noun, not only a namespace
	verbs    []oden.Command
	children map[string]*nounGroup
	// std maps a verb to the standard verb it was generated from
	// (Operation.Standard); listing marks the group whose help these
	// words print, which then leaves its standard verbs out of the
	// listing (standardLine names them).
	std     map[string]string
	listing bool
	// unavailable marks the standard verbs that cannot run on the noun
	// (Operation.Unavailable): commands that say why, never listed.
	unavailable map[string]bool
}

// markStandard records verb as g's standard verb op.Standard, and as
// unavailable when op cannot run on its noun; a named verb is left out.
func (g *nounGroup) markStandard(verb string, op ochazuke.Operation) {
	if op.Standard == "" {
		return
	}
	if g.std == nil {
		g.std, g.unavailable = map[string]string{}, map[string]bool{}
	}
	g.std[verb] = op.Standard
	g.unavailable[verb] = op.Unavailable != ""
}

// at is the group of a dotted noun name, made as needed.
func (g *nounGroup) at(name string) *nounGroup {
	for _, part := range strings.Split(name, ".") {
		if g.children == nil {
			g.children = map[string]*nounGroup{}
		}
		child := g.children[part]
		if child == nil {
			child = &nounGroup{}
			g.children[part] = child
		}
		g = child
	}
	return g
}

// find is the group of a dotted noun name, nil when there is none.
func (g *nounGroup) find(name string) *nounGroup {
	for _, part := range strings.Split(name, ".") {
		if g = g.children[part]; g == nil {
			return nil
		}
	}
	return g
}

// names are the nested groups' names, sorted.
func (g *nounGroup) names() []string {
	names := make([]string, 0, len(g.children))
	for name := range g.children {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// command is the oden group of g named name: its verbs, then its nested
// groups. The summary lists the verbs, then the nested nouns; with line
// (cli.standard-verbs line) the named verbs come first and the standard
// ones follow after "· std", in the standard verb table's order. A
// listing group leaves its standard verbs out of its children.
func (g *nounGroup) command(name string, line bool) oden.Command {
	children := make([]oden.Command, 0, len(g.verbs)+len(g.children))
	summary := make([]string, 0, len(g.verbs)+len(g.children)+1)
	for _, verb := range g.verbs {
		switch {
		case g.unavailable[verb.Name] || line && g.std[verb.Name] != "":
			if g.listing {
				continue
			}
		default:
			summary = append(summary, verb.Name)
		}
		children = append(children, verb)
	}
	if std := g.standardVerbs(); line && len(std) > 0 {
		summary = append(summary, "· std")
		for _, verb := range std {
			summary = append(summary, verb.Name)
		}
	}
	nested := g.names()
	for _, child := range nested {
		nestedCommand := g.children[child].command(child, line)
		nestedCommand.Hidden = strings.HasPrefix(child, "__")
		children = append(children, nestedCommand)
	}
	switch {
	case len(nested) > 4:
		summary = append(summary, fmt.Sprintf("(+%d nouns)", len(nested)))
	case len(nested) > 0:
		summary = append(summary, "("+strings.Join(nested, " ")+")")
	}
	source := "noun"
	if !g.noun {
		source = "namespace"
	}
	return oden.Command{Name: name, Summary: strings.Join(summary, " "), Source: source, Children: children}
}

// standardVerbs are g's standard verbs that can run, in the standard
// verb table's order (ochazuke.StandardVerbs), a verb of an unknown row
// last.
func (g *nounGroup) standardVerbs() []oden.Command {
	var out []oden.Command
	for _, verb := range g.verbs {
		if g.std[verb.Name] != "" && !g.unavailable[verb.Name] {
			out = append(out, verb)
		}
	}
	order := ochazuke.StandardVerbs()
	rank := func(c oden.Command) int {
		if i := slices.Index(order, g.std[c.Name]); i >= 0 {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(out, func(a, b oden.Command) int { return rank(a) - rank(b) })
	return out
}

// standardLine is the help line naming g's standard verbs, each with its
// aliases in parentheses: "Standard verbs (bin task VERB --help): get,
// list, set (patch), ...".
func (g *nounGroup) standardLine(invocation string) string {
	std := g.standardVerbs()
	names := make([]string, len(std))
	for i, verb := range std {
		names[i] = verb.Name
		if len(verb.Aliases) > 0 {
			names[i] += " (" + strings.Join(verb.Aliases, ", ") + ")"
		}
	}
	return fmt.Sprintf("Standard verbs (%s VERB --help for one): %s", invocation, strings.Join(names, ", "))
}

// listedGroup is the group whose listing args ask for, and the words that
// named it: words naming groups (resolved as oden resolves them, prefixes
// included), then nothing or one help flag. Anything else (a verb, an
// unknown word, another flag) is a command run, not a listing: nil.
func listedGroup(top map[string]*nounGroup, names []string, aliases map[string][]string, args []string, line bool) (*nounGroup, []string) {
	var g *nounGroup
	var words []string
	level := make([]*oden.Command, 0, len(names))
	for _, name := range names {
		c := top[name].command(name, line)
		c.Aliases = aliases[name]
		level = append(level, &c)
	}
	for i, word := range args {
		if word == "-h" || word == "--help" {
			if i == len(args)-1 && g != nil {
				return g, words
			}
			return nil, nil
		}
		res := oden.NewCatalog("", level, nil, nil).Resolve(word)
		if !res.Resolved() || res.Entry.Command == nil || res.Entry.Command.Children == nil {
			return nil, nil
		}
		name := res.Entry.Command.Name
		if g == nil {
			g = top[name]
		} else {
			g = g.children[name]
		}
		if g == nil {
			return nil, nil
		}
		words = append(words, name)
		level = level[:0]
		for j := range res.Entry.Command.Children {
			level = append(level, &res.Entry.Command.Children[j])
		}
	}
	return g, words
}

// presetDetails is the help line naming an alias's preset arguments.
func presetDetails(a ochazuke.ActionAlias) string {
	if len(a.Args) == 0 {
		return ""
	}
	names := make([]string, 0, len(a.Args))
	for k := range a.Args {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = fmt.Sprintf("%s=%v", k, a.Args[k])
	}
	return "\nPresets: " + strings.Join(parts, " ")
}

// verbDetails is a verb command's details in help: its kind, its result,
// the confirmation it asks and why it is unavailable, each when it has one.
func verbDetails(op ochazuke.Operation) string {
	kind := op.Kind
	if kind == "" {
		kind = "interaction"
	}
	details := fmt.Sprintf("Kind: %s", kind)
	if op.Result != "" {
		details += "\nResult: " + op.Result
	}
	if op.Confirm != "" {
		details += "\nConfirmation: " + op.Confirm
	}
	if op.Unavailable != "" {
		details += "\nUnavailable: " + op.Unavailable
	}
	return details
}

// deprecatedNounWords are the words of a deprecated noun alias a followed
// by rest, read as its noun's: a renamed noun's verb whose name changed
// (RenameNoun: chooser open is picker into) keeps its old meaning.
func deprecatedNounWords(r *ochazuke.Runtime, a ochazuke.NounAlias, rest []string) []string {
	if len(rest) > 0 {
		if old, ok := r.ActionAlias(a.Alias + "." + rest[0]); ok {
			if op, ok := r.Operation(old.Action); ok {
				return append(append(strings.Split(op.Noun(), "."), op.Name()), rest[1:]...)
			}
		}
	}
	return append(strings.Split(a.Noun, "."), rest...)
}

// commandAliases are the aliases of key that get commands: not one that
// shadows a verb (its noun.verb is the verb it shadows, settings set: it
// runs by its bare word only), nor one spelled under a renamed noun's old
// name (no group of its own, so help never lists the old noun; the word
// is read by deprecatedNounWords).
func commandAliases(r *ochazuke.Runtime, key string) []ochazuke.ActionAlias {
	var out []ochazuke.ActionAlias
	for _, a := range r.AliasesOf(key) {
		if _, renamed := r.DeprecatedNounAlias(a.Noun); a.Shadows || renamed {
			continue
		}
		out = append(out, a)
	}
	return out
}

// defaultActionWords is args with a noun's default action put after the
// noun's words when no action word follows them (nothing, or only flags
// that are not help), as the reader reads them; else args.
func (sh *Shell) defaultActionWords(args []string) []string {
	// NounWords leaves a word for the verb: the "" after the words.
	noun, rest, ok := sh.Runtime.NounWords(append(slices.Clone(args), ""))
	rest = rest[:len(rest)-1]
	if !ok || len(rest) > 0 && (!strings.HasPrefix(rest[0], "-") || rest[0] == "-h" || rest[0] == "--help") {
		return args
	}
	n := sh.Runtime.Nouns[noun]
	if n == nil || n.DefaultAction == "" {
		return args
	}
	cs := sh.reading(n.Namespace(), func(m *ochazuke.Noun) bool { return m == n }).Read(append([]string{noun}, rest...))
	if len(cs) == 0 || cs[0].NounOnly() || cs[0].Action.Via != ochazuke.ViaOmitted {
		return args
	}
	return longWords(cs[0])
}

// addGroup adds a top-level noun group, merged into an application command
// of its name (mergeInto). A framework noun reached by its short name (list
// for ochazuke.collection) yields (oden's Command.Yield): when the tool's own
// nouns read the words, the reader's reading runs (o-lane list is lane
// list), through Claims.
func (sh *Shell) addGroup(app *oden.App, group oden.Command) {
	if n := sh.Runtime.Nouns[group.Name]; n != nil && len(group.Aliases) > 0 {
		group.Yield = n.Namespace() == ochazuke.FrameworkNamespace
	}
	if !sh.mergeInto(app, group) {
		app.Add(group)
	}
}

// mergeInto files a noun group's verbs under an application command of the
// same name, when the application added one before Mount (o-questions'
// review): a command with children gains the verbs it lacks; a command
// with none hides the noun, so Mount says so on stderr. It reports
// whether such a command was there.
func (sh *Shell) mergeInto(app *oden.App, group oden.Command) bool {
	e := app.Catalog().Lookup(group.Name)
	if e == nil || e.Kind != oden.KindInline || e.Command == nil || odenBuiltin(e) {
		return false
	}
	if e.Command.Children == nil {
		fmt.Fprintf(sh.stderr(), "warning: %s: the command %q hides the noun %s and its verbs (%s); rename the command, or give it children and Mount adds the noun's verbs\n", app.Name, group.Name, group.Name, group.Summary)
		return true
	}
	for _, verb := range group.Children {
		if !slices.ContainsFunc(e.Command.Children, func(c oden.Command) bool { return c.Name == verb.Name }) {
			e.Command.Children = append(e.Command.Children, verb)
		}
	}
	return true
}

// odenBuiltin reports whether e is one of oden's own builtins (help,
// state, ...), which a noun group owns its name over: the entry a bare
// app with builtins has under that name, with the same summary.
func odenBuiltin(e *oden.Entry) bool {
	b := oden.New("probe", nil, oden.WithBuiltins()).Catalog().Lookup(e.Name)
	return b != nil && b.Summary == e.Summary
}

// readerWords is the argv to run: when its first word names no command
// exactly (q w up, a prefix), the long spelling of the one operation the
// reader reads the words as, as the palette reads them; else argv as it is,
// for oden to resolve (an exact name, a prefix the reader reads as no one
// operation, a prefix of a command that is none of Mount's noun groups:
// an application command or an external bin, which the reader does not
// know).
func (sh *Shell) readerWords(app *oden.App, groups []string) []string {
	if len(app.Args) == 0 || app.Catalog().Lookup(app.Args[0]) != nil || !onlyGroups(app.Catalog().Resolve(app.Args[0]).Candidates, groups) {
		return app.Args
	}
	long, err := sh.verbFirstWords(app.Args, sh.binScope())
	switch {
	case err == nil:
		return long
	case !errors.Is(err, oden.ErrNotFound):
		// Several of the tool's operations fit: the word is, for this run,
		// a command that says so (as a NOUN PROP word is a command for
		// one run), before oden reads it as a prefix of some other command.
		app.Add(oden.Command{Name: app.Args[0], Summary: "ambiguous here", Source: "reader", Run: func(context.Context, []string) error {
			fmt.Fprintf(app.Stderr, "%s: %v\n", app.Name, err)
			return &oden.ExitError{Code: 1}
		}})
	}
	return app.Args
}

// onlyGroups reports whether every entry is one of Mount's top-level noun
// groups.
func onlyGroups(entries []*oden.Entry, groups []string) bool {
	return !slices.ContainsFunc(entries, func(e *oden.Entry) bool { return !slices.Contains(groups, e.Name) })
}
