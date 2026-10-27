package cli

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/unixsuperhero/oden"
)

// Mount maps the runtime registry to oden command groups and operation metadata.
// A dotted noun nests (app.settings is the group settings in the group app);
// a framework alias (settings) is a top-level group too while no noun of
// that name shadows it; a dotted first word (app.settings) is read as the
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
	// A dotted noun is one word too (bin app.settings list): the word is
	// read as the groups it nests in before oden resolves it.
	if len(app.Args) > 0 && strings.Contains(app.Args[0], ".") {
		if noun := r.CanonicalNoun(app.Args[0]); r.Nouns[noun] != nil {
			app.Args = append(strings.Split(noun, "."), app.Args[1:]...)
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
		g := root.at(noun)
		g.noun = true
		g.verbs = append(g.verbs, oden.Command{
			Name: verb, Summary: op.Description, Source: op.Key(),
			Usage:      "[ARG...] [--target ID] [--json ...] [--ARG VALUE] [--format F] [--yes]",
			Parameters: parameters, Details: details,
			Run: func(ctx context.Context, args []string) error {
				return sh.Run(ctx, append([]string{noun, verb}, args...))
			},
		})
	}
	// Each dotted noun nests: app.settings is the group settings inside
	// the group app (bin app settings list).
	top := map[string]bool{}
	for _, name := range root.names() {
		top[name] = true
		app.Add(root.children[name].command(name))
	}
	// A short name (settings for app.settings) is a group of its own too
	// while no noun of that name shadows it, so bin settings list works.
	for _, a := range r.Aliases() {
		if a.Shadowed || top[a.Alias] {
			continue
		}
		if g := root.find(a.Noun); g != nil {
			top[a.Alias] = true
			app.Add(g.command(a.Alias))
		}
	}
	app.HelpText = `CLI execution:
  Each call uses a fresh local runtime, not an already-running TUI.
  The framework's nouns are app.<noun> (app settings list, app.settings list);
  a short name (settings list) reaches one unless the tool has a noun of its own.
  --target selects an instance; some operations require an existing instance.
  --yes accepts confirmation; otherwise the CLI asks on stdin.
  Supply prompt values as arguments: non-confirmation UI prompts need the TUI.
  Bare arguments fill parameters in the order shown; named flags take precedence.
  Boolean parameters use named flags unless explicitly declared positional.
  Results are JSON; --format table|markdown|toon prints a list's rows as a
  table from the noun's columns (table cuts to the width on a terminal).
  Scheduled work settles before the command exits.
  Interaction verbs may require UI state and can have no lasting CLI effect.`
	app.Add(oden.Command{
		Name:    "catalog",
		Summary: "print the nouns, verbs, bindings and pages as JSON",
		Source:  "cli",
		Run: func(context.Context, []string) error {
			data, err := catalog(sh.Runtime, sh.Bin, sh.version(), !sh.StateCache.Off)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(sh.stdout(), "%s\n", data)
			return err
		},
	})
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
// and the groups of the nouns nested under it (app.settings under app).
type nounGroup struct {
	noun     bool // a registered noun, not only a namespace
	verbs    []oden.Command
	children map[string]*nounGroup
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
// groups. The summary lists the verbs, then the nested nouns.
func (g *nounGroup) command(name string) oden.Command {
	children := append([]oden.Command{}, g.verbs...)
	summary := make([]string, 0, len(g.verbs)+len(g.children))
	for _, verb := range g.verbs {
		summary = append(summary, verb.Name)
	}
	nested := g.names()
	for _, child := range nested {
		children = append(children, g.children[child].command(child))
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
