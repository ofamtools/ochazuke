package markdown

import (
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

func wantLink(t *testing.T, d *Doc, url string) {
	t.Helper()
	l, ok := d.FocusedLink()
	if !ok || l.URL != url {
		t.Fatalf("link %+v/%v, want %q; links %+v", l, ok, url, d.Links())
	}
}

func TestLinkStopsMotionAndCounts(t *testing.T) {
	source := "# Start\n\nBefore [a](https://a.example) between [b](https://b.example) after.\n\nTail.\n"
	r, d := newDoc(t, Source{Load: func() (string, error) { return source, nil }})
	d.View(80, 0, mono)
	for _, c := range []struct {
		keys    []string
		url     string
		element int
	}{
		{[]string{"j"}, "https://a.example", 1},
		{[]string{"j"}, "https://b.example", 1},
		{[]string{"j"}, "", 2},
		{[]string{"k"}, "https://b.example", 1},
		{[]string{"k"}, "https://a.example", 1},
		{[]string{"2", "j"}, "", 2},
		{[]string{"2", "k"}, "https://a.example", 1},
		{[]string{"}"}, "", 2},
		{[]string{"[", "p"}, "https://a.example", 1},
	} {
		if err := press(t, r, c.keys...); err != nil {
			t.Fatal(err)
		}
		if e, _ := d.Cursor(); e != c.element {
			t.Fatalf("%v: element %d, want %d", c.keys, e, c.element)
		}
		if c.url != "" {
			wantLink(t, d, c.url)
		} else if l, ok := d.FocusedLink(); ok {
			t.Fatalf("%v: unexpected %+v", c.keys, l)
		}
	}
	if err := r.Settings().Set(LinkStopsSetting, "false"); err != nil {
		t.Fatal(err)
	}
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	if e, _ := d.Cursor(); e != 2 {
		t.Fatal("opt-out did not retain element movement")
	}
	if err := press(t, r, "[", "u"); err != nil {
		t.Fatal(err)
	}
	wantLink(t, d, "https://b.example")
}

func TestLinksInAllElementsAndMarkup(t *testing.T) {
	for _, style := range []ochazuke.Appearance{mono, {Theme: "dark", Dark: true}, {Theme: "light"}} {
		for _, c := range []struct {
			name, source string
			urls         []string
		}{
			{"heading", "# Before [heading](https://h.example) after", []string{"https://h.example"}},
			{"bullet", "- before [bullet](https://b.example) after", []string{"https://b.example"}},
			{"task", "- [ ] before [task](https://t.example) after", []string{"https://t.example"}},
			{"quote", "> before [quote](https://q.example) after", []string{"https://q.example"}},
			{"table", "| col | col |\n|---|---|\n| [one](https://one.example) | [two](https://two.example) |", []string{"https://one.example", "https://two.example"}},
			{"table email", "| contact |\n|---|\n| <a@example.com> |", []string{"mailto:a@example.com"}},
			{"reference", "Before [reference][r] after.\n\n[r]: https://ref.example", []string{"https://ref.example"}},
			{"duplicate", "[same](https://same.example) [same](https://same.example)", []string{"https://same.example", "https://same.example"}},
			{"nested styles", "[**bold** and *italic* and `code`](https://style.example)", []string{"https://style.example"}},
			{"unicode", "[日本 café é 👩‍💻](https://unicode.example)", []string{"https://unicode.example"}},
			{"autolink", "Before <https://auto.example> and <a@example.com>", []string{"https://auto.example", "mailto:a@example.com"}},
			{"anchor", "[section](#section)", []string{"#section"}},
			{"note", "- item\n  > **owner** 2026-10-31: before [note](https://note.example)", []string{"https://note.example"}},
			{"code", "`[inline](https://no.example)`\n\n```\n[code](https://no.example)\n```", nil},
		} {
			t.Run(style.Theme+"/"+c.name, func(t *testing.T) {
				_, d := newDoc(t, Source{Load: func() (string, error) { return c.source, nil }})
				d.View(80, 0, style)
				got := d.Links()
				if len(got) != len(c.urls) {
					t.Fatalf("links %+v want %v; lines %q; cells %+v", got, c.urls, d.lay.lines, linkCells(d.lay.lines, 0, len(d.lay.lines)-1))
				}
				for i, url := range c.urls {
					if got[i].URL != url {
						t.Fatalf("link %d: %+v", i, got[i])
					}
				}
			})
		}
	}
}

func TestLinkVerbsOpenCopyHistoryAndEdit(t *testing.T) {
	source := "[one](https://one.example) and [two](https://two.example)\n\nTail."
	r, d := newDoc(t, Source{Load: func() (string, error) { return source, nil }, Save: func(s string) error { source = s; return nil }})
	d.View(80, 0, mono)
	opened := []string{}
	d.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	for _, keys := range [][]string{{"enter"}, {"g", "x"}, {"y", "l"}, {"y", "y"}, {"]", "u"}} {
		if err := press(t, r, keys...); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(opened, ",") != "https://one.example,https://one.example" {
		t.Fatal(opened)
	}
	wantLink(t, d, "https://two.example")
	if err := d.CopyLink(); err != nil {
		t.Fatal(err)
	}
	if got := r.Registers().Named["\""].Yank.Text; got != "https://two.example" {
		t.Fatal(got)
	}
	for _, verb := range []string{"link-open", "link-copy", "next-link", "previous-link"} {
		op := r.Operations[DocNoun+"."+verb]
		if op.HistoryKindOf() != ochazuke.HistoryAction || op.Description == "" || op.Parameters["id"].Type != "string" {
			t.Fatalf("incomplete verb %+v", op)
		}
	}
	sentinel := errors.New("opener failed")
	d.OpenURL = func(string) error { return sentinel }
	if err := d.OpenLink(); !errors.Is(err, sentinel) {
		t.Fatalf("opener error %v", err)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: DocNoun, Verb: "link-copy", Args: ochazuke.Arguments{"id": d.ID()}}); err != nil {
		t.Fatal(err)
	}
	if err := press(t, r, "i"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.FocusedLink(); ok {
		t.Fatal("editing exposes link focus")
	}
	if err := d.OpenLink(); err == nil {
		t.Fatal("editing opened a URL")
	}
	if err := press(t, r, "esc"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "[one](https://one.example)") {
		t.Fatal(source)
	}
}

func TestLinkFocusSurvivesReflowAndScroll(t *testing.T) {
	source := "# Start\n\n" + strings.Repeat("prefix words ", 35) + "[first](https://first.example) middle " + strings.Repeat("words ", 35) + "[second](https://second.example) tail.\n\nEnd."
	r, d := newDoc(t, Source{Load: func() (string, error) { return source, nil }})
	d.View(40, 8, mono)
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	wantLink(t, d, "https://first.example")
	if view := ansi.Strip(d.View(40, 8, mono)); !strings.Contains(view, "first") {
		t.Fatalf("focused first link hidden: %s", view)
	}
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	wantLink(t, d, "https://second.example")
	d.View(54, 8, mono)
	wantLink(t, d, "https://second.example")
	if view := ansi.Strip(d.View(54, 8, mono)); !strings.Contains(view, "second") {
		t.Fatalf("reflow hid link: %s", view)
	}
	for i := 0; i < 10; i++ {
		d.ScrollLines(1)
		view := ansi.Strip(d.View(54, 8, mono))
		if l, ok := d.FocusedLink(); ok && !strings.Contains(view, l.Text) {
			t.Fatalf("scroll hid %s: %s", l.Text, view)
		}
	}
}

func TestLinkFocusUsesThemeAccent(t *testing.T) {
	source := "Before [focus](https://f.example) after."
	_, d := newDoc(t, Source{Load: func() (string, error) { return source, nil }})
	a := ochazuke.Appearance{Theme: "dark", Dark: true, Accent: lipgloss.Color("#123456")}
	view := d.View(80, 0, a)
	if !strings.Contains(view, ochazuke.NewStyles(a).Accent.Underline(true).Reverse(true).Render("focus https://f.example")) {
		t.Fatalf("theme focus missing: %q", view)
	}
	if w := ansi.StringWidth(view); w != 80 {
		t.Fatalf("focus changed width: %d", w)
	}
}

func TestLinkBindingsLeaderAndCount(t *testing.T) {
	source := "[one](https://one.example) [two](https://two.example)\n\n[three](https://three.example)"
	r, d := newDoc(t, Source{Load: func() (string, error) { return source, nil }})
	d.View(80, 0, mono)
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	if err := press(t, r, "2", "]", "u"); err != nil {
		t.Fatal(err)
	}
	wantLink(t, d, "https://three.example")
	opened := ""
	d.OpenURL = func(url string) error { opened = url; return nil }
	if err := r.DefineMode("leader_pending", true); err != nil {
		t.Fatal(err)
	}
	for _, keys := range [][]string{{"enter"}, {"g", "x"}, {"[", "u"}, {"y", "l"}, {"j"}, {"k"}, {"]", "u"}} {
		if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.mode", Verb: "enter", Args: ochazuke.Arguments{"mode": "leader_pending"}}); err != nil {
			t.Fatal(err)
		}
		if err := press(t, r, keys...); err != nil {
			t.Fatal(err)
		}
	}
	if opened != "https://three.example" {
		t.Fatal(opened)
	}
	if got := r.Registers().Named["\""].Yank.Text; got != "https://two.example" {
		t.Fatalf("captured y l: %s", got)
	}
	wantLink(t, d, "https://three.example")
	for _, verb := range []string{"next-link", "previous-link"} {
		if r.Operations[DocNoun+"."+verb].Parameters["steps"].Type != "integer" {
			t.Fatal("no count", verb)
		}
	}
}

func TestLinkTableDrawingOrderAndHiddenTasks(t *testing.T) {
	source := "| left | right |\n|---|---|\n| before many many many many many words [late](https://same.example) | [early](https://same.example) |\n\n- [x] [hidden](https://hidden.example)\n- [ ] [visible](https://visible.example)"
	r, d := newDoc(t, Source{Load: func() (string, error) { return source, nil }})
	d.View(46, 0, mono)
	links := d.Links()
	if len(links) != 4 || links[0].Text != "early" || links[1].Text != "late" {
		t.Fatalf("table drawing order: %+v", links)
	}
	if err := r.Settings().Set(DoneSetting, "hide"); err != nil {
		t.Fatal(err)
	}
	for _, l := range d.Links() {
		if strings.Contains(l.URL, "hidden") {
			t.Fatal("hidden task link remains a stop")
		}
	}
}

func TestLinkStopsSpillAndLineMode(t *testing.T) {
	r, d := newDoc(t, Source{Load: func() (string, error) { return "[first](https://first.example) [last](https://last.example)", nil }})
	_, err := r.AddList("next", []ochazuke.ListItem{{ID: "next-row", Label: "next", Noun: LineNoun}}, ochazuke.Parent("notes"))
	if err != nil {
		t.Fatal(err)
	}
	d.View(80, 0, mono)
	r.Placed("next")
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	wantLink(t, d, "https://last.example")
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "next-row" {
		t.Fatalf("link edge did not spill: %s", r.Focus)
	}
	source := "Before [first](https://first.example) " + strings.Repeat("word ", 30) + "after.\n\nTail."
	r, d = newDoc(t, Source{Load: func() (string, error) { return source, nil }})
	if err := r.Settings().Set(CursorSetting, "line"); err != nil {
		t.Fatal(err)
	}
	d.View(30, 0, mono)
	if err := press(t, r, "j"); err != nil {
		t.Fatal(err)
	}
	if line := d.cursorLine(); line != 1 {
		t.Fatalf("line mode moved to %d", line)
	}
}

func TestLinkFragmentFocusIsOnTheLink(t *testing.T) {
	source := "section before [section](#section) after."
	_, d := newDoc(t, Source{Load: func() (string, error) { return source, nil }})
	view := d.View(80, 0, mono)
	wantLink(t, d, "#section")
	link, _ := d.FocusedLink()
	if link.runs[0].start != 17 {
		t.Fatalf("fragment focus on ordinary text: %+v; %q", link.runs, view)
	}
}
