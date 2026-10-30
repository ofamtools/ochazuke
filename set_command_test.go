package ochazuke

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// setPalette is sessionFixture with its Model's palette open over the app.
func setPalette(t *testing.T) (*Runtime, *Palette, string) {
	t.Helper()
	r, m, dir := sessionFixture(t)
	if err := r.DeclareSetting(Setting{Name: "paths", Type: SettingList, Default: []string{"a b", "c,d", "e"}, Description: "Paths"}); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	return r, m.Palette, dir
}

// A rest parameter (Parameter.Rest) takes the line from its first word to
// the end, as typed: no quotes needed, and quotes and spaces kept.
func TestRestParameterTakesTheLineVerbatim(t *testing.T) {
	r, p, _ := setPalette(t)
	for _, tc := range []struct{ line, rest string }{
		{"settings apply scrolloff=5 wrap", "scrolloff=5 wrap"},
		{"apply settings  scrolloff=5   nowrap ", "scrolloff=5   nowrap "},
		{`set theme=light paths="x y",z`, `theme=light paths="x y",z`},
		{`se paths=a\ b`, `paths=a\ b`},
	} {
		c := p.parse(tc.line)
		if c.Err != nil || c.Op.Key() != "ochazuke.settings.apply" || c.Args["line"] != tc.rest {
			t.Fatalf("%q: op %s args %v err %v", tc.line, c.Op.Key(), c.Args, c.Err)
		}
	}
	paletteLine(t, r, "settings apply scrolloff=5 wrap")
	if r.Settings().Int("scrolloff") != 5 || !r.Settings().Bool("wrap") {
		t.Fatalf("scrolloff %d wrap %v", r.Settings().Int("scrolloff"), r.Settings().Bool("wrap"))
	}
	// Quoting stays as it was for every other parameter.
	if c := p.parse(`settings set theme "light"`); c.Err != nil || c.Args["value"] != "light" {
		t.Fatalf("quoted word: %v %v", c.Args, c.Err)
	}
}

// Register refuses two rest parameters, a rest parameter that is not a
// string, and a required parameter after it.
func TestRestParameterRegistration(t *testing.T) {
	run := func(*Runtime, Context, Arguments) (any, error) { return nil, nil }
	for _, params := range []map[string]Parameter{
		{"a": {Type: "string", Rest: true}, "b": {Type: "string", Rest: true}},
		{"a": {Type: "integer", Rest: true}},
		{"a": {Type: "string", Rest: true}, "b": {Type: "string", Required: true}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("registered %v", params)
				}
			}()
			NewRuntime().Register(Operation{Noun: "thing", Verb: "do", Parameters: params, Positional: []string{"a"}, Run: run})
		}()
	}
}

// set, se, setl, setlocal, setg and setglobal are the :set line; options
// and opt open the Settings page; set alone lists what differs.
func TestSetAliases(t *testing.T) {
	_, p, _ := setPalette(t)
	for _, word := range []string{"set", "se", "setl", "setlocal", "setg", "setglobal"} {
		c := p.parse(word + " scrolloff?")
		if c.Err != nil || c.Op.Key() != "ochazuke.settings.apply" || c.Args["line"] != "scrolloff?" || c.Via != word && c.Via != "ochazuke.settings."+word {
			t.Fatalf("%s: %s %v via %q err %v", word, c.Op.Key(), c.Args, c.Via, c.Err)
		}
	}
	for _, word := range []string{"options", "opt"} {
		if c := p.parse(word); !c.Complete() || c.Op.Key() != "ochazuke.settings.edit" {
			t.Fatalf("%s: %s %v", word, c.Op.Key(), c.Err)
		}
	}
	if c := p.parse("set"); !c.Complete() || c.Op.Key() != "ochazuke.settings.apply" || len(c.Args) != 0 {
		t.Fatalf("bare set: %s %v %v", c.Op.Key(), c.Args, c.Err)
	}
}

// Plan decision 4: set NOUN runs that noun's set verb when NOUN is a noun
// with one; an exact noun wins, then a setting (exact, then a unique
// prefix); a word both a noun's prefix and a setting's is ambiguous.
func TestSetResolvesByTheWordAfterIt(t *testing.T) {
	r, p, _ := setPalette(t)
	if err := r.DeclareSetting(
		Setting{Name: "mode-line", Type: SettingBoolean, Default: false, Description: "A setting named like a noun's prefix"},
		Setting{Name: "con", Type: SettingBoolean, Default: false, Description: "A setting that is a noun's prefix"},
	); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ line, op, err string }{
		{"set mode normal", "ochazuke.mode.set", ""},
		{"set focus", "ochazuke.focus.set", ""}, // exact noun over focus-display
		{"set settings theme light", "ochazuke.settings.set", ""},
		{"set appearance", "ochazuke.appearance.set", ""},
		{"set con", "ochazuke.settings.apply", ""}, // an exact setting over the noun confirm
		{"set foc", "", `"foc" is ambiguous: noun focus (focus set), or line focus-display`},
		{"set mod", "", `"mod" is ambiguous: noun mode (mode set), or line modal-dim, modal-highlights, modal.anchor, mode-badge.key-case, mode-line`},
		{"set focus-display=stack", "ochazuke.settings.apply", ""},
		{"set scroll", "ochazuke.settings.apply", ""}, // a setting's prefix only
		{"set conf", "ochazuke.confirm.set", ""},      // a noun's prefix only
		{"set zzz", "ochazuke.settings.apply", ""},    // neither: the line says what is wrong
		{"set nowrap scrolloff=3", "ochazuke.settings.apply", ""},
	} {
		c := p.parse(tc.line)
		if tc.err != "" {
			if c.Err == nil || c.Err.Error() != tc.err {
				t.Errorf("%q: err %v, want %q", tc.line, c.Err, tc.err)
			}
			continue
		}
		if c.Err != nil || c.Op.Key() != tc.op {
			t.Errorf("%q: %s err %v, want %s", tc.line, c.Op.Key(), c.Err, tc.op)
		}
	}
	paletteLine(t, r, "set mode normal")
	if r.Mode.Name != "normal" {
		t.Fatalf("mode %q", r.Mode.Name)
	}
}

// :verbose set opt? is settings.apply {verbose: true} over opt?.
func TestVerboseSet(t *testing.T) {
	r, p, _ := setPalette(t)
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	for _, line := range []string{"verbose set scrolloff?", "verb se scrolloff?", "verbose scrolloff?"} {
		c := p.parse(line)
		if c.Err != nil || c.Op.Key() != "ochazuke.settings.apply" || c.Args["line"] != "scrolloff?" || c.Args["verbose"] != true {
			t.Fatalf("%q: %s %v %v", line, c.Op.Key(), c.Args, c.Err)
		}
	}
	paletteLine(t, r, "verbose set scrolloff?")
	if got := sink.LastInfo(); !strings.HasPrefix(got, "scrolloff=2 — default, declared by ") {
		t.Fatalf("verbose: %q", got)
	}
}

// Completion over the line (plan rows 31 to 34): names with no and inv
// for booleans and all, all& first; = inserts the current value, then an
// enum's values or true and false; -= offers a list's items.
func TestSetLineCompletion(t *testing.T) {
	r, p, _ := setPalette(t)
	s := r.Settings()
	ids := func(line string) []string {
		var out []string
		for _, c := range settingsCompletions(s, line, len([]rune(line))) {
			out = append(out, c.ID)
		}
		return out
	}
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"scr", []string{"scrolloff"}},
		{"nowrap w", []string{"nowrap wrap"}},
		{"now", []string{"nowrap"}},
		{"invw", []string{"invwrap"}},
		{"all", []string{"all", "all&"}},
		{"wrap all", nil},
		{"scrolloff=", []string{"scrolloff=2"}},
		{"theme=", []string{"theme=dark", "theme=light", "theme=mono"}},
		{"theme=l", []string{"theme=light"}},
		{"wrap=", []string{"wrap=false", "wrap=true"}},
		{"paths=", []string{`paths=a\ b,c\,d,e`}},
		{"paths-=", []string{`paths-=a\ b`, `paths-=c\,d`, "paths-=e"}},
		{"paths-=c", []string{`paths-=c\,d`}},
		{"scrolloff+=", nil},
		{"zzz=", nil},
	} {
		if got := ids(tc.line); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q completes %q, want %q", tc.line, got, tc.want)
		}
	}
	// In the middle of the line, only the text up to the cursor counts.
	if got := settingsCompletions(s, "scr wrap", 3); len(got) != 1 || got[0].ID != "scrolloff" {
		t.Fatalf("mid-line: %v", got)
	}

	// On the palette, tab splices the choice over the rest's text, with
	// no space after it, so = can follow.
	paletteRequest(t, r, "set", "palette", Arguments{"text": "set nowrap scr"})
	entries := p.completions()
	if len(entries) != 1 || entries[0].Value != "nowrap scrolloff" || !entries[0].Splice || entries[0].From != 4 {
		t.Fatalf("entries %+v", entries)
	}
	paletteRequest(t, r, "complete", "palette", nil)
	if p.Text() != "set nowrap scrolloff" {
		t.Fatalf("after tab: %q", p.Text())
	}
	paletteRequest(t, r, "set", "palette", Arguments{"text": "set scrolloff="})
	paletteRequest(t, r, "complete", "palette", nil)
	if p.Text() != "set scrolloff=2" {
		t.Fatalf("= tab: %q", p.Text())
	}
	// set and a space completes the names; the header names the line.
	paletteRequest(t, r, "set", "palette", Arguments{"text": "set "})
	if entries := p.completions(); len(entries) < 5 || !slices.ContainsFunc(entries, func(e PaletteEntry) bool { return e.Value == "scrolloff" }) || entries[len(entries)-1].Value != "all&" {
		t.Fatalf("set space: %+v", entries)
	}
	if slot, ok := p.currentSlot(); !ok || slot.Name != "line" {
		t.Fatalf("slot %+v %v", slot, ok)
	}
}

// The CLI joins a rest parameter's words, and reads a bare set as the
// palette does.
func TestSetFromCLIRequests(t *testing.T) {
	r, _, _ := setPalette(t)
	reading, ok, err := r.ReadBareAlias(r.Context(), []string{"set", "mode", "normal"}, nil)
	if err != nil || !ok || reading.Noun != "mode" {
		t.Fatalf("set mode: %+v %v %v", reading, ok, err)
	}
	reading, ok, err = r.ReadBareAlias(r.Context(), []string{"verbose", "set", "scrolloff?"}, nil)
	if err != nil || !ok || reading.Skip != 2 || reading.Args["verbose"] != true || reading.Noun != "" {
		t.Fatalf("verbose set: %+v %v %v", reading, ok, err)
	}
	if _, _, err := r.ReadBareAlias(r.Context(), []string{"set", "foc"}, nil); err == nil {
		t.Fatal("set foc read without an ambiguity")
	}
	q := r.CanonicalRequest(Request{Verb: "set"})
	if q.Key() != "ochazuke.settings.apply" || q.Via != "set" {
		t.Fatalf("bare set request: %+v", q)
	}
	if q := r.CanonicalRequest(Request{Noun: "settings", Verb: "set"}); q.Key() != "ochazuke.settings.set" {
		t.Fatalf("settings set: %+v", q)
	}
}

// setCollision is a row of testdata/set-collisions.json: a noun word that
// is a setting's name or a prefix of one, in the family's apps, and how
// set WORD reads: "NOUN set" or "line" (the :set line).
type setCollision struct {
	Noun     string   `json:"noun"`
	Settings []string `json:"settings"`
	Reads    string   `json:"reads"`
	Apps     []string `json:"apps"`
}

// Every family app's nouns against its settings (gathered from each
// app's catalog and settings names): set WORD reads as the fixture says,
// so a new clash shows up here before release. A noun word that the
// framework has is the framework's noun; an app's own noun is registered
// without a set verb, as each is today.
func TestSetCollisionsAcrossTheFamily(t *testing.T) {
	data, err := os.ReadFile("testdata/set-collisions.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []setCollision
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	r, p, _ := setPalette(t)
	declared := map[string]bool{}
	for _, one := range r.Settings().Declared() {
		declared[one.Name] = true
	}
	for _, row := range rows {
		for _, name := range row.Settings {
			if !declared[name] {
				declared[name] = true
				if err := r.DeclareSetting(Setting{Name: name, Type: SettingBoolean, Default: false, Description: "fixture"}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if r.Nouns[r.CanonicalNoun(row.Noun)] == nil {
			r.Register(Operation{Noun: row.Noun, Verb: "list", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
		}
	}
	for _, row := range rows {
		c := p.parse("set " + row.Noun)
		got := "line"
		if c.Op.Key() != "ochazuke.settings.apply" {
			got = p.r.DisplayNoun(c.Op.Noun) + " " + c.Op.Verb
		}
		if c.Err != nil || got != row.Reads {
			t.Errorf("set %s reads %q (err %v), fixture says %q", row.Noun, got, c.Err, row.Reads)
		}
	}
	// The framework's own: nouns with a set verb whose word is a
	// setting's name or prefix.
	var own []string
	for _, word := range r.shadowNouns("set") {
		if !r.shadowsNoun(word, "set") {
			continue // a store's standard set never reads a setting's word
		}
		for _, one := range r.Settings().Declared() {
			if strings.HasPrefix(one.Name, word) && !slices.Contains(own, word) && slices.ContainsFunc(rows, func(c setCollision) bool { return c.Noun == word }) {
				own = append(own, word)
			}
		}
	}
	slices.Sort(own)
	if want := []string{"focus", "palette", "setting", "settings"}; !reflect.DeepEqual(own, want) {
		t.Fatalf("framework collisions %v, want %v", own, want)
	}
}

// History recall spells a :set line back as it was typed: by its alias,
// the rest as it was.
func TestSetLineRecall(t *testing.T) {
	r, p, _ := setPalette(t)
	paletteLine(t, r, `set scrolloff=5 paths=x\ y,z`)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "verbose set scrolloff?")
	paletteRequest(t, r, "open", "palette", nil)
	if got := p.recallLines(); len(got) < 2 || got[0] != "verbose scrolloff?" || got[1] != `set scrolloff=5 paths=x\ y,z` {
		t.Fatalf("recall %q", got)
	}
}

// A noun registered later takes a bare alias's word (o-inventory's verb
// noun over the framework's verb alias): the alias gives way, recorded.
func TestBareAliasGivesWayToALaterNoun(t *testing.T) {
	r := NewRuntime()
	r.Register(Operation{Noun: "verb", Verb: "list", Run: func(*Runtime, Context, Arguments) (any, error) { return "listed", nil }})
	if _, ok := r.bareAlias("verb"); ok {
		t.Fatal("the verb alias outlived the verb noun")
	}
	if got := r.ActionAliasProblems(); !slices.Contains(got, "alias verb of settings.apply dropped: a noun of that name is registered") {
		t.Fatalf("problems %q", got)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	if c := p.parse("verb list"); c.Op.Key() != "verb.list" {
		t.Fatalf("verb list: %s %v", c.Op.Key(), c.Err)
	}
	if c := p.parse("verbose set scrolloff?"); c.Op.Key() != "ochazuke.settings.apply" {
		t.Fatalf("verbose still: %s %v", c.Op.Key(), c.Err)
	}
}
