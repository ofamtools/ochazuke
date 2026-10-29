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
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/hujson"

	tea "charm.land/bubbletea/v2"
)

// Setting types: the value a setting holds, as its getter returns it.
const (
	SettingString   = "string"   // string; Enum limits it
	SettingInteger  = "integer"  // int
	SettingNumber   = "number"   // float64
	SettingBoolean  = "boolean"  // bool
	SettingDuration = "duration" // time.Duration, written as "400ms"
	SettingList     = "list"     // []string, written as a JSON array
)

// Setting is one declared user preference: its name, type, default, the
// line the settings list shows and, for a string, the allowed values.
// Settings are not for secrets: the file is plain JSON in the config
// directory.
type Setting struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Default     any      `json:"default"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	// Section files the setting under a section of the Settings page; ""
	// takes the name before its last dot ("ext.ai.poll-every" is in
	// "ext.ai"), and a name without a dot is in SettingsGeneral
	// (SectionName).
	Section string `json:"section,omitempty"`
	// Validate, when set, checks a value of the right type further: Set
	// refuses a value it rejects and a file value it rejects reads the
	// default, as a value of the wrong type does.
	Validate func(value any) error `json:"-"`
	// Deprecated are older names of the setting, kept for one release:
	// settings.get, set and reset, Value and settings.json read them as
	// this setting, each warning once, and a write moves the file's value
	// to this name. A declaration under an old name declares this one.
	Deprecated []string `json:"deprecated,omitempty"`
}

// SettingValue is a setting with its current value, as settings.list and
// settings.get answer: Value and Default in their file form (a duration
// as "400ms"), Set when settings.json holds a value for it.
type SettingValue struct {
	Setting
	Value any  `json:"value"`
	Set   bool `json:"set"`
}

// Settings holds the declared settings and their values from settings.json
// in the config directory. It is part of the config component, reached
// through Runtime.Settings(). The file is read on first use (and again
// when the config directory changes), written atomically (a temp file
// renamed over it) on every Set and Reset, and keeps every key it holds:
// an unknown or invalid key is reported and written back unchanged, never
// dropped. Without a config directory the values last for the session.
// Model polls the file (SettingsPoll) so a value another process
// set (the CLI) reaches a running TUI.
type Settings struct {
	config    *Config
	declared  []Setting
	values    map[string]any             // valid declared values from the file, typed
	raw       map[string]json.RawMessage // the file as read, unknown keys included
	path      string                     // the file the values came from
	loaded    bool
	broken    error // the file does not parse: nothing is written over it
	stamp     fileStamp
	observers []settingObserver
	problems  []string
	page      settingsPage    // the Settings page (settings_view.go)
	warned    map[string]bool // deprecated names already warned about
	late      []string        // warnings given before a Model took the reports (flushWarnings)
	// framework are the names declared from the embedded rows when the
	// runtime was made (startupSettings) that no application declared
	// since: an application's DeclareSetting replaces such a row.
	framework map[string]bool
}

type settingObserver struct {
	name string
	fn   func(name string, value any)
}

type fileStamp struct {
	mod  time.Time
	size int64
	ok   bool
}

func statFile(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{info.ModTime(), info.Size(), true}
}

// Settings returns the settings of the config component.
func (r *Runtime) Settings() *Settings { return r.Config().settings }

// DeclareSetting declares settings; each must have a unique name, a known
// type and a default of that type (in the enum when there is one).
func (r *Runtime) DeclareSetting(settings ...Setting) error {
	s := r.Settings()
	for _, one := range settings {
		if one.Name == "" || strings.ContainsAny(one.Name, " \t\n") {
			return fmt.Errorf("setting name %q must be one word", one.Name)
		}
		if current, ok := s.renamed(one.Name); ok {
			// An old name: the application's default stands for the
			// current setting, unless an application declared that
			// already.
			if s.find(current.Name) != nil && !s.framework[current.Name] {
				continue
			}
			def := one.Default
			one = current
			one.Default = def
		}
		framework := s.find(one.Name)
		if framework != nil && !s.framework[one.Name] {
			return fmt.Errorf("setting %q is already declared", one.Name)
		}
		if len(one.Enum) > 0 && one.Type != SettingString {
			return fmt.Errorf("setting %s: only a string setting takes an enum", one.Name)
		}
		def, err := coerceSetting(one, one.Default)
		if err != nil {
			return fmt.Errorf("setting %s default: %w", one.Name, err)
		}
		one.Default = def
		one.Enum = slices.Clone(one.Enum)
		if framework != nil {
			// The application's own declaration of a framework
			// setting: its row (default, description) replaces the
			// embedded one, the embedded check kept when it gives none.
			if one.Validate == nil {
				one.Validate = framework.Validate
			}
			old := s.current(*framework)
			*framework = one
			delete(s.framework, one.Name)
			if s.loaded {
				delete(s.values, one.Name)
				s.parse(one)
			}
			s.changed(one.Name, old)
			r.rebindSettingRows(one.Name)
			continue
		}
		// The application's settings list before the framework rows
		// no application declared, as they did when those were
		// declared at Model.Init.
		at := slices.IndexFunc(s.declared, func(d Setting) bool { return s.framework[d.Name] })
		if at < 0 {
			at = len(s.declared)
		}
		s.declared = slices.Insert(s.declared, at, one)
		if s.loaded {
			s.withdrawUnknown(one)
			s.parse(one)
		}
		r.rebindSettingRows(one.Name)
	}
	return nil
}

// withdrawUnknown takes back the unknown-setting problems the file gave
// for a setting declared after it was read (a theme, a segment, an
// extension's setting): the key is known now.
func (s *Settings) withdrawUnknown(one Setting) {
	for _, name := range append([]string{one.Name}, one.Deprecated...) {
		p := fmt.Sprintf("%s: unknown setting %q (kept)", SettingsFile, name)
		if !slices.Contains(s.problems, p) {
			continue
		}
		s.problems = slices.DeleteFunc(s.problems, func(q string) bool { return q == p })
		if s.config.Withdraw != nil {
			s.config.Withdraw(p)
		}
	}
}

// renamed is the setting a deprecated name now stands for: a declared
// setting listing it, else a framework setting listing it (the old name
// warns once). False for a current or unknown name.
func (s *Settings) renamed(name string) (Setting, bool) {
	if s.find(name) != nil {
		return Setting{}, false
	}
	for _, one := range s.declared {
		if slices.Contains(one.Deprecated, name) {
			s.deprecated(name, one.Name)
			return one, true
		}
	}
	if one, ok := frameworkSetting(name); ok && one.Name != name {
		s.deprecated(name, one.Name)
		return one, true
	}
	return Setting{}, false
}

// canonical is the name a setting is declared under: name itself, or the
// setting a deprecated name stands for.
func (s *Settings) canonical(name string) string {
	if one, ok := s.renamed(name); ok {
		return one.Name
	}
	return name
}

// deprecated warns once that old is an old name of current.
func (s *Settings) deprecated(old, current string) {
	if s.warned[old] {
		return
	}
	if s.warned == nil {
		s.warned = map[string]bool{}
	}
	s.warned[old] = true
	warning := fmt.Sprintf("setting %q is deprecated: use %q", old, current)
	if s.config.Report == nil {
		s.late = append(s.late, warning)
		return
	}
	s.config.report(warning)
}

// flushWarnings reports the deprecation warnings given before the config
// had a Report (an application declaring an old name at setup); NewModel
// calls it once it set Report.
func (s *Settings) flushWarnings() {
	late := s.late
	s.late = nil
	for _, w := range late {
		s.config.report(w)
	}
}

// known reports whether settings.json may hold name: a declared setting or
// an old name of one.
func (s *Settings) known(name string) bool {
	if s.find(name) != nil {
		return true
	}
	for _, one := range s.declared {
		if slices.Contains(one.Deprecated, name) {
			return true
		}
	}
	return false
}

func (s *Settings) find(name string) *Setting {
	for i := range s.declared {
		if s.declared[i].Name == name {
			return &s.declared[i]
		}
	}
	return nil
}

// Declared lists the declared settings in declaration order.
func (s *Settings) Declared() []Setting { return slices.Clone(s.declared) }

// Value is a setting's current value (the file's when it holds a valid
// one, else the default), typed as its getter returns it.
func (s *Settings) Value(name string) (any, bool) {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return nil, false
	}
	v := s.current(*one)
	if list, ok := v.([]string); ok {
		v = slices.Clone(list)
	}
	return v, true
}

// current is the value without reading the file.
func (s *Settings) current(one Setting) any {
	if v, ok := s.values[one.Name]; ok {
		return v
	}
	return one.Default
}

// String is a string setting's value ("" when undeclared or another type).
func (s *Settings) String(name string) string { v, _ := s.Value(name); t, _ := v.(string); return t }

// Int is an integer setting's value.
func (s *Settings) Int(name string) int { v, _ := s.Value(name); t, _ := v.(int); return t }

// Float is a number setting's value.
func (s *Settings) Float(name string) float64 { v, _ := s.Value(name); t, _ := v.(float64); return t }

// Bool is a boolean setting's value.
func (s *Settings) Bool(name string) bool { v, _ := s.Value(name); t, _ := v.(bool); return t }

// Strings is a list setting's value, a copy (nil when undeclared or
// another type).
func (s *Settings) Strings(name string) []string {
	v, _ := s.Value(name)
	t, _ := v.([]string)
	return slices.Clone(t)
}

// Duration is a duration setting's value.
func (s *Settings) Duration(name string) time.Duration {
	v, _ := s.Value(name)
	t, _ := v.(time.Duration)
	return t
}

// Observe calls fn with a setting's new value whenever it changes (Set,
// Reset, or a reload that read another value); name "" observes every
// setting. Observers run on the caller's goroutine, the update loop.
func (s *Settings) Observe(name string, fn func(name string, value any)) {
	s.observers = append(s.observers, settingObserver{name, fn})
}

// List is every declared setting with its value, in declaration order.
func (s *Settings) List() []SettingValue {
	s.ensure()
	out := make([]SettingValue, 0, len(s.declared))
	for _, one := range s.declared {
		out = append(out, s.entry(one))
	}
	return out
}

// Get is one setting with its value.
func (s *Settings) Get(name string) (SettingValue, error) {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return SettingValue{}, s.unknown(name)
	}
	return s.entry(*one), nil
}

func (s *Settings) entry(one Setting) SettingValue {
	v, set := s.values[one.Name]
	if !set {
		v = one.Default
	}
	one.Default = fileForm(one.Default)
	return SettingValue{Setting: one, Value: fileForm(v), Set: set}
}

func (s *Settings) unknown(name string) error {
	names := make([]string, len(s.declared))
	for i, one := range s.declared {
		names[i] = one.Name
	}
	if len(names) == 0 {
		return fmt.Errorf("unknown setting %q: this application declares none", name)
	}
	return fmt.Errorf("unknown setting %q: one of %s", name, strings.Join(names, ", "))
}

// Set validates value against the setting's type and enum (a string is
// parsed: "3", "true", "400ms"), writes settings.json and tells the
// observers. A value that does not validate changes nothing.
func (s *Settings) Set(name string, value any) error {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return s.unknown(name)
	}
	name = one.Name
	v, err := coerceSetting(*one, value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	data, err := json.Marshal(fileForm(v))
	if err != nil {
		return err
	}
	old, _ := s.Value(name)
	raw := maps.Clone(s.raw)
	raw[name] = data
	for _, old := range one.Deprecated {
		delete(raw, old)
	}
	if err := s.write(raw); err != nil {
		return err
	}
	s.values[name] = v
	s.withdrawFor(name)
	s.changed(name, old)
	return nil
}

// Reset removes a setting's value from settings.json, so it reads its
// default again.
func (s *Settings) Reset(name string) error {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return s.unknown(name)
	}
	name = one.Name
	names := append([]string{name}, one.Deprecated...)
	if !slices.ContainsFunc(names, func(n string) bool { _, ok := s.raw[n]; return ok }) {
		return nil
	}
	old, _ := s.Value(name)
	raw := maps.Clone(s.raw)
	for _, n := range names {
		delete(raw, n)
	}
	if err := s.write(raw); err != nil {
		return err
	}
	delete(s.values, name)
	s.withdrawFor(name)
	s.changed(name, old)
	return nil
}

// Reload reads settings.json again and tells the observers of every value
// that changed. It returns the problems the file has.
func (s *Settings) Reload() []string {
	before := map[string]any{}
	if s.loaded {
		for _, one := range s.declared {
			before[one.Name] = s.current(one)
		}
	}
	s.load()
	for _, one := range s.declared {
		if old, ok := before[one.Name]; ok {
			s.changed(one.Name, old)
		}
	}
	return slices.Clone(s.problems)
}

// ReloadIfChanged reloads when the file changed on disk since it was last
// read or written (another process set a value); it reports whether it did.
func (s *Settings) ReloadIfChanged() bool {
	if !s.loaded || s.path == "" || s.path != s.config.Path(SettingsFile) {
		return false
	}
	if statFile(s.path) == s.stamp {
		return false
	}
	s.Reload()
	return true
}

// Problems are the problems the last read of settings.json found.
func (s *Settings) Problems() []string { s.ensure(); return slices.Clone(s.problems) }

func (s *Settings) changed(name string, old any) {
	now := s.current(*s.find(name))
	if sameSetting(now, old) {
		return
	}
	for _, o := range s.observers {
		if o.name == "" || o.name == name {
			o.fn(name, now)
		}
	}
}

// ensure reads the file on first use, and again when the config directory
// changed since (Config.App set after a first read), telling the observers.
func (s *Settings) ensure() {
	switch {
	case !s.loaded:
		s.load()
	case s.path != s.config.Path(SettingsFile):
		s.Reload()
	}
}

func (s *Settings) load() {
	for _, p := range s.problems {
		if s.config.Withdraw != nil {
			s.config.Withdraw(p)
		}
	}
	s.loaded, s.broken, s.problems = true, nil, nil
	s.raw, s.values = map[string]json.RawMessage{}, map[string]any{}
	s.path = s.config.Path(SettingsFile)
	s.stamp = fileStamp{}
	if s.path == "" {
		return
	}
	s.stamp = statFile(s.path)
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil {
		err = DecodeJSONC(data, &s.raw)
		if err == nil && s.raw == nil {
			s.raw = map[string]json.RawMessage{}
		}
	}
	if err != nil {
		s.raw = map[string]json.RawMessage{}
		s.broken = err
		s.problem(SettingsFile + ": not a JSON object, using the defaults: " + err.Error())
		return
	}
	names := slices.Sorted(maps.Keys(s.raw))
	for _, name := range names {
		if !s.known(name) {
			s.problem(fmt.Sprintf("%s: unknown setting %q (kept)", SettingsFile, name))
		}
	}
	for _, one := range s.declared {
		s.parse(one)
	}
}

// parse reads one declared setting's value from the raw file.
func (s *Settings) parse(one Setting) {
	data, ok := s.raw[one.Name]
	for _, old := range one.Deprecated {
		if _, held := s.raw[old]; !held {
			continue
		}
		if ok {
			s.problem(fmt.Sprintf("%s: %s: the deprecated %q is ignored beside it (kept)", SettingsFile, one.Name, old))
			continue
		}
		data, ok = s.raw[old], true
		s.problem(fmt.Sprintf("%s: %s: read from the deprecated %q; rename it", SettingsFile, one.Name, old))
	}
	if !ok {
		return
	}
	var v any
	if err := json.Unmarshal(data, &v); err == nil {
		if v, err = coerceSetting(one, v); err == nil {
			s.values[one.Name] = v
			return
		}
		s.problem(fmt.Sprintf("%s: %s: %v, using the default (kept)", SettingsFile, one.Name, err))
		return
	}
	s.problem(fmt.Sprintf("%s: %s: not JSON, using the default (kept)", SettingsFile, one.Name))
}

func (s *Settings) problem(p string) {
	s.problems = append(s.problems, p)
	s.config.report(p)
}

// withdrawFor takes back the problems about one setting once a value was
// written for it.
func (s *Settings) withdrawFor(name string) {
	prefix := SettingsFile + ": " + name + ": "
	s.problems = slices.DeleteFunc(s.problems, func(p string) bool {
		if strings.HasPrefix(p, prefix) {
			if s.config.Withdraw != nil {
				s.config.Withdraw(p)
			}
			return true
		}
		return false
	})
}

// write replaces settings.json atomically with raw: a temp file in the
// same directory, renamed over the old one.
func (s *Settings) write(raw map[string]json.RawMessage) error {
	if s.path == "" {
		// No config directory (a sandbox): the values live for the session.
		s.raw = raw
		return nil
	}
	if s.broken != nil {
		return fmt.Errorf("%s does not parse; fix or remove it first: %w", s.path, s.broken)
	}
	// An existing file is edited in place, so the user's comments and
	// layout stay (patchJSONCObject); a new or unreadable one is written
	// whole.
	var data []byte
	if old, err := os.ReadFile(s.path); err == nil {
		if patched, ok := patchJSONCObject(old, raw); ok {
			data = bytes.TrimSuffix(patched, []byte("\n"))
		}
	}
	if data == nil {
		indented, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return err
		}
		data = indented
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	s.raw = raw
	s.stamp = statFile(s.path)
	return nil
}

// coerceSetting turns a value into the setting's type: a JSON value of the
// type, or a string to parse (a CLI or palette word), then applies the
// setting's Validate and then its enum, so Validate can say why a value
// is not offered (a terminal engine this build lacks).
func coerceSetting(one Setting, value any) (any, error) {
	v, err := coerceType(one, value)
	if err == nil && one.Validate != nil {
		err = one.Validate(v)
	}
	if text, ok := v.(string); err == nil && ok && len(one.Enum) > 0 && !slices.Contains(one.Enum, text) {
		err = fmt.Errorf("must be one of %s", strings.Join(one.Enum, ", "))
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

func coerceType(one Setting, value any) (any, error) {
	text, isText := value.(string)
	switch one.Type {
	case SettingString:
		if !isText {
			return nil, errors.New("must be a string")
		}
		return text, nil
	case SettingInteger:
		switch n := value.(type) {
		case int:
			return n, nil
		case int64:
			return int(n), nil
		case float64:
			if n == float64(int(n)) {
				return int(n), nil
			}
		case string:
			if v, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
				return v, nil
			}
		}
		return nil, errors.New("must be an integer")
	case SettingNumber:
		switch n := value.(type) {
		case int:
			return float64(n), nil
		case float64:
			return n, nil
		case string:
			if v, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return v, nil
			}
		}
		return nil, errors.New("must be a number")
	case SettingBoolean:
		switch b := value.(type) {
		case bool:
			return b, nil
		case string:
			if v, err := strconv.ParseBool(strings.TrimSpace(b)); err == nil {
				return v, nil
			}
		}
		return nil, errors.New("must be true or false")
	case SettingDuration:
		switch d := value.(type) {
		case time.Duration:
			return d, nil
		case string:
			if v, err := time.ParseDuration(strings.TrimSpace(d)); err == nil {
				return v, nil
			}
		}
		return nil, errors.New(`must be a duration such as "400ms" or "2s"`)
	case SettingList:
		return coerceList(value)
	}
	return nil, fmt.Errorf("unknown setting type %q", one.Type)
}

// listShape is the error a value that is not a list of strings gets.
var listShape = errors.New(`must be a list of strings, a JSON array such as ["a", "b"]`)

// coerceList reads a list setting's value: a []string, a JSON array of
// strings (as settings.json decodes it), or a word holding a JSON array
// (the CLI's settings set NAME '["a","b"]'). Never nil: no items is [].
func coerceList(value any) (any, error) {
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...), nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text, ok := item.(string)
			if !ok {
				return nil, listShape
			}
			out = append(out, text)
		}
		return out, nil
	case string:
		var out []string
		if err := json.Unmarshal([]byte(strings.TrimSpace(v)), &out); err != nil || out == nil {
			return nil, listShape
		}
		return out, nil
	}
	return nil, listShape
}

// fileForm is a typed value as settings.json and the CLI show it (a list
// as a copy, so a caller cannot change the stored value).
func fileForm(v any) any {
	switch t := v.(type) {
	case time.Duration:
		return t.String()
	case []string:
		return slices.Clone(t)
	}
	return v
}

// sameSetting reports whether two typed values are equal; a list compares
// item by item (== on two slices in interfaces panics).
func sameSetting(a, b any) bool {
	if x, ok := a.([]string); ok {
		y, ok := b.([]string)
		return ok && slices.Equal(x, y)
	}
	if _, ok := b.([]string); ok {
		return false
	}
	return a == b
}

// SettingsPoll is how often a Model checks settings.json for a change
// another process made (the CLI's settings set).
const SettingsPoll = time.Second

// SettingsPollMsg is the Model's settings.json check.
type SettingsPollMsg struct{}

// settingsPoll schedules the next check while there is a file to watch.
func (m *Model) settingsPoll() tea.Cmd {
	s := m.Runtime.Settings()
	if len(s.declared) == 0 || m.Runtime.Config().Path(SettingsFile) == "" {
		return nil
	}
	return tea.Tick(SettingsPoll, func(time.Time) tea.Msg { return SettingsPollMsg{} })
}

func registerSettings(r *Runtime) {
	s := &Settings{config: r.Config(), values: map[string]any{}, raw: map[string]json.RawMessage{}, framework: map[string]bool{}}
	r.Config().settings = s
	// The framework settings every application has are known from the
	// start: an application may read or set them before its Model
	// starts, and a settings.json naming them loads with no warning.
	for _, one := range startupSettings() {
		// Each runtime its own copies: a list default is never shared.
		one.Default, _ = coerceSetting(one, one.Default)
		one.Enum = slices.Clone(one.Enum)
		one.Deprecated = slices.Clone(one.Deprecated)
		s.declared = append(s.declared, one)
		s.framework[one.Name] = true
	}
	// Rows naming a setting ({terminal-leader}) follow its value.
	r.Config().settings.Observe("", func(name string, _ any) { r.rebindSettingRows(name) })
	name := Parameter{Type: "string", Required: true, Description: "Setting name", Source: "ochazuke.settings.names"}
	r.Register(Operation{Verb: "names", Noun: "ochazuke.settings", Description: "List the names of the declared settings", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names := []string{}
		for _, one := range r.Settings().declared {
			names = append(names, one.Name)
		}
		return names, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "ochazuke.settings", Description: "List the declared settings with their values, defaults and descriptions", Result: "setting[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Settings().List(), nil
	}})
	r.Register(Operation{Verb: "get", Noun: "ochazuke.settings", Description: "Show one setting and its value", Parameters: map[string]Parameter{"name": name}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Settings().Get(StringArg(a, "name", ""))
	}})
	r.Register(Operation{Verb: "set", Noun: "ochazuke.settings", PersistArgs: true, Description: "Set a setting, checked against its type and allowed values, and save settings.json", Parameters: map[string]Parameter{"name": name, "value": {Type: "string", Required: true, Description: "The value: text, a number, true or false, a duration such as 400ms, or a JSON array of strings for a list"}}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		s := r.Settings()
		if err := s.Set(StringArg(a, "name", ""), a["value"]); err != nil {
			return nil, err
		}
		return s.Get(StringArg(a, "name", ""))
	}})
	r.Register(Operation{Verb: "reset", Noun: "ochazuke.settings", Description: "Remove a setting's value from settings.json so it reads its default", Parameters: map[string]Parameter{"name": name}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		s := r.Settings()
		if err := s.Reset(StringArg(a, "name", "")); err != nil {
			return nil, err
		}
		return s.Get(StringArg(a, "name", ""))
	}})
	r.Register(Operation{Verb: "reload", Noun: "ochazuke.settings", Description: "Read settings.json again; observers see every value that changed", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.Settings().Reload()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "path", Noun: "ochazuke.settings", Description: "Print the settings file: settings.json beside bindings.json", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.Config().Path(SettingsFile)
		if path == "" {
			return nil, errors.New("this application has no config directory")
		}
		return path, nil
	}})
}

// patchJSONCObject is the JSONC object in old with its members set to
// raw: a changed value is replaced where it stands, a member raw lacks is
// removed, a new one is appended (names sorted) at the file's indent.
// Comments, whitespace and the member order stay as the user wrote them;
// false when old is not a JSONC object.
func patchJSONCObject(old []byte, raw map[string]json.RawMessage) ([]byte, bool) {
	v, err := hujson.Parse(old)
	if err != nil {
		return nil, false
	}
	obj, ok := v.Value.(*hujson.Object)
	if !ok {
		return nil, false
	}
	indent := hujson.Extra("\n  ")
	seen := map[string]bool{}
	var members []hujson.ObjectMember
	for i, m := range obj.Members {
		lit, ok := m.Name.Value.(hujson.Literal)
		var name string
		if !ok || json.Unmarshal(lit, &name) != nil {
			return nil, false
		}
		if i == 0 {
			before := m.Name.BeforeExtra
			if at := bytes.LastIndexByte(before, '\n'); at >= 0 && len(bytes.TrimSpace(before[at:])) == 0 {
				indent = append(hujson.Extra(nil), before[at:]...)
			}
		}
		value, keep := raw[name]
		if !keep {
			continue
		}
		seen[name] = true
		current := m.Value.Clone()
		current.Standardize()
		if !sameJSON(current.Pack(), value) {
			parsed, err := hujson.Parse(value)
			if err != nil {
				return nil, false
			}
			m.Value.Value = parsed.Value
		}
		members = append(members, m)
	}
	var added []string
	for name := range raw {
		if !seen[name] {
			added = append(added, name)
		}
	}
	slices.Sort(added)
	for i, name := range added {
		parsed, err := hujson.Parse(raw[name])
		if err != nil {
			return nil, false
		}
		quoted, err := json.Marshal(name)
		if err != nil {
			return nil, false
		}
		before := append(hujson.Extra(nil), indent...)
		if i == 0 {
			// What followed the last member (an inline comment, the
			// line break before "}") stays with it: the new members go
			// after it, and "}" keeps its own line.
			tail := obj.AfterExtra
			cut := bytes.LastIndexByte(tail, '\n')
			if cut < 0 {
				cut = len(tail)
			}
			if len(bytes.TrimSpace(tail[:cut])) > 0 {
				before = append(append(hujson.Extra(nil), tail[:cut]...), indent...)
			}
			if cut < len(tail) {
				obj.AfterExtra = append(hujson.Extra(nil), tail[cut:]...)
			} else {
				obj.AfterExtra = hujson.Extra("\n")
			}
		}
		members = append(members, hujson.ObjectMember{
			Name:  hujson.Value{BeforeExtra: before, Value: hujson.Literal(quoted)},
			Value: hujson.Value{BeforeExtra: hujson.Extra(" "), Value: parsed.Value},
		})
	}
	obj.Members = members
	return v.Pack(), true
}

// sameJSON reports whether two JSON texts are the same value, spelled
// alike once compacted.
func sameJSON(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}
