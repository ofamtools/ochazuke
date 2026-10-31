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

	"ofam.dev/ochazuke/refdoc"
)

// ReferenceDir is the directory config.export writes to by default, inside
// the config directory.
const ReferenceDir = "reference"

// The files config.export writes.
const (
	SettingsReferenceFile = "settings.reference.json"
	BindingsReferenceFile = "bindings.reference.json"
	PropsReferenceFile    = "props.reference.json"
	ActionsExampleFile    = "actions.example.json"
	ThemesExampleFile     = "themes.example.json"
	ReferenceReadme       = "README.md"
)

// ConfigPaths are where the application's user files live: Config is the
// config directory (bindings.json, settings.json, actions.json,
// themes.json), State the state directory (history.jsonl, panes.json) and
// Reference the directory config.export writes to by default. A directory
// the application has none of is "".
type ConfigPaths struct {
	Config    string `json:"config"`
	State     string `json:"state"`
	Reference string `json:"reference"`
}

// Paths are the config, state and default reference directories.
func (c *Config) Paths() ConfigPaths {
	out := ConfigPaths{Config: c.Directory(), State: c.StateDirectory()}
	if out.Config != "" {
		out.Reference = filepath.Join(out.Config, ReferenceDir)
	}
	return out
}

// ConfigExport is what config.export wrote: the directory, the files in
// it (absolute paths, in the order written) and where the live files are.
type ConfigExport struct {
	Dir    string   `json:"dir"`
	Files  []string `json:"files"`
	Config string   `json:"config"`
	State  string   `json:"state"`
}

// exportFile is one file of an export: its name in the directory and its
// content.
type exportFile struct {
	name string
	data []byte
}

// ExportConfig writes a complete reference of the user configuration into
// dir ("" is <config dir>/reference): settings.json with every declared
// setting at its current value, settings.reference.json with each
// setting's type, default, choices, description and value, and
// bindings.reference.json with every stored table in the grouped
// bindings format (FormatBindings of BindingsDoc), settings.reference.json
// and actions.example.json one row per line (FormatRows). Each file is written
// atomically, a temp file renamed over it. A file that would land on one
// of the live config files (dir is the config directory) is refused unless
// force is set, and then nothing is written.
func (r *Runtime) ExportConfig(dir string, force bool) (ConfigExport, error) {
	c := r.Config()
	r.EnsureConfigSettings()
	paths := c.Paths()
	if dir == "" {
		dir = paths.Reference
	}
	if dir == "" {
		return ConfigExport{}, errors.New("this application has no config directory: name a directory to export to")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ConfigExport{}, err
	}
	files, err := r.exportFiles()
	if err != nil {
		return ConfigExport{}, err
	}
	if !force {
		for _, f := range files {
			if live := c.liveFile(filepath.Join(abs, f.name)); live != "" {
				return ConfigExport{}, fmt.Errorf("%s is the live %s; export elsewhere, or pass --force to overwrite it", filepath.Join(abs, f.name), live)
			}
		}
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return ConfigExport{}, err
	}
	out := ConfigExport{Dir: abs, Files: []string{}, Config: paths.Config, State: paths.State}
	for _, f := range files {
		path := filepath.Join(abs, f.name)
		if err := writeFileAtomic(path, f.data); err != nil {
			return out, err
		}
		out.Files = append(out.Files, path)
		r.RecordWrittenFile(path)
	}
	return out, nil
}

// exportFiles are the files of an export, in the order they are written.
func (r *Runtime) exportFiles() ([]exportFile, error) {
	settings := r.Settings().List()
	current, err := settingsObject(settings)
	if err != nil {
		return nil, err
	}
	reference, err := FormatRows(settings)
	if err != nil {
		return nil, err
	}
	bindings, err := FormatBindings(r.BindingsDoc())
	if err != nil {
		return nil, err
	}
	actions, err := FormatRowsJSON([]byte(actionsExample))
	if err != nil {
		return nil, err
	}
	props, err := r.propsReference()
	if err != nil {
		return nil, err
	}
	return []exportFile{
		{ReferenceReadme, r.referenceReadme(len(settings), len(r.allBindings()))},
		{SettingsFile, current},
		{SettingsReferenceFile, reference},
		{BindingsReferenceFile, bindings},
		{PropsReferenceFile, props},
		{ActionsExampleFile, actions},
		{ThemesExampleFile, themesExample()},
	}, nil
}

// propsReference is props.reference.json: the reference model's standard
// verbs and nouns with props (ReferenceModel, the standard-verbs page's
// data), one noun per line.
func (r *Runtime) propsReference() ([]byte, error) {
	m := r.ReferenceModel()
	nouns := m.Nouns
	if nouns == nil {
		nouns = []refdoc.Noun{}
	}
	var b strings.Builder
	b.WriteString("{\n  \"standardVerbs\": [\n")
	for i, v := range m.StandardVerbs {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		b.WriteString("    " + string(data))
		if i < len(m.StandardVerbs)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ],\n  \"nouns\": [\n")
	for i, n := range nouns {
		data, err := json.Marshal(n)
		if err != nil {
			return nil, err
		}
		b.WriteString("    " + string(data))
		if i < len(nouns)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return []byte(b.String()), nil
}

// actionPlaceholderHelp describes every template name an action may use,
// in the order the README lists them; a test keeps it equal to
// actionPlaceholders.
var actionPlaceholderHelp = [][2]string{
	{"target", "the id of the instance the verb runs on"},
	{"id", "the same as target"},
	{"label", "the instance's label, else its id"},
	{"noun", "the action's noun"},
	{"verb", "the action's verb"},
	{"data", "the instance's Data: a string as is, else JSON"},
	{"data.KEY", "one field of Data when Data is a JSON object"},
	{"args", "the arguments as one JSON object"},
	{"PARAMETER", "the value of a parameter the entry declares (by its name)"},
}

// referenceReadme is the export directory's README.md: what each file is,
// where the live files are, and why copying defaults pins them.
func (r *Runtime) referenceReadme(settings, rows int) []byte {
	c := r.Config()
	app := c.App
	if app == "" {
		app = "APP"
	}
	paths := c.Paths()
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	line("# %s configuration reference", app)
	line("")
	line("Written by `%s config export`. Nothing in this directory is read by %s: it is a reference to copy from. Export again to refresh it; the files here are replaced.", app, app)
	line("")
	line("## Live files")
	line("")
	if paths.Config == "" {
		line("This application has no config directory, so it reads none of these files.")
	} else {
		line("%s reads these from the config directory `%s` (`%s config path` prints it):", app, paths.Config, app)
		line("")
		line("| File | What it holds | Reload |")
		line("|---|---|---|")
		line("| `%s` | Key rows that replace (same noun or instance, mode and keys) or unbind (`\"request\": null`) the defaults | `%s bindings reload`, or restart |", BindingsFile, app)
		line("| `%s` | Values of the declared settings, by name | read again when it changes |", SettingsFile)
		line("| `%s` | Verbs that run external commands | `%s actions reload`, or restart |", ActionsFile, app)
		line("| `%s` | Colour schemes the `%s` setting can name | `%s themes reload`, or restart |", ThemesFile, ThemeSetting, app)
		line("")
		line("`%s config reload` reads all of them again at once.", app)
	}
	if paths.State != "" {
		line("")
		line("The state directory `%s` holds `%s` (the action history) and `%s` (pane dividers). %s writes them; they are not configuration.", paths.State, HistoryFile, PanesFile, app)
	}
	line("")
	line("## Files here")
	line("")
	line("| File | What it is | Use |")
	line("|---|---|---|")
	line("| `%s` | Every declared setting (%d) at its current value | Copy the lines you want to change into the live `%s` |", SettingsFile, settings, SettingsFile)
	line("| `%s` | Each setting's type, default, allowed values (`enum`), description and current value (`set` is true when the live file holds it) | Read it to see what a setting takes |", SettingsReferenceFile)
	line("| `%s` | Every key row (%d) of every noun, instance and mode, in the grouped `bindings.json` format (`%s bindings export`): one group per `noun/mode` context, a key per line | Copy a group, or a key line into a group, into the live `%s` and change its keys or action |", BindingsReferenceFile, rows, app, BindingsFile)
	line("| `%s` | The standard verbs (what each needs of a store) and every noun with props: its store's kind, each prop's type, flags and default, its verbs (`standard` names the standard verb a verb was generated from); the reference's `standard-verbs` page as data | Read it to see what `NOUN set PROP VALUE` and the other standard verbs take |", PropsReferenceFile)
	line("| `%s` | Two example actions on the `app` noun that use every field | Adapt an entry into the live `%s` |", ActionsExampleFile, ActionsFile)
	line("| `%s` | Two example colour schemes using every role | Adapt a scheme into the live `%s` |", ThemesExampleFile, ThemesFile)
	line("")
	line("## Copying defaults pins them")
	line("")
	line("A value or row in a live file overrides the default from then on. If you copy a whole file in, every default it holds is pinned: when a later version of %s changes a default, you keep the old one. Copy only what you change.", app)
	line("")
	line("The bindings reference shows the rows as they are now, the live `%s` rows included. A row in `%s` replaces the row with the same noun or instance, mode, keys and `when` (the editor's `when: input` aside, which is the framework's): to change a row that has a `when`, copy it with its `when`. A row without `when` replaces only the unguarded row on its keys, and a `null` value (or `{\"when\": ..., \"unbind\": true}`) unbinds only the row with the same `when`. Config files are JSONC: `//` and `/* */` comments and trailing commas are fine.", BindingsFile, BindingsFile)
	line("")
	line("## Action templates")
	line("")
	line("`{{name}}` in `command`, `base`, `target`, `label` and `dir` expands to:")
	line("")
	for _, p := range actionPlaceholderHelp {
		line("- `{{%s}}`: %s", p[0], p[1])
	}
	line("")
	line("An argv word that is one placeholder and expands to nothing is left out. Parameters are `string`, `integer` or `boolean`; `required`, `enum`, `source` (an operation, noun.verb, listing more choices), `sourceArgs` (source parameter to sibling field; empty for no dependencies), `free` (accept a value outside them) and `description` are optional.")
	line("")
	line("## Theme roles")
	line("")
	line("`background` is `adaptive`, `light` or `dark`. `text`, `muted`, `accent`, `border`, `surface` and `error` are each one colour or `{\"light\": ..., \"dark\": ...}`; a colour is `#RRGGBB`, `#RGB` or an ANSI index `0` to `255`. A role left out keeps the built-in palette's. `error` must differ from `accent` and `muted`. The built-in themes are %s.", strings.Join(BuiltinThemes, ", "))
	line("")
	line("## Chrome colour")
	line("")
	line("The `%s` setting (%s) says how much colour the chrome draws. Each level is a family of tones named `chrome.<level>.<role>`, so a theme's `tones` change any of it: `{\"adaptive\": {\"tones\": {\"chrome.rich.border.state.done\": \"border\"}}}` draws done panes in the plain border colour again. A role with no row at the level draws as before the levels; `off` has no rows. A chrome row's colour may name the role `mode`, the current mode's badge colour; an application's accent replaces `accent`, `border` and `mode` in chrome rows. The roles:", ChromeColorSetting, strings.Join(ChromeLevels, ", "))
	line("")
	line("| Role | Draws |")
	line("|---|---|")
	for _, r := range chromeRoleHelp {
		line("| `%s` | %s |", r[0], r[1])
	}
	line("")
	line("A pane's border takes the first of: `border.<tone>` (its state), the application's accent, `border.focused` (focused), `border`, the theme's border. Focus is also marked by heavy lines. The badge takes the first of `mode.<name>`, `mode.pending` (a pending mode), `mode.<extends>`, `mode`. The state tones read as ok (`state.done`), warn (`state.needs-input`), error (`state.errored`) and busy (`state.working`).")
	line("")
	line("Compared with Vim: the roles are Vim's highlight groups for the chrome. `border` and `border.split` are `hl-WinSeparator`/`hl-VertSplit`, `border.modal` and `border.completion` Neovim's `hl-FloatBorder`, the title and footer rows `hl-StatusLine` (`hl-StatusLineNC` is the `dim-unfocused` setting). Vim has one `hl-ModeMsg` colour for every mode; a colour per mode (`mode.<name>`) is what statusline plugins add, and a border coloured by its pane's state (`border.<tone>`) is like tmux's `pane-active-border-style`, not Vim. themes.json with `themes reload` is `:colorscheme` and `:highlight`; the rows are edited in the file. Selection stays reverse video, unlike `hl-Visual`.")
	return []byte(b.String())
}

// chromeRoleHelp names each chrome role and what it draws (the themes
// reference's "Chrome colour" table).
var chromeRoleHelp = [][2]string{
	{"border", "an unfocused panel's border (rich: a teal)"},
	{"border.focused", "a focused panel's border (rich: the mode's colour, bold)"},
	{"border.<tone>", "a panel or card given that tone, e.g. `border.state.working` (rich: every agent state; subtle: needs-input and errored)"},
	{"border.modal", "a modal's border (rich: the mode's colour)"},
	{"border.completion", "the completion popup's border (rich: the mode's colour)"},
	{"border.split", "the path picker's split rule"},
	{"panel.title", "an unfocused panel's title"},
	{"panel.title.focused", "a focused panel's title (rich: the mode's colour)"},
	{"title.mark", "the title row's ◆"},
	{"title", "the application's title"},
	{"mode.<name>", "the mode badge in that mode, e.g. `mode.insert`, `mode.visual-line` (rich fills the badge, subtle colours its label)"},
	{"mode.pending", "the badge in a pending mode with no row of its own"},
	{"mode", "the badge in a mode with no row"},
	{"hint.key", "a key on a hint line, the footer's and a modal's (rich: the accent, bold; subtle: the text colour)"},
	{"hint.text", "what the key does"},
	{"hint.sep", "the ` · ` between hints"},
}

// actionsExample is actions.example.json: one entry in the command form,
// one in the base form and a foreground one, together every field an
// action takes, and an alias row. Both go
// on the app noun, which every application has, under verbs no
// application defines, so the file installs as it is.
const actionsExample = `[
  {
    "noun": "app",
    "verb": "example-run",
    "description": "Example: run a program with the instance's fields as argv words",
    "command": ["printf", "%s %s %s\\n", "{{label}}", "{{mode}}", "{{data.path}}"],
    "json": true,
    "label": "example {{label}}",
    "confirm": "Run on {{label}} ({{mode}})?",
    "parameters": {
      "mode": {"type": "string", "required": true, "enum": ["fast", "full"], "description": "How to run"},
      "count": {"type": "integer", "description": "How many times"},
      "verbose": {"type": "boolean", "description": "Say more"}
    },
    "positional": ["mode", "count"],
    "dir": "{{data.dir}}",
    "timeout": "30s",
    "quiet": false
  },
  {
    "noun": "app",
    "verb": "example-call",
    "description": "Example: call another ochazuke CLI as BASE NOUN VERB --json ARGS --target TARGET",
    "base": ["other-app"],
    "call": "item show",
    "target": "{{data.id}}",
    "label": "{{noun}}.{{verb}} {{target}}",
    "parameters": {
      "name": {"type": "string", "source": "settings.names", "sourceArgs": {}, "free": true, "description": "A setting name; free accepts one the source does not list"},
      "page": {"type": "string", "noun": "pages", "description": "A page; noun lists the noun's rows (pages.list) or its live instances"},
      "out": {"type": "path", "path": "file", "base": "~", "new": true, "description": "A file to write; a path field opens the path chooser (browse from base, recent, typed); new allows a file not there yet"}
    },
    "quiet": true,
    "history": "fetch"
  },
  {
    "noun": "app",
    "verb": "example-open",
    "description": "Example: hand the terminal to a full-screen program; the TUI resumes when it exits",
    "command": ["less", "{{data.path}}"],
    "parameters": {
      "search": {"type": "string", "rest": true, "description": "Text to look for; rest takes the rest of the palette line as typed (the CLI joins the remaining words)"}
    },
    "foreground": true
  },
  {"alias": "example-open!", "action": "app.example-open", "args": {}, "shadows": false, "palette": false, "paramAliases": [{"alias": "query", "param": "search", "since": "v1", "removeAfter": "v2", "note": "the old name of search"}], "note": "Example: an alias row; args presets the action's arguments; paramAliases keeps old parameter names working (--query for --search, with a one-time notice); shadows lets a bare alias take a verb's name and yield to NOUN VERB; palette keeps a bare alias to the command line in the app (the CLI leaves the word to its own commands)"}
]
`

// themesExample is themes.example.json: "example" is the built-in palette
// with every role as a light and a dark colour and every built-in tone, "example-dark" a dark
// scheme with one colour per role, an ANSI index among them.
func themesExample() []byte {
	var b bytes.Buffer
	pair := func(c SchemeColor) string { return fmt.Sprintf(`{"light": %q, "dark": %q}`, c.Light, c.Dark) }
	s := defaultScheme
	b.WriteString("{\n  \"example\": {\n")
	fmt.Fprintf(&b, "    \"background\": %q,\n", s.Background)
	roles := []struct {
		name string
		c    SchemeColor
	}{{"text", s.Text}, {"muted", s.Muted}, {"accent", s.Accent}, {"border", s.Border}, {"surface", s.Surface}, {"error", s.Error}}
	for _, role := range roles {
		fmt.Fprintf(&b, "    %q: %s,\n", role.name, pair(role.c))
	}
	b.WriteString("    \"tones\": {\n")
	names := slices.Sorted(maps.Keys(defaultTones))
	for i, name := range names {
		t := defaultTones[name]
		fg := pair(t.Fg)
		if t.Fg.Light == t.Fg.Dark {
			fg = fmt.Sprintf("%q", t.Fg.Light)
		}
		fmt.Fprintf(&b, "      %q: {\"fg\": %s, \"bold\": %t, \"dim\": %t}", name, fg, t.Bold, t.Dim)
		if i < len(names)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(`    }
  },
  "example-dark": {
    "background": "dark",
    "text": "#DCE9E3",
    "muted": "#93A7A1",
    "accent": "#C9A227",
    "border": "240",
    "surface": "#172720",
    "error": "#FF8A80",
    "tones": {"state.working": "#5FAFD7", "build.cached": {"fg": "muted", "dim": true}}
  }
}
`)
	return b.Bytes()
}

// settingsObject is settings.json holding every setting at its value, in
// declaration order.
func settingsObject(settings []SettingValue) ([]byte, error) {
	if len(settings) == 0 {
		return []byte("{}\n"), nil
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, one := range settings {
		name, err := json.Marshal(one.Name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(one.Value)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "  %s: %s", name, value)
		if i < len(settings)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}

// liveFiles are the files the framework reads from the config directory.
var liveFiles = []string{BindingsFile, SettingsFile, ActionsFile, ThemesFile}

// liveFile is the name of the live config file path is, "" when it is
// none: the same file name in the config directory, compared after
// resolving symbolic links in both directories.
func (c *Config) liveFile(path string) string {
	name := filepath.Base(path)
	if !slices.Contains(liveFiles, name) {
		return ""
	}
	dir := c.Directory()
	if dir == "" {
		return ""
	}
	if resolveDir(filepath.Dir(path)) == resolveDir(dir) {
		return name
	}
	return ""
}

// resolveDir is a directory as an absolute path with its symbolic links
// resolved, as far as it exists.
func resolveDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	var rest []string
	for p := abs; ; p = filepath.Dir(p) {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			slices.Reverse(rest)
			return filepath.Join(append([]string{real}, rest...)...)
		}
		if parent := filepath.Dir(p); parent == p {
			return abs
		}
		rest = append(rest, filepath.Base(p))
	}
}

// writeFileAtomic replaces path with data: a temp file in the same
// directory, renamed over it, so a reader sees the old file or the new
// one, never part of either.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func registerConfigExport(r *Runtime) {
	r.Register(Operation{Verb: "path", Noun: "ochazuke.config", Description: "Print the config directory (bindings.json, settings.json, actions.json, themes.json), the state directory (history.jsonl, panes.json) and the default config export directory", Result: "paths", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		paths := r.Config().Paths()
		if paths.Config == "" && paths.State == "" {
			return nil, errors.New("this application has no config or state directory")
		}
		return paths, nil
	}})
	r.Register(Operation{Verb: "export", Noun: "ochazuke.config", Description: "Write a reference of the user configuration (every setting, every key row) into a directory, default <config dir>/reference; never over the live files without force", Parameters: map[string]Parameter{
		"dir":   {Type: "string", Description: "Directory to write to (default <config dir>/reference)"},
		"force": {Type: "boolean", Description: "Allow writing over a live config file (dir is the config directory)"},
	}, Positional: []string{"dir"}, Result: "export", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		force, _ := a["force"].(bool)
		return r.ExportConfig(StringArg(a, "dir", ""), force)
	}})
}
