package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/markdown"
)

// Every framework setting has a context point, and setting check finds no
// point that names nothing, over a runtime holding every framework
// feature that declares settings (the model, the picker, markdown, grid,
// the terminal leader and a rail).
func TestFrameworkSettingsHaveKnownContexts(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	markdown.Register(r)
	grid.Register(r)
	r.EnsureTerminalLeader()
	if _, err := r.AddPage("p", "Page", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("") }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddRail("p/rail", ochazuke.RailSpec{}, ochazuke.Parent("p")); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(ochazuke.Request{Noun: "ochazuke.setting", Verb: "check"})
	if err != nil {
		t.Fatal(err)
	}
	if problems := got.([]string); len(problems) > 0 {
		t.Fatalf("setting check:\n%v", problems)
	}
	s := r.Settings()
	count := 0
	for _, one := range s.List() {
		if s.OwnerOf(one.Name) == ochazuke.OwnerFramework {
			count++
			if len(s.Points(one.Name)) == 0 {
				t.Errorf("%s has no context point", one.Name)
			}
		}
	}
	t.Logf("%d framework settings, each with a context point", count)
}
