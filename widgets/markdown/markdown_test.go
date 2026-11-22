package markdown

import (
	"regexp"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m|\x1b\]8;[^\x07]*\x07`)

func render(t *testing.T, n ui.ViewNode, w, h int) string {
	t.Helper()
	f, _ := ui.Render(n, ui.DefaultDarkTheme(), n.ID, w, h)
	rows := strings.Split(ansiRe.ReplaceAllString(f.ANSI, ""), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return strings.Join(rows, "\n")
}

func cmd(id string, count int) core.Command { return core.Command{ID: id, Count: count} }

func do(t *testing.T, m *Model, c core.Command) Action {
	t.Helper()
	a, ok := m.HandleCommand(c)
	if !ok {
		t.Fatalf("%s not handled", c.ID)
	}
	return a
}

const doc = `# Title
intro with [site](https://example.com) and ` + "`[code](no)`" + ` and ![img](pic.png)

## Install
` + "```sh" + `
# not a heading
go install
` + "```" + `
see [docs](docs/README.md "Docs")
## Usage
text [again](#install)
### Deep
` + "```go" + `
fmt.Println()
` + "```" + `
bye
`

func TestHeadingsAndJumps(t *testing.T) {
	m := New("Doc", doc)
	m.PageSize = 5
	var got []string
	for _, h := range m.Headings {
		got = append(got, h.Text)
	}
	if strings.Join(got, "|") != "Title|Install|Usage|Deep" || m.Headings[2].Line != 9 {
		t.Fatalf("headings %+v", m.Headings)
	}
	do(t, m, cmd("motion.block.next", 0))
	if m.Offset != 3 || m.SelectedHeading != 1 {
		t.Fatalf("next: offset %d sel %d", m.Offset, m.SelectedHeading)
	}
	do(t, m, cmd("motion.block.next", 2))
	if m.Offset != 11 || m.SelectedHeading != 3 {
		t.Fatalf("next x2: offset %d sel %d", m.Offset, m.SelectedHeading)
	}
	do(t, m, cmd("motion.block.next", 0))
	if m.Offset != 11 {
		t.Fatalf("next at last heading moved to %d", m.Offset)
	}
	do(t, m, cmd("motion.block.prev", 0))
	if m.Offset != 9 || m.SelectedHeading != 2 {
		t.Fatalf("prev: offset %d sel %d", m.Offset, m.SelectedHeading)
	}
	do(t, m, core.Command{ID: CmdHeadingJump, Args: map[string]any{"index": 0}})
	if m.Offset != 0 || m.SelectedHeading != 0 {
		t.Fatalf("jump index 0: %d/%d", m.Offset, m.SelectedHeading)
	}
	do(t, m, cmd(CmdTOCNext, 0))
	if m.SelectedHeading != 1 || m.Offset != 0 {
		t.Fatalf("toc.next scrolled: %d/%d", m.SelectedHeading, m.Offset)
	}
	do(t, m, cmd(CmdHeadingJump, 0))
	if m.Offset != 3 {
		t.Fatalf("jump selected: %d", m.Offset)
	}
	do(t, m, cmd("motion.down", 4))
	if m.Offset != 7 || m.SelectedHeading != 1 {
		t.Errorf("down x4: %d/%d", m.Offset, m.SelectedHeading)
	}
	do(t, m, cmd("motion.page.down", 0))
	if m.Offset != 12 || m.SelectedHeading != 3 {
		t.Errorf("page down: %d/%d", m.Offset, m.SelectedHeading)
	}
	do(t, m, cmd("motion.half.up", 0))
	if m.Offset != 10 {
		t.Errorf("half up: %d", m.Offset)
	}
	do(t, m, cmd("motion.bottom", 0))
	if m.Offset != 11 {
		t.Errorf("bottom: %d", m.Offset)
	}
	do(t, m, cmd("motion.page.up", 9))
	if m.Offset != 0 {
		t.Errorf("page up clamp: %d", m.Offset)
	}
	do(t, m, cmd("motion.down", 99))
	if m.Offset != m.total()-1 {
		t.Errorf("down clamp: %d", m.Offset)
	}
	if _, ok := m.HandleCommand(cmd("nope", 0)); ok {
		t.Error("unknown command handled")
	}
}

func TestTOCView(t *testing.T) {
	m := New("Doc", doc)
	do(t, m, cmd(CmdTOCToggle, 0))
	v := m.View("md")
	if v.Kind != ui.KindSplit || v.Scope != "markdown" || !v.Focusable || v.CacheKey == 0 || len(v.Children) != 2 {
		t.Fatalf("node %+v", v)
	}
	if v.Facts["showTOC"] != true || v.Facts["headingCount"] != 4 || v.Facts["heading"] != "Title" {
		t.Errorf("facts %+v", v.Facts)
	}
	if v.Children[1].Children[0].Kind != ui.KindMarkdown {
		t.Errorf("right child %+v", v.Children[1])
	}
	out := render(t, v, 80, 22)
	for _, want := range []string{"Contents", "▸ Title", "    Deep", "Usage"} {
		if !strings.Contains(out, want) {
			t.Errorf("TOC render missing %q in:\n%s", want, out)
		}
	}
	do(t, m, cmd(CmdTOCToggle, 0))
	if v := m.View("md"); v.Kind != ui.KindPanel || v.Children[0].Kind != ui.KindMarkdown {
		t.Errorf("hidden TOC should give panel > markdown: %+v", v)
	}
}

func TestLinks(t *testing.T) {
	m := New("Doc", doc)
	if len(m.Links) != 3 {
		t.Fatalf("links (code and image excluded): %+v", m.Links)
	}
	if l := m.Links[0]; l.Label != "site" || l.Href != "https://example.com" || l.Line != 1 || l.Col != 11 {
		t.Errorf("link 0: %+v", l)
	}
	if l := m.Links[1]; l.Href != "docs/README.md" || l.Line != 8 {
		t.Errorf("link 1: %+v", l)
	}
	if a := do(t, m, cmd(CmdLinkOpen, 0)); a != (Action{Kind: "open", Target: "https://example.com"}) {
		t.Errorf("open first: %+v", a)
	}
	do(t, m, cmd(CmdLinkNext, 0))
	if a := do(t, m, cmd(CmdLinkOpen, 0)); a.Kind != "open" || a.Target != "docs/README.md" {
		t.Errorf("open second: %+v", a)
	}
	m.PageSize = 5
	do(t, m, cmd(CmdLinkNext, 0))
	if m.ActiveLinkIdx != 2 || m.Offset != 6 {
		t.Errorf("third link: idx %d, offset %d (want revealed at 6)", m.ActiveLinkIdx, m.Offset)
	}
	do(t, m, cmd(CmdLinkNext, 0))
	if m.ActiveLinkIdx != 0 || m.Offset != 1 {
		t.Errorf("wrap: idx %d offset %d", m.ActiveLinkIdx, m.Offset)
	}
	do(t, m, cmd(CmdLinkPrev, 0))
	if m.ActiveLinkIdx != 2 {
		t.Errorf("prev wrap: %d", m.ActiveLinkIdx)
	}
	v := m.View("md")
	if len(v.Badges) != 1 || !strings.Contains(v.Badges[0].Text, "#install") || v.Facts["activeLink"] != "#install" {
		t.Errorf("active link not shown: %+v %+v", v.Badges, v.Facts)
	}
	empty := New("e", "no links")
	if a := do(t, empty, cmd(CmdLinkOpen, 0)); a != (Action{}) {
		t.Errorf("open with no links: %+v", a)
	}
	do(t, empty, cmd(CmdLinkNext, 0))
}

func TestCodeYank(t *testing.T) {
	m := New("Doc", doc)
	if len(m.CodeBlocks) != 2 {
		t.Fatalf("blocks %+v", m.CodeBlocks)
	}
	if b := m.CodeBlocks[0]; b.Lang != "sh" || b.StartLine != 4 || b.EndLine != 7 || b.Content != "# not a heading\ngo install" {
		t.Errorf("block 0 %+v", b)
	}
	for off, want := range map[int]string{0: "# not a heading\ngo install", 5: "# not a heading\ngo install", 9: "# not a heading\ngo install", 11: "fmt.Println()", 13: "fmt.Println()", 15: "fmt.Println()"} {
		m.Offset = off
		if a := do(t, m, cmd(CmdCodeYank, 0)); a != (Action{Kind: "yank", Text: want}) {
			t.Errorf("offset %d: %+v, want %q", off, a, want)
		}
	}
	if a := do(t, m, core.Command{ID: CmdCodeYank, Args: map[string]any{"index": 0.0}}); !strings.HasPrefix(a.Text, "# not") {
		t.Errorf("explicit index: %+v", a)
	}
	if a := do(t, New("e", "plain"), cmd(CmdCodeYank, 0)); a != (Action{}) {
		t.Errorf("no blocks: %+v", a)
	}
}

func TestRender(t *testing.T) {
	m := New("Doc", doc)
	v := m.View("md")
	if v.Kind != ui.KindPanel || v.ID != "md" || v.Scope != "markdown" || !v.Focusable || v.Children[0].Kind != ui.KindMarkdown {
		t.Fatalf("node %+v", v)
	}
	out := render(t, v, 60, 16)
	for _, want := range []string{"Doc", "Title", "intro with site", "Install", "go install"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q in:\n%s", want, out)
		}
	}
	k1 := v.CacheKey
	m.Offset = 9
	v = m.View("md")
	out = render(t, v, 60, 16)
	if strings.Contains(out, "Install") || !strings.Contains(out, "Usage") || v.CacheKey == k1 {
		t.Errorf("scrolled render (cacheKey changed=%v):\n%s", v.CacheKey != k1, out)
	}
	if v.Facts["heading"] != "Usage" {
		t.Errorf("facts %+v", v.Facts)
	}
	m.Offset = 6 // inside the sh block: keeps code styling
	if got := m.View("md").Children[0].Props["markdown"].(string); !strings.HasPrefix(got, "```sh\ngo install") {
		t.Errorf("mid-block source %q", got)
	}
	if a, b := m.View("md").CacheKey, m.View("md").CacheKey; a != b {
		t.Error("cache key not deterministic")
	}
}
