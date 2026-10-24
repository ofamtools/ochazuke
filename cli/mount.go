package cli

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/unixsuperhero/oden"
)

// Mount registers every noun of the shell's runtime as an inline command
// of app that runs the shell with the full noun name, so oden's prefix
// matching resolves nouns (BIN ta launch is task launch) and a noun wins
// over a same-named BIN-<noun> executable on PATH, which help lists as
// shadowed. It also adds the command catalog, which prints Catalog of the
// runtime under the shell's Bin and Version (after the nouns, so a noun
// named catalog wins). Mount leaves app.Default to the application.
func Mount(app *oden.App, sh *Shell) {
	verbs := map[string][]string{}
	for _, op := range sh.Runtime.Operations {
		verbs[op.Noun] = append(verbs[op.Noun], op.Verb)
	}
	nouns := make([]string, 0, len(verbs))
	for noun := range verbs {
		nouns = append(nouns, noun)
	}
	sort.Strings(nouns)
	for _, noun := range nouns {
		sort.Strings(verbs[noun])
		app.Add(oden.Command{
			Name:    noun,
			Summary: strings.Join(verbs[noun], " "),
			Source:  "noun",
			Run: func(ctx context.Context, rest []string) error {
				return sh.Run(ctx, append([]string{noun}, rest...))
			},
		})
	}
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
