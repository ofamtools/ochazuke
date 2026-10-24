// Package cli is the generic command line of an ochazuke application: the
// NOUN VERB shell over a runtime (Shell), the oden mount that makes every
// noun a command of the bin (Mount) and the catalog answer that describes
// the runtime as JSON (Catalog). It knows no noun, verb or key; the
// application passes in its bin name, usage line and the hooks that reach
// its own state.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// Shell runs BIN NOUN VERB [--target ID] [--json '{...}'] [--ARG VALUE]
// [--yes]: one registered operation in this process through the runtime's
// CLI entry (Runtime.HandleCall, recorded with the history source "cli"),
// the same registry the keys and the palette dispatch into. A command's
// words are the operation key, its --ARG flags are typed by the operation's
// own parameters, and everything else is the framework's. A VERB may be a
// unique prefix of one of the noun's verbs (a noun's prefix is resolved by
// the oden shell, see Mount).
//
// After the operation it settles the work the operation scheduled (the
// pending commands, delivered through Runtime.Deliver as the Bubble Tea
// loop would), so an application verb's job finishes before the process
// exits. A confirm modal the operation opened is answered: --yes runs
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
	// settles what it scheduled and resolves the target again.
	Reveal func(target string) error
	// Owners maps a noun with no instance of its own to the instance its
	// verbs act for; a command of that noun without --target targets it.
	Owners map[string]string
	// Settle, when set, runs before each settle round (an application
	// without a screen removes the terminals an operation attached); its
	// errors join the command's.
	Settle func() error

	in *bufio.Reader
}

// New is a Shell over r for bin with the standard usage line.
func New(r *ochazuke.Runtime, bin string, stdin io.Reader, stdout, stderr io.Writer) *Shell {
	return &Shell{Runtime: r, Bin: bin, Usage: DefaultUsage(bin), Stdin: stdin, Stdout: stdout, Stderr: stderr}
}

// DefaultUsage is the usage line of a bin with no global flags of its own.
func DefaultUsage(bin string) string {
	return "usage: " + bin + " NOUN VERB [--target ID] [--json '{...}'] [--ARG VALUE] [--yes]\n(operations list prints every NOUN VERB and its parameters)"
}

// Run executes one command and prints its result as JSON on Stdout.
func (s *Shell) Run(ctx context.Context, args []string) error {
	if s.Prepare != nil {
		if err := s.Prepare(ctx); err != nil {
			return err
		}
	}
	if len(args) < 2 {
		return errors.New(s.usage())
	}
	r := s.Runtime
	verb, err := s.verb(args[0], args[1])
	if err != nil {
		return err
	}
	q, yes, err := s.request(args[0], verb, args[2:])
	if err != nil {
		return err
	}
	if q.Target != "" && q.Target != "focused-input" && r.Instances[q.Target] == nil && s.Reveal != nil {
		if err := s.Reveal(q.Target); err != nil {
			return fmt.Errorf("target %s: %w", q.Target, err)
		}
		if err := s.settle(); err != nil {
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
	jobs := len(r.Jobs())
	value, err := s.call(q)
	if err != nil {
		if len(candidates) > 1 {
			return fmt.Errorf("%w; pass --target, one of: %s", err, listed(candidates))
		}
		return err
	}
	for {
		top := r.Instances[r.TopModal()]
		if top == nil {
			break
		}
		if top.Noun != "confirm" {
			return fmt.Errorf("%s opened a %s modal, which needs the TUI; pass its arguments instead (%s operations list)", q.Key(), top.Noun, s.Bin)
		}
		if !yes && !s.accepts(top.Payload.Prompt) {
			if _, err := s.call(ochazuke.Request{Verb: "pop", Noun: "modal"}); err != nil {
				return err
			}
			return fmt.Errorf("declined: %s", top.Payload.Prompt)
		}
		if value, err = s.call(ochazuke.Request{Verb: "accept", Noun: "confirm"}); err != nil {
			return err
		}
	}
	settled := s.settle()
	for _, j := range r.Jobs()[min(jobs, len(r.Jobs())):] {
		fmt.Fprintf(s.stderr(), "job #%d %s %s\n", j.Job, j.Verb, j.State)
	}
	if err := s.print(value); err != nil {
		return err
	}
	return settled
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

func (s *Shell) stderr() io.Writer {
	if s.Stderr == nil {
		return os.Stderr
	}
	return s.Stderr
}

// verb resolves a verb word of noun: the exact verb, else the one verb
// of noun starting with it. Several are an error naming them; none is the
// word unchanged, which request reports.
func (s *Shell) verb(noun, word string) (string, error) {
	if _, ok := s.Runtime.Operations[noun+"."+word]; ok {
		return word, nil
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
	if id := s.Owners[noun]; id != "" && s.Runtime.Instances[id] != nil {
		return []string{id}
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

// request builds the operation request: --target, --json and --yes are the
// shell's; every other --NAME VALUE (or --NAME=VALUE) is an argument typed
// by the operation's parameter of that name.
func (s *Shell) request(noun, verb string, flags []string) (ochazuke.Request, bool, error) {
	q := ochazuke.Request{Noun: noun, Verb: verb, Args: ochazuke.Arguments{}}
	op, ok := s.Runtime.Operations[q.Key()]
	if !ok {
		return q, false, fmt.Errorf("unknown operation %q (%s %s); operations list prints them", q.Key(), noun, verb)
	}
	yes := false
	for i := 0; i < len(flags); i++ {
		name, ok := strings.CutPrefix(flags[i], "--")
		if !ok || name == "" {
			return q, false, fmt.Errorf("unexpected argument %q; %s", flags[i], s.usage())
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
			yes = true
			continue
		case name == "target":
			v, err := takeValue()
			if err != nil {
				return q, false, err
			}
			q.Target = v
			continue
		case name == "json":
			v, err := takeValue()
			if err != nil {
				return q, false, err
			}
			args, err := ochazuke.ParseArguments(v)
			if err != nil {
				return q, false, err
			}
			for k, v := range args {
				q.Args[k] = v
			}
			continue
		}
		p, ok := op.Parameters[name]
		if !ok {
			return q, false, fmt.Errorf("%s has no parameter %q", q.Key(), name)
		}
		if p.Type == "boolean" && !inline && (i+1 >= len(flags) || strings.HasPrefix(flags[i+1], "--")) {
			q.Args[name] = true
			continue
		}
		v, err := takeValue()
		if err != nil {
			return q, false, err
		}
		if q.Args[name], err = typed(p.Type, v); err != nil {
			return q, false, fmt.Errorf("--%s: %w", name, err)
		}
	}
	return q, yes, nil
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
	s.Runtime.HandleCall(ochazuke.CallMsg{Request: q, Reply: reply})
	res := <-reply
	if res.Error != "" {
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

// settle runs the scheduled work until none is left, delivering every
// message on this goroutine as the Bubble Tea loop would, and returns the
// errors of the completions it delivered.
func (s *Shell) settle() error {
	var errs []error
	for {
		if s.Settle != nil {
			errs = append(errs, s.Settle())
		}
		cmd := s.Runtime.PendingCommands()
		if cmd == nil {
			break
		}
		errs = append(errs, s.deliver(cmd))
	}
	return errors.Join(errs...)
}

func (s *Shell) deliver(cmd tea.Cmd) error {
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var errs []error
		for _, c := range msg {
			if c != nil {
				errs = append(errs, s.deliver(c))
			}
		}
		return errors.Join(errs...)
	case nil:
		return nil
	default:
		_, err := s.Runtime.Deliver(msg)
		return err
	}
}

// print writes the result as JSON: an array one compact element per line
// (bindings list, history list read as tables), anything else indented.
func (s *Shell) print(value any) error {
	w := s.stdout()
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
