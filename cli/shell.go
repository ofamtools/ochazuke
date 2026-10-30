// Package cli is the generic command line of an ochazuke application: the
// NOUN VERB shell over a runtime (Shell), the oden mount that makes every
// noun a command of the bin (Mount) and the catalog answer that describes
// the runtime as JSON (Catalog). It knows no noun, verb or key; the
// application passes in its bin name, usage line and the hooks that reach
// its own state.
//
// The catalog is data for a host that mounts the bin: each noun carries its
// pin (a pinned noun is modal-only, so a host can skip it without naming
// it) and each verb whose result lists rows carries rows, the noun those
// rows belong to (declared with NounBuilder.Rows). A parameter's source
// (a pick list's noun.verb) names an operation of the producing runtime,
// kept verbatim: a host has no such operation of its own, so it runs the
// source through the extension's bin (BIN NOUN VERB --json '{}') and reads
// the choices from its JSON answer.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// Shell runs BIN NOUN VERB [ARG...] [--target ID] [--json '{...}'] [--ARG VALUE]
// [--format F] [--yes]: one registered operation in this process through the runtime's
// CLI entry (Runtime.HandleCall, recorded with the history source "cli"),
// the same registry the keys and the palette dispatch into. A command's
// words are the operation key, its --ARG flags are typed by the operation's
// own parameters, and everything else is the framework's. A VERB may be a
// unique prefix of one of the noun's verbs (a noun's prefix is resolved by
// the oden shell, see Mount). Bare words after NOUN VERB (not starting with
// --) are positional arguments: they fill the parameters no --ARG or --json
// set (a bare --json, with no value after it, asks for the JSON output the
// shell always prints and is accepted and ignored), in Operation.ParameterOrder (required first, then by name), typed
// like flags; more words than parameters is an error naming them. Named
// flags win wherever they appear.
//
// After the operation it settles the work the operation scheduled (Settle:
// the pending commands, delivered through Runtime.Deliver as the Bubble Tea
// loop would), so an application verb's job finishes before the process
// exits, and prints the job's delivered value instead of its ticket (the
// values in job order when the command started several). A failed job is
// the command's error (a non-zero exit) and nothing is printed. Each job
// the command started is reported on Stderr as "job #N noun.verb STATE"
// ("job #N noun.verb LABEL STATE" for a labelled job),
// except a job of a quiet verb (Operation.Quiet, as the jobs ticker hides
// it) that did not fail: a failure always prints. A confirm modal the
// operation opened is answered: --yes (or -y) runs
// confirm.accept, otherwise the prompt is printed and a line read from
// Stdin (y or yes accepts; anything else, or end of input, declines with
// modal.pop and a non-zero exit). Any other modal (a prompt, the palette)
// needs the TUI and fails, naming the arguments to pass instead.
//
// A command without --target has no focus to act from, so the shell names
// the owner: Owners[noun] when that instance exists (an application
// declares it for a noun with no instance of its own), else the one
// instance of the noun when exactly one exists. With several the command
// runs as given and, if it fails, the error lists them to pass as --target;
// with none it runs as given.
//
// The result prints as JSON unless --format names another of Formats
// (json, table, markdown, toon; the grammars are on Formats). A tabular
// result (an array of objects: a list verb's rows, a state verb's Lines of
// Row, bindings list) prints one table per noun, in the columns the noun
// declares over the rows' cells, else the records' fields; table cuts to
// the terminal's width and drops columns by Priority only when Stdout is a
// terminal (Width), markdown and toon never cut. A result that is not
// tabular prints as JSON with a note on Stderr. --format combines with
// --json '{...}' (the arguments); a bare --json asks for json and refuses
// another --format; an operation declaring its own format parameter takes
// --format as that argument and prints JSON.
//
// What handlers tell the user while the command runs (ctx.Notify,
// ctx.Error, ctx.Info, Runtime.UI, a job's Then included) prints on Stderr
// as "notice: TEXT", "error: TEXT" and "status: TEXT" lines beside the
// "warning:" lines; the
// shell is the runtime's UI sink until Run returns (messages posted before
// any sink attached print first).
//
// Errors are returned for oden.Report: every failure is exit status 1
// (oden.ExitCode of a plain error).
type Shell struct {
	Runtime *ochazuke.Runtime
	Bin     string // the bin name, in messages and the catalog
	Usage   string // the usage line printed for a short command
	Version string // the catalog's version; "" is the main module's build version
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	// Prepare, when set, runs before every command (an application
	// starts a daemon its verbs need); its error ends the command.
	Prepare func(ctx context.Context) error
	// Reveal, when set, makes an instance exist for a --target no list
	// shows yet (the application loads the rows that hold it); the shell
	// settles what it scheduled and resolves the target again. A target
	// the runtime already resolves (an instance, or a foreign id
	// Runtime.ResolveTarget maps to one) skips it; after it, ExecuteAt
	// consults ResolveTarget again, so the two compose: Reveal loads the
	// rows, the resolver maps the foreign id to the row that holds it.
	Reveal func(target string) error
	// Owners maps a noun with no instance of its own to the instance its
	// verbs act for; a command of that noun without --target targets it.
	Owners map[string]string
	// Settle, when set, runs before each settle round (an application
	// without a screen removes the terminals an operation attached); its
	// errors join the command's.
	Settle func() error
	// StateCache is where the state verb's answers are kept for deltas
	// (--since, StateDeltaVersion); the zero value caches under
	// $XDG_CACHE_HOME/<Bin>/state.
	StateCache StateCache
	// Width is the width --format table cuts to: 0 (the default) is the
	// terminal's width when Stdout is a terminal and no cut otherwise; a
	// negative Width never cuts.
	Width int

	in *bufio.Reader
}

// New is a Shell over r for bin with the standard usage line.
func New(r *ochazuke.Runtime, bin string, stdin io.Reader, stdout, stderr io.Writer) *Shell {
	return &Shell{Runtime: r, Bin: bin, Usage: DefaultUsage(bin), Stdin: stdin, Stdout: stdout, Stderr: stderr}
}

// DefaultUsage is the usage line of a bin with no global flags of its own,
// followed by UsageHint.
func DefaultUsage(bin string) string {
	return "usage: " + bin + " NOUN VERB [ARG...] [--target ID] [--json '{...}'] [--ARG VALUE] [--format " + strings.Join(Formats, "|") + "] [--yes]\n" + UsageHint(bin)
}

// UsageHint is the line under a usage line that points at oden's help: the
// command list and one noun's verbs and parameters.
func UsageHint(bin string) string {
	return "(" + bin + " help lists the commands; " + bin + " NOUN --help lists a noun's verbs and parameters)"
}

// Run executes one command and prints its result on Stdout, as JSON
// unless --format names another format.
func (s *Shell) Run(ctx context.Context, args []string) error {
	if len(args) < 2 && (len(args) == 0 || !s.bareAlias(args[0])) {
		return errors.New(s.usage())
	}
	// What handlers tell the user (ctx.Notify, ctx.Error, ctx.Info) prints on
	// Stderr while the command runs, setup's messages first.
	previous := s.Runtime.UI().Attach(stderrSink{s.stderr()})
	defer s.Runtime.UI().Attach(previous)
	if s.Prepare != nil {
		if err := s.Prepare(ctx); err != nil {
			return err
		}
	}
	r := s.Runtime
	// The user's actions.json adds verbs (Mount usually installed them
	// already); its rows may be named by bindings.json, so it goes first.
	for _, problem := range r.EnsureUserActions() {
		fmt.Fprintln(s.stderr(), "warning:", problem)
	}
	// The user bindings file applies to the CLI too, so bindings list
	// shows the effective rows; a skipped row is a warning, not an error.
	for _, problem := range r.EnsureUserBindings() {
		fmt.Fprintln(s.stderr(), "warning:", problem)
	}
	r.EnsureConfigSettings()
	// The noun is the longest run of leading words naming one: app
	// settings, ochazuke.settings and settings (an alias) are ochazuke.settings.
	var noun, verb string
	var flags []string
	var presets ochazuke.Arguments
	reading, bare, err := r.ReadBareAlias(r.Context(), args, nil)
	if err != nil {
		return err
	}
	if bare && reading.Noun != "" {
		// A bare alias that shadows a verb, followed by a noun with that
		// verb, is the noun's verb: bin set mode normal is bin mode set
		// normal.
		args = append([]string{reading.Noun, reading.Alias.Verb}, args[2:]...)
		bare = false
	}
	prop := ""
	if bare {
		// A bare alias (w!) is its own noun and verb: bin w! --force. The
		// action runs by the alias's name (Request.Via); aliases of the
		// same action after it add their presets (verbose set).
		noun, verb, flags = "", reading.Alias.Name(), args[reading.Skip:]
		presets = reading.Args
	} else {
		words, rest, ok := r.NounWords(args)
		if !ok {
			words, rest = args[0], args[1:]
		}
		v, p, err := s.nounWord(words, rest[0])
		if err != nil {
			return err
		}
		noun, verb, prop, flags = words, v, p, rest[1:]
	}
	var q ochazuke.Request
	var opts options
	if prop != "" {
		q, opts, err = s.propRequest(noun, prop, flags)
	} else {
		q, opts, err = s.request(noun, verb, flags)
	}
	if err != nil {
		return err
	}
	if len(presets) > 0 {
		op, _ := r.Operation(q.Key())
		q.Args = op.WithPresets(q.Args, presets)
	}
	if q.Target != "" && q.Target != "focused-input" && !resolves(r, q.Target) && s.Reveal != nil {
		if err := s.Reveal(q.Target); err != nil {
			return fmt.Errorf("target %s: %w", q.Target, err)
		}
		if err := Settle(r, s.Settle); err != nil {
			return fmt.Errorf("target %s: %w", q.Target, err)
		}
	}
	var candidates []string
	if q.Target == "" {
		candidates = s.ownersOf(q.Noun)
		if len(candidates) == 1 {
			q.Target = candidates[0]
		}
	}
	var fingerprint string
	if opts.since != nil {
		var lines Lines
		var ok bool
		if lines, fingerprint, ok = s.stateShortcut(q, *opts.since); ok {
			return s.print(lines)
		}
	}
	last := lastJob(r)
	if !s.StateCache.Off {
		// A watch keeps its versions in the state cache, so a later
		// command's --since resumes.
		defer r.SetWatchVersions(r.SetWatchVersions(cacheVersions{s}))
	}
	value, err := s.call(q)
	if err != nil {
		var bad *ochazuke.ArgumentError
		if len(candidates) > 1 && !errors.As(err, &bad) {
			return fmt.Errorf("%w; pass --target, one of: %s", err, listed(candidates))
		}
		return err
	}
	if w, ok := value.(*ochazuke.Watch); ok {
		return s.stream(ctx, w)
	}
	for {
		top := r.Instances[r.TopModal()]
		if top == nil {
			break
		}
		if top.Noun != "ochazuke.confirm" {
			return fmt.Errorf("%s opened a %s modal, which needs the TUI; pass its arguments instead (%s operations list)", q.Key(), top.Noun, s.Bin)
		}
		if !opts.yes && !s.accepts(top.Payload.Prompt) {
			if _, err := s.call(ochazuke.Request{Verb: "pop", Noun: "ochazuke.modal"}); err != nil {
				return err
			}
			return fmt.Errorf("declined: %s", top.Payload.Prompt)
		}
		if value, err = s.call(ochazuke.Request{Verb: "accept", Noun: "ochazuke.confirm"}); err != nil {
			return err
		}
	}
	// The jobs the command started are the running jobs newer than the
	// call; their delivered values are the command's result, not the ticket.
	launched := map[uint64]bool{}
	for _, j := range r.Jobs() {
		if j.Job > last && j.State == ochazuke.JobRunning {
			launched[j.Job] = true
		}
	}
	var results []any
	settled := settle(r, s.Settle, func(c ochazuke.Completion) {
		if launched[c.Job] && c.Err == nil {
			results = append(results, c.Value)
		}
	})
	for _, j := range r.Jobs() {
		if j.Job > last && (j.State == ochazuke.JobFailed || !r.Operations[j.Verb].Quiet) {
			name := r.DisplayKey(j.Verb)
			if j.Label != "" {
				name += " " + j.Label
			}
			fmt.Fprintf(s.stderr(), "job #%d %s %s\n", j.Job, name, j.State)
		}
	}
	if settled != nil {
		return settled
	}
	switch {
	case len(launched) == 1 && len(results) == 1:
		value = results[0]
	case len(launched) > 1 && len(results) > 0:
		value = results
	}
	if opts.since != nil {
		lines, err := s.stateDelta(q, *opts.since, fingerprint, value)
		if err != nil {
			return err
		}
		return s.printVerbatim(lines)
	}
	return s.output(q, opts.format, value)
}

// lastJob is the newest job id the runtime lists, 0 for none.
func lastJob(r *ochazuke.Runtime) uint64 {
	var last uint64
	for _, j := range r.Jobs() {
		last = max(last, j.Job)
	}
	return last
}

func (s *Shell) usage() string {
	if s.Usage != "" {
		return s.Usage
	}
	return DefaultUsage(s.Bin)
}

func (s *Shell) stdout() io.Writer {
	if s.Stdout == nil {
		return os.Stdout
	}
	return s.Stdout
}

// stderrSink prints what handlers tell the user as lines on Stderr, beside
// the shell's warning: lines: "notice: TEXT", "error: TEXT" (ctx.Error)
// and "status: TEXT" (ctx.Info), UIMessage.Line. A line cannot
// be taken back, so a cleared notice or an empty status prints nothing.
type stderrSink struct{ w io.Writer }

func (s stderrSink) Notify(_ ochazuke.NoticeID, text string) {
	s.line(ochazuke.UIMessage{Kind: ochazuke.UINotice, Text: text})
}
func (s stderrSink) ClearNotice(ochazuke.NoticeID) {}
func (s stderrSink) Status(text string) {
	s.line(ochazuke.UIMessage{Kind: ochazuke.UIError, Text: text})
}
func (s stderrSink) Info(text string) { s.line(ochazuke.UIMessage{Kind: ochazuke.UIInfo, Text: text}) }
func (s stderrSink) line(msg ochazuke.UIMessage) {
	if line := msg.Line(); line != "" {
		fmt.Fprintln(s.w, line)
	}
}

func (s *Shell) stderr() io.Writer {
	if s.Stderr == nil {
		return os.Stderr
	}
	return s.Stderr
}

// nounWord reads the word after noun (owner decision 18, the palette's
// rule, Runtime.ReadNounWord): a verb of the noun (an exact verb or alias,
// an exact prop, a unique verb prefix, a unique prop prefix, in that
// order) answers the verb; a prop answers the prop (NOUN PROP [VALUE]);
// any other word goes to verb, which names what is wrong.
func (s *Shell) nounWord(noun, word string) (verb, prop string, err error) {
	if _, ok := s.Runtime.Operation(noun + "." + word); ok {
		return word, "", nil
	}
	var verbs []string
	for _, op := range s.Runtime.Operations {
		if op.Noun == noun {
			verbs = append(verbs, op.Verb)
		}
	}
	sort.Strings(verbs)
	switch read := s.Runtime.ReadNounWord(noun, word, verbs); read.Kind {
	case ochazuke.NounWordProp:
		return "", read.Value, nil
	case ochazuke.NounWordVerb:
		return read.Value, "", nil
	}
	verb, err = s.verb(noun, word)
	return verb, "", err
}

// propRequest builds NOUN PROP [VALUE] (plan 2.6): a prop-shaped request
// that reads the prop (no value), writes it (VALUE) or flips a boolean
// (--toggle), for the thing --KEY names (the flag named after the key
// prop), else the one the command acts for. The shell's own flags work as
// for any command.
func (s *Shell) propRequest(noun, prop string, flags []string) (ochazuke.Request, options, error) {
	r := s.Runtime
	n := r.Noun(noun)
	base, ok := r.Operations[n.Name+".set"]
	if !ok {
		base, ok = r.Operations[n.Name+".get"]
	}
	if !ok {
		return ochazuke.Request{}, options{}, fmt.Errorf("%s %s: the %s cannot be read or written", noun, prop, noun)
	}
	// The words the shorthand takes: VALUE, --toggle and the subject's
	// --KEY.
	op := ochazuke.Operation{Noun: n.Name, Verb: prop, Parameters: map[string]ochazuke.Parameter{
		"value":  {Type: ochazuke.AnyParameterType},
		"toggle": {Type: "boolean"},
	}, Positional: []string{"value"}}
	for name, p := range base.Parameters {
		if p.Focus {
			op.Parameters[name] = p
		}
	}
	q, opts, err := s.parseFlags(ochazuke.Request{Noun: n.Name, Verb: prop, Args: ochazuke.Arguments{}}, op, flags)
	if err != nil {
		return q, opts, err
	}
	for name := range op.Parameters {
		named := slices.ContainsFunc(flags, func(f string) bool {
			return f == "--"+name || strings.HasPrefix(f, "--"+name+"=") || strings.HasPrefix(f, "--json")
		})
		if _, set := q.Args[name]; set && name != "value" && name != "toggle" && !named {
			// One word only, as in the palette: the thing is --KEY.
			return q, opts, fmt.Errorf("%s %s takes one value; name the %s with --%s", noun, prop, r.DisplayNoun(n.Name), name)
		}
	}
	value := q.Args["value"]
	toggle, _ := q.Args["toggle"].(bool)
	delete(q.Args, "value")
	delete(q.Args, "toggle")
	out := ochazuke.PropShorthand(n.Name, prop, value, toggle, q.Args)
	out.Target = q.Target
	return out, opts, nil
}

// verb resolves a verb word of noun: the exact verb, else the one verb
// of noun starting with it. Several are an error naming them; none is the
// word unchanged, which request reports.
func (s *Shell) verb(noun, word string) (string, error) {
	if _, ok := s.Runtime.Operation(noun + "." + word); ok {
		return word, nil // the verb, or an alias spelled whole
	}
	var matches []string
	for _, op := range s.Runtime.Operations {
		if op.Noun == noun && strings.HasPrefix(op.Verb, word) {
			matches = append(matches, op.Verb)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return word, nil
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("%s %s is ambiguous: %s", noun, word, strings.Join(matches, ", "))
}

// ownersOf is the instances a command of noun without --target can act
// for: the declared owner when it exists, else every instance of noun (app
// aside), in id order.
func (s *Shell) ownersOf(noun string) []string {
	for _, name := range []string{noun, s.Runtime.DisplayNoun(noun)} {
		if id := s.Owners[name]; id != "" && s.Runtime.Instances[id] != nil {
			return []string{id}
		}
	}
	var ids []string
	for id, inst := range s.Runtime.Instances {
		if id != "app" && inst.Noun == noun {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// maxCandidates is how many instance ids an ambiguous command's error lists.
const maxCandidates = 10

// listed joins ids for an error, at most maxCandidates of them.
func listed(ids []string) string {
	if len(ids) <= maxCandidates {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(ids[:maxCandidates], ", "), len(ids)-maxCandidates)
}

// options are the shell's own flags of a command: --yes and the output
// format.
type options struct {
	yes    bool
	format string
	since  *sinceRequest // --since on a state verb (stateDelta)
}

// request builds the operation request: --target, --json, --format and
// --yes (or -y) are the shell's; every other --NAME VALUE (or --NAME=VALUE) is an
// argument typed by the operation's parameter of that name. --json VALUE
// is the arguments as a JSON object; a bare --json (last, or followed by
// another --flag) asks for JSON output, the default, and is otherwise
// ignored. --format VALUE (one of Formats) picks the output; with a bare
// --json only json agrees. An operation that declares a format parameter
// of its own takes --format as that argument, and the shell prints JSON.
func (s *Shell) request(noun, verb string, flags []string) (ochazuke.Request, options, error) {
	q := s.Runtime.CanonicalRequest(ochazuke.Request{Noun: noun, Verb: verb, Args: ochazuke.Arguments{}})
	op, ok := s.Runtime.Operations[q.Key()]
	if !ok {
		return q, options{}, fmt.Errorf("unknown operation %q (%s %s); operations list prints them", q.Key(), noun, verb)
	}
	return s.parseFlags(q, op, flags)
}

// parseFlags reads a command's words into q's arguments for op (request).
func (s *Shell) parseFlags(q ochazuke.Request, op ochazuke.Operation, flags []string) (ochazuke.Request, options, error) {
	var opts options
	bareJSON := false
	var words []string
	for i := 0; i < len(flags); i++ {
		if flags[i] == "-y" {
			// -y is --yes.
			opts.yes = true
			continue
		}
		name, ok := strings.CutPrefix(flags[i], "--")
		if !ok {
			words = append(words, flags[i])
			continue
		}
		if name == "" {
			return q, opts, fmt.Errorf("unexpected argument %q; %s", flags[i], s.usage())
		}
		name, value, inline := strings.Cut(name, "=")
		takeValue := func() (string, error) {
			if inline {
				return value, nil
			}
			if i+1 >= len(flags) {
				return "", fmt.Errorf("--%s needs a value", name)
			}
			i++
			return flags[i], nil
		}
		switch {
		case name == "yes":
			opts.yes = true
			continue
		case name == "format" && !hasParameter(op, "format"):
			v, err := takeValue()
			if err != nil {
				return q, opts, err
			}
			if !slices.Contains(Formats, v) {
				return q, opts, fmt.Errorf("--format %q: one of %s", v, strings.Join(Formats, ", "))
			}
			opts.format = v
			continue
		case (name == SinceFlag || name == SinceCacheFlag) && s.speaksDelta(op, name):
			v, err := takeValue()
			if err != nil {
				return q, opts, err
			}
			if opts.since == nil {
				opts.since = &sinceRequest{hashes: map[string]string{}}
			}
			if name == SinceCacheFlag {
				opts.since.cache = v
				continue
			}
			if opts.since.hashes, err = ParseSince(v); err != nil {
				return q, opts, err
			}
			continue
		case name == "target":
			v, err := takeValue()
			if err != nil {
				return q, opts, err
			}
			q.Target = v
			continue
		case name == "json" && !inline && (i+1 >= len(flags) || strings.HasPrefix(flags[i+1], "--")):
			// A bare --json asks for JSON output, the default.
			bareJSON = true
			continue
		case name == "json":
			v, err := takeValue()
			if err != nil {
				return q, opts, err
			}
			args, err := ochazuke.ParseArguments(v)
			if err != nil {
				return q, opts, err
			}
			for k, v := range args {
				q.Args[k] = v
			}
			continue
		}
		p, ok := op.Parameters[name]
		if !ok {
			return q, opts, fmt.Errorf("%s has no parameter %q", q.Key(), name)
		}
		if p.Type == "boolean" && !inline && (i+1 >= len(flags) || strings.HasPrefix(flags[i+1], "--")) {
			q.Args[name] = true
			continue
		}
		v, err := takeValue()
		if err != nil {
			return q, opts, err
		}
		if q.Args[name], err = typed(p.Type, v); err != nil {
			return q, opts, fmt.Errorf("--%s: %w", name, err)
		}
	}
	if err := positional(q, op, words); err != nil {
		return q, opts, err
	}
	if opts.since != nil && opts.format != "" && opts.format != "json" {
		return q, opts, fmt.Errorf("--%s answers JSON Lines; --format %s prints a table", SinceFlag, opts.format)
	}
	if bareJSON && opts.format != "" && opts.format != "json" {
		return q, opts, fmt.Errorf("a bare --json asks for JSON output and --format %s for another; pass one (--json '{...}' passes arguments)", opts.format)
	}
	return q, opts, nil
}

// hasParameter reports whether op declares a parameter called name.
func hasParameter(op ochazuke.Operation, name string) bool {
	_, ok := op.Parameters[name]
	return ok
}

// positional fills the parameters no flag set from words, in
// Operation.ParameterOrder, typed by each parameter. A boolean takes a
// bare word only when Operation.Positional names it; otherwise it is set
// with --NAME.
func positional(q ochazuke.Request, op ochazuke.Operation, words []string) error {
	if len(words) == 0 {
		return nil
	}
	var open []string
	for _, name := range op.ParameterOrder() {
		if op.Parameters[name].Type == "boolean" && !slices.Contains(op.Positional, name) {
			continue
		}
		if _, set := q.Args[name]; !set {
			open = append(open, name)
		}
	}
	if op.SubjectOmitted(open, words[0]) {
		// The optional key word (plan 2.6): a first word that is the next
		// parameter's (a prop name, prop=value) leaves the subject to the
		// focus or --KEY.
		open = open[1:]
	}
	if rest := slices.IndexFunc(open, func(name string) bool { return op.Parameters[name].Rest }); rest >= 0 {
		open = open[:rest+1]
	} else if len(words) > len(open) {
		takes := "no positional arguments"
		if len(open) > 0 {
			takes = fmt.Sprintf("%d positional argument(s): %s", len(open), strings.Join(open, ", "))
		}
		return fmt.Errorf("unexpected argument %q; %s takes %s", words[len(open)], q.Key(), takes)
	}
	for i, word := range words {
		name := open[i]
		if op.Parameters[name].Rest {
			// A rest parameter (Parameter.Rest) takes the remaining
			// words, joined by spaces: bin settings apply scrolloff=5 nowrap.
			rest := words[i:]
			if op.Parameters[name].Pairs {
				// NAME=VALUE words keep a value holding spaces
				// one word (text="buy milk"), as the shell gave it.
				rest = slices.Clone(rest)
				for j, w := range rest {
					if k, v, ok := strings.Cut(w, "="); ok && strings.ContainsAny(v, " \t") && !strings.HasPrefix(v, `"`) {
						rest[j] = k + "=" + strconv.Quote(v)
					}
				}
			}
			q.Args[name] = strings.Join(rest, " ")
			return nil
		}
		v, err := typed(op.Parameters[name].Type, word)
		if err != nil {
			return fmt.Errorf("%s (argument %d): %w", name, i+1, err)
		}
		q.Args[name] = v
	}
	return nil
}

// typed converts a flag value to its parameter's type; object and array
// values are JSON.
func typed(kind, v string) (any, error) {
	switch kind {
	case "integer":
		return strconv.Atoi(v)
	case "boolean":
		return strconv.ParseBool(v)
	case "object", "array":
		var out any
		err := json.Unmarshal([]byte(v), &out)
		return out, err
	}
	return v, nil
}

// call runs one request through the runtime's CLI entry.
func (s *Shell) call(q ochazuke.Request) (any, error) {
	reply := make(chan ochazuke.Response, 1)
	s.Runtime.HandleCall(ochazuke.CallMsg{Request: q, Reply: reply, Stream: true})
	res := <-reply
	if res.Error != "" {
		if res.Argument {
			return nil, &ochazuke.ArgumentError{Err: errors.New(res.Error)}
		}
		return nil, errors.New(res.Error)
	}
	return res.Result, nil
}

// accepts prints a confirm prompt and reads the answer from Stdin.
func (s *Shell) accepts(prompt string) bool {
	fmt.Fprintf(s.stderr(), "%s [y/N] ", prompt)
	if s.in == nil {
		stdin := s.Stdin
		if stdin == nil {
			stdin = os.Stdin
		}
		s.in = bufio.NewReader(stdin)
	}
	line, _ := s.in.ReadString('\n')
	fmt.Fprintln(s.stderr())
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// Settle runs r's scheduled work until none is left, delivering every
// message on this goroutine as the Bubble Tea loop would (Runtime.Deliver),
// and returns the errors of the completions it delivered, a failed job's
// among them. before, when set, runs before each round and its errors join
// the result. A host without a screen (a CLI, --smoke, a test) settles with
// it instead of driving a Bubble Tea program.
func Settle(r *ochazuke.Runtime, before func() error) error {
	return settle(r, before, nil)
}

// settle is Settle, handing every current completion it delivers to seen.
func settle(r *ochazuke.Runtime, before func() error, seen func(ochazuke.Completion)) error {
	var errs []error
	for {
		if before != nil {
			errs = append(errs, before())
		}
		cmd := r.PendingCommands()
		if cmd == nil {
			break
		}
		errs = append(errs, deliver(r, cmd, seen))
	}
	return errors.Join(errs...)
}

func deliver(r *ochazuke.Runtime, cmd tea.Cmd, seen func(ochazuke.Completion)) error {
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var errs []error
		for _, c := range msg {
			if c != nil {
				errs = append(errs, deliver(r, c, seen))
			}
		}
		return errors.Join(errs...)
	case nil:
		return nil
	case ochazuke.Completion:
		current := r.Accepts(msg.Ticket)
		_, err := r.Deliver(msg)
		if current && seen != nil {
			seen(msg)
		}
		return err
	default:
		_, err := r.Deliver(msg)
		return err
	}
}

// print writes the result as JSON: Lines as JSON Lines, an array one
// compact element per line (bindings list, history list read as tables),
// anything else indented.
func (s *Shell) print(value any) error {
	w := s.stdout()
	if text, ok := value.(ochazuke.Verbatim); ok {
		_, err := io.WriteString(w, string(text))
		return err
	}
	if lines, ok := value.(Lines); ok {
		bw := bufio.NewWriter(w)
		for _, line := range lines {
			data, err := json.Marshal(line)
			if err != nil {
				return err
			}
			bw.Write(append(data, '\n'))
		}
		return bw.Flush()
	}
	if raw, ok := value.(json.RawMessage); ok {
		// HandleCall's result: an encoded value in the handler's key order.
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) == nil && len(items) > 0 {
			value = items
		}
	}
	if items, ok := value.([]json.RawMessage); ok {
		lines := make([]string, len(items))
		for i, item := range items {
			var b bytes.Buffer
			if err := json.Compact(&b, item); err != nil {
				return err
			}
			lines[i] = b.String()
		}
		_, err := fmt.Fprintf(w, "[\n  %s\n]\n", strings.Join(lines, ",\n  "))
		return err
	}
	if items, ok := value.([]any); ok && len(items) > 0 {
		lines := make([]string, len(items))
		for i, item := range items {
			data, err := json.Marshal(item)
			if err != nil {
				return err
			}
			lines[i] = string(data)
		}
		_, err := fmt.Fprintf(w, "[\n  %s\n]\n", strings.Join(lines, ",\n  "))
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// resolves reports whether target names an instance now, directly or
// through Runtime.ResolveTarget.
func resolves(r *ochazuke.Runtime, target string) bool {
	_, err := r.TargetInstance(target)
	return err == nil
}

// bareAlias reports whether word is a bare action alias (w!), which runs
// with no other words; an alias kept to the palette (ActionAlias.Palette,
// help) is not one here, so the word stays the shell's.
func (s *Shell) bareAlias(word string) bool {
	a, ok := s.Runtime.ActionAlias(word)
	return ok && a.Bare && a.Verb == word && !a.Palette
}
