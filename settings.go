package ochazuke

import (
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

	tea "charm.land/bubbletea/v2"
)

// Setting types: the value a setting holds, as its getter returns it.
const (
	SettingString   = "string"   // string; Enum limits it
	SettingInteger  = "integer"  // int
	SettingNumber   = "number"   // float64
	SettingBoolean  = "boolean"  // bool
	SettingDuration = "duration" // time.Duration, written as "400ms"
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
// dropped. Model polls the file (SettingsPoll) so a value another process
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
		if s.find(one.Name) != nil {
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
		s.declared = append(s.declared, one)
		if s.loaded {
			s.parse(one)
		}
	}
	return nil
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
	one := s.find(name)
	if one == nil {
		return nil, false
	}
	if v, ok := s.values[name]; ok {
		return v, true
	}
	return one.Default, true
}

// String is a string setting's value ("" when undeclared or another type).
func (s *Settings) String(name string) string { v, _ := s.Value(name); t, _ := v.(string); return t }

// Int is an integer setting's value.
func (s *Settings) Int(name string) int { v, _ := s.Value(name); t, _ := v.(int); return t }

// Float is a number setting's value.
func (s *Settings) Float(name string) float64 { v, _ := s.Value(name); t, _ := v.(float64); return t }

// Bool is a boolean setting's value.
func (s *Settings) Bool(name string) bool { v, _ := s.Value(name); t, _ := v.(bool); return t }

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
	one := s.find(name)
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
	one := s.find(name)
	if one == nil {
		return s.unknown(name)
	}
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
	one := s.find(name)
	if one == nil {
		return s.unknown(name)
	}
	if _, ok := s.raw[name]; !ok {
		return nil
	}
	old, _ := s.Value(name)
	raw := maps.Clone(s.raw)
	delete(raw, name)
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
			before[one.Name], _ = s.Value(one.Name)
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
	now, _ := s.Value(name)
	if now == old {
		return
	}
	for _, o := range s.observers {
		if o.name == "" || o.name == name {
			o.fn(name, now)
		}
	}
}

// ensure reads the file on first use, and again when the config directory
// changed since.
func (s *Settings) ensure() {
	if !s.loaded || s.path != s.config.Path(SettingsFile) {
		s.load()
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
		err = json.Unmarshal(data, &s.raw)
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
		if s.find(name) == nil {
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
		return errors.New("this application has no config directory")
	}
	if s.broken != nil {
		return fmt.Errorf("%s does not parse; fix or remove it first: %w", s.path, s.broken)
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
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
// type, or a string to parse (a CLI or palette word).
func coerceSetting(one Setting, value any) (any, error) {
	text, isText := value.(string)
	switch one.Type {
	case SettingString:
		if !isText {
			return nil, errors.New("must be a string")
		}
		if len(one.Enum) > 0 && !slices.Contains(one.Enum, text) {
			return nil, fmt.Errorf("must be one of %s", strings.Join(one.Enum, ", "))
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
	}
	return nil, fmt.Errorf("unknown setting type %q", one.Type)
}

// fileForm is a typed value as settings.json and the CLI show it.
func fileForm(v any) any {
	if d, ok := v.(time.Duration); ok {
		return d.String()
	}
	return v
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
	r.Config().settings = &Settings{config: r.Config(), values: map[string]any{}, raw: map[string]json.RawMessage{}}
	name := Parameter{Type: "string", Required: true, Description: "Setting name", Source: "settings.names"}
	r.Register(Operation{Verb: "names", Noun: "settings", Description: "List the names of the declared settings", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names := []string{}
		for _, one := range r.Settings().declared {
			names = append(names, one.Name)
		}
		return names, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "settings", Description: "List the declared settings with their values, defaults and descriptions", Result: "setting[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Settings().List(), nil
	}})
	r.Register(Operation{Verb: "get", Noun: "settings", Description: "Show one setting and its value", Parameters: map[string]Parameter{"name": name}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Settings().Get(StringArg(a, "name", ""))
	}})
	r.Register(Operation{Verb: "set", Noun: "settings", Description: "Set a setting, checked against its type and allowed values, and save settings.json", Parameters: map[string]Parameter{"name": name, "value": {Type: "string", Required: true, Description: "The value: text, a number, true or false, or a duration such as 400ms"}}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		s := r.Settings()
		if err := s.Set(StringArg(a, "name", ""), a["value"]); err != nil {
			return nil, err
		}
		return s.Get(StringArg(a, "name", ""))
	}})
	r.Register(Operation{Verb: "reset", Noun: "settings", Description: "Remove a setting's value from settings.json so it reads its default", Parameters: map[string]Parameter{"name": name}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		s := r.Settings()
		if err := s.Reset(StringArg(a, "name", "")); err != nil {
			return nil, err
		}
		return s.Get(StringArg(a, "name", ""))
	}})
	r.Register(Operation{Verb: "reload", Noun: "settings", Description: "Read settings.json again; observers see every value that changed", Result: "problem[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		problems := r.Settings().Reload()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "path", Noun: "settings", Description: "Print the settings file: settings.json beside bindings.json", Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		path := r.Config().Path(SettingsFile)
		if path == "" {
			return nil, errors.New("this application has no config directory")
		}
		return path, nil
	}})
}
