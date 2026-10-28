package ochazuke

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ActionsFile is the user actions file in the config directory, beside
// bindings.json.
const ActionsFile = "actions.json"

// Action is one entry of actions.json: a verb on an existing noun that
// runs an external command. It becomes an application operation
// (noun.verb) like any other, so binding rows, the palette, the CLI, ?
// help and the catalog see it; its work runs off the loop as a job
// labelled Label, and the command's standard output is the job's result
// (JSON when it parses, a JSON value per line when every line does, else
// the text).
//
// The command is either Command, an argv template run directly (never
// through a shell), or Base, the convention for a CLI with a JSON form:
// BASE NOUN VERB --json ARGS [--target TARGET], where NOUN VERB is Call
// ("noun verb", default this action's noun and verb), ARGS the action's
// arguments as one JSON object and TARGET the Target template when it is
// not empty. JSON appends --json ARGS to a Command too.
//
// Templates ({{name}}) in Command, Base, Label, Dir and Target expand to:
// target or id (the instance the verb runs on), label (its label, else
// its id), noun and verb (the action's), data (the instance's Data: a string as is, else
// JSON), data.KEY (one field of Data as a JSON object), args (the
// arguments as JSON) or a declared parameter's value. An argv element that
// is exactly one placeholder and expands to nothing is left out, so an
// optional parameter disappears instead of passing "". Confirm is a
// confirm prompt template, as NounBuilder.Confirm takes. History is the
// kind of history entry it records (Operation.HistoryKind): "fetch" for a
// command that only reads; empty is a change, as for any application verb.
//
// Actions are read only from the user's own actions.json (Config.Path):
// never from a catalog, a binding row, a socket call or an application's
// data, so nothing but the user's file decides what runs.
//
// An entry with an "alias" field is an alias row instead (AliasRow): one
// more name for an existing action, which runs nothing of its own.
type Action struct {
	Noun        string               `json:"noun"`
	Verb        string               `json:"verb"`
	Description string               `json:"description,omitempty"`
	Command     Argv                 `json:"command,omitempty"`
	Base        Argv                 `json:"base,omitempty"`
	Call        string               `json:"call,omitempty"`
	Target      string               `json:"target,omitempty"`
	JSON        bool                 `json:"json,omitempty"`
	Label       string               `json:"label,omitempty"`
	Confirm     string               `json:"confirm,omitempty"`
	Parameters  map[string]Parameter `json:"parameters,omitempty"`
	Positional  []string             `json:"positional,omitempty"`
	Dir         string               `json:"dir,omitempty"`
	Timeout     string               `json:"timeout,omitempty"`
	Quiet       bool                 `json:"quiet,omitempty"`
	History     string               `json:"history,omitempty"`

	// written is the noun as the file spells it (an alias: list), when
	// Noun was resolved to the noun it names (ochazuke.list); {{noun}} and the
	// default call keep the file's spelling.
	written string
}

// Key is the action's operation key, noun.verb.
func (a Action) Key() string { return a.Noun + "." + a.Verb }

// spelled is the noun as the file spells it.
func (a Action) spelled() string {
	if a.written != "" {
		return a.written
	}
	return a.Noun
}

// writtenKey is the key as the file spells it, for messages.
func (a Action) writtenKey() string { return a.spelled() + "." + a.Verb }

// Argv is a command line: a JSON array of words, or a string split on
// spaces (use the array for a word holding a space).
type Argv []string

// UnmarshalJSON reads an array of strings or one string split on spaces.
func (v *Argv) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*v = strings.Fields(s)
		return nil
	}
	var words []string
	if err := json.Unmarshal(data, &words); err != nil {
		return errors.New("must be a string or an array of strings")
	}
	*v = words
	return nil
}

// actionPlaceholders are the template names every action may use.
var actionPlaceholders = map[string]bool{"target": true, "id": true, "label": true, "noun": true, "verb": true, "data": true, "args": true}

var actionField = regexp.MustCompile(`\{\{\s*([\w.-]+)\s*\}\}`)

// actionTypes are the parameter types an action declares.
var actionTypes = map[string]bool{"string": true, "integer": true, "boolean": true}

// check reports why an action cannot be installed on r, nil when it can.
func (a Action) check(r *Runtime) error {
	switch {
	case a.Noun == "" || a.Verb == "":
		return errors.New("noun and verb are required")
	case strings.ContainsAny(a.Verb, ". \t\n"):
		return fmt.Errorf("verb %q must be one word without a dot", a.Verb)
	case r.Nouns[a.Noun] == nil:
		return fmt.Errorf("unknown noun %q (an action adds a verb to an existing noun)", a.spelled())
	case r.Operations[a.Key()].Noun != "":
		return fmt.Errorf("%s is already defined", a.writtenKey())
	case len(a.Command) > 0 && len(a.Base) > 0:
		return errors.New("command and base exclude each other")
	case len(a.Command) == 0 && len(a.Base) == 0:
		return errors.New("command or base is required")
	case len(a.Command) > 0 && (a.Call != "" || a.Target != ""):
		return errors.New("call and target go with base, not command")
	case len(a.Base) > 0 && a.JSON:
		return errors.New("json goes with command; base always passes --json")
	case a.Call != "" && len(strings.Fields(a.Call)) < 2:
		return fmt.Errorf("call %q must be the noun's words and the verb", a.Call)
	}
	for name, p := range a.Parameters {
		if name == "" || strings.ContainsAny(name, ". \t\n") || actionPlaceholders[name] {
			return fmt.Errorf("parameter name %q must be one word, not a dot or a reserved name", name)
		}
		if !actionTypes[p.Type] {
			return fmt.Errorf("parameter %s: type must be string, integer or boolean", name)
		}
	}
	for _, name := range a.Positional {
		if _, ok := a.Parameters[name]; !ok {
			return fmt.Errorf("positional %q is not a declared parameter", name)
		}
	}
	switch a.History {
	case "", HistoryAction, HistoryChange, HistoryFetch:
	default:
		return fmt.Errorf("history %q must be action, change or fetch", a.History)
	}
	if a.Timeout != "" {
		if d, err := time.ParseDuration(a.Timeout); err != nil || d <= 0 {
			return fmt.Errorf("timeout %q must be a duration such as \"30s\"", a.Timeout)
		}
	}
	templates := append(append(append([]string{}, a.Command...), a.Base...), a.Label, a.Dir, a.Target)
	for _, t := range templates {
		for _, m := range actionField.FindAllStringSubmatch(t, -1) {
			name := m[1]
			if _, declared := a.Parameters[name]; declared || actionPlaceholders[name] || strings.HasPrefix(name, "data.") && len(name) > len("data.") {
				continue
			}
			return fmt.Errorf("unknown placeholder {{%s}}", name)
		}
	}
	return nil
}

// actionValues are the template values of one run.
func (a Action) values(c Ctx) func(name string) string {
	inst := c.Instance
	var fields map[string]any
	return func(name string) string {
		switch name {
		case "target", "id":
			return inst.ID
		case "label":
			if inst.Label != "" {
				return inst.Label
			}
			return inst.ID
		case "noun":
			return a.spelled()
		case "verb":
			return a.Verb
		case "data":
			return templateText(inst.Data)
		case "args":
			return argsJSON(c.Args)
		}
		if key, ok := strings.CutPrefix(name, "data."); ok {
			if fields == nil {
				fields = map[string]any{}
				if data, err := json.Marshal(inst.Data); err == nil {
					_ = json.Unmarshal(data, &fields)
				}
			}
			return templateText(fields[key])
		}
		return templateText(c.Args[name])
	}
}

// templateText is a value as one template word: a string as is, nothing
// for nil, else its JSON.
func templateText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func argsJSON(args Arguments) string {
	if args == nil {
		args = Arguments{}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// expand fills one template.
func expand(t string, value func(string) string) string {
	return actionField.ReplaceAllStringFunc(t, func(m string) string {
		return value(actionField.FindStringSubmatch(m)[1])
	})
}

// argv is the command line of one run.
func (a Action) argv(c Ctx, value func(string) string) []string {
	var out []string
	words := a.Command
	if len(a.Base) > 0 {
		words = a.Base
	}
	for _, w := range words {
		got := expand(w, value)
		if got == "" && actionField.FindString(w) == strings.TrimSpace(w) {
			continue // an empty lone placeholder is left out
		}
		out = append(out, got)
	}
	if len(a.Base) > 0 {
		call := a.Call
		if call == "" {
			call = a.spelled() + " " + a.Verb
		}
		out = append(out, strings.Fields(call)...)
		out = append(out, "--json", argsJSON(c.Args))
		if t := expand(a.Target, value); t != "" {
			out = append(out, "--target", t)
		}
	} else if a.JSON {
		out = append(out, "--json", argsJSON(c.Args))
	}
	return out
}

// handler is the action's application verb.
func (a Action) handler() AppHandler {
	return func(c Ctx) Result {
		value := a.values(c)
		argv := a.argv(c, value)
		if len(argv) == 0 || argv[0] == "" {
			return Failed(fmt.Errorf("%s: the command is empty", a.Key()))
		}
		label := expand(a.Label, value)
		if label == "" {
			label = filepath.Base(argv[0])
		}
		dir := expand(a.Dir, value)
		var timeout time.Duration
		if a.Timeout != "" {
			timeout, _ = time.ParseDuration(a.Timeout)
		}
		env := append(os.Environ(),
			"OCHAZUKE_ACTION="+a.Key(),
			"OCHAZUKE_TARGET="+value("target"),
			"OCHAZUKE_LABEL="+value("label"),
			"OCHAZUKE_ARGS="+argsJSON(c.Args),
		)
		return Pending(func(ctx context.Context) (any, error) {
			return runAction(ctx, argv, dir, env, timeout)
		}).Label(label)
	}
}

// runAction runs one command off the loop: no standard input, its output
// captured. A failure names the command's last line of standard error.
func runAction(ctx context.Context, argv []string, dir string, env []string, timeout time.Duration) (any, error) {
	run := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		run, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(run, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = dir, env
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if run.Err() != nil {
			return nil, fmt.Errorf("%s: timed out after %s", filepath.Base(argv[0]), timeout)
		}
		if last := lastLine(stderr.String()); last != "" {
			return nil, fmt.Errorf("%s: %v: %s", filepath.Base(argv[0]), err, last)
		}
		return nil, fmt.Errorf("%s: %w", filepath.Base(argv[0]), err)
	}
	return actionOutput(stdout.Bytes()), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// actionOutput is a command's output as a result: one JSON document, a
// JSON value per line (JSON Lines), else the trimmed text; nil when empty.
func actionOutput(out []byte) any {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil
	}
	if json.Valid(out) {
		return json.RawMessage(out)
	}
	var lines []json.RawMessage
	for line := range bytes.Lines(out) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			return string(out)
		}
		lines = append(lines, json.RawMessage(line))
	}
	return lines
}

// UserActions are the actions installed from actions.json, in file order.
func (r *Runtime) UserActions() []Action { return slices.Clone(r.Config().actions) }

// UserAliases are the alias rows installed from actions.json, in file
// order.
func (r *Runtime) UserAliases() []ActionAlias { return slices.Clone(r.Config().actionAliases) }

// AliasRow is an alias entry of actions.json: {"alias": "pane.k",
// "action": "pane.kill"} makes pane.k another name of pane.kill, for keys
// (bindings.json rows), the palette, the CLI and the API. Alias is
// noun.verb on a registered noun (a short noun name such as list works);
// Action is a registered action or alias. An alias may not take the name
// of an action or of another alias; such a row is skipped and reported, as
// bindings check reports a bad row. Note is free text listed with it.
type AliasRow struct {
	Alias  string `json:"alias"`
	Action string `json:"action"`
	Note   string `json:"note,omitempty"`
}

// checkUserAlias validates one alias row against r: an earlier row of the
// file (seen: alias name -> entry number) may not have declared it, then
// AddActionAlias's rules, plus that it is not already a name the framework
// or the application gave the same action.
func (r *Runtime) checkUserAlias(row AliasRow, seen map[string]int) (ActionAlias, error) {
	if noun, verb, ok := r.splitNounName(strings.TrimSpace(row.Alias)); ok {
		if n, dup := seen[noun+"."+verb]; dup {
			return ActionAlias{}, fmt.Errorf("alias %s is already declared by entry %d", r.DisplayKey(noun+"."+verb), n)
		}
	}
	a, err := r.checkAlias(ActionAlias{Alias: row.Alias, Action: row.Action, Note: row.Note, Source: AliasConfig})
	if err != nil {
		return a, err
	}
	if prev, ok := r.actionNames.aliases[a.Alias]; ok && prev.Source != AliasConfig {
		return a, fmt.Errorf("alias %s is already an alias of %s (%s)", r.DisplayKey(a.Alias), r.DisplayKey(prev.Action), prev.Source)
	}
	return a, nil
}

// CheckUserActions reports what a load of actions.json would skip, without
// installing anything: entries that do not parse or validate, and alias
// rows that clash with an action or another alias. An entry the last load
// installed unchanged is not a problem.
func (r *Runtime) CheckUserActions() []string {
	c := r.Config()
	if c == nil {
		return nil
	}
	path := c.Path(ActionsFile)
	if path == "" {
		return nil
	}
	installed := map[string]Action{}
	for _, a := range c.actions {
		installed[a.Key()] = a
	}
	entries, problems := readActions(path)
	// The last load's alias rows stand aside while the file is checked, so
	// a row is judged against the framework's and the application's names
	// and the rows before it, not against its own earlier install.
	for _, a := range c.actionAliases {
		r.removeAlias(a.Alias)
	}
	defer func() {
		for _, a := range c.actionAliases {
			r.actionNames.aliases[a.Alias] = a
		}
	}()
	seen := map[string]int{}
	for _, f := range entries {
		if f.alias != nil {
			a, err := r.checkUserAlias(*f.alias, seen)
			if err != nil {
				problems = append(problems, aliasProblem(f.n, f.alias.Alias, err))
				continue
			}
			seen[a.Alias] = f.n
			continue
		}
		a := f.action
		if noun := r.CanonicalNoun(a.Noun); noun != a.Noun {
			a.written, a.Noun = a.Noun, noun
		}
		if prev, ok := installed[a.Key()]; ok && reflect.DeepEqual(prev, a) {
			continue
		}
		if err := a.check(r); err != nil {
			problems = append(problems, fmt.Sprintf("%s: action %d (%s): %v (skipped)", ActionsFile, f.n, a.writtenKey(), err))
		}
	}
	return problems
}

func aliasProblem(n int, alias string, err error) string {
	return fmt.Sprintf("%s: alias %d (%s): %v (skipped)", ActionsFile, n, alias, err)
}

// ActionProblems are the problems the last read of actions.json found.
func (c *Config) ActionProblems() []string { return slices.Clone(c.actionProblems) }

// EnsureUserActions installs the user's actions.json once; later calls do
// nothing. Model.Init, cli.Mount and the CLI shell call it before the user
// bindings, so a bindings.json row can name an action's verb.
func (r *Runtime) EnsureUserActions() []string {
	c := r.Config()
	if c == nil || c.actionsLoaded {
		return nil
	}
	return r.ReloadUserActions()
}

// ReloadUserActions removes the actions a previous load installed, then
// reads actions.json again and installs each valid entry as an application
// verb. An entry that does not validate (an unknown noun, a verb that
// exists, a bad template) is skipped and reported, never fatal; a file
// that does not parse installs nothing. A missing file is no actions and
// no problem. It returns the problems, each also sent to Config.Report.
func (r *Runtime) ReloadUserActions() []string {
	c := r.Config()
	if c == nil {
		return nil
	}
	c.actionsLoaded = true
	for _, a := range c.actions {
		delete(r.Operations, a.Key())
		if n := r.Nouns[a.Noun]; n != nil {
			if i := slices.Index(n.Verbs, a.Verb); i >= 0 {
				n.Verbs = slices.Delete(n.Verbs, i, i+1)
			}
		}
	}
	c.actions = nil
	for _, a := range c.actionAliases {
		r.removeAlias(a.Alias)
	}
	c.actionAliases = nil
	if c.Withdraw != nil {
		for _, p := range c.actionProblems {
			c.Withdraw(p)
		}
	}
	c.actionProblems = nil
	path := c.Path(ActionsFile)
	if path == "" {
		return nil
	}
	actions, problems := readActions(path)
	// Actions first, then aliases, so an alias row may name an action the
	// same file adds.
	for _, f := range actions {
		if f.alias != nil {
			continue
		}
		if noun := r.CanonicalNoun(f.action.Noun); noun != f.action.Noun {
			f.action.written, f.action.Noun = f.action.Noun, noun
		}
		if err := f.action.check(r); err != nil {
			problems = append(problems, fmt.Sprintf("%s: action %d (%s): %v (skipped)", ActionsFile, f.n, f.action.writtenKey(), err))
			continue
		}
		r.installAction(f.action)
	}
	seen := map[string]int{}
	for _, f := range actions {
		if f.alias == nil {
			continue
		}
		a, err := r.checkUserAlias(*f.alias, seen)
		if err != nil {
			problems = append(problems, aliasProblem(f.n, f.alias.Alias, err))
			continue
		}
		if err := r.AddActionAlias(a); err != nil {
			problems = append(problems, aliasProblem(f.n, f.alias.Alias, err))
			continue
		}
		c.actionAliases = append(c.actionAliases, a)
		seen[a.Alias] = f.n
	}
	c.actionProblems = problems
	for _, p := range problems {
		c.report(p)
	}
	return slices.Clone(problems)
}

// installAction registers one checked action.
func (r *Runtime) installAction(a Action) {
	params := map[string]Parameter{}
	for name, p := range a.Parameters {
		params[name] = p
	}
	description := a.Description
	if description == "" {
		description = "Run " + filepath.Base(a.firstWord()) + " (" + ActionsFile + ")"
	}
	r.Register(Operation{Verb: a.Verb, Noun: a.Noun, Description: description, Parameters: params, Result: "JSON value", Positional: slices.Clone(a.Positional), Quiet: a.Quiet, HistoryKind: a.History, Confirm: a.Confirm, App: a.handler()})
	r.Nouns[a.Noun].Verbs = append(r.Nouns[a.Noun].Verbs, a.Verb)
	c := r.Config()
	c.actions = append(c.actions, a)
}

func (a Action) firstWord() string {
	if len(a.Base) > 0 {
		return a.Base[0]
	}
	if len(a.Command) > 0 {
		return a.Command[0]
	}
	return ""
}

// readActions parses actions.json: a JSON array of actions, each decoded
// on its own so a bad entry (an unknown field, a wrong type) skips only
// itself.
// fileAction is a decoded action and its 1-based position in the file.
type fileAction struct {
	n      int
	action Action
	alias  *AliasRow // an alias row instead of an action
}

func readActions(path string) ([]fileAction, []string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{ActionsFile + ": " + err.Error()}
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []string{ActionsFile + ": not a JSON array of actions: " + err.Error()}
	}
	var out []fileAction
	var problems []string
	for i, one := range raw {
		var probe map[string]json.RawMessage
		if json.Unmarshal(one, &probe) == nil {
			if _, ok := probe["alias"]; ok {
				dec := json.NewDecoder(bytes.NewReader(one))
				dec.DisallowUnknownFields()
				var row AliasRow
				if err := dec.Decode(&row); err != nil {
					problems = append(problems, fmt.Sprintf("%s: alias %d: %v (skipped)", ActionsFile, i+1, err))
					continue
				}
				out = append(out, fileAction{n: i + 1, alias: &row})
				continue
			}
		}
		dec := json.NewDecoder(bytes.NewReader(one))
		dec.DisallowUnknownFields()
		var a Action
		if err := dec.Decode(&a); err != nil {
			problems = append(problems, fmt.Sprintf("%s: action %d: %v (skipped)", ActionsFile, i+1, err))
			continue
		}
		out = append(out, fileAction{n: i + 1, action: a})
	}
	return out, problems
}

func registerActions(r *Runtime) {
	r.Register(Operation{Verb: "list", Noun: "ochazuke.actions", Description: "List the actions installed from actions.json: verbs that run an external command", Result: "action[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		out := r.UserActions()
		if out == nil {
			out = []Action{}
		}
		return out, nil
	}})
	r.Register(Operation{Verb: "reload", Noun: "ochazuke.actions", Description: "Read actions.json again: its verbs replace the last load's, a bad entry is skipped and reported; the user bindings are applied again", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.ReloadUserActions()
		if r.Config().bindingsLoaded {
			problems = append(problems, r.ReloadUserBindings()...)
		}
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "check", Noun: "ochazuke.actions", Description: "Report the entries of actions.json a load would skip, alias rows that clash with an action or another alias included, without installing anything", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.CheckUserActions()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "aliases", Noun: "ochazuke.actions", Description: "List every action alias: the framework's old names, the application's aliases and the alias rows of actions.json, each with the action it runs", Result: "alias[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.ActionAliases(), nil
	}})
	r.Register(Operation{Verb: "path", Noun: "ochazuke.actions", Description: "Print the user actions file: actions.json beside bindings.json", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.Config().Path(ActionsFile)
		if path == "" {
			return nil, errors.New("this application has no config directory")
		}
		return path, nil
	}})
}
