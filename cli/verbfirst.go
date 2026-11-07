package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// Verb-first words: a tool's argv may start with a verb alone (BIN add,
// BIN state) or, inside an extension's namespace group, with a verb of the
// namespace's nouns (mtm lane state). Runtime.ReadPair reads the words the
// way the palette reads a verb then a noun; the result is the same argv the
// long spelling (BIN NOUN VERB ...) would be, so flags, positionals and
// help parse exactly as there. Setting cli.verb-first (default on) turns
// it off.

// verbFirstWords is argv rewritten to the long spelling when its first
// words are a verb of the nouns scope allows, else ok is false (and err
// says why when the words did name verbs). A leading flag is never a verb.
func (s *Shell) verbFirstWords(words []string, scope ochazuke.PairScope) (rewritten []string, err error) {
	if len(words) == 0 || strings.HasPrefix(words[0], "-") || !s.Runtime.Settings().Bool(ochazuke.CLIVerbFirstSetting) {
		return nil, oden.ErrNotFound
	}
	pair, err := s.Runtime.ReadPair(words, scope)
	if err != nil {
		return nil, err
	}
	out := strings.Split(pair.Noun, ".")
	out = append(out, pair.Verb)
	return append(out, words[pair.Skip:]...), nil
}

// ownNoun is the PairScope.Prefer of a tool or namespace named ns: the
// noun called ns (lane), or ns.ns (lane.lane).
func ownNoun(ns string) func(string) bool {
	return func(noun string) bool { return noun == ns || noun == ns+"."+ns }
}

// binScope is the scope of the tool's own argv: every noun, the one named
// like the tool's namespace winning a tie. A bin whose name is no
// namespace (ExtensionNamespace errs) prefers none.
func binScope(app *oden.App) ochazuke.PairScope {
	ns, err := OwnNamespace(app.Name)
	if err != nil {
		return ochazuke.PairScope{}
	}
	return ochazuke.PairScope{Prefer: ownNoun(ns)}
}

// VerbFirstClaims is oden's App.Claims for a tool: true when argv is a
// verb-first spelling (BIN state with a noun that has the verb), so a
// machine builtin of that name gives way to the noun's verb.
func (s *Shell) VerbFirstClaims(app *oden.App) func(args []string) bool {
	return func(args []string) bool {
		_, err := s.verbFirstWords(args, binScope(app))
		return err == nil
	}
}

// VerbFirstDefault is an oden Default that runs a verb-first spelling and
// hands every other word to next (the tool's own Default; nil reports an
// unknown word as oden does). Mount installs it over app.Default; a tool
// that sets Default afterwards chains it by hand:
//
//	app.Default = sh.VerbFirstDefault(app, mine)
func (s *Shell) VerbFirstDefault(app *oden.App, next func(context.Context, []string) error) func(context.Context, []string) error {
	running := false
	return func(ctx context.Context, args []string) error {
		if !running {
			rewritten, err := s.verbFirstWords(args, binScope(app))
			switch {
			case err == nil:
				// Run again as the long spelling: oden resolves, parses
				// and prints help as it does for it.
				running, app.Args = true, rewritten
				defer func() { running = false }()
				return app.Run(ctx)
			case !errors.Is(err, oden.ErrNotFound) && len(args) > 0 && strings.Contains(err.Error(), "ambiguous"):
				fmt.Fprintf(app.Stderr, "%s: %v\n", app.Name, err)
				return &oden.ExitError{Code: 1}
			}
		}
		switch {
		case next != nil:
			return next(ctx, args)
		case len(args) == 0:
			fmt.Fprintf(app.Stderr, "usage: %s COMMAND [ARGS...]\n\ncommands:\n", app.Name)
			app.List(app.Stderr, app.Catalog().Entries)
			return &oden.ExitError{Code: 1}
		}
		return oden.ErrNotFound
	}
}

// groupVerbFirst rewrites argv whose first word is a namespace group
// (lane of an extension's lane.* nouns) and whose second is a verb of the
// group's nouns that names no child of the group: mtm lane state is mtm
// lane lane state. top maps each group's name to its group.
func (s *Shell) groupVerbFirst(top map[string]*nounGroup, args []string, spelled bool) ([]string, bool) {
	if rewritten, ok := s.groupVerbFirstWords(top, args); ok {
		return rewritten, true
	}
	return args, spelled
}

func (s *Shell) groupVerbFirstWords(top map[string]*nounGroup, args []string) ([]string, bool) {
	if len(args) < 2 || strings.HasPrefix(args[1], "-") {
		return nil, false
	}
	ns := strings.TrimSuffix(args[0], NamespaceSuffix)
	g := top[ns+NamespaceSuffix]
	if g == nil || g.noun {
		return nil, false
	}
	if g.hasChildLike(args[1]) {
		return nil, false
	}
	prefix := ns + "."
	rewritten, err := s.verbFirstWords(args[1:], ochazuke.PairScope{
		Within: func(noun string) bool { return strings.HasPrefix(noun, prefix) },
		Prefer: ownNoun(ns),
	})
	if err != nil {
		return nil, false
	}
	return rewritten, true
}

// hasChildLike reports whether word names, or is a prefix of, a verb or a
// nested group of g: a word that already means something there.
func (g *nounGroup) hasChildLike(word string) bool {
	for _, v := range g.verbs {
		if strings.HasPrefix(v.Name, word) {
			return true
		}
		for _, a := range v.Aliases {
			if strings.HasPrefix(a, word) {
				return true
			}
		}
	}
	for name := range g.children {
		if strings.HasPrefix(name, word) {
			return true
		}
	}
	return false
}

// listedArgs are the words that may ask for a group's listing: none when
// they are a verb-first spelling of a machine builtin's name.
func (s *Shell) listedArgs(app *oden.App) []string {
	if s.verbFirstYields(app) {
		return nil
	}
	return app.Args
}

// verbFirstYields reports whether the tool's argv is a machine builtin's
// name (state) that VerbFirstClaims takes for a noun's verb: its words
// are then a verb, never a group's listing.
func (s *Shell) verbFirstYields(app *oden.App) bool {
	if len(app.Args) == 0 {
		return false
	}
	res := app.Catalog().Resolve(app.Args[0])
	return res.Resolved() && res.Exact && res.Entry.Kind == oden.KindInline && res.Entry.Command != nil &&
		res.Entry.Command.Yield && s.VerbFirstClaims(app)(app.Args)
}
