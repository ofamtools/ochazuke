package cli

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"sort"
	"strings"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/oden"
)

// Mount maps the runtime registry to oden command groups and operation metadata.
// A dotted noun nests (ochazuke.settings is the group settings in the group ochazuke);
// a framework alias (settings) is a top-level group too while no noun of
// that name shadows it; a dotted first word (ochazuke.settings) is read as the
// groups it nests in.
// Oden owns command resolution and help; the shell owns operation execution.
// The catalog command follows the nouns so a noun named catalog wins.
// extra also declares empty command groups before their operations are registered.
// Mount leaves app.Default to the application.
func Mount(app *oden.App, sh *Shell, extra ...string) {
	// The user's actions.json verbs are commands too, so oden's help and
	// dispatch know them.
	for _, problem := range sh.Runtime.EnsureUserActions() {
		fmt.Fprintln(sh.stderr(), "warning:", problem)
	}
	r := sh.Runtime
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
			app.Args = append(append(strings.Split(noun, "."), a.Verb), app.Args[2:]...)
		case err != nil || a.Shadows || reading.Skip > 1:
			name := app.Args[0]
			app.Add(oden.Command{Name: name, Summary: "alias of " + r.DisplayKey(a.Action), Source: a.Alias,
				Run: func(ctx context.Context, args []string) error {
					return sh.Run(ctx, append([]string{name}, args...))
				}})
		default:
			app.Args = append(append(strings.Split(a.Noun, "."), a.Verb), app.Args[1:]...)
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
			app.Args = append(strings.Split(a.Noun, "."), app.Args[1:]...)
		}
	}
	// A dotted noun is one word too (bin ochazuke.settings list): the word is
	// read as the groups it nests in before oden resolves it.
	if len(app.Args) > 0 && strings.Contains(app.Args[0], ".") {
		if noun := r.CanonicalNoun(app.Args[0]); r.Nouns[noun] != nil {
			app.Args = append(strings.Split(noun, "."), app.Args[1:]...)
		}
	}
	// The v0.16.0 spelling of a framework noun (bin app settings list) is
	// read as ochazuke settings list for one release, unless app has a verb
	// of that name (CanonicalNoun).
	if len(app.Args) > 1 && app.Args[0] == ochazuke.AppNoun {
		if old := ochazuke.AppNoun + "." + app.Args[1]; r.Nouns[old] == nil {
			if noun := r.CanonicalNoun(old); noun != old && r.Nouns[noun] != nil {
				app.Args = append(strings.Split(noun, "."), app.Args[2:]...)
			}
		}
	}
	// NOUN PROP [VALUE] (plan 2.6): a word after a noun that reads as one of
	// its props rather than a verb (Runtime.ReadNounWord, as the palette
	// reads it) is a command for this run only, by the prop's whole name,
	// so help still lists the verbs alone.
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
		noun, verb := op.Noun, op.Verb
		// An alias on the same noun is an oden alias of the verb's command
		// (help lists it beside the name, once); an alias under another
		// noun is a command in that noun's group naming the action it
		// runs (project associate, alias of pane associate).
		var aliases []string
		for _, a := range r.AliasesOf(op.Key()) {
			if a.Shadows {
				// Its noun.verb is the verb it shadows (settings set):
				// it runs by its bare word only (above).
				continue
			}
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
				Name: aliasVerb, Summary: "alias of " + r.DisplayKey(op.Key()) + ": " + op.Description, Source: a.Alias,
				Usage:      "[ARG...] [--target ID] [--json ...] [--ARG VALUE] [--format F] [--yes]",
				Parameters: aliasParams, Details: details + "\nAlias of: " + op.Key() + presetDetails(a),
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
		if op.Standard != "" {
			if g.std == nil {
				g.std = map[string]string{}
			}
			g.std[verb] = op.Standard
		}
		g.verbs = append(g.verbs, oden.Command{
			Name: verb, Aliases: aliases, Summary: op.Description, Source: op.Key(),
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
	var topNames []string
	top := map[string]*nounGroup{}
	for _, name := range root.names() {
		topNames, top[name] = append(topNames, name), root.children[name]
	}
	// A short name (settings for ochazuke.settings, doc for a mounted
	// json.doc) is a group of its own too while no noun of that name
	// shadows it, so bin settings list works; a deprecated one is not
	// listed (it is read above).
	reserved := map[string]bool{}
	for _, command := range sh.shellCommands() {
		reserved[command.Name] = true
	}
	for _, a := range r.Aliases() {
		if reserved[a.Alias] {
			continue
		}
		if a.Shadowed || a.Deprecated() || top[a.Alias] != nil {
			continue
		}
		if g := root.find(a.Noun); g != nil {
			topNames, top[a.Alias] = append(topNames, a.Alias), g
		}
	}
	// A noun's standard verbs are one line of its help (owner decision
	// 24, setting cli.standard-verbs): when these words ask for a group's
	// listing, that group lists its named verbs and the line names the
	// rest. Only the listing changes; every verb still runs.
	line := r.Settings().String(ochazuke.CLIStandardVerbsSetting) != ochazuke.CLIStandardVerbsList
	stdLine := ""
	if line {
		if g, words := listedGroup(top, topNames, app.Args, line); g != nil && len(g.std) > 0 {
			g.listing = true
			stdLine = g.standardLine(strings.Join(append([]string{app.Name}, words...), " "))
		}
	}
	for _, name := range topNames {
		app.Add(top[name].command(name, line))
	}
	app.HelpText = `CLI execution:
  Each call uses a fresh local runtime, not an already-running TUI.
  The framework's nouns are ochazuke.<noun> (ochazuke settings list, ochazuke.settings list);
  a short name (settings list) reaches one unless the tool has a noun of its own.
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
	app.Add(sh.shellCommands()...)
}

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
	children := []oden.Command{}
	for _, name := range []string{"serve", "tools", "instructions", "config"} {
		name := name
		children = append(children, oden.Command{Name: name, Summary: "MCP " + name, Run: func(ctx context.Context, args []string) error {
			return sh.Run(ctx, append([]string{"mcp", name}, args...))
		}})
	}
	return []oden.Command{catalog, {Name: "mcp", Summary: "Catalog MCP server", Children: children}}
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
		if line && g.std[verb.Name] != "" {
			if g.listing {
				continue
			}
		} else {
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
		children = append(children, g.children[child].command(child, line))
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

// standardVerbs are g's standard verbs in the standard verb table's
// order (ochazuke.StandardVerbs), a verb of an unknown row last.
func (g *nounGroup) standardVerbs() []oden.Command {
	var out []oden.Command
	for _, verb := range g.verbs {
		if g.std[verb.Name] != "" {
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
func listedGroup(top map[string]*nounGroup, names []string, args []string, line bool) (*nounGroup, []string) {
	var g *nounGroup
	var words []string
	level := make([]*oden.Command, 0, len(names))
	for _, name := range names {
		c := top[name].command(name, line)
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
