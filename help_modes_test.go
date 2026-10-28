package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// R6.10: a key entering a pending mode is a prefix row followed by the
// mode's rows as whole sequences, each with its steps; a plain prefix gets
// a made row; nothing is listed twice; a mode entering itself stops one
// level down.
func TestHelpListsPendingModeRowsAsSequences(t *testing.T) {
	r, _ := helpFixture(t)
	if err := r.DefineMode("loop", true); err != nil {
		t.Fatal(err)
	}
	r.Define("widget").Mode("normal").
		Bind("L", Enter("loop")).
		Bind("g c x", Do("widget.edit")).
		Mode("loop").
		Bind("x", Enter("loop")).
		Bind("y", Do("widget.open")).
		Bind("h", Do("widget.edit").Hide())
	rows := BindingHelp(r, r.Focus)
	byKey := map[string]HelpRow{}
	var keys []string
	seen := map[string]bool{}
	for _, row := range helpOrder(rows) {
		if seen[row.Section+":"+row.Key] {
			t.Fatalf("%q listed twice in %s", row.Key, row.Section)
		}
		seen[row.Section+":"+row.Key] = true
		if row.Section != "widget" {
			continue
		}
		byKey[row.Key] = row
		keys = append(keys, row.Key)
	}
	if want := []string{"enter", "i", "L", "L x", "L x x", "L x y", "L y", "g", "g c", "g c x", "g c esc", "g esc"}; !slices.Equal(keys, want) {
		t.Fatalf("keys %q, want %q", keys, want)
	}
	if l := byKey["L"]; !l.Group || l.Enters != "loop" || l.Operation != "mode.enter" || !strings.Contains(l.Description, "waits for the next key (3 keys)") {
		t.Fatalf("L row %+v", l)
	}
	if xx := byKey["L x x"]; xx.Operation != "mode.enter" || xx.Enters != "" || xx.Under != "L x" {
		t.Fatalf("L x x row %+v", xx)
	}
	if g := byKey["g"]; !g.Group || g.Operation != "" || g.Description != "waits for the next key (1 key)" {
		t.Fatalf("made g row %+v", g)
	}
	if gcx := byKey["g c x"]; gcx.Under != "g c" || byKey["g c"].Under != "g" {
		t.Fatalf("nesting: g c x under %q, g c under %q", gcx.Under, byKey["g c"].Under)
	}
	var steps []string
	for _, s := range byKey["L y"].Steps {
		steps = append(steps, s.Key+"|"+s.Mode+"|"+s.Operation)
	}
	if !slices.Equal(steps, []string{"L|loop|mode.enter", "y||widget.open"}) {
		t.Fatalf("L y steps %q", steps)
	}
	steps = nil
	for _, s := range byKey["g c x"].Steps {
		steps = append(steps, s.Key+"|"+s.Description)
	}
	if !slices.Equal(steps, []string{"g|waits for the next key (g …)", "c|waits for the next key (g c …)", "x|Edit the widget"}) {
		t.Fatalf("g c x steps %q", steps)
	}
}
