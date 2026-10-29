package ochazuke

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// verbsFixture is sessionFixture with a free text setting, a list and an
// integer whose Validate caps it, and a recording sink for the status line.
func verbsFixture(t *testing.T) (*Runtime, string, *RecordingSink) {
	t.Helper()
	r, _, dir := sessionFixture(t)
	if err := r.DeclareSetting(
		Setting{Name: "title", Type: SettingString, Default: "main", Description: "Window title"},
		Setting{Name: "paths", Type: SettingList, Default: []string{"a"}, Description: "Paths"},
		Setting{Name: "level", Type: SettingInteger, Default: 1, Description: "Level", Validate: func(v any) error {
			if v.(int) > 9 {
				return errors.New("must be at most 9")
			}
			return nil
		}},
	); err != nil {
		t.Fatal(err)
	}
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	return r, dir, sink
}

func runSetting(t *testing.T, r *Runtime, verb string, args Arguments) (any, error) {
	t.Helper()
	return r.Execute(Request{Noun: "ochazuke.setting", Verb: verb, Args: args})
}

// Each value verb computes the new value from the effective one, checks
// it whole, and writes the session layer by default (scope file writes
// settings.json).
func TestSettingValueVerbs(t *testing.T) {
	r, dir, sink := verbsFixture(t)
	s := r.Settings()
	steps := []struct {
		verb string
		args Arguments
		name string
		want any
	}{
		{"set", Arguments{"value": "5"}, "scrolloff", 5},
		{"increase", Arguments{"by": "3"}, "scrolloff", 8},
		{"decrease", Arguments{"by": "2"}, "scrolloff", 6},
		{"multiply", Arguments{"by": "2"}, "scrolloff", 12},
		{"multiply", Arguments{"by": "0.5"}, "ratio", 0.25},
		{"increase", Arguments{"by": "100ms"}, "debounce", 500 * time.Millisecond},
		{"toggle", nil, "wrap", true},
		{"cycle", Arguments{"step": -1}, "theme", "mono"},
		{"cycle", nil, "scrolloff", 13},
		{"append", Arguments{"value": "-x"}, "title", "main-x"},
		{"prepend", Arguments{"value": "my "}, "title", "my main-x"},
		{"remove", Arguments{"value": "-x"}, "title", "my main"},
		{"remove", Arguments{"value": "absent"}, "title", "my main"},
		{"append", Arguments{"value": "b,a,c"}, "paths", []string{"a", "b", "c"}},
		{"prepend", Arguments{"value": `["z","b"]`}, "paths", []string{"z", "a", "b", "c"}},
		{"remove", Arguments{"value": "a,nope"}, "paths", []string{"z", "b", "c"}},
		{"set", Arguments{"value": `x\,y,w`}, "paths", []string{"x,y", "w"}},
		{"reset", nil, "scrolloff", 2},
	}
	for _, st := range steps {
		args := Arguments{"name": st.name}
		for k, v := range st.args {
			args[k] = v
		}
		got, err := runSetting(t, r, st.verb, args)
		if err != nil {
			t.Fatalf("%s %s: %v", st.verb, st.name, err)
		}
		if v, _ := s.Value(st.name); !sameSetting(v, st.want) {
			t.Fatalf("%s %s = %#v; want %#v", st.verb, st.name, v, st.want)
		}
		if w, ok := got.(SettingWritten); !ok || w.Written != OriginSession {
			t.Fatalf("%s answered %#v", st.verb, got)
		}
	}
	if data := readJSONCFile(t, dir); data != nil {
		t.Fatalf("session writes reached settings.json: %v", data)
	}
	// scope file writes the file and drops the session value.
	if got, err := runSetting(t, r, "set", Arguments{"name": "title", "value": "x", "scope": "file"}); err != nil || got.(SettingWritten).Written != OriginFile || readSettingsFile(t, dir)["title"] != "x" {
		t.Fatalf("scope file: %#v %v", got, err)
	}
	if _, ok := s.Session("title"); ok {
		t.Fatal("a file write kept the session value")
	}
	// show puts name=value (a boolean name or noname) on the status line.
	for name, want := range map[string]string{"wrap": "wrap", "paths": `paths=x\,y,w`, "debounce": "debounce=500ms", "title": "title=x"} {
		if got, err := runSetting(t, r, "show", Arguments{"name": name}); err != nil || got.(SettingShown).Line != want || sink.LastInfo() != want {
			t.Fatalf("show %s: %#v %q %v", name, got, sink.LastInfo(), err)
		}
	}
	_, _ = runSetting(t, r, "toggle", Arguments{"name": "wrap"})
	if got, _ := runSetting(t, r, "show", Arguments{"name": "wrap", "verbose": true}); !strings.HasPrefix(got.(SettingShown).Line, "nowrap — set this session by ochazuke.setting.toggle (api, ") {
		t.Fatalf("verbose show %q", got.(SettingShown).Line)
	}
}

func readJSONCFile(t *testing.T, dir string) map[string]any {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, SettingsFile)); err != nil {
		return nil
	}
	return readJSONCSettings(t, dir)
}

// A verb that does not fit the setting's type, an operand of the wrong
// type and a result its checks refuse are errors that change nothing.
func TestSettingVerbErrors(t *testing.T) {
	r, _, _ := verbsFixture(t)
	for _, c := range []struct {
		verb string
		args Arguments
		want string
	}{
		{"toggle", Arguments{"name": "scrolloff"}, "scrolloff is an integer: setting.toggle is for on/off settings"},
		{"toggle", Arguments{"name": "theme"}, "theme is one of dark, light, mono, not on/off: use setting.cycle to step through them, or theme=VALUE"},
		{"increase", Arguments{"name": "title", "by": "1"}, "title is text: setting.increase is for numbers and durations"},
		{"multiply", Arguments{"name": "debounce", "by": "2"}, "debounce is a duration: setting.multiply is for numbers"},
		{"cycle", Arguments{"name": "debounce"}, "debounce is a duration: setting.cycle is for on/off settings, numbers and fixed values"},
		{"increase", Arguments{"name": "scrolloff", "by": "x"}, "scrolloff takes a whole number: x"},
		{"increase", Arguments{"name": "level", "by": "20"}, "level: must be at most 9"},
		{"set", Arguments{"name": "theme", "value": "blue"}, "theme: must be one of dark, light, mono"},
		{"set", Arguments{"name": "scrolloff", "value": "1", "scope": "disk"}, "scope must be one of session, file"},
		{"show", nil, "name a setting (no setting is focused)"},
	} {
		if _, err := runSetting(t, r, c.verb, c.args); err == nil || err.Error() != c.want {
			t.Errorf("%s %v: %v; want %q", c.verb, c.args, err, c.want)
		}
	}
	if len(r.Settings().Diff()) != 0 {
		t.Fatalf("a refused verb changed %+v", r.Settings().Diff())
	}
}

// settings.apply runs each form of a :set line as a request to its verb,
// and gives the same values as the verbs run directly (parity).
func TestSettingsApplyMatchesTheVerbs(t *testing.T) {
	rows := []struct {
		line string
		reqs []Request
	}{
		{"wrap", []Request{{Verb: "set", Args: Arguments{"name": "wrap", "value": "true"}}}},
		{"nowrap", []Request{{Verb: "set", Args: Arguments{"name": "wrap", "value": "false"}}}},
		{"wrap!", []Request{{Verb: "toggle", Args: Arguments{"name": "wrap"}}}},
		{"invwrap", []Request{{Verb: "toggle", Args: Arguments{"name": "wrap"}}}},
		{"scrolloff=5", []Request{{Verb: "set", Args: Arguments{"name": "scrolloff", "value": "5"}}}},
		{"scrolloff:5", []Request{{Verb: "set", Args: Arguments{"name": "scrolloff", "value": "5"}}}},
		{"scrolloff+=3", []Request{{Verb: "increase", Args: Arguments{"name": "scrolloff", "by": "3"}}}},
		{"scrolloff-=1", []Request{{Verb: "decrease", Args: Arguments{"name": "scrolloff", "by": "1"}}}},
		{"scrolloff^=4", []Request{{Verb: "multiply", Args: Arguments{"name": "scrolloff", "by": "4"}}}},
		{"debounce+=1s", []Request{{Verb: "increase", Args: Arguments{"name": "debounce", "by": "1s"}}}},
		{"ratio^=3", []Request{{Verb: "multiply", Args: Arguments{"name": "ratio", "by": "3"}}}},
		{"theme=li", []Request{{Verb: "set", Args: Arguments{"name": "theme", "value": "light"}}}},
		{"title+=!", []Request{{Verb: "append", Args: Arguments{"name": "title", "value": "!"}}}},
		{"title^=a\\ ", []Request{{Verb: "prepend", Args: Arguments{"name": "title", "value": "a "}}}},
		{"title-=ai", []Request{{Verb: "remove", Args: Arguments{"name": "title", "value": "ai"}}}},
		{"paths+=b,c", []Request{{Verb: "append", Args: Arguments{"name": "paths", "value": "b,c"}}}},
		{`paths^=["z"]`, []Request{{Verb: "prepend", Args: Arguments{"name": "paths", "value": `["z"]`}}}},
		{"paths-=a", []Request{{Verb: "remove", Args: Arguments{"name": "paths", "value": "a"}}}},
		{"paths=", []Request{{Verb: "set", Args: Arguments{"name": "paths", "value": "[]"}}}},
		{"scrolloff=7 scrolloff&", []Request{{Verb: "set", Args: Arguments{"name": "scrolloff", "value": "7"}}, {Verb: "reset", Args: Arguments{"name": "scrolloff"}}}},
		{"scrolloff+=1 scrolloff+=1 wrap", []Request{{Verb: "increase", Args: Arguments{"name": "scrolloff", "by": "1"}}, {Verb: "increase", Args: Arguments{"name": "scrolloff", "by": "1"}}, {Verb: "set", Args: Arguments{"name": "wrap", "value": "true"}}}},
	}
	names := []string{"wrap", "scrolloff", "debounce", "ratio", "theme", "title", "paths"}
	for _, row := range rows {
		viaLine, _, _ := verbsFixture(t)
		if _, err := viaLine.Execute(Request{Noun: "ochazuke.settings", Verb: "apply", Args: Arguments{"line": row.line}}); err != nil {
			t.Fatalf("apply %q: %v", row.line, err)
		}
		direct, _, _ := verbsFixture(t)
		for _, q := range row.reqs {
			q.Noun = "ochazuke.setting"
			if _, err := direct.Execute(q); err != nil {
				t.Fatalf("%q: %s: %v", row.line, q.Key(), err)
			}
		}
		for _, name := range names {
			a, _ := viaLine.Settings().Value(name)
			b, _ := direct.Settings().Value(name)
			if !sameSetting(a, b) {
				t.Errorf("%q: %s is %#v by the line, %#v by the verbs", row.line, name, a, b)
			}
		}
	}
}

// A :set line is all or nothing: a word the parser refuses, or one whose
// result a verb refuses (level+=20 past Validate), changes nothing, the
// words before it included.
func TestSettingsApplyAllOrNothing(t *testing.T) {
	r, _, _ := verbsFixture(t)
	s := r.Settings()
	apply := func(line string) error {
		_, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "apply", Args: Arguments{"line": line}})
		return err
	}
	if err := apply("scrolloff=5 level+=20"); err == nil || err.Error() != "level: must be at most 9; nothing was changed" {
		t.Fatalf("refused result: %v", err)
	}
	var line *setLineError
	if err := apply("scrolloff=5 nope"); !errors.As(err, &line) || line.Word != 1 || !strings.HasSuffix(err.Error(), "; nothing was changed") {
		t.Fatalf("refused word: %v", err)
	}
	if err := apply("level+=5 level+=5"); err == nil || !strings.Contains(err.Error(), "level: must be at most 9") {
		t.Fatalf("a later word over an earlier one's value: %v", err)
	}
	if s.Int("scrolloff") != 2 || s.Int("level") != 1 || len(s.Diff()) != 0 {
		t.Fatalf("a refused line changed scrolloff %d level %d", s.Int("scrolloff"), s.Int("level"))
	}
}

// A line is one undo step on the Settings page; the page's undo puts back
// the session values and the file as they were.
func TestSettingsApplyIsOneUndoStep(t *testing.T) {
	r, dir, _ := verbsFixture(t)
	s := r.Settings()
	_ = s.Set("title", "saved")
	_, _ = s.SetSession("wrap", true)
	before := len(s.page.undo)
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "apply", Args: Arguments{"line": "scrolloff=5 nowrap title&"}}); err != nil {
		t.Fatal(err)
	}
	if len(s.page.undo) != before+1 || s.Int("scrolloff") != 5 || s.Bool("wrap") || s.String("title") != "main" {
		t.Fatalf("undo steps %d, scrolloff %d wrap %v title %q", len(s.page.undo)-before, s.Int("scrolloff"), s.Bool("wrap"), s.String("title"))
	}
	if readSettingsFile(t, dir)["scrolloff"] != nil {
		t.Fatal("a session line wrote settings.json")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if s.Int("scrolloff") != 2 || !s.Bool("wrap") || s.String("title") != "saved" || readSettingsFile(t, dir)["title"] != "saved" {
		t.Fatalf("after undo scrolloff %d wrap %v title %q", s.Int("scrolloff"), s.Bool("wrap"), s.String("title"))
	}
	if v, ok := s.Session("wrap"); !ok || v != true {
		t.Fatalf("undo lost the earlier session value: %v %v", v, ok)
	}
	// With settings.autosave on, the line writes the file at once.
	_ = s.Set(SettingsAutosaveSetting, true)
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "apply", Args: Arguments{"line": "scrolloff=6"}}); err != nil || readSettingsFile(t, dir)["scrolloff"] != 6.0 {
		t.Fatalf("autosave: %v %v", err, readSettingsFile(t, dir))
	}
}

// Show forms reach the status line together; a leading verbose adds each
// value's origin; the empty line and all answer lists (the palette opens
// them as results); all& asks first.
func TestSettingsApplyShowsAndLists(t *testing.T) {
	r, _, sink := verbsFixture(t)
	apply := func(line string) (any, error) {
		return r.Execute(Request{Noun: "ochazuke.settings", Verb: "apply", Args: Arguments{"line": line}})
	}
	got, err := apply("scrolloff=4 scrolloff? wrap? title paths")
	if err != nil || sink.LastInfo() != "scrolloff=4  nowrap  title=main  paths=a" {
		t.Fatalf("shows %q %v", sink.LastInfo(), err)
	}
	if a, ok := got.(SettingsApplied); !ok || len(a.Changed) != 1 || a.Changed[0].Name != "scrolloff" || len(a.Shown) != 4 {
		t.Fatalf("answer %#v", got)
	}
	if _, err := apply("verbose scrolloff?"); err != nil || !strings.HasPrefix(sink.LastInfo(), "scrolloff=4 — set this session by ochazuke.settings.apply (api, ") {
		t.Fatalf("verbose %q %v", sink.LastInfo(), err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "apply", Args: Arguments{"line": "modal-dim", "verbose": true}}); err != nil || sink.LastInfo() != "modal-dim=35 — default, settings/ochazuke.json (ochazuke)" {
		t.Fatalf("verbose argument %q %v", sink.LastInfo(), err)
	}
	if got, err := apply(""); err != nil || len(got.([]SettingDiff)) != 1 {
		t.Fatalf("empty line %#v %v", got, err)
	}
	if got, err := apply("all"); err != nil || len(got.([]SettingValue)) != len(r.Settings().Declared()) {
		t.Fatalf("all %T %v", got, err)
	}
	if _, err := apply("all& wrap"); err != nil || r.TopModal() != "confirm" || r.Instances[r.TopModal()].Payload.Prompt != "Reset all 1 changed setting to their defaults for this session?" {
		t.Fatalf("all& did not ask: %q %v", r.TopModal(), err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.confirm", Verb: "accept"}); err != nil || r.Settings().Int("scrolloff") != 2 || !r.Settings().Bool("wrap") {
		t.Fatalf("accept: %v %d", err, r.Settings().Int("scrolloff"))
	}
}

// The Settings page's keys run the setting verbs with scope file, so the
// page saves at once whether or not a session holds values; a palette
// verb on the focused row without a scope writes the session.
func TestSettingsPageKeysRunTheSettingVerbs(t *testing.T) {
	r, m, dir := settingsPageFixture(t)
	if !r.Settings().HasSession() {
		t.Fatal("the page fixture has no session")
	}
	_, _ = r.Execute(Request{Noun: "ochazuke.settings", Verb: "edit"})
	_ = r.SetFocus(settingRowPrefix + "scrolloff")
	for _, key := range []string{"l", "l", "h"} {
		keytest.Press(m, key)
	}
	if r.Settings().Int("scrolloff") != 3 || readSettingsFile(t, dir)["scrolloff"] != 3.0 {
		t.Fatalf("l l h: %d %v", r.Settings().Int("scrolloff"), readSettingsFile(t, dir))
	}
	last := r.History(1)[0]
	if last.Request.Key() != "ochazuke.setting.cycle" || last.Request.Args["scope"] != "file" {
		t.Fatalf("h ran %s %v", last.Request.Key(), last.Request.Args)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "cycle"}); err != nil || r.Settings().Int("scrolloff") != 4 || readSettingsFile(t, dir)["scrolloff"] != 3.0 {
		t.Fatalf("palette cycle: %v %d", err, r.Settings().Int("scrolloff"))
	}
	keytest.Press(m, "r")
	if r.Settings().Int("scrolloff") != 2 || readSettingsFile(t, dir)["scrolloff"] != nil {
		t.Fatalf("r: %d %v", r.Settings().Int("scrolloff"), readSettingsFile(t, dir))
	}
}
