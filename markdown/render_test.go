package markdown

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

const sample = "# Notes\n\nSome *text* that is long enough to wrap at forty columns, surely.\n\n- [ ] milk\n- [x] bread\n\n```go\nfunc main() {}\n```\n"

// Block renders in the appearance's Glamour style, no wider than the
// width, without Glamour's blank first and last lines.
func TestBlockStylesAndWidth(t *testing.T) {
	for _, tc := range []struct {
		a     ochazuke.Appearance
		style string
	}{
		{ochazuke.Appearance{Theme: "adaptive", Dark: true}, StyleDark},
		{ochazuke.Appearance{Theme: "adaptive", Dark: false}, StyleLight},
		{ochazuke.Appearance{Theme: "light", Dark: true}, StyleLight},
		{ochazuke.Appearance{Theme: "mono", Dark: true}, StylePlain},
	} {
		if got := StyleOf(tc.a); got != tc.style {
			t.Fatalf("%s dark=%v: style %s, want %s", tc.a.Theme, tc.a.Dark, got, tc.style)
		}
		text := Block(sample, 40, tc.a).Text
		lines := strings.Split(text, "\n")
		if strings.TrimSpace(ansi.Strip(lines[0])) == "" || strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
			t.Fatalf("%s: blank first or last line:\n%s", tc.style, ansi.Strip(text))
		}
		for _, line := range lines {
			if w := ansi.StringWidth(line); w > 40 {
				t.Fatalf("%s: line %q is %d wide", tc.style, ansi.Strip(line), w)
			}
		}
		plain := ansi.Strip(text)
		for _, want := range []string{"Notes", "wrap", "milk", "bread", "func main"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("%s: %q missing:\n%s", tc.style, want, plain)
			}
		}
		colored := strings.Contains(text, "38;5;") || strings.Contains(text, "38;2;")
		if colored == (tc.style == StylePlain) {
			t.Fatalf("%s: colored=%v:\n%q", tc.style, colored, text)
		}
	}
}

func TestBlockNoColorIsPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := StyleOf(ochazuke.Appearance{Theme: "dark", Dark: true}); got != StylePlain {
		t.Fatalf("NO_COLOR: style %s", got)
	}
}

// A rendering is cached by text, width and style: the same inputs give
// the same lines without running Glamour again.
func TestBlockIsCached(t *testing.T) {
	a := ochazuke.Appearance{Theme: "mono"}
	first := Block(sample, 50, a).Text
	key := renderKey{sample, 0, 50, StylePlain, options{}}
	cache.Lock()
	_, ok := cache.renderings[key]
	cache.Unlock()
	if !ok {
		t.Fatal("the rendering was not cached")
	}
	if Block(sample, 50, a).Text != first {
		t.Fatal("a cached rendering differs")
	}
	if Block(sample, 30, a).Text == first {
		t.Fatal("a new width must render again")
	}
	if Block("", 30, a).Text != "" {
		t.Fatal("empty text renders nothing")
	}
}

// Block is a detail's text: the detail wraps nothing further and J/K
// scroll it a line at a time.
func TestBlockInADetailScrolls(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	d, err := r.AddDetail("notes", func() string { return "one" })
	if err != nil {
		t.Fatal(err)
	}
	text := Block(sample, 40, ochazuke.Appearance{Theme: "mono"}).Text
	total := strings.Count(text, "\n") + 1
	body, marker := d.Window(text, 40, 4)
	if marker == "" || !strings.HasSuffix(marker, " / "+strconv.Itoa(total)) {
		t.Fatalf("marker %q for %d lines", marker, total)
	}
	first := strings.Split(body, "\n")[0]
	d.Scroll(1)
	body, _ = d.Window(text, 40, 4)
	if strings.Split(body, "\n")[0] == first || d.Offset() != 1 {
		t.Fatalf("scroll did not move: offset %d", d.Offset())
	}
}
