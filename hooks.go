package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Hook rows: hooks.json in the application's own config directory.
//
// ochazuke provides the mechanism only: Emit, the event registry, the
// loader, the check and the verbs. There is no framework or global hooks
// file. The application owns its hooks config: each application's Runtime
// reads <its config dir>/hooks.json (~/.config/matomeru/hooks.json for mtm,
// ~/.config/o-json/hooks.json for o-json), merged after the application's
// own default rows and the ones its extensions declare in its catalog
// (AddDefaultHooks). Extensions have no hooks file of their own.
//
//	[
//	  {"on": "surface.left", "where": {"changed": "task"}, "do": "tasks-git.sync", "args": {"reason": "edit-session"}},
//	  {"on": "app.quitting", "where": {"changed": "task"}, "do": "tasks-git.sync", "from": {"changes": "changes"}}
//	]
//
// One verb may have many rows, and one event many rows; they run in order
// (the defaults, then the file's). A file row with the same on, where and
// do as a default replaces it in place; {"on", "do", "off": true} drops the
// defaults on that event naming that verb (with where, only those with the
// same where; without do, every default on the event).

// HooksFile is the hooks file in the config directory, HooksReferenceFile
// what hooks export writes by default (in <config dir>/reference).
const (
	HooksFile          = "hooks.json"
	HooksReferenceFile = "hooks.reference.json"
)

// HooksEnabledSetting turns every hook off (the events are still counted
// and emitted into nothing).
const HooksEnabledSetting = "hooks.enabled"

// HookAllFields, as a from value, passes the whole event (its fields as an
// object) as the argument.
const HookAllFields = "*"

// HookSourceUser is the source of hooks.json's rows.
const HookSourceUser = "user"

// HookRow attaches a verb to an event: when On is emitted and every Where
// field matches, Do runs with Args, plus each From argument read from the
// event's field it names (HookAllFields: the whole event).
//
// A where value matches a field that equals it (strings, numbers and
// booleans compared as text) or, for a list field such as changed, that
// holds it; a where value that is a list matches any of its values. That is
// all the logic there is: no expressions.
//
// Source is who declared the row: "user" for hooks.json, the application
// ("app") or the extension that declared a default. It is ignored in the
// file, so an exported file loads back as is.
type HookRow struct {
	On     string            `json:"on"`
	Where  map[string]any    `json:"where,omitempty"`
	Do     string            `json:"do,omitempty"`
	Args   Arguments         `json:"args,omitempty"`
	From   map[string]string `json:"from,omitempty"`
	Off    bool              `json:"off,omitempty"`
	Source string            `json:"source,omitempty"`
}

// Matches reports whether every where field matches fields.
func (h HookRow) Matches(fields map[string]any) bool {
	for name, want := range h.Where {
		got, ok := fields[name]
		if !ok || !whereMatches(got, want) {
			return false
		}
	}
	return true
}

func whereMatches(got, want any) bool {
	if wants, ok := want.([]any); ok {
		return slices.ContainsFunc(wants, func(w any) bool { return whereMatches(got, w) })
	}
	switch g := got.(type) {
	case []string:
		return slices.Contains(g, whereText(want))
	case []any:
		return slices.ContainsFunc(g, func(one any) bool { return whereText(one) == whereText(want) })
	}
	return whereText(got) == whereText(want)
}

func whereText(v any) string { return fmt.Sprint(v) }

// request is the row's verb as a request (its args copied).
func (h HookRow) request() (Request, error) {
	q, err := actionRequest(h.Do)
	if err != nil {
		return Request{}, err
	}
	q.Args = maps.Clone(h.Args)
	return q, nil
}

// sameHook reports whether two rows are one row for replacing: the same
// event, verb and where.
func (r *Runtime) sameHook(a, b HookRow) bool {
	return a.On == b.On && r.CanonicalKey(a.Do) == r.CanonicalKey(b.Do) && sameWhere(a.Where, b.Where)
}

func sameWhere(a, b map[string]any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return bytes.Equal(ja, jb)
}

// hookState is the hook rows: the defaults the application and its
// extensions declared, and hooks.json's rows as last read.
type hookState struct {
	defaults   []HookRow
	user       []HookRow
	loaded     bool
	loadedPath string // the hooks.json path the rows were read from
	reloadOn   bool
	// problems are the last read's problems Config.Report was sent, taken
	// back before the next read.
	problems []string
}

// AddDefaultHooks adds default hook rows, after the ones added before: the
// application's own (Source "" is "app") or an extension's (its catalog's
// rows, cli.CatalogHooks, with the extension's name as Source). The user's
// hooks.json replaces or drops them. A row needs on and do.
func (r *Runtime) AddDefaultHooks(rows ...HookRow) error {
	h := &r.eventHub().hooks
	for i, row := range rows {
		if row.On == "" || row.Do == "" || row.Off {
			return fmt.Errorf("default hook %d: a default row needs on and do (and is never off)", i+1)
		}
		if row.Source == "" || row.Source == HookSourceUser {
			row.Source = AppNoun
		}
		h.defaults = append(h.defaults, row)
	}
	return nil
}

// DefaultHooks are the declared default rows, in order.
func (r *Runtime) DefaultHooks() []HookRow { return slices.Clone(r.eventHub().hooks.defaults) }

// HookRows are the effective rows in the order they run: the defaults with
// hooks.json's rows applied (a same row replaces a default in place, an
// off row drops defaults), then the file's other rows in file order.
// hooks.json is read the first time the rows are needed and read again
// whenever the config directory is another than the one it was read from
// (an application naming its directory after an event of its setup, a
// sandbox pointing it at its own), so the rows are always the current
// directory's.
func (r *Runtime) HookRows() []HookRow {
	h := &r.eventHub().hooks
	if path := r.HooksPath(); (!h.loaded && path != "") || (h.loaded && path != h.loadedPath) {
		r.ReloadHooks()
	}
	out := slices.Clone(h.defaults)
	for _, u := range h.user {
		if u.Off {
			out = slices.DeleteFunc(out, func(d HookRow) bool {
				return d.Source != HookSourceUser && d.On == u.On && (u.Do == "" || r.CanonicalKey(d.Do) == r.CanonicalKey(u.Do)) && (len(u.Where) == 0 || sameWhere(d.Where, u.Where))
			})
			continue
		}
		if i := slices.IndexFunc(out, func(d HookRow) bool { return d.Source != HookSourceUser && r.sameHook(d, u) }); i >= 0 {
			out[i] = u
			continue
		}
		out = append(out, u)
	}
	return out
}

// hasHookOn reports whether a row runs on event.
func (r *Runtime) hasHookOn(event string) bool {
	return slices.ContainsFunc(r.HookRows(), func(h HookRow) bool { return h.On == event && !h.Off })
}

// hooksEnabled is the hooks.enabled setting (on when undeclared).
func (r *Runtime) hooksEnabled() bool {
	v, ok := r.Settings().Value(HooksEnabledSetting)
	if !ok {
		return true
	}
	on, _ := v.(bool)
	return on
}

// EnsureHooks reads the application's hooks.json once; later calls do
// nothing (HookRows reads it again when the config directory changed).
// Model.Init and the CLI shell call it after the user bindings, so a
// problem is a notice at startup. It returns the problems, each also sent to Config.Report.
func (r *Runtime) EnsureHooks() []string {
	h := &r.eventHub().hooks
	if h.loaded {
		return nil
	}
	return r.ReloadHooks()
}

// ReloadHooks reads hooks.json again: the problems of the last read are
// taken back, a row that does not validate is skipped and reported (an
// unknown event or where key is a warning; the row loads), a file that does
// not parse loads no rows. A missing file is no rows and no problem.
// config.reload reads it too.
func (r *Runtime) ReloadHooks() []string {
	c := r.Config()
	h := &r.eventHub().hooks
	h.loaded, h.loadedPath = true, r.HooksPath()
	if !h.reloadOn && c != nil {
		h.reloadOn = true
		r.OnConfigReload(HooksFile, func() []string {
			r.withdrawHookProblems()
			problems := r.loadHooks()
			return problems
		})
	}
	r.withdrawHookProblems()
	problems := r.loadHooks()
	h.problems = problems
	if c != nil {
		for _, p := range problems {
			c.report(p)
		}
	}
	return problems
}

func (r *Runtime) withdrawHookProblems() {
	c := r.Config()
	h := &r.eventHub().hooks
	if c != nil && c.Withdraw != nil {
		for _, p := range h.problems {
			c.Withdraw(p)
		}
	}
	h.problems = nil
}

// loadHooks reads the file into the user rows and returns its problems.
func (r *Runtime) loadHooks() []string {
	h := &r.eventHub().hooks
	h.user = nil
	rows, problems := r.readHooksFile()
	for _, f := range rows {
		bad, skip := r.checkHook(f.row)
		for _, b := range bad {
			problems = append(problems, hookProblem(HooksFile+": "+f.where, f.row, b))
		}
		if skip {
			continue
		}
		f.row.Source = HookSourceUser
		h.user = append(h.user, f.row)
	}
	return problems
}

// fileHook is a decoded row and where it is ("row N").
type fileHook struct {
	where string
	row   HookRow
}

// HooksPath is the application's hooks.json, "" when it has no config
// directory.
func (r *Runtime) HooksPath() string {
	if c := r.Config(); c != nil {
		return c.Path(HooksFile)
	}
	return ""
}

// readHooksFile parses hooks.json (JSONC): an array of rows, or an object
// {"hooks": [rows]}. A row with an unknown field is skipped.
func (r *Runtime) readHooksFile() ([]fileHook, []string) {
	path := r.HooksPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{HooksFile + ": " + err.Error()}
	}
	return parseHooks(data)
}

func parseHooks(data []byte) ([]fileHook, []string) {
	std, err := StandardizeJSONC(data)
	if err != nil {
		return nil, []string{HooksFile + ": not a hooks file: " + err.Error()}
	}
	var raw []json.RawMessage
	if trimmed := bytes.TrimSpace(std); len(trimmed) > 0 && trimmed[0] == '{' {
		var doc struct {
			Hooks []json.RawMessage `json:"hooks"`
		}
		if err := jsonPositionError(data, json.Unmarshal(std, &doc)); err != nil {
			return nil, []string{HooksFile + ": not a hooks file: " + err.Error()}
		}
		raw = doc.Hooks
	} else if len(bytes.TrimSpace(std)) > 0 {
		if err := jsonPositionError(data, json.Unmarshal(std, &raw)); err != nil {
			return nil, []string{HooksFile + ": not a JSON array of hook rows: " + err.Error()}
		}
	}
	var rows []fileHook
	var problems []string
	for i, one := range raw {
		var row HookRow
		dec := json.NewDecoder(bytes.NewReader(one))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&row); err != nil {
			problems = append(problems, fmt.Sprintf("%s: row %d: %v (skipped)", HooksFile, i+1, err))
			continue
		}
		rows = append(rows, fileHook{fmt.Sprintf("row %d", i+1), row})
	}
	return rows, problems
}

// hookProblem is one report line; a warning starts "warning: ".
func hookProblem(where string, row HookRow, p string) string {
	what := row.On
	if row.Do != "" {
		what += " → " + row.Do
	}
	if rest, ok := strings.CutPrefix(p, "warning: "); ok {
		return fmt.Sprintf("warning: %s: %s: %s", where, what, rest)
	}
	return fmt.Sprintf("%s: %s: %s (skipped)", where, what, p)
}

// checkHook checks one row against the runtime: the problems (warnings
// start "warning: ") and whether the row cannot load. An unknown event or
// where key is a warning, since the application may emit an event it never
// declared; a missing on or do, an unknown verb, an argument the verb does
// not take and a bad from are errors.
func (r *Runtime) checkHook(row HookRow) (problems []string, skip bool) {
	if row.On == "" {
		return []string{"a row needs on (the event)"}, true
	}
	spec, known := r.Event(row.On)
	if !known {
		problems = append(problems, fmt.Sprintf("warning: unknown event %q (events list shows the declared ones)", row.On))
	}
	for _, name := range slices.Sorted(maps.Keys(row.Where)) {
		if known && !spec.Field(name) {
			problems = append(problems, fmt.Sprintf("warning: where key %q is not a field of %s", name, row.On))
		}
	}
	if row.Off {
		if len(row.Args) > 0 || len(row.From) > 0 {
			problems = append(problems, "warning: an off row runs nothing; its args and from are ignored")
		}
		return problems, false
	}
	if row.Do == "" {
		return append(problems, "a row needs do (the verb, noun.verb)"), true
	}
	op, err := r.hookOperation(row.Do)
	if err != nil {
		return append(problems, err.Error()), true
	}
	if err := validatePartialArguments(op, row.Args); err != nil {
		return append(problems, err.Error()), true
	}
	for _, arg := range slices.Sorted(maps.Keys(row.From)) {
		if _, ok := op.Parameters[arg]; !ok {
			return append(problems, fmt.Sprintf("%s has no argument %q (from)", op.Key(), arg)), true
		}
		if field := row.From[arg]; field != HookAllFields && known && !spec.Field(field) {
			problems = append(problems, fmt.Sprintf("warning: from %s: %q is not a field of %s", arg, field, row.On))
		}
	}
	return problems, false
}

// hookOperation is the operation a row's do names, through aliases.
func (r *Runtime) hookOperation(do string) (Operation, error) {
	if op, ok := r.Operations[r.CanonicalKey(do)]; ok {
		return op, nil
	}
	q, err := actionRequest(do)
	if err != nil {
		return Operation{}, err
	}
	q = r.modernRequest(r.canonicalRequest(q))
	if op, ok := r.Operations[q.Key()]; ok {
		return op, nil
	}
	return Operation{}, fmt.Errorf("unknown verb %q", do)
}

// CheckHooks reports what hooks check does: the rows of hooks.json that
// would be skipped or warn, and the same for the defaults (named by their
// source), without changing the loaded rows.
func (r *Runtime) CheckHooks() []string {
	rows, problems := r.readHooksFile()
	for _, f := range rows {
		bad, _ := r.checkHook(f.row)
		for _, b := range bad {
			problems = append(problems, hookProblem(HooksFile+": "+f.where, f.row, b))
		}
	}
	for i, row := range r.DefaultHooks() {
		bad, _ := r.checkHook(row)
		for _, b := range bad {
			problems = append(problems, hookProblem(fmt.Sprintf("default %d (%s)", i+1, row.Source), row, b))
		}
	}
	if problems == nil {
		problems = []string{}
	}
	return problems
}

// ExportHooks writes every effective row (HookRows, each with its source)
// to path, one row per line, in a form hooks.json reads back; "" is
// <config dir>/reference/hooks.reference.json. It never writes over the
// live hooks.json without force.
func (r *Runtime) ExportHooks(path string, force bool) (string, error) {
	if path == "" {
		ref := r.Config().Paths().Reference
		if ref == "" {
			return "", errors.New("this application has no config directory; give a path")
		}
		path = filepath.Join(ref, HooksReferenceFile)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if live := r.HooksPath(); !force && live != "" && filepath.Base(path) == HooksFile && resolveDir(filepath.Dir(path)) == resolveDir(filepath.Dir(live)) {
		return "", fmt.Errorf("%s is the live %s; export elsewhere, or pass --force to overwrite it", path, HooksFile)
	}
	rows := r.HookRows()
	if rows == nil {
		rows = []HookRow{}
	}
	data, err := FormatRows(rows)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// registerHooks registers the hooks and events nouns' verbs.
func registerHooks(r *Runtime) {
	r.Register(Operation{Verb: "list", Noun: "ochazuke.hooks", Description: "List the hook rows in the order they run: the application's and its extensions' defaults with hooks.json applied, each with its source", Parameters: map[string]Parameter{
		"event": {Type: "string", Description: "Only the rows on this event"},
	}, Positional: []string{"event"}, Result: "hook[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.EnsureHooks()
		event := StringArg(a, "event", "")
		out := []HookRow{}
		for _, row := range r.HookRows() {
			if event == "" || row.On == event {
				out = append(out, row)
			}
		}
		return out, nil
	}})
	r.Register(Operation{Verb: "check", Noun: "ochazuke.hooks", Description: "Report the hook rows that would be skipped (no on or do, an unknown verb, an argument the verb does not take) and as warnings the rows on an unknown event or with a where key or from field the event does not carry; hooks.json's rows and the defaults", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.CheckHooks(), nil
	}})
	r.Register(Operation{Verb: "export", Noun: "ochazuke.hooks", Description: "Write every hook row (the defaults with hooks.json applied, each with its source) to a file hooks.json reads back, default <config dir>/reference/hooks.reference.json; never over the live hooks.json without force", Parameters: map[string]Parameter{
		"path":  {Type: "string", Description: "File to write (default <config dir>/reference/hooks.reference.json)"},
		"force": {Type: "boolean", Description: "Allow writing over the live hooks.json"},
	}, Positional: []string{"path"}, Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.EnsureHooks()
		force, _ := a["force"].(bool)
		return r.ExportHooks(StringArg(a, "path", ""), force)
	}})
	r.Register(Operation{Verb: "reload", Noun: "ochazuke.hooks", Description: "Read hooks.json again: a bad row is skipped and reported", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.ReloadHooks()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "path", Noun: "ochazuke.hooks", Description: "Print the application's hooks file: <config dir>/hooks.json ($XDG_CONFIG_HOME/<app>/hooks.json, else ~/.config/<app>/hooks.json)", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.HooksPath()
		if path == "" {
			return nil, errors.New("this application has no config directory")
		}
		return path, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "ochazuke.events", Description: "List the declared events (the framework's surface.opened, surface.left and app.quitting, then the application's and its extensions'), with their fields", Result: "event[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Events(), nil
	}})
	r.Register(Operation{Verb: "emit", Noun: "ochazuke.events", Description: "Emit an event: the hook rows on it whose where matches the fields run", Parameters: map[string]Parameter{
		"event":  {Type: "string", Required: true, Description: "The event's name (noun.happening)"},
		"fields": {Type: "string", Description: "The event's fields as a JSON object"},
	}, Positional: []string{"event"}, Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		event := StringArg(a, "event", "")
		if event == "" {
			return nil, errors.New("event is required")
		}
		fields := map[string]any{}
		switch v := a["fields"].(type) {
		case nil:
		case string:
			if strings.TrimSpace(v) != "" {
				if err := json.Unmarshal([]byte(v), &fields); err != nil {
					return nil, fmt.Errorf("fields: %w", err)
				}
			}
		case map[string]any:
			fields = v
		default:
			return nil, errors.New("fields is a JSON object")
		}
		r.Emit(event, fields)
		return true, nil
	}})
}
