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
	groups := map[string][]oden.Command{}
	for _, noun := range extra {
		groups[noun] = []oden.Command{}
	}
	for noun := range sh.Runtime.Nouns {
		groups[noun] = []oden.Command{}
	}
	for _, op := range sh.Runtime.Catalog() {
		parameters := make([]oden.Parameter, 0, len(op.Parameters))
		for _, name := range op.ParameterOrder() {
			p := op.Parameters[name]
			parameters = append(parameters, oden.Parameter{
				Name: name, Type: p.Type, Description: p.Description,
				Required: p.Required, Choices: p.Enum, Source: p.Source, Free: p.Free,
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
		groups[noun] = append(groups[noun], oden.Command{
			Name: verb, Summary: op.Description, Source: op.Key(),
			Usage:      "[ARG...] [--target ID] [--json ...] [--ARG VALUE] [--format F] [--yes]",
			Parameters: parameters, Details: details,
			Run: func(ctx context.Context, args []string) error {
				return sh.Run(ctx, append([]string{noun, verb}, args...))
			},
		})
	}
	nouns := make([]string, 0, len(groups))
	for noun := range groups {
		nouns = append(nouns, noun)
	}
	sort.Strings(nouns)
	for _, noun := range nouns {
		verbs := make([]string, 0, len(groups[noun]))
		for _, command := range groups[noun] {
			verbs = append(verbs, command.Name)
		}
		app.Add(oden.Command{
			Name: noun, Summary: strings.Join(verbs, " "),
			Source: "noun", Children: groups[noun],
		})
	}
	app.HelpText = `CLI execution:
  Each call uses a fresh local runtime, not an already-running TUI.
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
			data, err := Catalog(sh.Runtime, sh.Bin, sh.version())
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
