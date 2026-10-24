package cli

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/oden"
)

// Mount registers the application nouns of the shell's runtime (a noun
// with at least one KindApplication verb) as inline commands of app that
// run the shell with the full noun name, so oden's prefix matching
// resolves nouns (BIN ta launch is task launch) and a noun wins over a
// same-named BIN-<noun> executable on PATH, which help lists as shadowed.
// The framework's own nouns (modal, mode, input, ...) are not mounted, so
// their names neither crowd help nor make an application noun's prefix
// ambiguous; they stay reachable as BIN NOUN VERB through an app.Default
// that runs the shell. extra names framework nouns to mount anyway (mtm
// mounts bindings and history so bind li resolves). It also adds the
// command catalog, which prints Catalog of the runtime under the shell's
// Bin and Version (after the nouns, so a noun named catalog wins). Mount
// leaves app.Default to the application.
func Mount(app *oden.App, sh *Shell, extra ...string) {
	verbs := map[string][]string{}
	mounted := map[string]bool{}
	for _, noun := range extra {
		mounted[noun] = true
	}
	for _, op := range sh.Runtime.Operations {
		verbs[op.Noun] = append(verbs[op.Noun], op.Verb)
		if op.Kind == ochazuke.KindApplication {
			mounted[op.Noun] = true
		}
	}
	for noun := range verbs {
		if !mounted[noun] {
			delete(verbs, noun)
		}
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
