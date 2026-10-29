package ochazuke

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

// The session layer: a value set for this session lives in memory above
// settings.json and is not written, as Vim's :set changes the running
// editor and the vimrc persists. The effective value is the session's,
// else the file's, else the declared default; observers fire when it
// changes, whichever layer moved.
//
// The writes-through rule. SetSession, ResetSession and ResetAll write
// the session layer only while the runtime has a session (a Model's Init
// ran: StartSession) and settings.autosave is off; otherwise they write
// settings.json at once, today's commit-on-change behaviour. Writing the
// file (Set, Reset, the Settings page, settings set, the page's undo)
// drops that setting's session value, so what was just written is what
// shows. A file change the poll reads leaves a session value on top and
// marks the setting changed on disk (Origin.Disk). Write and WriteAll move
// session values into the file; Revert and RevertAll drop them.
//
// Provenance: Origin says which layer a value comes from and where: the
// default (the embedded file, or the application file and line that
// declared it), settings.json (its path and the key's line), or the
// session (the request's history source, the name it ran by and the time).

// The framework settings of the session layer.
const (
	// SettingsAutosaveSetting makes every session write go to
	// settings.json at once (boolean, default false: session values stay
	// in memory until settings.write).
	SettingsAutosaveSetting = "settings.autosave"
	// SettingsMarkSessionSetting draws a session value bold on the
	// Settings page with the persisted value beside it (boolean, default
	// true).
	SettingsMarkSessionSetting = "settings.mark-session"
)

// The layers a setting's value comes from (Origin.Layer).
const (
	OriginDefault = "default"
	OriginFile    = "file"
	OriginSession = "session"
)

// Origin is where a setting's value comes from, as :verbose names it.
// Layer is OriginDefault, OriginFile or OriginSession. For a default,
// Path is the embedded settings file (settings/ochazuke.json) or the
// application's file:line that declared it, and By is "ochazuke" or
// "app". For the file, Path is settings.json and Line the line of its
// key (0 when not found). For a session value, Source is the history
// source of the request that set it ("palette", "key:<keys>", "cli",
// "api", "macro:<r>"), Via the name it ran by (an alias such as
// setting.save, or the action's key) and At the time. Disk, when set, is
// when the poll (or settings.reload) read another value for the setting
// from settings.json: another process changed it under this runtime.
type Origin struct {
	Layer  string    `json:"layer"`
	Path   string    `json:"path,omitempty"`
	Line   int       `json:"line,omitempty"`
	By     string    `json:"by,omitempty"`
	Source string    `json:"source,omitempty"`
	Via    string    `json:"via,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Disk   time.Time `json:"disk,omitzero"`
}

// String is the origin as one line: "default, settings/ochazuke.json
// (ochazuke)", "set in PATH line N", "set this session by NAME (palette,
// 14:02)", each followed by "; settings.json changed on disk at 14:05"
// when the file moved under the runtime.
func (o Origin) String() string {
	var out string
	switch o.Layer {
	case OriginSession:
		out = "set this session"
		if o.Via != "" {
			out += " by " + o.Via
		}
		var where []string
		if o.Source != "" {
			where = append(where, o.Source)
		}
		if !o.At.IsZero() {
			where = append(where, o.At.Format("15:04"))
		}
		if len(where) > 0 {
			out += " (" + strings.Join(where, ", ") + ")"
		}
	case OriginFile:
		out = "set in " + cmp.Or(o.Path, SettingsFile)
		if o.Line > 0 {
			out += fmt.Sprintf(" line %d", o.Line)
		}
	default:
		out = "default"
		switch {
		case o.By == AliasApp && o.Path != "":
			out += ", declared by the application at " + o.Path
		case o.Path != "":
			out += ", " + o.Path
			if o.By != "" {
				out += " (" + o.By + ")"
			}
		}
	}
	if !o.Disk.IsZero() {
		out += "; " + SettingsFile + " changed on disk at " + o.Disk.Format("15:04")
	}
	return out
}

// SettingDiff is one setting whose session value differs from its
// persisted one, as settings.diff answers: the name, the persisted value
// (settings.json's, else the default: SavedIn says which, "file" or
// "default"), the session value, both in their file form, and the
// session value's origin.
type SettingDiff struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Saved   any    `json:"saved"`
	SavedIn string `json:"savedIn"`
	Session any    `json:"session"`
	Origin  Origin `json:"origin"`
}

// sessionValue is one value of the session layer: typed, whether a
// reset put it there (the default: writing it removes the file's key),
// and where it came from.
type sessionValue struct {
	value  any
	reset  bool
	origin Origin
}

// StartSession marks the runtime as having a session: a running UI whose
// session values last until it exits. Model.Init calls it. A runtime
// without one (a one-shot CLI process, a script) writes every session
// write to settings.json.
func (s *Settings) StartSession() { s.live = true }

// HasSession reports whether the runtime has a session (StartSession).
func (s *Settings) HasSession() bool { return s.live }

// Autosave is the settings.autosave setting: session writes go to the file.
func (s *Settings) Autosave() bool { return s.Bool(SettingsAutosaveSetting) }

// writesFile reports whether a session write goes to settings.json: no
// session to hold it, or autosave on.
func (s *Settings) writesFile() bool { return !s.live || s.Autosave() }

// Session is a setting's session value, typed, false when it has none.
func (s *Settings) Session(name string) (any, bool) {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return nil, false
	}
	v, ok := s.session[one.Name]
	if !ok {
		return nil, false
	}
	if list, isList := v.value.([]string); isList {
		return slices.Clone(list), true
	}
	return v.value, true
}

// SetSession sets a setting for this session: value is checked as Set
// checks it and lives above settings.json, unwritten. It writes the file
// instead (Set) when the runtime has no session or settings.autosave is
// on. It returns the layer written, OriginSession or OriginFile.
func (s *Settings) SetSession(name string, value any) (string, error) {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return "", s.unknown(name)
	}
	if s.writesFile() {
		return OriginFile, s.Set(one.Name, value)
	}
	v, err := coerceSetting(*one, value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", one.Name, err)
	}
	s.putSession(*one, v, false)
	return OriginSession, nil
}

// ResetSession sets a setting back to its default for this session (the
// file keeps its value until Write, which then removes it), or resets the
// file (Reset) when the runtime has no session or autosave is on. It
// returns the layer written.
func (s *Settings) ResetSession(name string) (string, error) {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return "", s.unknown(name)
	}
	if s.writesFile() {
		return OriginFile, s.Reset(one.Name)
	}
	if !s.fileHolds(*one) {
		// Nothing persisted beneath: the default shows once the session
		// value is gone.
		_ = s.Revert(one.Name)
		return OriginSession, nil
	}
	s.putSession(*one, one.Default, true)
	return OriginSession, nil
}

// putSession stores a session value with the running request's origin
// and tells the observers when the effective value moved.
func (s *Settings) putSession(one Setting, v any, reset bool) {
	old := s.current(one)
	if list, ok := v.([]string); ok {
		v = slices.Clone(list)
	}
	o := Origin{Layer: OriginSession, At: s.clock()}
	if s.source != nil {
		o.Source, o.Via = s.source()
	}
	if s.session == nil {
		s.session = map[string]sessionValue{}
	}
	s.session[one.Name] = sessionValue{value: v, reset: reset, origin: o}
	s.changed(one.Name, old)
	s.relayer()
}

// Revert drops a setting's session value, so settings.json's value (or
// the default) shows again. A setting without one is left alone.
func (s *Settings) Revert(name string) error {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return s.unknown(name)
	}
	old := s.current(*one)
	if s.dropSession(one.Name) {
		s.changed(one.Name, old)
		s.relayer()
	}
	return nil
}

// RevertAll drops every session value; it returns the names reverted.
func (s *Settings) RevertAll() []string {
	s.ensure()
	names := s.sessionNames()
	for _, name := range names {
		_ = s.Revert(name)
	}
	return names
}

// Write moves a setting's session value into settings.json (a value a
// reset put there removes the file's key). A setting without one is left
// alone; false reports that nothing was written.
func (s *Settings) Write(name string) (bool, error) {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return false, s.unknown(name)
	}
	v, ok := s.session[one.Name]
	if !ok {
		return false, nil
	}
	var err error
	if v.reset {
		err = s.Reset(one.Name)
	} else {
		err = s.Set(one.Name, v.value)
	}
	if err != nil {
		return false, err
	}
	s.relayer()
	return true, nil
}

// WriteAll moves every session value into settings.json; it returns the
// names written. The first failure stops it, the ones before staying
// written.
func (s *Settings) WriteAll() ([]string, error) {
	s.ensure()
	var done []string
	for _, name := range s.sessionNames() {
		if _, err := s.Write(name); err != nil {
			return done, err
		}
		done = append(done, name)
	}
	return done, nil
}

// ResetAll sets every setting back to its default by the writes-through
// rule: with a session (and autosave off) each setting not at its default
// gets the default as a session value (Write removes the file's keys,
// RevertAll takes it back); otherwise every declared setting's key is
// removed from settings.json in one write (unknown keys stay) and the
// session values are dropped. It returns the layer written and the names
// it reset.
func (s *Settings) ResetAll() (string, []string, error) {
	s.ensure()
	names := s.notDefault()
	if !s.writesFile() {
		for _, name := range names {
			if _, err := s.ResetSession(name); err != nil {
				return OriginSession, nil, err
			}
		}
		return OriginSession, names, nil
	}
	before := map[string]any{}
	raw := maps.Clone(s.raw)
	for _, one := range s.declared {
		before[one.Name] = s.current(one)
		delete(raw, one.Name)
		for _, old := range one.Deprecated {
			delete(raw, old)
		}
	}
	if len(raw) != len(s.raw) {
		if err := s.write(raw); err != nil {
			return OriginFile, nil, err
		}
	}
	for _, one := range s.declared {
		delete(s.values, one.Name)
		s.dropSession(one.Name)
		s.withdrawFor(one.Name)
	}
	for _, one := range s.declared {
		s.changed(one.Name, before[one.Name])
	}
	s.relayer()
	return OriginFile, names, nil
}

// notDefault names the settings whose effective value is not the default
// or whose key settings.json holds, in declaration order: what ResetAll
// changes.
func (s *Settings) notDefault() []string {
	var out []string
	for _, one := range s.declared {
		if s.fileHolds(one) || !sameSetting(s.current(one), one.Default) {
			out = append(out, one.Name)
		}
	}
	return out
}

// fileHolds reports whether settings.json holds a key for the setting
// (its name or an old one), valid or not.
func (s *Settings) fileHolds(one Setting) bool {
	for _, n := range append([]string{one.Name}, one.Deprecated...) {
		if _, ok := s.raw[n]; ok {
			return true
		}
	}
	return false
}

// Diff lists the settings whose session value differs from the persisted
// one, in declaration order: what settings.write would write and the
// review screen shows.
func (s *Settings) Diff() []SettingDiff {
	s.ensure()
	out := []SettingDiff{}
	for _, one := range s.declared {
		v, ok := s.session[one.Name]
		if !ok || sameSetting(v.value, s.persisted(one)) {
			continue
		}
		in := OriginDefault
		if _, held := s.values[one.Name]; held {
			in = OriginFile
		}
		o := v.origin
		o.Disk = s.disk[one.Name]
		out = append(out, SettingDiff{Name: one.Name, Type: one.Type, Saved: fileForm(s.persisted(one)), SavedIn: in, Session: fileForm(v.value), Origin: o})
	}
	return out
}

// differsInSession reports whether a setting's session value differs from
// its persisted one (the Settings page's marker).
func (s *Settings) differsInSession(one Setting) bool {
	v, ok := s.session[one.Name]
	return ok && !sameSetting(v.value, s.persisted(one))
}

// Origin is where a setting's effective value comes from.
func (s *Settings) Origin(name string) (Origin, error) {
	s.ensure()
	one := s.find(s.canonical(name))
	if one == nil {
		return Origin{}, s.unknown(name)
	}
	var o Origin
	v, inSession := s.session[one.Name]
	_, inFile := s.values[one.Name]
	switch {
	case inSession:
		o = v.origin
	case inFile:
		o = Origin{Layer: OriginFile, Path: s.path, Line: s.keyLine(*one)}
	default:
		o = s.defaultOrigin(one.Name)
	}
	o.Disk = s.disk[one.Name]
	return o, nil
}

// defaultOrigin is where a setting's default was declared: the embedded
// file of a framework row, else the application's file:line.
func (s *Settings) defaultOrigin(name string) Origin {
	if at := s.declaredAt[name]; at != "" {
		return Origin{Layer: OriginDefault, Path: at, By: AliasApp}
	}
	for file, rows := range frameworkSettingFiles() {
		if file == "" {
			continue
		}
		if slices.ContainsFunc(rows, func(one Setting) bool { return one.Name == name }) {
			return Origin{Layer: OriginDefault, Path: "settings/" + file, By: FrameworkNamespace}
		}
	}
	return Origin{Layer: OriginDefault}
}

// declaredBy records where an application declared a setting: the first
// caller outside the framework's own package, its tests counting as
// callers (a framework row the framework declares from its embedded
// files records nothing).
func (s *Settings) declaredBy(name string) {
	pcs := make([]uintptr, 16)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		f, more := frames.Next()
		pkg := f.Function
		if i := strings.LastIndex(pkg, "/"); i >= 0 {
			if j := strings.Index(pkg[i:], "."); j >= 0 {
				pkg = pkg[:i+j]
			}
		}
		if pkg != frameworkPackage || strings.HasSuffix(f.File, "_test.go") {
			if s.declaredAt == nil {
				s.declaredAt = map[string]string{}
			}
			if f.File != "" {
				s.declaredAt[name] = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			return
		}
		if !more {
			delete(s.declaredAt, name)
			return
		}
	}
}

// frameworkPackage is the framework's import path, as runtime.Frame
// names a function's package.
const frameworkPackage = "github.com/unixsuperhero/ochazuke"

// keyLine is the line of settings.json holding the setting's key (its
// first top-level spelling), 0 when not found.
func (s *Settings) keyLine(one Setting) int {
	if s.path == "" {
		return 0
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return 0
	}
	for _, n := range append([]string{one.Name}, one.Deprecated...) {
		quoted, _ := json.Marshal(n)
		re := regexp.MustCompile(regexp.QuoteMeta(string(quoted)) + `\s*:`)
		if at := re.FindIndex(data); at != nil {
			return 1 + strings.Count(string(data[:at[0]]), "\n")
		}
	}
	return 0
}

// dropSession removes a setting's session value; it reports whether
// there was one. The caller tells the observers.
func (s *Settings) dropSession(name string) bool {
	if _, ok := s.session[name]; !ok {
		delete(s.disk, name)
		return false
	}
	delete(s.session, name)
	delete(s.disk, name)
	return true
}

// changedOnDisk marks a setting whose settings.json value another
// process changed.
func (s *Settings) changedOnDisk(name string) {
	if s.disk == nil {
		s.disk = map[string]time.Time{}
	}
	s.disk[name] = s.clock()
}

// clock is the time an origin records: the Model's clock (Model.Now) once
// a Model is made, else the wall clock.
func (s *Settings) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// sessionNames are the settings holding a session value, in declaration
// order.
func (s *Settings) sessionNames() []string {
	var out []string
	for _, one := range s.declared {
		if _, ok := s.session[one.Name]; ok {
			out = append(out, one.Name)
		}
	}
	return out
}

// relayer tells the Settings page a layer moved without the effective
// value changing (a write, a revert of an equal value, a file change
// under a session value), so its marker redraws.
func (s *Settings) relayer() {
	if s.relayered != nil {
		s.relayered()
	}
}

// cliWriteCheck asks before a settings verb writes settings.json from a
// CLI process (the request's source is cli and the runtime has no
// session): prompt says what would be written, "" when nothing would be
// (the verb then runs, or fails with its own error). --yes answers it.
func cliWriteCheck(prompt func(r *Runtime, a Arguments) string) VerbCheck {
	return VerbCheck{Name: "settings.cli-write", Check: func(r *Runtime, _ Context, a Arguments) Verdict {
		if r.history.source != "cli" || r.Settings().HasSession() || r.Config().Path(SettingsFile) == "" {
			return Allow()
		}
		if p := prompt(r, a); p != "" {
			return AskFirst(p)
		}
		return Allow()
	}}
}

// setPrompt is settings set's question: the value it would write.
func setPrompt(r *Runtime, a Arguments) string {
	s := r.Settings()
	s.ensure()
	one := s.find(s.canonical(StringArg(a, "name", "")))
	if one == nil {
		return ""
	}
	v, err := coerceSetting(*one, a["value"])
	if err != nil {
		return ""
	}
	return fmt.Sprintf("Write %s = %s to %s?", one.Name, settingWord(v), r.Config().Path(SettingsFile))
}

// resetPrompt is settings reset's question, when the file holds the key.
func resetPrompt(r *Runtime, a Arguments) string {
	s := r.Settings()
	s.ensure()
	one := s.find(s.canonical(StringArg(a, "name", "")))
	if one == nil || !s.fileHolds(*one) {
		return ""
	}
	return fmt.Sprintf("Remove %s from %s (its default is %s)?", one.Name, r.Config().Path(SettingsFile), settingWord(one.Default))
}

// writePrompt is the write verbs' question: the session values they
// would write (a CLI process holds none, so it never asks there today).
func writePrompt(r *Runtime, a Arguments) string {
	s := r.Settings()
	names := s.sessionNames()
	if name := StringArg(a, "name", ""); name != "" {
		names = slices.DeleteFunc(names, func(n string) bool { return n != s.canonical(name) })
	}
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("Write %s to %s?", strings.Join(names, ", "), r.Config().Path(SettingsFile))
}

// settingWord is a typed value as one word of a prompt: a list as
// comma-separated items, empty text as "".
func settingWord(v any) string {
	switch t := fileForm(v).(type) {
	case string:
		if t == "" {
			return `""`
		}
		return t
	case []string:
		if len(t) == 0 {
			return "[]"
		}
		return strings.Join(t, ",")
	default:
		return fmt.Sprint(t)
	}
}

// settingOfArgs is the setting a verb acts on: its name argument, else
// the focused Settings row's.
func settingOfArgs(r *Runtime, c Context, a Arguments) (*Setting, error) {
	s := r.Settings()
	s.ensure()
	if name := StringArg(a, "name", ""); name != "" {
		if one := s.find(s.canonical(name)); one != nil {
			return one, nil
		}
		return nil, s.unknown(name)
	}
	if one := focusedSetting(r, focusOf(c)); one != nil {
		return one, nil
	}
	return nil, errors.New("name a setting (no setting is focused)")
}

// registerSettingsSession registers the session layer's verbs on the
// setting and settings nouns, their aliases and the Settings page's
// refresh on a layer change.
func registerSettingsSession(r *Runtime) {
	s := r.Settings()
	s.relayered = func() { refreshSettingsPage(r) }
	name := Parameter{Type: "string", Description: "Setting name (default: the focused Settings row)", Source: "ochazuke.settings.names"}
	r.Register(Operation{Verb: "revert", Noun: "ochazuke.setting", Description: "Drop the setting's session value, so its saved value shows again", Parameters: map[string]Parameter{"name": name}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		one, err := settingOfArgs(r, c, a)
		if err != nil {
			return nil, err
		}
		if err := r.Settings().Revert(one.Name); err != nil {
			return nil, err
		}
		return r.Settings().Get(one.Name)
	}})
	r.Register(Operation{Verb: "write", Noun: "ochazuke.setting", Checks: []VerbCheck{cliWriteCheck(writePrompt)}, Description: "Write the setting's session value to settings.json", Parameters: map[string]Parameter{"name": name}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		one, err := settingOfArgs(r, c, a)
		if err != nil {
			return nil, err
		}
		if _, err := r.Settings().Write(one.Name); err != nil {
			return nil, err
		}
		return r.Settings().Get(one.Name)
	}})
	r.Register(Operation{Verb: "write", Noun: "ochazuke.settings", Checks: []VerbCheck{cliWriteCheck(writePrompt)}, Description: "Write every session value to settings.json", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names, err := r.Settings().WriteAll()
		if names == nil {
			names = []string{}
		}
		return names, err
	}})
	r.Register(Operation{Verb: "revert", Noun: "ochazuke.settings", Description: "Drop every session value, so the saved values show again", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names := r.Settings().RevertAll()
		if names == nil {
			names = []string{}
		}
		return names, nil
	}})
	r.Register(Operation{Verb: "diff", Noun: "ochazuke.settings", Description: "List the settings whose session value differs from the saved one: name, saved value, session value and origin", Result: "setting-diff[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Settings().Diff(), nil
	}})
	r.Register(Operation{Verb: "reset-all", Noun: "ochazuke.settings", Checks: []VerbCheck{resetAllCheck}, Description: "Reset every setting to its default: for this session, or in settings.json without a session or with settings.autosave", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		_, names, err := r.Settings().ResetAll()
		if names == nil {
			names = []string{}
		}
		return names, err
	}})
	// Aliases: save for write on both nouns, and the bare mksettings
	// (after Vim's :mkvimrc).
	for _, a := range []ActionAlias{
		{Alias: "ochazuke.setting.save", Action: "ochazuke.setting.write"},
		{Alias: "ochazuke.settings.save", Action: "ochazuke.settings.write"},
		{Alias: "mksettings", Action: "ochazuke.settings.write", Note: "after Vim's :mkvimrc"},
	} {
		if err := r.AddActionAlias(a); err != nil {
			panic(err)
		}
	}
}

// resetAllCheck asks before settings.reset-all from every path, naming
// how many settings it resets and where; nothing to reset runs at once.
var resetAllCheck = VerbCheck{Name: "settings.reset-all", Check: func(r *Runtime, _ Context, _ Arguments) Verdict {
	s := r.Settings()
	s.ensure()
	n := len(s.notDefault())
	if n == 0 {
		return Allow()
	}
	where := "for this session"
	if s.writesFile() {
		where = "in " + SettingsFile
		if path := r.Config().Path(SettingsFile); path != "" {
			where = "in " + path
		}
	}
	noun := "settings"
	if n == 1 {
		noun = "setting"
	}
	return AskFirst(fmt.Sprintf("Reset all %d changed %s to their defaults %s?", n, noun, where))
}}
