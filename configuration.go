package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func registerConfiguration(r *Runtime) {
	r.Register(Operation{Verb: "register", Noun: "mode", Description: "Register an application-defined mode name; pending modes are exclusive and one-shot", Parameters: map[string]Parameter{"name": {Type: "string", Required: true}, "pending": {Type: "boolean"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		name := StringArg(a, "name", "")
		pending, _ := a["pending"].(bool)
		if err := r.DefineMode(name, pending); err != nil {
			return nil, err
		}
		return name, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "modes", Description: "List registered interaction modes, pending modes included", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names := make([]string, 0, len(r.Modes)+len(r.pendingModes))
		for name := range r.Modes {
			names = append(names, name)
		}
		for name := range r.pendingModes {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}})
	r.Register(Operation{Verb: "load", Noun: "bindings", Description: "Install binding rows from a JSON file: [{noun|instance, mode, keys|pattern, request|null, from, when}]", Parameters: map[string]Parameter{"path": {Type: "string", Required: true}}, Result: "binding[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		data, err := os.ReadFile(StringArg(a, "path", ""))
		if err != nil {
			return nil, err
		}
		return r.LoadBindings(data)
	}})
	r.Register(Operation{Verb: "path", Noun: "bindings", Description: "Print the user bindings file: $XDG_CONFIG_HOME/<app>/bindings.json, else ~/.config/<app>/bindings.json", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.Config().Path(BindingsFile)
		if path == "" {
			return nil, errors.New("this application has no config directory")
		}
		return path, nil
	}})
	r.Register(Operation{Verb: "reload", Noun: "bindings", Description: "Read the user bindings file again: its rows replace or unbind the defaults, a bad row is skipped and reported", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.ReloadUserBindings()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "check", Noun: "bindings", Description: "Report the rows of the user bindings file that would be skipped, and any merged row that does not validate", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.CheckUserBindings()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "select", Noun: "context", Description: "Focus an instance by id, or a nested path; omit input to leave text editing", Parameters: map[string]Parameter{"instance": {Type: "string", Description: "Instance ID: an input, list, row, page or modal body"}, "path": {Type: "string", Description: "JSON array of instance IDs, outermost first, beginning with app (when instance is omitted)"}, "input": {Type: "string"}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "instance", "")
		if id == "" {
			if _, ok := a["path"]; !ok {
				return nil, errors.New("instance or path is required")
			}
			var path []string
			if err := json.Unmarshal([]byte(StringArg(a, "path", "")), &path); err != nil {
				return nil, err
			}
			if len(path) == 0 || path[0] != "app" {
				return nil, errors.New("interaction path must start at app")
			}
			id = path[len(path)-1]
			if input := StringArg(a, "input", ""); input != "" {
				if r.Input(input) == nil || r.Instances[input].Parent != id {
					return nil, errors.New("input does not belong to this interaction path")
				}
				id = input
			}
			if got := r.InstancePath(id); len(got) == 0 || !samePath(got, path, r.Input(id) != nil) {
				return nil, fmt.Errorf("invalid path for %q", id)
			}
		}
		if err := r.SetFocus(id); err != nil {
			return nil, err
		}
		return r.Context(), nil
	}})
}

// samePath checks a declared path against an instance path, allowing the
// input itself to be omitted from the declared path.
func samePath(got, want []string, input bool) bool {
	if input {
		got = got[:len(got)-1]
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// LoadBindings installs rows from a JSON array in the binding row shape and
// returns the installed rows.
func (r *Runtime) LoadBindings(data []byte) ([]Binding, error) {
	var rows []Binding
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("bindings: %w", err)
	}
	for i, row := range rows {
		if err := r.Bind(row); err != nil {
			return nil, fmt.Errorf("bindings[%d] %s: %w", i, row.Keys+row.Pattern, err)
		}
	}
	return rows, nil
}

// configID is the single config instance under app.
const configID = "__config"

// BindingsFile and SettingsFile are the file names in the config directory.
const (
	BindingsFile = "bindings.json"
	SettingsFile = "settings.json"
)

// Config is the application's per-user configuration directory and what
// the framework loaded from it: the user bindings file (bindings.json,
// rows in the bindings.load shape that replace or unbind the defaults by
// noun, mode and keys) and the declared settings (settings.json). It is
// one instance under app, reached through Runtime.Config(), never a
// Runtime field.
//
// The directory is Dir when set, else $XDG_CONFIG_HOME/<App>, else
// ~/.config/<App>. App defaults to the binary's name (a test binary,
// NAME.test, has none), so every application gets the files without code;
// an application whose directory is named otherwise sets App before the
// Model starts (mtm sets "matomeru"). With App and Dir both empty there is
// no directory: nothing is read or written.
type Config struct {
	App string
	Dir string
	// Report, when set, receives every problem as it is found: a bindings
	// row that was skipped, a file that does not parse, an unknown or
	// invalid setting. NewModel sets it to Model.Notify, so problems are
	// persistent footer notices.
	Report func(problem string)
	// Withdraw, when set, takes back a problem Report sent once it no
	// longer holds (bindings.reload of a fixed file); NewModel sets it to
	// drop the notice.
	Withdraw func(problem string)

	// bindingsLoaded is set once the user bindings were applied (Model.Init,
	// the CLI shell); bindings.reload applies them again.
	bindingsLoaded bool
	settings       *Settings
	// applied is the undo log of the user rows installed from the file,
	// in order: reload takes them out, newest first, and restores the rows
	// each one replaced before it reads the file again.
	applied []appliedRow
	// problems are the last bindings load's problems.
	problems []string
}

// appliedRow is one installed user row and the default rows it displaced.
type appliedRow struct {
	row      Binding // normalized, as Bind installed it
	replaced []Binding
}

func registerConfig(r *Runtime) {
	inst := r.Instance(configID, Kind("config"))
	inst.Component = &Config{App: defaultAppName()}
}

// defaultAppName is the binary's base name without .exe; a go test binary
// (NAME.test) has no config directory.
func defaultAppName() string {
	if len(os.Args) == 0 {
		return ""
	}
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if strings.HasSuffix(name, ".test") {
		return ""
	}
	return name
}

// Config returns the config component.
func (r *Runtime) Config() *Config {
	c, _ := r.Instances[configID].Component.(*Config)
	return c
}

// HandleMessage: the config does no off-loop work.
func (c *Config) HandleMessage(tea.Msg) bool { return false }

// PendingCommand: nothing is ever scheduled.
func (c *Config) PendingCommand() tea.Cmd { return nil }

// Close keeps everything: the config lives as long as the runtime.
func (c *Config) Close() {}

// Directory is the config directory, "" when there is none.
func (c *Config) Directory() string {
	if c.Dir != "" {
		return c.Dir
	}
	if c.App == "" {
		return ""
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, c.App)
}

// Path is a file in the config directory, "" when there is none.
func (c *Config) Path(name string) string {
	dir := c.Directory()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// Disable gives the runtime no config directory (App and Dir empty): a
// sandbox such as an o-story session reads and writes no user files.
func (c *Config) Disable() { c.App, c.Dir = "", "" }

func (c *Config) report(problem string) {
	if c.Report != nil {
		c.Report(problem)
	}
}

// BindingProblems are the problems the last load of the user bindings
// found (rows skipped, a file that does not parse).
func (c *Config) BindingProblems() []string { return append([]string(nil), c.problems...) }

// EnsureUserBindings applies the user bindings file once; later calls do
// nothing. Model.Init and the CLI shell call it, after the application
// defined its own rows, so a file row replaces an application default.
func (r *Runtime) EnsureUserBindings() []string {
	c := r.Config()
	if c == nil || c.bindingsLoaded {
		return nil
	}
	return r.ReloadUserBindings()
}

// ReloadUserBindings takes out the rows a previous load installed (the
// rows they replaced come back), then reads the bindings file again and
// installs its rows one by one: a row that does not validate is skipped
// and reported, never fatal, and a file that does not parse installs
// nothing. A missing file is no rows and no problem. It returns the
// problems, each also sent to Config.Report.
func (r *Runtime) ReloadUserBindings() []string {
	c := r.Config()
	if c == nil {
		return nil
	}
	c.bindingsLoaded = true
	r.unapplyUserBindings()
	if c.Withdraw != nil {
		for _, p := range c.problems {
			c.Withdraw(p)
		}
	}
	c.problems = nil
	path := c.Path(BindingsFile)
	if path == "" {
		return nil
	}
	rows, problems := r.readUserBindings(path)
	for _, f := range rows {
		if err := r.applyUserRow(f.row); err != nil {
			problems = append(problems, rowProblem(f.n, f.row, err))
		}
	}
	if _, err := r.ValidateBindings(); err != nil {
		problems = append(problems, BindingsFile+": "+err.Error())
	}
	c.problems = problems
	for _, p := range problems {
		c.report(p)
	}
	return append([]string(nil), problems...)
}

// CheckUserBindings reports what a load of the bindings file would skip,
// without installing anything, and any row of the merged tables that no
// longer validates.
func (r *Runtime) CheckUserBindings() []string {
	c := r.Config()
	path := c.Path(BindingsFile)
	var problems []string
	if path != "" {
		rows, bad := r.readUserBindings(path)
		problems = bad
		for _, f := range rows {
			if _, err := r.validateRow(f.row); err != nil {
				problems = append(problems, rowProblem(f.n, f.row, err))
			}
		}
	}
	if _, err := r.ValidateBindings(); err != nil {
		problems = append(problems, err.Error())
	}
	return problems
}

func rowProblem(n int, row Binding, err error) string {
	return fmt.Sprintf("%s: row %d %s: %v (skipped)", BindingsFile, n, row.Keys+row.Pattern, err)
}

// fileRow is a decoded row and its 1-based position in the file.
type fileRow struct {
	n   int
	row Binding
}

// readUserBindings parses the file; a missing file is no rows.
func (r *Runtime) readUserBindings(path string) ([]fileRow, []string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{BindingsFile + ": " + err.Error()}
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []string{BindingsFile + ": not a JSON array of rows: " + err.Error()}
	}
	var rows []fileRow
	var problems []string
	for i, one := range raw {
		var row Binding
		if err := json.Unmarshal(one, &row); err != nil {
			problems = append(problems, fmt.Sprintf("%s: row %d: %v (skipped)", BindingsFile, i+1, err))
			continue
		}
		rows = append(rows, fileRow{i + 1, row})
	}
	return rows, problems
}

// applyUserRow installs one user row and logs the rows it replaced.
func (r *Runtime) applyUserRow(row Binding) error {
	normalized, err := r.validateRow(row)
	if err != nil {
		return err
	}
	var replaced []Binding
	for _, b := range r.tableRows(normalized) {
		if b.same(normalized) && !isPrefixCancel(b) {
			replaced = append(replaced, b)
		}
	}
	if err := r.Bind(row); err != nil {
		return err
	}
	c := r.Config()
	c.applied = append(c.applied, appliedRow{row: normalized, replaced: replaced})
	return nil
}

// tableRows are the rows of the table a row belongs to.
func (r *Runtime) tableRows(row Binding) []Binding {
	if row.Instance != "" {
		return r.Instances[row.Instance].Bindings
	}
	return r.Nouns[row.Noun].Bindings[row.Mode]
}

// unapplyUserBindings undoes the logged user rows, newest first.
func (r *Runtime) unapplyUserBindings() {
	c := r.Config()
	for i := len(c.applied) - 1; i >= 0; i-- {
		a := c.applied[i]
		restore := func(rows []Binding) []Binding {
			rows = removeRows(rows, a.row)
			return withPrefixCancels(append(rows, a.replaced...))
		}
		switch {
		case a.row.Instance != "":
			if inst := r.Instances[a.row.Instance]; inst != nil {
				inst.Bindings = restore(inst.Bindings)
			}
		default:
			if noun := r.Nouns[a.row.Noun]; noun != nil {
				noun.Bindings[a.row.Mode] = restore(noun.Bindings[a.row.Mode])
			}
		}
	}
	if len(c.applied) > 0 {
		r.CancelPendingBinding()
	}
	c.applied = nil
}
