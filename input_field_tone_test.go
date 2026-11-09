package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A drawn input carries the input-field tone's background across its whole
// width, light and dark; mono and a theme tone without a bg draw it flat.
func TestInputFieldToneFillsWidth(t *testing.T) {
	for theme, hex := range map[string]string{"dark": "#262C34", "light": "#EEF1F5"} {
		r := NewRuntime()
		r.Config().Disable()
		m := NewModel(r)
		m.Width, m.Height = 40, 10
		p, err := r.AddPage("test", "Test", func(*Runtime, int, int) Block { return Text("x") })
		if err != nil {
			t.Fatal(err)
		}
		in := NewInput("name", "abc", false)
		if err = r.AddInput(in, Parent(p.ID)); err != nil {
			t.Fatal(err)
		}
		m.Appearance.Theme = theme
		m.View()
		block := InputBlock(in, 12, 1)
		if got := ansi.StringWidth(block.Text); got != 12 {
			t.Fatalf("%s: width %d in %q", theme, got, block.Text)
		}
		want := "\x1b[" + bgSGR(hex) + "m"
		if !strings.HasPrefix(block.Text, want) || !strings.HasSuffix(block.Text, "abc"+strings.Repeat(" ", 9)+"\x1b[m") {
			t.Fatalf("%s: %q, want prefix %q", theme, block.Text, want)
		}
	}
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	m.Width, m.Height = 40, 10
	p, _ := r.AddPage("test", "Test", func(*Runtime, int, int) Block { return Text("x") })
	in := NewInput("name", "abc", false)
	_ = r.AddInput(in, Parent(p.ID))
	m.Appearance.Theme = "mono"
	m.View()
	if got := InputBlock(in, 12, 1).Text; strings.Contains(got, "48;") {
		t.Fatalf("mono: %q", got)
	}
}
