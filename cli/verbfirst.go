package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// Verb-first words: a tool's argv may start with an action alone (BIN
// add, BIN state) or, inside an extension's namespace group, with an action
// of the namespace's nouns (mtm lane state). The reader
// (ochazuke.Reading) reads the words as the palette does, both orders,
// over the tool's own operations (the framework's, Operation.Builtin,
// aside: a tool's argv names its own nouns); a word that leaves the noun
// out reads as the namespace's main noun (Noun.Main) first. The result is
// the same argv the long spelling (BIN NOUN VERB ...) would be, so flags,
// positionals and help parse exactly as there. Setting cli.verb-first
// (default on) turns it off.

// reading is the CLI's reader in namespace: the operations within keeps
// (nil: all), the framework's aside, the rest of the argv binding when the
// shell parses it as the operation's flags, the namespace's main noun
// first.
func (s *Shell) reading(namespace string, within func(*ochazuke.Noun) bool) ochazuke.Reading {
	rd := s.Runtime.ReadingWithin(func(c ochazuke.Candidate) bool {
		return !c.Operation.Builtin && c.Operation.Unavailable == "" && (within == nil || within(c.Noun.Noun))
	})
	rd.Bind = s.binds
	rd.Prefer = ochazuke.MainNoun(namespace)
	return rd
}

// binds is how far the rest of the argv binds on a candidate: all of it
// when the shell reads it as the operation's flags and arguments, else
// none.
func (s *Shell) binds(c ochazuke.Candidate) int {
	if _, _, err := s.request(c.Operation.Noun(), c.Operation.Name(), c.Rest); err != nil {
		return 0
	}
	return 1
}

// verbFirstWords is argv rewritten to the long spelling when the reader
// reads its leading words as one operation of the scope, else err says
// why: oden.ErrNotFound when they name nothing, the ambiguity listing the
// noun verb pairs when several fit. A leading flag is never a verb.
func (s *Shell) verbFirstWords(words []string, rd ochazuke.Reading) (rewritten []string, err error) {
	if len(words) == 0 || strings.HasPrefix(words[0], "-") || !s.Runtime.Settings().Bool(ochazuke.CLIVerbFirstSetting) {
		return nil, oden.ErrNotFound
	}
	cs := slices.DeleteFunc(rd.Read(words), ochazuke.Candidate.NounOnly)
	switch {
	case len(cs) == 0:
		return nil, oden.ErrNotFound
	case slices.ContainsFunc(cs, func(c ochazuke.Candidate) bool { return c.Operation != cs[0].Operation }):
		return nil, s.Runtime.AmbiguityOf(cs)
	}
	return longWords(cs[0]), nil
}

// longWords is the long spelling of a candidate: its operation's noun
// words and name (an alias runs as the action it names), then the rest.
func longWords(c ochazuke.Candidate) []string {
	out := append(strings.Split(c.Operation.Noun(), "."), c.Operation.Name())
	return append(out, c.Rest...)
}

// binScope is the reading of the tool's own argv: every noun, the main
// noun of the program's namespace first.
func (s *Shell) binScope() ochazuke.Reading {
	return s.reading(ochazuke.DefaultNamespace, nil)
}

// VerbFirstClaims is oden's App.Claims for a tool: true when argv is a
// verb-first spelling (BIN state with a noun that has the verb), so a
// machine builtin of that name gives way to the noun's verb.
func (s *Shell) VerbFirstClaims(app *oden.App) func(args []string) bool {
	return func(args []string) bool {
		_, err := s.verbFirstWords(args, s.binScope())
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
			rewritten, err := s.verbFirstWords(args, s.binScope())
			switch {
			case err == nil:
				// Run again as the long spelling: oden resolves, parses
				// and prints help as it does for it.
				running, app.Args = true, rewritten
				defer func() { running = false }()
				return app.Run(ctx)
			case !errors.Is(err, oden.ErrNotFound):
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
	rewritten, err := s.verbFirstWords(args[1:], s.reading(ns, func(n *ochazuke.Noun) bool { return n.Namespace() == ns }))
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
