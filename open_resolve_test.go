package ochazuke

import (
	"reflect"
	"sync"
	"testing"
)

// fakeOpener records what would have been launched; nothing starts.
type fakeOpener struct {
	mu   sync.Mutex
	runs [][]string
}

func (f *fakeOpener) opener(goos string) Opener {
	return Opener{GOOS: goos, Start: func(name string, args ...string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.runs = append(f.runs, append([]string{name}, args...))
		return nil
	}}
}

func openFixture(t *testing.T) (*Runtime, *fakeOpener, *[]Request) {
	t.Helper()
	r := NewRuntime()
	f := &fakeOpener{}
	r.SetOpener(f.opener("darwin"))
	var ran []Request
	record := func(noun, verb string) Operation {
		return Operation{Key: KeyOf(noun, verb), Description: "test", Parameters: map[string]Parameter{"id": {Type: "string"}, "target": {Type: "string"}}, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			ran = append(ran, Request{Noun: noun, Verb: verb, Args: a})
			return nil, nil
		}}
	}
	r.Register(record("lane", "open"))
	r.Register(record("doc", "show"))
	r.Register(record("viewer", "view"))
	return r, f, &ran
}

func TestOpenerCommands(t *testing.T) {
	t.Parallel()
	cases := []struct {
		goos         string
		open, reveal []string
	}{
		{"darwin", []string{"open", "--", "/a b"}, []string{"open", "-R", "--", "/a b"}},
		{"linux", []string{"xdg-open", "/a b"}, []string{"xdg-open", "/"}},
		{"windows", []string{"rundll32", "url.dll,FileProtocolHandler", "/a b"}, []string{"explorer", "/select,/a b"}},
	}
	for _, c := range cases {
		f := &fakeOpener{}
		o := f.opener(c.goos)
		if err := o.Open("/a b"); err != nil {
			t.Fatal(err)
		}
		if err := o.Reveal("/a b"); err != nil {
			t.Fatal(err)
		}
		want := [][]string{c.open, c.reveal}
		if !reflect.DeepEqual(f.runs, want) {
			t.Errorf("%s: ran %v, want %v", c.goos, f.runs, want)
		}
	}
}

func TestResolveOpenRowKinds(t *testing.T) {
	t.Parallel()
	r, _, _ := openFixture(t)
	cases := []struct {
		text, kind, row string
		verb, target    string
	}{
		{"https://example.com/x", OpenActionOS, "scheme", "", ""},
		{"mailto:a@b.c", OpenActionOS, "scheme", "", ""},
		{"/tmp/x.bin", OpenActionOS, "prefix", "", ""},
		{"./notes", OpenActionOS, "prefix", "", ""},
		{"photo.PNG", OpenActionOS, "ext", "", ""},
		{"lane:abc123", OpenActionVerb, "noun", "open", "abc123"},
		{"doc:7", OpenActionVerb, "noun", "show", "7"},
	}
	for _, c := range cases {
		_, a, err := r.ResolveOpen(c.text)
		if err != nil {
			t.Fatal(err)
		}
		if a.Kind != c.kind || a.Unmatched || a.Row == nil || openRowKindNames[a.Row.level()] != c.row || a.Verb != c.verb || a.ID != c.target {
			t.Errorf("%q: %+v row %+v", c.text, a, a.Row)
		}
	}
}

func TestResolveOpenSpecificity(t *testing.T) {
	t.Parallel()
	r, _, _ := openFixture(t)
	// A scheme beats an extension; a noun's own row beats the any-noun row.
	if err := r.Settings().Set(OpenOpenersSetting, `[
		{"ext":"png","action":"viewer","to":"viewer.view"},
		{"noun":"lane","action":"page","to":"lanes"},
		{"noun":"doc","action":"verb","to":"open"}]`); err != nil {
		t.Fatal(err)
	}
	_, a, _ := r.ResolveOpen("https://x/y.png")
	if a.Kind != OpenActionOS || a.Row.Scheme != "https" {
		t.Errorf("scheme over ext: %+v", a)
	}
	_, a, _ = r.ResolveOpen("y.png")
	if a.Kind != OpenActionViewer || a.Noun != "viewer" || a.Verb != "view" || a.Arg != "y.png" {
		t.Errorf("user ext row: %+v", a)
	}
	_, a, _ = r.ResolveOpen("lane:1")
	if a.Kind != OpenActionPage || a.Page != "lanes" {
		t.Errorf("exact noun row: %+v", a)
	}
	// doc has no open verb: its row does not match, the any-noun row takes show.
	_, a, _ = r.ResolveOpen("doc:1")
	if a.Kind != OpenActionVerb || a.Verb != "show" {
		t.Errorf("noun without the verb falls to the any-noun row: %+v", a)
	}
}

func TestResolveOpenUnmatched(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"nonsense words", "ghost:1"} {
		r, f, _ := openFixture(t)
		_, a, _ := r.ResolveOpen(text)
		if a.Kind != OpenActionOS || !a.Unmatched || a.Row != nil {
			t.Errorf("%q default os: %+v", text, a)
		}
		if _, err := r.Open(text); err != nil || len(f.runs) != 1 || f.runs[0][len(f.runs[0])-1] != text {
			t.Errorf("%q os open ran %v %v", text, f.runs, err)
		}
		if err := r.Settings().Set(OpenUnmatchedSetting, OpenUnmatchedNone); err != nil {
			t.Fatal(err)
		}
		a, err := r.Open(text)
		if err != nil || a.Kind != OpenActionNone || !a.Unmatched || len(f.runs) != 1 {
			t.Errorf("%q none: %+v %v ran %v", text, a, err, f.runs)
		}
	}
	r, f, _ := openFixture(t)
	if a, err := r.Open("  "); err != nil || a.Kind != OpenActionNone || len(f.runs) != 0 {
		t.Errorf("empty text: %+v %v", a, err)
	}
}

func TestOpenRunsTheAction(t *testing.T) {
	t.Parallel()
	r, f, ran := openFixture(t)
	if _, err := r.Open("https://example.com"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"open", "--", "https://example.com"}}; !reflect.DeepEqual(f.runs, want) {
		t.Fatalf("ran %v", f.runs)
	}
	if err := r.Settings().Set(OpenOpenersSetting, `[{"ext":"log","action":"os-reveal"},{"ext":"cfg","action":"viewer","to":"viewer.view"}]`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Open("/var/x.log"); err != nil {
		t.Fatal(err)
	}
	if got := f.runs[1]; !reflect.DeepEqual(got, []string{"open", "-R", "--", "/var/x.log"}) {
		t.Fatalf("reveal ran %v", got)
	}
	if _, err := r.Open("lane:abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Open("a.cfg"); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 2 || (*ran)[0].Verb != "open" || (*ran)[0].Args["id"] != "abc" || (*ran)[1].Noun != "viewer" || (*ran)[1].Args["target"] != "a.cfg" {
		t.Fatalf("verbs ran %+v", *ran)
	}
	if len(f.runs) != 2 {
		t.Fatalf("verb rows must not reach the OS: %v", f.runs)
	}
}

func TestOpenersSettingRefusesBadRows(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	for _, bad := range []string{`{}`, `[{"action":"os-open"}]`, `[{"ext":"a","scheme":"b","action":"os-open"}]`,
		`[{"ext":"a","action":"nope"}]`, `[{"ext":"a","action":"page"}]`, `[{"ext":"a","action":"viewer","to":"x"}]`, `[{"ext":"a","action":"os-open","extra":1}]`} {
		if err := r.Settings().Set(OpenOpenersSetting, bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestBuiltinOpenerRowsAreValid(t *testing.T) {
	t.Parallel()
	for i, row := range BuiltinOpenerRows() {
		if err := row.check(); err != nil {
			t.Errorf("row %d %+v: %v", i+1, row, err)
		}
	}
}

func TestAboutOpenUsesTheRuntimeOpener(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	f := &fakeOpener{}
	r.SetOpener(f.opener("linux"))
	if err := (AboutEnv{}).open(r, "/opt/bin/x"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"xdg-open", "/opt/bin/x"}}; !reflect.DeepEqual(f.runs, want) {
		t.Fatalf("ran %v", f.runs)
	}
}

var openRowKindNames = []string{"noun", "noun", "scheme", "ext", "prefix"}
