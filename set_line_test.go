package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// setLineSettings are the declared settings the :set line tests read:
// one per class, and names that share prefixes.
func setLineSettings() []Setting {
	percent := func(v any) error {
		if n, _ := v.(int); n < 0 || n > 100 {
			return errors.New("must be 0 to 100")
		}
		return nil
	}
	engine := func(v any) error {
		if v == "ghostty" {
			return errors.New("ghostty is not in this build")
		}
		return nil
	}
	return []Setting{
		{Name: "help.pick-view", Type: SettingBoolean, Default: false},
		{Name: "modal-dim", Type: SettingInteger, Default: 35, Validate: percent},
		{Name: "zoom", Type: SettingNumber, Default: 1.0},
		{Name: "sequence-timeout", Type: SettingDuration, Default: 400 * time.Millisecond},
		{Name: "title", Type: SettingString, Default: ""},
		{Name: "focus-display", Type: SettingString, Default: "noun", Enum: []string{"off", "noun", "stack"}, Deprecated: []string{"focus-mode"}},
		{Name: "layout", Type: SettingString, Default: "tabs", Enum: []string{"split", "stack", "tabs"}},
		{Name: "path.no-auto-list", Type: SettingList, Default: []string{}},
		{Name: "terminal.engine", Type: SettingString, Default: "vt", Validate: engine},
		{Name: "tree.zoom", Type: SettingBoolean, Default: false},
		{Name: "tree.zoom.root", Type: SettingBoolean, Default: false},
		{Name: "tree.breadcrumb", Type: SettingBoolean, Default: true},
		{Name: "notify", Type: SettingBoolean, Default: true},
		{Name: "tify", Type: SettingBoolean, Default: false},
	}
}

// setGot is an operation as the tests compare it: the key without the
// framework namespace (setting.toggle, settings.diff), the name, the
// argument and the value.
type setGot struct {
	Form, Key, Name, Arg string
	Value                any
}

func (g setGot) String() string {
	return fmt.Sprintf("%s %s %s %s=%#v", g.Form, g.Key, g.Name, g.Arg, g.Value)
}

func setGots(ops []setOp) []setGot {
	out := make([]setGot, len(ops))
	for i, op := range ops {
		out[i] = setGot{op.Form, strings.TrimPrefix(op.Noun, FrameworkNamespace+".") + "." + op.Verb, op.Setting.Name, op.Arg, op.Value}
	}
	return out
}

func wantShow(name string) setGot  { return setGot{"", "setting.show", name, "", nil} }
func wantQuery(name string) setGot { return setGot{"?", "setting.show", name, "", nil} }
func wantSet(form, name string, v any) setGot {
	return setGot{form, "setting.set", name, "value", v}
}

// TestSetLineForms has one case per row of the plan's form table that the
// line grammar reads (rows 2, 21, 22, 28 to 30 are command words, read
// before the line; 31 to 34 are TestSetCursorAt; 35 to 38 are not line
// forms).
func TestSetLineForms(t *testing.T) {
	cases := []struct {
		row  int
		line string
		want []setGot
	}{
		{1, "", []setGot{{"line", "settings.diff", "", "", nil}}},
		{1, "   ", []setGot{{"line", "settings.diff", "", "", nil}}},
		{3, "all", []setGot{{"all", "settings.list", "", "", nil}}},
		{4, "all&", []setGot{{"all&", "settings.reset-all", "", "", nil}}},
		{5, "modal-dim?", []setGot{wantQuery("modal-dim")}},
		{5, "help.pick-view?", []setGot{wantQuery("help.pick-view")}},
		{6, "help.pick-view", []setGot{wantSet("", "help.pick-view", true)}},
		{7, "modal-dim", []setGot{wantShow("modal-dim")}},
		{7, "title", []setGot{wantShow("title")}},
		{8, "nohelp.pick-view", []setGot{wantSet("no", "help.pick-view", false)}},
		{9, "help.pick-view!", []setGot{{"!", "setting.toggle", "help.pick-view", "", nil}}},
		{10, "invhelp.pick-view", []setGot{{"inv", "setting.toggle", "help.pick-view", "", nil}}},
		{11, "modal-dim&", []setGot{{"&", "setting.reset", "modal-dim", "", nil}}},
		{12, "modal-dim&vim", []setGot{{"&", "setting.reset", "modal-dim", "", nil}}},
		{14, "modal-dim=40", []setGot{wantSet("=", "modal-dim", 40)}},
		{14, "help.pick-view=false", []setGot{wantSet("=", "help.pick-view", false)}},
		{14, "focus-display=st", []setGot{wantSet("=", "focus-display", "stack")}},
		{14, "sequence-timeout=2s", []setGot{wantSet("=", "sequence-timeout", 2*time.Second)}},
		{14, "zoom=0.5", []setGot{wantSet("=", "zoom", 0.5)}},
		{14, "title=", []setGot{wantSet("=", "title", "")}},
		{15, "modal-dim:40", []setGot{wantSet("=", "modal-dim", 40)}},
		{16, "modal-dim+=5", []setGot{{"+=", "setting.increase", "modal-dim", "by", 5}}},
		{16, "sequence-timeout+=100ms", []setGot{{"+=", "setting.increase", "sequence-timeout", "by", 100 * time.Millisecond}}},
		{16, "path.no-auto-list+=/mnt/", []setGot{{"+=", "setting.append", "path.no-auto-list", "value", []string{"/mnt/"}}}},
		{17, "zoom^=2", []setGot{{"^=", "setting.multiply", "zoom", "by", 2.0}}},
		{17, "title^=pre-", []setGot{{"^=", "setting.prepend", "title", "value", "pre-"}}},
		{18, "modal-dim-=5", []setGot{{"-=", "setting.decrease", "modal-dim", "by", 5}}},
		{18, "path.no-auto-list-=a,b", []setGot{{"-=", "setting.remove", "path.no-auto-list", "value", []string{"a", "b"}}}},
		{19, "modal-dim<", []setGot{{"<", "setting.revert", "modal-dim", "", nil}}},
		{20, "help.pick-view notree.zoom modal-dim=3 all", []setGot{
			wantSet("", "help.pick-view", true), wantSet("no", "tree.zoom", false), wantSet("=", "modal-dim", 3), {"all", "settings.list", "", "", nil},
		}},
		{24, `title=a\ b`, []setGot{wantSet("=", "title", "a b")}},
		{24, `title="a b"`, []setGot{wantSet("=", "title", "a b")}},
		{24, `title=a\\b`, []setGot{wantSet("=", "title", `a\b`)}},
		{24, `title=say\"hi\"`, []setGot{wantSet("=", "title", `say"hi"`)}},
		{24, `title=x"a b"y`, []setGot{wantSet("=", "title", "xa by")}},
		{25, "title=a|b", []setGot{wantSet("=", "title", "a|b")}},
		{26, "title=$HOME", []setGot{wantSet("=", "title", "$HOME")}},
		{27, "foc=noun", []setGot{wantSet("=", "focus-display", "noun")}},
		{27, "nohelp", []setGot{wantSet("no", "help.pick-view", false)}},
		{27, "tree.zoom!", []setGot{{"!", "setting.toggle", "tree.zoom", "", nil}}},
		{27, "Modal-Dim?", []setGot{wantQuery("modal-dim")}},
		{27, "focus-mode=stack", []setGot{wantSet("=", "focus-display", "stack")}},
		{27, "notify", []setGot{wantSet("", "notify", true)}},
		{27, "nonotify", []setGot{wantSet("no", "notify", false)}},
	}
	settings := setLineSettings()
	for _, c := range cases {
		t.Run(fmt.Sprintf("row%d/%s", c.row, c.line), func(t *testing.T) {
			ops, err := parseSetLine(c.line, settings)
			if err != nil {
				t.Fatalf("parseSetLine(%q): %v", c.line, err)
			}
			if got := setGots(ops); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("parseSetLine(%q)\n got %v\nwant %v", c.line, got, c.want)
			}
		})
	}
}

// TestSetLineTypeTable has one case per cell of the plan's type table:
// each form on a setting of each class gives the cell's verb, or the
// error naming what the class takes.
func TestSetLineTypeTable(t *testing.T) {
	type sample struct {
		name, word string
		value      any
	}
	classes := []struct {
		class string
		sample
	}{
		{SettingBoolean, sample{"help.pick-view", "true", true}},
		{SettingInteger, sample{"modal-dim", "3", 3}},
		{SettingNumber, sample{"zoom", "1.5", 1.5}},
		{SettingDuration, sample{"sequence-timeout", "100ms", 100 * time.Millisecond}},
		{SettingString, sample{"title", "ab", "ab"}},
		{setEnum, sample{"focus-display", "noun", "noun"}},
		{SettingList, sample{"path.no-auto-list", "a,b", []string{"a", "b"}}},
	}
	// Each form, how it is written, and per class (in the order above)
	// the verb, or "!" and the error.
	onOff := func(name, is, hint string) string { return "!" + name + " is " + is + ", not on/off: " + hint }
	enumOnOff := onOff("focus-display", "one of off, noun, stack", "use setting.cycle to step through them, or focus-display=VALUE")
	forms := []struct {
		form, spell string
		cells       [7]string
	}{
		{"", "%s", [7]string{"set", "show", "show", "show", "show", "show", "show"}},
		{"no", "no%s", [7]string{"set",
			onOff("modal-dim", "an integer", "use modal-dim=N"), onOff("zoom", "a number", "use zoom=N"),
			onOff("sequence-timeout", "a duration", "use sequence-timeout=400ms"), onOff("title", "text", "use title=TEXT"),
			enumOnOff, onOff("path.no-auto-list", "a list", "use path.no-auto-list=a,b")}},
		{"!", "%s!", [7]string{"toggle",
			onOff("modal-dim", "an integer", "use modal-dim=N"), onOff("zoom", "a number", "use zoom=N"),
			onOff("sequence-timeout", "a duration", "use sequence-timeout=400ms"), onOff("title", "text", "use title=TEXT"),
			enumOnOff, onOff("path.no-auto-list", "a list", "use path.no-auto-list=a,b")}},
		{"inv", "inv%s", [7]string{"toggle",
			onOff("modal-dim", "an integer", "use modal-dim=N"), onOff("zoom", "a number", "use zoom=N"),
			onOff("sequence-timeout", "a duration", "use sequence-timeout=400ms"), onOff("title", "text", "use title=TEXT"),
			enumOnOff, onOff("path.no-auto-list", "a list", "use path.no-auto-list=a,b")}},
		{"?", "%s?", [7]string{"show", "show", "show", "show", "show", "show", "show"}},
		{"&", "%s&", [7]string{"reset", "reset", "reset", "reset", "reset", "reset", "reset"}},
		{"&", "%s&vim", [7]string{"reset", "reset", "reset", "reset", "reset", "reset", "reset"}},
		{"<", "%s<", [7]string{"revert", "revert", "revert", "revert", "revert", "revert", "revert"}},
		{"=", "%s=WORD", [7]string{"set", "set", "set", "set", "set", "set", "set"}},
		{"=", "%s:WORD", [7]string{"set", "set", "set", "set", "set", "set", "set"}},
		{"+=", "%s+=WORD", [7]string{
			"!help.pick-view takes = only: += is for numbers, durations, text and lists",
			"increase", "increase", "increase", "append",
			"!focus-display takes = only: += is for numbers, durations, text and lists",
			"append"}},
		{"^=", "%s^=WORD", [7]string{
			"!help.pick-view takes = only: ^= is for numbers, text and lists",
			"multiply", "multiply",
			"!sequence-timeout takes =, += and -=: ^= is for numbers, text and lists",
			"prepend",
			"!focus-display takes = only: ^= is for numbers, text and lists",
			"prepend"}},
		{"-=", "%s-=WORD", [7]string{
			"!help.pick-view takes = only: -= is for numbers, durations, text and lists",
			"decrease", "decrease", "decrease", "remove",
			"!focus-display takes = only: -= is for numbers, durations, text and lists",
			"remove"}},
	}
	settings := setLineSettings()
	cells := 0
	for _, f := range forms {
		for i, c := range classes {
			line := strings.Replace(fmt.Sprintf(f.spell, c.name), "WORD", c.word, 1)
			want := f.cells[i]
			cells++
			t.Run(c.class+"/"+line, func(t *testing.T) {
				ops, err := parseSetLine(line, settings)
				if msg, isErr := strings.CutPrefix(want, "!"); isErr {
					if err == nil || err.Error() != msg {
						t.Fatalf("parseSetLine(%q) = %v, %v; want error %q", line, setGots(ops), err, msg)
					}
					return
				}
				if err != nil {
					t.Fatalf("parseSetLine(%q): %v", line, err)
				}
				if len(ops) != 1 || ops[0].Verb != want || ops[0].Noun != setSettingNoun || ops[0].Setting.Name != c.name || ops[0].Form != f.form {
					t.Fatalf("parseSetLine(%q) = %v; want %s %s.%s", line, setGots(ops), f.form, setSettingNoun, want)
				}
				var value any
				switch {
				case ops[0].Arg != "" && f.form == "":
					value = true
				case ops[0].Arg != "" && f.form == "no":
					value = false
				case ops[0].Arg != "":
					value = c.value
				}
				if !reflect.DeepEqual(ops[0].Value, value) {
					t.Fatalf("parseSetLine(%q) value = %#v; want %#v", line, ops[0].Value, value)
				}
			})
		}
	}
	if cells != 13*7 {
		t.Fatalf("type table cells = %d", cells)
	}
}

// TestSetLineValues covers the value syntax: lists (commas, \, and JSON
// arrays), escapes, enum prefixes and number types.
func TestSetLineValues(t *testing.T) {
	cases := []struct {
		line string
		want any
	}{
		{"path.no-auto-list=a,b", []string{"a", "b"}},
		{`path.no-auto-list=a\,b,c`, []string{"a,b", "c"}},
		{`path.no-auto-list="x,y",z`, []string{"x,y", "z"}},
		{`path.no-auto-list=a\ b,c`, []string{"a b", "c"}},
		{`path.no-auto-list=["a b","c"]`, []string{"a b", "c"}},
		{`path.no-auto-list=["a", "b"]`, []string{"a", "b"}},
		{`path.no-auto-list=[]`, []string{}},
		{"path.no-auto-list=", []string{}},
		{"path.no-auto-list=one", []string{"one"}},
		{"focus-display=NOUN", "noun"},
		{"focus-display=o", "off"},
		{"layout=sp", "split"},
		{"modal-dim^=2", 2},
		{"zoom+=1", 1.0},
		{"sequence-timeout-=1s", time.Second},
		{"help.pick-view=1", true},
		{"title=héllo", "héllo"},
		{`title=trailing\`, `trailing\`},
	}
	settings := setLineSettings()
	for _, c := range cases {
		ops, err := parseSetLine(c.line, settings)
		if err != nil {
			t.Fatalf("parseSetLine(%q): %v", c.line, err)
		}
		if len(ops) != 1 || !reflect.DeepEqual(ops[0].Value, c.want) {
			t.Fatalf("parseSetLine(%q) = %v; want value %#v", c.line, setGots(ops), c.want)
		}
	}
	ops, err := parseSetLine(`path.no-auto-list=["a", "b"] modal-dim=3`, settings)
	if err != nil || len(ops) != 2 || ops[1].Value != 3 {
		t.Fatalf("a JSON array with spaces then a word = %v, %v", setGots(ops), err)
	}
}

// TestSetLineErrors has every error text (golden strings).
func TestSetLineErrors(t *testing.T) {
	cases := []struct {
		line, want string
	}{
		{"focus-dispaly", "unknown setting focus-dispaly: did you mean focus-display, modal-dim, notify?"},
		{"nofocsu", "unknown setting nofocsu: did you mean focus-display, zoom, layout?"},
		{"=3", "a setting name comes before the operator"},
		{"tree", "tree is ambiguous: tree.zoom, tree.zoom.root, tree.breadcrumb"},
		{"t", "t is ambiguous: title, terminal.engine, tree.zoom, tree.zoom.root, tree.breadcrumb, …"},
		{"notree", "notree is ambiguous: notree.zoom, notree.zoom.root, notree.breadcrumb"},
		{"nomodal-dim", "modal-dim is an integer, not on/off: use modal-dim=N"},
		{"focus-display!", "focus-display is one of off, noun, stack, not on/off: use setting.cycle to step through them, or focus-display=VALUE"},
		{"modal-dim=3x", "modal-dim takes a whole number: 3x"},
		{"modal-dim=0x1f", "modal-dim takes a whole number: 0x1f"},
		{"modal-dim=", `modal-dim takes a whole number: ""`},
		{"modal-dim^=1.5", "modal-dim takes a whole number: 1.5"},
		{"modal-dim=200", "modal-dim: must be 0 to 100"},
		{"zoom=x", "zoom takes a number: x"},
		{"help.pick-view=maybe", "help.pick-view takes true or false: maybe"},
		{"focus-display=stak", "focus-display has no value stak: off, noun, stack"},
		{"focus-display=", `focus-display has no value "": off, noun, stack`},
		{"layout=s", "layout: s is ambiguous: split, stack"},
		{"sequence-timeout=soon", "sequence-timeout takes a duration such as 400ms or 2s: soon"},
		{"sequence-timeout+=soon", "sequence-timeout takes a duration such as 400ms or 2s: soon"},
		{"path.no-auto-list=[1]", `path.no-auto-list takes a list such as a,b or ["a","b"]: [1]`},
		{"focus-display+=x", "focus-display takes = only: += is for numbers, durations, text and lists"},
		{"terminal.engine=ghostty", "terminal.engine: ghostty is not in this build"},
		{"modal-dim?x", "unexpected text after modal-dim?: x"},
		{"modal-dim<x", "unexpected text after modal-dim<: x"},
		{"modal-dim&x", "unexpected text after modal-dim&: x"},
		{"help.pick-view!x", "unexpected text after help.pick-view!: x"},
		{"mod?x", "unexpected text after modal-dim?: x"},
		{"nohelp.pick-view=1", "unexpected text after nohelp.pick-view: =1"},
		{"invhelp.pick-view?", "unexpected text after invhelp.pick-view: ?"},
		{"focus-display&vi", "settings have one default: use focus-display&"},
		{"help.pick-view modal-dim=3x title=x", "modal-dim takes a whole number: 3x; nothing was changed"},
		{"all=1", "unknown setting all: did you mean help.pick-view, zoom, title?"},
	}
	settings := setLineSettings()
	for _, c := range cases {
		ops, err := parseSetLine(c.line, settings)
		if err == nil || err.Error() != c.want {
			t.Errorf("parseSetLine(%q) = %v, %v\nwant error %q", c.line, setGots(ops), err, c.want)
		}
		if ops != nil {
			t.Errorf("parseSetLine(%q) answered operations with its error", c.line)
		}
	}
	if _, err := parseSetLine("x", nil); err == nil || err.Error() != "unknown setting x: this application declares none" {
		t.Errorf("no settings: %v", err)
	}
	_, err := parseSetLine("help.pick-view modal-dim=3x", settings)
	var arg *ArgumentError
	var line *setLineError
	if !errors.As(err, &arg) || !errors.As(err, &line) || line.Word != 1 {
		t.Fatalf("a refused word is an ArgumentError naming the word: %#v", err)
	}
	if _, err := parseSetLine("x", []Setting{{Name: "x", Type: "color"}}); err == nil || err.Error() != "x has a type :set does not read: color" {
		t.Errorf("unknown type: %v", err)
	}
}

func TestSetOpRequest(t *testing.T) {
	ops, err := parseSetLine("modal-dim+=5 all help.pick-view!", setLineSettings())
	if err != nil {
		t.Fatal(err)
	}
	want := []Request{
		{Noun: "ochazuke.setting", Verb: "increase", Args: Arguments{"name": "modal-dim", "by": 5}},
		{Noun: "ochazuke.settings", Verb: "list"},
		{Noun: "ochazuke.setting", Verb: "toggle", Args: Arguments{"name": "help.pick-view"}},
	}
	for i, op := range ops {
		if got := op.request(); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("op %d request = %#v; want %#v", i, got, want[i])
		}
	}
}

// TestSetWords checks the word spans, in runes.
func TestSetWords(t *testing.T) {
	line := `  modal-dim+=5 title=hé\ llo  foc? path.no-auto-list=["a", "b"]x`
	type span struct {
		Start, End, NameEnd, OpEnd int
		Name, Op, Rest             string
	}
	want := []span{
		{2, 14, 11, 13, "modal-dim", "+=", "5"},
		{15, 28, 20, 21, "title", "=", `hé\ llo`},
		{30, 34, 33, 34, "foc", "?", ""},
		{35, 64, 52, 53, "path.no-auto-list", "=", `["a", "b"]x`},
	}
	words := setWords(line)
	if len(words) != len(want) {
		t.Fatalf("setWords = %+v", words)
	}
	rs := []rune(line)
	for i, w := range words {
		got := span{w.Start, w.End, w.NameEnd, w.OpEnd, w.Name, w.Op, w.Rest}
		if got != want[i] {
			t.Errorf("word %d = %+v; want %+v", i, got, want[i])
		}
		if w.Text != string(rs[w.Start:w.End]) {
			t.Errorf("word %d text %q", i, w.Text)
		}
	}
	if words := setWords(`title=[a b`); len(words) != 2 || words[0].Rest != "[a" {
		t.Errorf("an unclosed [ reads as any value: %+v", words)
	}
}

// TestSetCursorAt covers the completion positions (form rows 31 to 34:
// a name, the value after =, and the items after -=).
func TestSetCursorAt(t *testing.T) {
	cases := []struct {
		line          string
		cursor        int
		word, words   int
		part          string
		start         int
		typed, wordOp string
	}{
		{"", 0, 0, 1, setPartName, 0, "", ""},
		{"foc", 3, 0, 1, setPartName, 0, "foc", ""},
		{"foc", 1, 0, 1, setPartName, 0, "f", ""},
		{"nohel", 5, 0, 1, setPartName, 0, "nohel", ""},
		{"foc ", 4, 1, 2, setPartName, 4, "", ""},
		{"a  b", 2, 1, 3, setPartName, 2, "", ""},
		{"a b", 2, 1, 2, setPartName, 2, "", ""},
		{"modal-dim=", 10, 0, 1, setPartValue, 10, "", "="},
		{"modal-dim=4", 11, 0, 1, setPartValue, 10, "4", "="},
		{"path.no-auto-list-=a,b", 22, 0, 1, setPartValue, 19, "a,b", "-="},
		{"modal-dim+=", 10, 0, 1, setPartOperator, 9, "+", "+="},
		{"modal-dim?", 10, 0, 1, setPartOperator, 9, "?", "?"},
		{`title="a b" x`, 9, 0, 2, setPartValue, 6, `"a `, "="},
		{`title="a b" x`, 13, 1, 2, setPartName, 12, "x", ""},
		{"héllo=wörld", 11, 0, 1, setPartValue, 6, "wörld", "="},
		{"foc", 99, 0, 1, setPartName, 0, "foc", ""},
	}
	for _, c := range cases {
		got := setCursorAt(c.line, c.cursor)
		if got.Word != c.word || len(got.Words) != c.words || got.Part != c.part || got.Start != c.start || got.Typed != c.typed || got.Words[got.Word].Op != c.wordOp {
			t.Errorf("setCursorAt(%q, %d) = word %d of %d, %s at %d %q, op %q; want word %d of %d, %s at %d %q, op %q",
				c.line, c.cursor, got.Word, len(got.Words), got.Part, got.Start, got.Typed, got.Words[got.Word].Op,
				c.word, c.words, c.part, c.start, c.typed, c.wordOp)
		}
	}
}
