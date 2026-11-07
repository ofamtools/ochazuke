package markdown

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

const linkURLsDoc = "# Notes\n\nSee [props.go:231](file:///Users/x/proj/ochazuke/props.go#L231) and [two](https://example.com/a/long/path/that/would/wrap) then <https://auto.example/x> and www.bare.example, ok.\n\n- item [ln](https://x.example/z).\n\nTail.\n"

func linkURLsView(t *testing.T, value string, width, height int) (*ochazuke.Runtime, *Doc, string) {
	t.Helper()
	r, d := newDoc(t, Source{Load: func() (string, error) { return linkURLsDoc, nil }})
	if value != "" {
		if err := r.Settings().Set(LinkURLsSetting, value); err != nil {
			t.Fatal(err)
		}
	}
	return r, d, ansi.Strip(d.View(width, height, mono))
}

// Each markdown.link-urls value: always draws the URLs after the text,
// focused and never draw only the text. Autolinks keep their written text.
func TestLinkURLsSettingValues(t *testing.T) {
	for _, c := range []struct {
		value string
		urls  bool
	}{{"", false}, {LinkURLsFocused, false}, {LinkURLsAlways, true}, {LinkURLsNever, false}} {
		_, d, view := linkURLsView(t, c.value, 80, 0)
		for _, text := range []string{"props.go:231", "two", "https://auto.example/x", "www.bare.example", "ln"} {
			if !strings.Contains(view, text) {
				t.Fatalf("%q: %q missing: %s", c.value, text, view)
			}
		}
		for _, url := range []string{"file:///Users/x/proj/ochazuke/props.go#L231", "https://example.com/a/long", "https://x.example/z"} {
			if strings.Contains(view, url) != c.urls {
				t.Fatalf("%q: url %q shown %v, want %v: %s", c.value, url, !c.urls, c.urls, view)
			}
		}
		if !c.urls && (strings.Contains(view, "  and") || strings.Contains(view, "ln .")) {
			t.Fatalf("%q: hidden URL left its space: %s", c.value, view)
		}
		if got := len(d.Links()); got != 5 {
			t.Fatalf("%q: %d links, want 5: %+v", c.value, got, d.Links())
		}
	}
}

// With link-urls=focused the focused link's URL is the line under the
// document; never and always draw no such line. Open and copy use the URL.
func TestLinkURLsFocusedShowsURL(t *testing.T) {
	for _, value := range []string{LinkURLsFocused, LinkURLsNever, LinkURLsAlways} {
		r, d, _ := linkURLsView(t, value, 80, 10)
		if err := press(t, r, "j"); err != nil {
			t.Fatal(err)
		}
		wantLink(t, d, "file:///Users/x/proj/ochazuke/props.go#L231")
		lines := strings.Split(ansi.Strip(d.View(80, 10, mono)), "\n")
		if len(lines) > 10 {
			t.Fatalf("%s: %d lines in a height of 10", value, len(lines))
		}
		last := strings.TrimSpace(lines[len(lines)-1])
		footer := strings.HasPrefix(last, "→ ")
		if footer != (value == LinkURLsFocused) {
			t.Fatalf("%s: footer %q", value, last)
		}
		if footer && last != "→ file:///Users/x/proj/ochazuke/props.go#L231" {
			t.Fatalf("%s: footer %q", value, last)
		}
		if err := d.MoveLink(1, 1); err != nil {
			t.Fatal(err)
		}
		lines = strings.Split(ansi.Strip(d.View(80, 10, mono)), "\n")
		if last = strings.TrimSpace(lines[len(lines)-1]); value == LinkURLsFocused && last != "→ https://example.com/a/long/path/that/would/wrap" {
			t.Fatalf("next link footer %q", last)
		}
		var opened string
		d.OpenURL = func(u string) error { opened = u; return nil }
		if err := d.OpenLink(); err != nil || opened != "https://example.com/a/long/path/that/would/wrap" {
			t.Fatalf("%s: opened %q, %v", value, opened, err)
		}
		if err := d.CopyLink(); err != nil {
			t.Fatal(err)
		}
	}
	// No focused link (the heading): no footer.
	_, d, view := linkURLsView(t, LinkURLsFocused, 80, 0)
	if _, ok := d.FocusedLink(); ok || strings.Contains(view, "→") {
		t.Fatalf("footer without a focused link: %s", view)
	}
}

// Narrow and wrapped, every link's cells hold exactly its text, and the
// focus highlight paints those cells, with the URLs hidden.
func TestLinkURLsHiddenWrapPositions(t *testing.T) {
	r, d, _ := linkURLsView(t, LinkURLsFocused, 24, 0)
	lines := strings.Split(ansi.Strip(d.View(24, 0, mono)), "\n")
	for _, l := range d.Links() {
		var got strings.Builder
		for _, run := range l.runs {
			plain := []rune(lines[run.line])
			got.WriteString(string(plain[run.start:min(run.end, len(plain))]))
		}
		if compact(got.String()) != compact(l.Text) {
			t.Fatalf("link %q cells hold %q", l.Text, got.String())
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > 24 {
				t.Fatalf("line wider than 24: %q", line)
			}
		}
	}
	urls := []string{}
	for range d.Links() {
		if err := press(t, r, "]", "u"); err != nil {
			break
		}
		l, ok := d.FocusedLink()
		if !ok {
			t.Fatal("no focused link")
		}
		urls = append(urls, l.URL)
		view := d.View(24, 0, mono)
		if !strings.Contains(view, "\x1b[") {
			t.Fatal("no highlight")
		}
	}
	want := []string{"file:///Users/x/proj/ochazuke/props.go#L231", "https://example.com/a/long/path/that/would/wrap", "https://auto.example/x", "http://www.bare.example", "https://x.example/z"}
	if strings.Join(urls, " ") != strings.Join(want, " ") {
		t.Fatalf("link order %v, want %v", urls, want)
	}
}

// Tables draw a link's text without its URL too.
func TestLinkURLsHiddenInTables(t *testing.T) {
	src := "| name | target |\n|---|---|\n| first | before [cell](https://cell.example) after |\n"
	for _, value := range []string{LinkURLsFocused, LinkURLsAlways} {
		r, d := newDoc(t, Source{Load: func() (string, error) { return src, nil }})
		if err := r.Settings().Set(LinkURLsSetting, value); err != nil {
			t.Fatal(err)
		}
		view := ansi.Strip(d.View(80, 0, mono))
		if strings.Contains(view, "https://cell.example") != (value == LinkURLsAlways) || !strings.Contains(view, "cell") {
			t.Fatalf("%s: %s", value, view)
		}
		if len(d.Links()) != 1 || d.Links()[0].URL != "https://cell.example" {
			t.Fatalf("%s: links %+v", value, d.Links())
		}
	}
}

// The setting is an enum ochazuke.setting.cycle steps (what a bindings.json
// row reaches), and a change lays the document out again.
func TestLinkURLsCycleRelayouts(t *testing.T) {
	r, d, view := linkURLsView(t, "", 80, 0)
	if strings.Contains(view, "https://x.example/z") {
		t.Fatal("default draws URLs")
	}
	for _, want := range []string{LinkURLsAlways, LinkURLsNever, LinkURLsFocused} {
		if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.setting", Verb: "cycle", Args: ochazuke.Arguments{"name": LinkURLsSetting, "step": 1}}); err != nil {
			t.Fatal(err)
		}
		if got := r.Settings().String(LinkURLsSetting); got != want {
			t.Fatalf("cycled to %q, want %q", got, want)
		}
		view := ansi.Strip(d.View(80, 0, mono))
		if strings.Contains(view, "https://x.example/z") != (want == LinkURLsAlways) {
			t.Fatalf("%s: %s", want, view)
		}
	}
}

// stripHrefs drops the marker and the space before it, never an indent.
func TestStripHrefs(t *testing.T) {
	for in, want := range map[string]string{
		"a" + hrefMarker + ".":                "a.",
		"a \x1b[1m\x1b[m" + hrefMarker + " b": "a\x1b[1m\x1b[m b",
		"  " + hrefMarker + "next":            "  next",
		"x":                                   "x",
		"one " + hrefMarker + " two " + hrefMarker: "one two",
	} {
		if got := stripHrefs(in); got != want {
			t.Fatalf("stripHrefs(%q) = %q, want %q", in, got, want)
		}
	}
}
