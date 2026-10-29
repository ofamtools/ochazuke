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
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func registerConfiguration(r *Runtime) {
	r.Register(Operation{Verb: "register", Noun: "ochazuke.mode", Description: "Register an application-defined mode name; pending modes are exclusive and one-shot", Parameters: map[string]Parameter{"name": {Type: "string", Required: true}, "pending": {Type: "boolean"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		name := StringArg(a, "name", "")
		pending, _ := a["pending"].(bool)
		if err := r.DefineMode(name, pending); err != nil {
			return nil, err
		}
		return name, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "ochazuke.modes", Description: "List registered interaction modes, pending modes included", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	r.Register(Operation{Verb: "load", Noun: "ochazuke.bindings", Description: "Install binding rows from a bindings file (JSONC): the grouped format {bindings: [{context, bindings: {key: action}}]} or an array of rows", Parameters: map[string]Parameter{"path": {Type: "string", Required: true}}, Result: "binding[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		data, err := os.ReadFile(StringArg(a, "path", ""))
		if err != nil {
			return nil, err
		}
		return r.LoadBindings(data)
	}})
	r.Register(Operation{Verb: "export", Noun: "ochazuke.bindings", Description: "Write every stored key row to a file in the grouped bindings format (what config export writes as bindings.reference.json); never over the live bindings.json without force", Parameters: map[string]Parameter{
		"path":  {Type: "string", Required: true, Description: "File to write"},
		"force": {Type: "boolean", Description: "Allow writing over the live bindings.json"},
	}, Positional: []string{"path"}, Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path, err := filepath.Abs(StringArg(a, "path", ""))
		if err != nil {
			return nil, err
		}
		if force, _ := a["force"].(bool); !force && r.Config().liveFile(path) != "" {
			return nil, fmt.Errorf("%s is the live %s; export elsewhere, or pass --force to overwrite it", path, BindingsFile)
		}
		data, err := FormatBindings(r.BindingsDoc())
		if err != nil {
			return nil, err
		}
		if err := writeFileAtomic(path, data); err != nil {
			return nil, err
		}
		return path, nil
	}})
	r.Register(Operation{Verb: "path", Noun: "ochazuke.bindings", Description: "Print the user bindings file: $XDG_CONFIG_HOME/<app>/bindings.json, else ~/.config/<app>/bindings.json", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.Config().Path(BindingsFile)
		if path == "" {
			return nil, errors.New("this application has no config directory")
		}
		return path, nil
	}})
	r.Register(Operation{Verb: "reload", Noun: "ochazuke.bindings", Description: "Read the user bindings file again: its rows replace or unbind the defaults, a bad row is skipped and reported", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.ReloadUserBindings()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "check", Noun: "ochazuke.bindings", Description: "Report the rows of the user bindings file that would be skipped, any merged row that does not validate, and as warnings the file's guard rows (debts) and any row that can never answer; with debts, every guard row of every table too; the status line names the format read (grouped or rows)", Parameters: map[string]Parameter{"debts": {Type: "boolean", Description: "Also list every table's guard rows (rows with when), the debts left to move onto actions' availability"}}, Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := append(r.CheckUserBindings(), r.StandardShadows()...)
		if debts, _ := a["debts"].(bool); debts {
			lines := r.BindingDebts()
			for _, line := range lines {
				problems = append(problems, "warning: "+line)
			}
			problems = append(problems, fmt.Sprintf("warning: %d guard rows in the tables", len(lines)))
		}
		if problems == nil {
			problems = []string{}
		}
		if format := r.Config().BindingsFormat(); format != "" {
			r.UI().Info(fmt.Sprintf("%s: %s format", BindingsFile, format))
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "select", Noun: "ochazuke.context", Description: "Focus an instance by id, or a nested path; omit input to leave text editing", Parameters: map[string]Parameter{"instance": {Type: "string", Description: "Instance ID: an input, list, row, page or modal body"}, "path": {Type: "string", Description: "JSON array of instance IDs, outermost first, beginning with app (when instance is omitted)"}, "input": {Type: "string"}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	doc, _, err := ParseBindings(data)
	if err != nil {
		return nil, fmt.Errorf("bindings: %w", err)
	}
	rows := doc.Rows()
	// The file is one batch: a key repeated in a table is an alternative
	// after the first, not a replacement.
	batch := rowBatch{}
	for i, row := range rows {
		if err := r.bindRows([]Binding{row}, true, batch); err != nil {
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
	// StateDir, when set, is the state directory (the history file);
	// else $XDG_STATE_HOME/<App>, else ~/.local/state/<App>.
	StateDir string
	// NoHistoryFile keeps the action history in memory only: an
	// application opts out of history.jsonl.
	NoHistoryFile bool

	// history is the history file's writer once EnsureHistory started it.
	history        *historyStore
	historyStarted bool
	// panes are the pane dividers by page id (PanesFile), read on first
	// use.
	panes map[string]int
	// places is the record of where each noun was seen (NounPlacesFile),
	// with its writer once EnsureNounPlaces started it.
	places *placements
	// tables are the table views by id (TablesFile), read on first use.
	tables map[string]TableState
	// schemes are themes.json's valid schemes, themeProblems its last
	// read's problems; themeSetting is set when the framework declared the
	// theme setting (the application did not).
	schemes       map[string]Scheme
	themeProblems []string
	themesLoaded  bool
	themesPath    string
	themeSetting  bool
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
	// bindingsFormat is the format the last read of bindings.json found
	// (BindingsGrouped, BindingsRows; "" for no file).
	bindingsFormat string
	// actions are the entries of actions.json installed as verbs, in
	// file order; actionProblems the last read's problems.
	actions        []Action
	actionAliases  []ActionAlias // the alias rows installed from actions.json, in file order
	actionProblems []string
	actionsLoaded  bool
	// hooks are the application's files config.reload reads after the
	// framework's (OnConfigReload), in registration order.
	hooks []*configHook
	// themeListeners take the schemes again after config.reload read
	// themes.json (the appearance's).
	themeListeners []func()
	// segments are the status segments, their segments.json rows and
	// their last answers (segment.go).
	segments *segments
}

// appliedRow is one installed user row and the row with its identity it
// displaced (none when it added a row), with that row's index, so the undo
// puts it back where it stood.
type appliedRow struct {
	row      Binding // normalized, as Bind installed it
	replaced []Binding
	at       int
}

func registerConfig(r *Runtime) {
	inst := r.Instance(configID, Kind("ochazuke.config"))
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
// sandbox such as an o-story session reads and writes no user files, and
// keeps no history file.
func (c *Config) Disable() { c.App, c.Dir, c.StateDir = "", "", "" }

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
	batch := rowBatch{}
	for _, f := range rows {
		notice, err := r.applyUserRow(f.row, batch)
		switch {
		case err != nil:
			problems = append(problems, rowProblem(f.where, f.row, err))
		case notice != "":
			problems = append(problems, fmt.Sprintf("%s: %s %s: %s", BindingsFile, f.where, f.row.Keys+f.row.Pattern, notice))
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
// longer validates; then warnings ("warning: " lines): each guard row of
// the file (a debt, BindingDebts) and each merged row that can never
// answer (UnreachableBindings). A row spelled by an action alias is accepted and gets a
// line saying what it resolves to (not "skipped"). The problems of
// actions.json (CheckUserActions) follow.
func (r *Runtime) CheckUserBindings() []string {
	c := r.Config()
	path := c.Path(BindingsFile)
	var problems []string
	if path != "" {
		rows, bad := r.readUserBindings(path)
		problems = bad
		for _, f := range rows {
			row, err := r.validateRow(f.row)
			if err != nil {
				problems = append(problems, rowProblem(f.where, f.row, err))
				continue
			}
			if note := r.aliasRowNote(f.where, row); note != "" {
				problems = append(problems, note)
			}
			if row.identityGuard() != "" && !row.Unbind {
				problems = append(problems, fmt.Sprintf("warning: %s: %s %s: when %s is a guard row, a debt: the first row on a key whose action is available answers, so a row needs no condition when its action reports availability", BindingsFile, f.where, row.Keys+row.Pattern, row.When))
			}
		}
	}
	if _, err := r.ValidateBindings(); err != nil {
		problems = append(problems, err.Error())
	}
	// A row the merged tables never reach (an earlier row on its key is
	// available whenever it is) is a warning: it loads, and does nothing.
	for _, line := range r.UnreachableBindings() {
		problems = append(problems, "warning: "+line)
	}
	// actions.json is the other half of the user's verbs: its skipped
	// entries and alias clashes are reported here too.
	problems = append(problems, r.CheckUserActions()...)
	return problems
}

// aliasRowNote says what a file row spelled by an alias resolves to ("" for
// a row naming its operation directly): accepted, and named so the user
// can see it, with the new name when the alias is an old one.
func (r *Runtime) aliasRowNote(where string, row Binding) string {
	if row.Unbind || row.Request.Via == "" {
		return ""
	}
	a, ok := r.actionNames.aliases[row.Request.Via]
	if !ok {
		return ""
	}
	note := fmt.Sprintf("%s: %s %s: %s resolves to %s (an alias, %s)", BindingsFile, where, row.Keys+row.Pattern, r.DisplayKey(a.Alias), r.DisplayKey(a.Action), a.Source)
	if a.Deprecated() {
		note += "; " + a.notice(r.DisplayKey) + ": rename the row to " + r.DisplayKey(a.Action)
	}
	return note
}

func rowProblem(where string, row Binding, err error) string {
	return fmt.Sprintf("%s: %s %s: %v (skipped)", BindingsFile, where, row.Keys+row.Pattern, err)
}

// fileRow is a decoded row and where it is in the file: "row N" (the
// rows format, 1-based) or its group's context (the grouped format).
type fileRow struct {
	where string
	row   Binding
}

// BindingsFormat is the format the last read of bindings.json found:
// BindingsGrouped, BindingsRows, or "" when there was no file.
func (c *Config) BindingsFormat() string { return c.bindingsFormat }

// readUserBindings parses the file in either format (ParseBindings); a
// missing file is no rows. A row or key that cannot be read is skipped and
// reported; the rest load. The format read is kept for bindings check
// (Config.BindingsFormat).
func (r *Runtime) readUserBindings(path string) ([]fileRow, []string) {
	c := r.Config()
	c.bindingsFormat = ""
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{BindingsFile + ": " + err.Error()}
	}
	std, err := StandardizeJSONC(data)
	if err != nil {
		return nil, []string{BindingsFile + ": not a bindings file: " + err.Error()}
	}
	if trimmed := bytes.TrimSpace(std); len(trimmed) > 0 && trimmed[0] == '[' {
		c.bindingsFormat = BindingsRows
		var raw []json.RawMessage
		if err := jsonPositionError(data, json.Unmarshal(std, &raw)); err != nil {
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
			rows = append(rows, fileRow{fmt.Sprintf("row %d", i+1), row})
		}
		return rows, problems
	}
	doc, format, bad, err := parseBindingsFile(data)
	if err != nil {
		msg := err.Error()
		if !strings.HasPrefix(msg, "not a bindings file") {
			msg = "not a bindings file: " + msg
		}
		return nil, []string{BindingsFile + ": " + msg}
	}
	c.bindingsFormat = format
	var problems []string
	for _, e := range bad {
		problems = append(problems, BindingsFile+": "+e.Error())
	}
	problems = append(problems, r.fileModeProblems(doc)...)
	var rows []fileRow
	for _, g := range doc.Groups {
		for _, row := range g.Rows {
			rows = append(rows, fileRow{g.label(), row})
		}
	}
	return rows, problems
}

// fileModeProblems checks a bindings file's "modes" and "extends" against
// the application's: a user file cannot define a mode or change one's
// properties or a table's extends, so a value that differs is reported
// and ignored (the export, which writes the application's own, loads
// clean).
func (r *Runtime) fileModeProblems(doc BindingsDoc) []string {
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(doc.Modes)) {
		spec := doc.Modes[name]
		switch {
		case !r.Modes[name] && !r.pendingModes[name]:
			problems = append(problems, fmt.Sprintf("%s: modes %s: unknown mode; bindings.json cannot define one (ignored)", BindingsFile, name))
		case spec.Pending != r.pendingModes[name] || spec.Passthrough != r.passthroughModes[name]:
			problems = append(problems, fmt.Sprintf("%s: modes %s: pending %v, passthrough %v differ from the application's (ignored)", BindingsFile, name, r.pendingModes[name], r.passthroughModes[name]))
		case spec.Extends != r.modeExtends[name]:
			problems = append(problems, fmt.Sprintf("%s: modes %s: extends %q differs from the application's %q (ignored)", BindingsFile, name, spec.Extends, r.modeExtends[name]))
		case spec.Selection != r.selectionModes[name]:
			problems = append(problems, fmt.Sprintf("%s: modes %s: selection %q differs from the application's %q (ignored)", BindingsFile, name, spec.Selection, r.selectionModes[name]))
		}
	}
	for _, g := range doc.Groups {
		if g.Instance != "" || g.Extends == "" {
			continue
		}
		var have string
		if n := r.Nouns[g.Noun]; n != nil {
			have = n.Extends[g.Mode]
		}
		if have != g.Extends {
			problems = append(problems, fmt.Sprintf("%s: %s: extends %q differs from the application's %q (ignored)", BindingsFile, g.label(), g.Extends, have))
		}
	}
	return problems
}

// applyUserRow installs one user row and logs the rows it replaced. The
// notice, when not empty, says why the row will not answer everywhere it
// looks like it should: a guarded row on its keys answers first.
func (r *Runtime) applyUserRow(row Binding, batch rowBatch) (string, error) {
	normalized, err := r.validateRow(row)
	if err != nil {
		return "", err
	}
	normalized = withImplicitGuard(normalized)
	var replaced []Binding
	at := -1
	followup := batch[sourceKey(normalized)]
	if !followup {
		// The file's first row with this identity replaces every row with
		// it (its later ones are alternatives after it). A generated
		// prefix cancel is not logged: withPrefixCancels brings it back on
		// its own.
		for i, b := range r.tableRows(normalized) {
			if b.same(normalized) && !isPrefixCancel(b) {
				if at < 0 {
					at = i
				}
				replaced = append(replaced, b)
			}
		}
	}
	if err := r.bindRows([]Binding{row}, false, batch); err != nil {
		return "", err
	}
	c := r.Config()
	c.applied = append(c.applied, appliedRow{row: normalized, replaced: replaced, at: at})
	// An alternative after the file's first row on the identity says
	// nothing the first row's notice did not.
	if followup {
		return "", nil
	}
	return shadowNotice(r.tableRows(normalized), normalized, len(replaced) > 0), nil
}

// shadowNotice explains a user row that a guarded row on the same keys in
// its table answers before, so the row does not silently stand behind it:
// a capture row on its keys (it answers first in the capture pass while
// its guard holds, even when this row replaced the bubbling one), a
// guarded row ahead of a row that was added rather than replacing one,
// or, for an unbind without when, the guarded rows it left in place.
func shadowNotice(rows []Binding, row Binding, replaced bool) string {
	describe := func(b Binding) string {
		capture := ""
		if b.Capture {
			capture = ", capture"
		}
		return fmt.Sprintf("the default %s row (when %q%s)", b.Request.Key(), b.When, capture)
	}
	if row.Unbind {
		if row.identityGuard() != "" {
			return ""
		}
		for _, b := range rows {
			if b.sameKeys(row) && b.identityGuard() != "" {
				return fmt.Sprintf("unbinds only the row without when; %s stays (add \"when\": %q to unbind it too)", describe(b), b.When)
			}
		}
		return ""
	}
	at := slices.IndexFunc(rows, row.same)
	for i, b := range rows {
		if !b.sameKeys(row) || b.same(row) || b.identityGuard() == "" {
			continue
		}
		if b.Capture && !row.Capture {
			return fmt.Sprintf("%s answers first while its guard holds; to replace it, add \"when\": %q (and \"capture\": true to keep it ahead of inner rows)", describe(b), b.When)
		}
		if !replaced && i < at && b.Capture == row.Capture {
			return fmt.Sprintf("%s answers first while its guard holds; to replace it, add \"when\": %q", describe(b), b.When)
		}
	}
	return ""
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
			for k, b := range a.replaced {
				rows = insertRow(rows, a.at+k, b)
			}
			return withPrefixCancels(rows)
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
