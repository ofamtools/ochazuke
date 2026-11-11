package ochazuke

import (
	"strings"
	"testing"
)

func TestOpenVerbs(t *testing.T) {
	t.Parallel()
	r, f, _ := openFixture(t)
	got, err := r.Execute(Request{Noun: OpenNoun, Verb: "resolve", Args: Arguments{"text": "https://x.test/a"}})
	res, ok := got.(openResult)
	if err != nil || !ok || res.Action != OpenActionOS || len(f.runs) != 0 {
		t.Fatalf("resolve %#v %v ran %v", got, err, f.runs)
	}
	if _, err := r.Execute(Request{Noun: OpenNoun, Verb: "run", Args: Arguments{"text": "https://x.test/a"}}); err != nil || len(f.runs) != 1 {
		t.Fatalf("run %v ran %v", err, f.runs)
	}
	op, _ := r.Operation("ochazuke.opener.run")
	if op.AgentRightOf(false) != AgentConfirm {
		t.Fatal("open.run needs confirmation from an agent")
	}
	if err := r.Settings().Set(OpenModalSetting, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: OpenNoun, Verb: "show"}); err == nil {
		t.Fatal("open.show is off with open.modal=false")
	}
	if _, err := r.Execute(Request{Noun: OpenNoun, Verb: "run", Args: Arguments{"text": "y.png"}}); err != nil || len(f.runs) != 2 {
		t.Fatalf("run still opens with the modal off: %v %v", err, f.runs)
	}
}

func TestOpenAndFocusedActionsDefaultKeys(t *testing.T) {
	t.Parallel()
	r, f := pathVerbFixture(t)
	r.Config().Disable()
	m := NewModel(r)
	m.Width, m.Height = 100, 28
	r.Instance("d1", Kind("note")).Data = Thing{"id": "d1"}
	r.SetFocus("d1")
	formKeys(t, r, "g", "F")
	if len(f.runs) != 1 || f.runs[0][2] != "/tmp/notes.md" {
		t.Fatalf("open by key ran %v", f.runs)
	}
	formKeys(t, r, "g", "X")
	if r.TopModal() != ArgsFormID {
		t.Fatalf("open-with must ask for the application, modal %q", r.TopModal())
	}
	typeKeys(t, r, "Preview")
	formKeys(t, r, "enter")
	if len(f.runs) != 2 || strings.Join(f.runs[1], " ") != "open -a Preview -- /tmp/notes.md" {
		t.Fatalf("open-with by key ran %v", f.runs)
	}
	formKeys(t, r, "g", ".")
	if r.TopModal() != FocusActionsKind {
		t.Fatalf("focused-actions key opened %q", r.TopModal())
	}
	ch := r.Chooser(FocusActionsKind)
	ch.SetQuery("yank-path")
	formKeys(t, r, "enter")
	if got := r.Registers().Named["\""].Yank.Text; got != "/tmp/notes.md" {
		t.Fatalf("focused action yanked %q", got)
	}
	formKeys(t, r, "g", "o")
	if r.RecordEditor(OpenModalID) == nil {
		t.Fatalf("Open key did not open its form: %q", r.TopModal())
	}
	if frame := m.View().Content; !strings.Contains(frame, "Open") {
		t.Fatalf("Open form missing from frame: %s", frame)
	}
}

func TestOpenPaletteAliasesKeepNounVerbs(t *testing.T) {
	t.Parallel()
	for _, alias := range []string{"e", "edit", "open"} {
		t.Run(alias, func(t *testing.T) {
			r, _, _ := openFixture(t)
			r.Config().Disable()
			NewPalette(r)
			paletteRequest(t, r, "open", "palette", nil)
			paletteLine(t, r, alias+" /tmp/new-file.json")
			ed := r.RecordEditor(OpenModalID)
			if ed == nil || ed.Values()["text"] != "/tmp/new-file.json" {
				t.Fatalf("%s did not prefill Open: %v", alias, ed)
			}
		})
	}
	r, _, ran := openFixture(t)
	p := NewPalette(r)
	if got := parsed(p, "open lane abc"); got != "lane.open" {
		t.Fatalf("explicit noun open resolved as %s", got)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "open lane abc")
	if len(*ran) != 1 || (*ran)[0].Args["id"] != "abc" {
		t.Fatalf("noun open ran %v", *ran)
	}
}
