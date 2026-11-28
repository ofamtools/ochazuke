package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/bindings"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/pathpicker"
)

// fixtureFS is the real file system with a fake home, so ~ paths stay in
// the temporary fixture.
type fixtureFS struct {
	pathpicker.OSFS
	home string
}

func (f fixtureFS) HomeDir() (string, error) { return f.home, nil }

type driver struct {
	a     *app
	fails []string
}

// send delivers msg and feeds back the messages its commands produce.
func (d *driver) send(msg tea.Msg) {
	_, cmd := d.a.sh.Update(msg)
	d.pump(cmd)
}

func (d *driver) pump(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case m := <-out:
		switch m := m.(type) {
		case tea.BatchMsg:
			for _, c := range m {
				d.pump(c)
			}
		case tea.QuitMsg:
		default:
			d.send(m)
		}
	case <-time.After(40 * time.Millisecond): // prefix timeouts are not waited for
	}
}

var named = map[string]tea.KeyPressMsg{
	"tab": {Code: tea.KeyTab}, "shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
	"enter": {Code: tea.KeyEnter}, "esc": {Code: tea.KeyEscape}, "backspace": {Code: tea.KeyBackspace},
	"space": {Code: tea.KeySpace, Text: " "}, "down": {Code: tea.KeyDown}, "up": {Code: tea.KeyUp},
}

// key presses named keys, alt+N, or each rune of a word typed as is.
func (d *driver) key(names ...string) {
	for _, n := range names {
		switch {
		case named[n].Code != 0:
			d.send(named[n])
		case strings.HasPrefix(n, "alt+"):
			d.send(tea.KeyPressMsg{Code: rune(n[len("alt+")]), Mod: tea.ModAlt})
		default:
			for _, r := range n {
				d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

func (d *driver) check(name string, ok bool) {
	status := "ok  "
	if !ok {
		status = "FAIL"
		d.fails = append(d.fails, name)
	}
	s := d.a.sh.State
	fmt.Printf("%s %-52s tab=%-5s mode=%-12s focus=%s\n", status, name, s.ActivePage, d.a.eng.Mode(s), s.ActiveFrame().FocusID)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m|\x1b\]8;[^\a]*\a`)

func (d *driver) plain() string { return ansiRe.ReplaceAllString(d.a.sh.View().Content, "") }
func (d *driver) mode() string  { return d.a.eng.Mode(d.a.sh.State) }

// fixture builds home and work folders under a temporary directory.
func fixture() (root string, o options, err error) {
	root, err = os.MkdirTemp("", "ochazuke-widgets-smoke-")
	if err != nil {
		return "", o, err
	}
	home, work := filepath.Join(root, "home"), filepath.Join(root, "work")
	for _, dir := range []string{filepath.Join(home, "docs"), filepath.Join(work, "src")} {
		if err = os.MkdirAll(dir, 0o755); err != nil {
			return root, o, err
		}
	}
	for path, body := range map[string]string{
		filepath.Join(home, "notes.txt"): "hello from notes\n", filepath.Join(home, ".hidden"): "x",
		filepath.Join(home, "docs", "a.txt"): "a", filepath.Join(work, "README.md"): "# work\n",
	} {
		if err = os.WriteFile(path, []byte(body), 0o644); err != nil {
			return root, o, err
		}
	}
	return root, options{fs: fixtureFS{home: home}, start: home, roots: []pathpicker.Root{{Label: "home", Path: home}, {Label: "work", Path: work}}}, nil
}

func runSmoke() error {
	root, o, err := fixture()
	if root != "" {
		defer os.RemoveAll(root)
	}
	if err != nil {
		return err
	}
	a, err := newApp(o)
	if err != nil {
		return err
	}
	d := &driver{a: a}
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	plain := d.plain()
	d.check("five labelled tabs and a footer render", strings.Contains(plain, "1 Table") && strings.Contains(plain, "5 Bindings") && strings.Contains(plain, "next tab"))

	d.smokeTable()
	d.smokeForm()
	d.smokePaths(o)
	d.smokeVim()
	d.smokeKeys()
	d.smokeKeyboard()

	d.key("?")
	help, _ := a.sh.State.ActiveFrame().Data.(ui.ViewNode)
	d.check("? opens contextual help from widgets/bindings", d.mode() == "modal" && help.Kind == ui.KindTable && strings.Contains(d.plain(), "Keys that resolve here"))
	d.key("esc")
	d.check("esc closes help", len(a.sh.State.Modals) == 0)
	if len(a.themes) > 1 {
		before := a.sh.Theme.Name
		d.key("t")
		d.check("t switches to the next theme preset", a.sh.Theme.Name != before)
	}
	d.key("q")
	d.check("q quits", slices.ContainsFunc(a.sh.LastEffects, func(e core.Effect) bool { _, ok := e.(core.QuitEffect); return ok }))

	if len(d.fails) > 0 {
		return fmt.Errorf("%d smoke checks failed: %s", len(d.fails), strings.Join(d.fails, "; "))
	}
	fmt.Println("smoke: all checks passed")
	return nil
}

func (d *driver) smokeTable() {
	m := d.a.table
	rows := func() int { return len(m.Filtered()) }
	d.check("table starts unfiltered", d.a.sh.State.ActivePage == "table" && rows() == len(packages))
	d.key("/", "wid", "enter")
	d.check("/wid substring filter", d.mode() == "normal" && rows() == 7)
	d.key("/", "esc", "m", "/", "wpp", "enter")
	d.check("m fuzzy: wpp finds widgets/pathpicker", m.MatchMode == "fuzzy" && rows() == 1)
	d.key("m", "/", "backspace", "backspace", "backspace", "^(ui|tea)$", "enter")
	d.check("m regex: ^(ui|tea)$", m.MatchMode == "regex" && rows() == 2 && m.FilterErr == "")
	d.key("f")
	d.check("f filters one column", m.FilterField == "name" && rows() == 2)
	d.key("f", "f", "f", "f", "/", "esc", "m")
	d.check("filter cleared, all columns, substring", m.FilterField == "" && m.Query == "" && m.MatchMode == "substring")
	d.key("s")
	first := func() string { r, _ := m.FocusedRow(); return r.ID }
	d.check("s sorts by name ascending", m.SortField == "name" && first() == "buffer")
	d.key("s")
	d.check("s again sorts descending", first() == "widgets/table")
	d.key("g")
	headers := func() (n int) {
		for _, it := range m.Items() {
			if it.Header {
				n++
			}
		}
		return n
	}
	d.check("g groups by a unique column", m.GroupField == "name" && headers() == len(packages))
	d.key("g")
	d.check("g again groups by tier", m.GroupField == "tier" && headers() == 4)
	d.key("j", "space", "j", "space")
	d.check("space marks rows", len(m.Marked) == 2)
	d.key("g", "g", "g")
	d.check("g cycles back to ungrouped", m.GroupField == "")
}

func (d *driver) smokeForm() {
	m, sh := d.a.form, d.a.sh
	d.key("alt+2")
	d.check("alt+2 opens the form", sh.State.ActivePage == "form" && sh.Theme.Density == ui.DensityBalanced)
	before := d.plain()
	d.key("d")
	d.check("d switches to spacious density and repaints", sh.Theme.Density == ui.DensitySpacious && d.plain() != before)
	d.key("d")
	d.check("d switches to compact density and repaints", sh.Theme.Density == ui.DensityCompact && d.plain() != before)
	d.key("d")
	d.check("d returns to balanced", sh.Theme.Density == ui.DensityBalanced && d.plain() == before)
	d.key("S")
	d.check("S rejects a missing required name", strings.Contains(d.a.note, "fix name"))
	d.key("enter", "api", "enter")
	d.check("enter edits text and leaves", d.mode() == "normal" && m.Values["name"] == "api")
	d.key("j", "j", "l", "j", "space")
	d.check("l cycles the enum, space toggles the bool", m.Values["level"] == "warn" && m.Values["tls"] == true)
	d.key("S")
	d.check("S submits typed values", strings.HasPrefix(d.a.note, "submitted") && strings.Contains(d.a.note, "level=warn") && strings.Contains(d.a.note, "port=8080"))
}

func (d *driver) smokePaths(o options) {
	m := d.a.paths
	home, work := o.roots[0].Path, o.roots[1].Path
	focusedName := func() string { e, _ := m.Focused(); return e.Name }
	d.key("alt+3")
	d.check("alt+3 opens paths at the first root", d.a.sh.State.ActivePage == "paths" && m.Dir == home)
	d.key("r", "r")
	d.check("r activates the first root, r again cycles on", m.Dir == work)
	d.key("1", "r")
	d.check("1r jumps to root 1", m.Dir == home)
	d.key("~", "/docs", "enter")
	d.check("typed ~/docs jumps there", d.mode() == "normal" && m.Dir == filepath.Join(home, "docs"))
	d.key("h")
	d.check("h goes to the parent", m.Dir == home)
	d.key("p")
	for i := 0; i < 8 && focusedName() != "notes.txt"; i++ {
		d.key("j")
	}
	d.check("p previews the focused file", m.ShowPreview && strings.Contains(d.plain(), "hello from notes"))
	d.key("+", "made", "enter")
	info, err := os.Stat(filepath.Join(home, "made"))
	d.check("+ creates a folder in the fixture", err == nil && info.IsDir())
	d.check("+ highlights the new folder", d.a.note == "created made" && focusedName() == "made")
	d.key("g", "g")
	d.check("g g reaches the pinned row", m.FocusedCurrent())
	d.key("enter")
	d.check("enter on the pinned row uses the current folder", d.a.note == "picked "+home && len(m.Recents) > 0 && m.Recents[0] == home)
	d.a.note = ""
	d.key("u")
	d.check("u uses the current folder", d.a.note == "picked "+home)
}

func (d *driver) smokeVim() {
	m := d.a.vim
	line := func() string { l, _, _ := strings.Cut(m.Value(), "\n"); return l }
	d.key("alt+4")
	d.check("alt+4 opens the vim input in passthrough", d.mode() == "vim" && m.Value() == vimText)
	d.key("w", "dw")
	d.check("w dw deletes a word", line() == "The brown fox")
	d.key(".")
	d.check(". repeats it", line() == "The fox")
	d.key("u")
	d.check("u undoes", line() == "The brown fox")
	d.key("ciw", "red")
	d.check("ciw enters insert mode", m.Mode() == input.ModeInsert)
	d.key("esc")
	d.check("esc ends the change", m.Mode() == input.ModeNormal && line() == "The red fox")
	d.key("dd")
	d.check("dd deletes the line", strings.HasPrefix(m.Value(), "jumps"))
	d.key("u", "tab")
	d.check("u restores it, tab still leaves the editor", strings.HasPrefix(m.Value(), "The red fox") && d.a.sh.State.ActivePage == "keys")
}

func (d *driver) smokeKeys() {
	m := d.a.keys
	focused := func() core.Binding { i, _ := m.Focused(); return m.Bindings()[i] }
	sorted := func() string { return fmt.Sprint(d.a.table.SortField, d.a.table.SortDir) }
	n := len(m.Bindings())
	d.key("/", "verb:table.sort.next", "enter")
	d.check("filter verb:table.sort.next finds one row", len(m.Filtered()) == 1 && focused().Command == "table.sort.next")
	d.key("r", "g")
	i, _ := m.Focused()
	kinds := func() (out []string) {
		for _, c := range bindings.AnalyzeBindings(modes, m.Bindings(), nil, d.a.specs...)[i].Conflicts {
			out = append(out, c.Kind)
		}
		return out
	}
	d.check("r g rebinds and reports the clash with group", slices.Equal(focused().Keys, k("g")) && slices.Contains(kinds(), "duplicate") && strings.Contains(d.plain(), "duplicate with row"))
	d.key("u")
	d.check("u undoes the rebind", slices.Equal(focused().Keys, k("s")))
	d.key("r", "S", "alt+1")
	sort := sorted()
	d.key("s")
	d.check("the old key s no longer sorts", sorted() == sort)
	d.key("S")
	d.check("the new key S sorts the table live", sorted() != sort)
	d.key("alt+5", "R")
	d.check("R resets the row", slices.Equal(focused().Keys, k("s")))
	free, _ := bindings.FindFreeBinding(modes, d.a.all(), "tab.table", "normal", false, nil)
	d.key("a")
	d.check("a copies the row onto FindFreeBinding's key", len(m.Bindings()) == n+1 && slices.Equal(m.Bindings()[n].Keys, free))
	d.key("x")
	d.check("x unbinds the focused row", focused().Unbind)
	d.key("u", "alt+1")
	sort = sorted()
	d.key(free...)
	d.check("the added key "+strings.Join(free, " ")+" sorts the table live", sorted() != sort)
	d.key("alt+5", "/", "esc")
	d.check("esc clears the editor filter", len(m.Filtered()) == n+1)
}

func (d *driver) smokeKeyboard() {
	facts := func() map[string]any { return d.a.keys.ViewNode("keys").Facts }
	d.key("v")
	d.check("v opens the keyboard and its prefix panel", facts()["viewMode"] == bindings.ViewKeyboard && strings.Contains(d.plain(), "Prefixes"))
	d.key("j")
	d.check("j opens the next prefix", facts()["activePrefix"] != "")
	d.key("k")
	d.check("k returns to Base", facts()["activePrefix"] == "")
	_ = d.plain()
	g := d.a.sh.State.Layout.Rects["keys.key.g"]
	d.send(tea.MouseClickMsg{X: g.X + 1, Y: g.Y + 1, Button: tea.MouseLeft})
	d.key("enter")
	d.check("Enter on the g cap opens the g layer", facts()["activePrefix"] == "g")
	d.key("enter")
	d.check("Enter on the final g shows its command", strings.Contains(d.plain(), "motion.top"))
	d.key("backspace", "s")
	d.check("Backspace clears the prefix and s switches Shift on", facts()["activePrefix"] == "" && facts()["shifted"] == true && strings.Contains(d.plain(), "Shift on"))
	d.key("v")
	d.check("v returns to the bindings table", facts()["viewMode"] == bindings.ViewTable)
}
