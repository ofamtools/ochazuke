package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAppearanceUpdatesAreAtomicSnapshots(t *testing.T) {
	r := NewRuntime()
	a := NewAppearance(r)
	result, err := r.Execute(Request{Verb: "set", Noun: "app.appearance", Args: Arguments{"theme": "dark", "density": "compact"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := result.(Appearance)
	if !ok || snapshot != (Appearance{Theme: "dark", Density: "compact", Dark: true}) {
		t.Fatalf("set returned %T %#v, want an appearance value", result, result)
	}
	for _, args := range []Arguments{
		{"theme": "light", "density": "crowded"},
		{"theme": "missing", "density": "comfortable"},
		{"theme": ""},
		{"density": ""},
		{"theme": 3, "density": "comfortable"},
		{"density": false, "theme": "light"},
		{"unknown": "light"},
	} {
		if _, err := r.Execute(Request{Verb: "set", Noun: "app.appearance", Args: args}); err == nil {
			t.Fatalf("accepted invalid appearance %#v", args)
		}
		if *a != (Appearance{Theme: "dark", Density: "compact", Dark: true}) {
			t.Fatalf("invalid update %#v partially mutated appearance: %#v", args, *a)
		}
	}
	got, err := r.Execute(Request{Verb: "get", Noun: "app.appearance"})
	if err != nil {
		t.Fatal(err)
	}
	before, ok := got.(Appearance)
	if !ok {
		t.Fatalf("get returned %T, want an appearance value", got)
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "app.appearance", Args: Arguments{"theme": "light"}}); err != nil {
		t.Fatal(err)
	}
	if *a != (Appearance{Theme: "light", Density: "compact", Dark: true}) || before != snapshot {
		t.Fatalf("partial update lost density or mutated prior snapshot: live=%#v before=%#v set=%#v", *a, before, snapshot)
	}
	before.Theme = "mono"
	if a.Theme != "light" {
		t.Fatal("editing returned snapshot changed live appearance")
	}
}

func TestListedThemesCanBeSelected(t *testing.T) {
	r := NewRuntime()
	a := NewAppearance(r)
	got, err := r.Execute(Request{Verb: "list", Noun: "app.themes"})
	if err != nil {
		t.Fatal(err)
	}
	themes, ok := got.([]string)
	if !ok {
		t.Fatalf("themes.list returned %T", got)
	}
	seen := map[string]bool{}
	for _, theme := range themes {
		if seen[theme] {
			t.Fatalf("duplicate theme %q", theme)
		}
		seen[theme] = true
		if _, err := r.Execute(Request{Verb: "set", Noun: "app.appearance", Args: Arguments{"theme": theme}}); err != nil || a.Theme != theme {
			t.Fatalf("listed theme %q cannot be selected: %#v, %v", theme, *a, err)
		}
	}
	for _, required := range []string{"adaptive", "dark", "light", "mono"} {
		if !seen[required] {
			t.Fatalf("theme %q is unavailable", required)
		}
	}
	// A caller may edit a list result without corrupting the next request.
	themes[0] = "changed"
	next, err := r.Execute(Request{Verb: "list", Noun: "app.themes"})
	if err != nil || next.([]string)[0] == "changed" {
		t.Fatalf("theme list aliases mutable state: %#v, %v", next, err)
	}
}

func TestModalPreservesBackdropAndCentersContent(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "mono"})
	background := strings.TrimSuffix(strings.Repeat(strings.Repeat("b", 80)+"\n", 24), "\n")
	view := ComposeModal(background, "Choose target", "modal-body", "Esc back", 80, 24, styles)
	rows := strings.Split(ansi.Strip(view), "\n")
	if len(rows) != 24 || rows[0] != strings.Repeat("b", 80) || rows[23] != strings.Repeat("b", 80) {
		t.Fatalf("backdrop perimeter disappeared:\n%s", view)
	}
	bodyRow := -1
	for row, line := range rows {
		if ansi.StringWidth(line) != 80 {
			t.Fatalf("row %d has width %d", row, ansi.StringWidth(line))
		}
		if strings.Contains(line, "modal-body") {
			bodyRow = row
			if !strings.HasPrefix(line, "bbbb") || !strings.HasSuffix(line, "bbbb") {
				t.Fatalf("modal covered the page rather than overlaying it: %q", line)
			}
		}
	}
	if bodyRow != 11 || !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatalf("content-sized rounded modal is missing or not centered:\n%s", view)
	}
}

func TestModalGeometryWithANSIWideTextAndTinyTerminals(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "adaptive"})
	background := strings.Repeat("\x1b[35m"+strings.Repeat("界", 60)+"\x1b[0m\n", 40)
	body := strings.Repeat("\x1b[32m選択 👩‍💻 e\u0301 "+strings.Repeat("界", 80)+"\x1b[0m\n", 30)
	for _, size := range [][2]int{{100, 40}, {80, 24}, {19, 9}, {9, 7}, {5, 5}, {3, 3}, {2, 2}, {1, 1}, {0, 10}, {10, 0}, {-1, 4}, {4, -1}} {
		width, height := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
			view := ComposeModal(background, "選択", body, "Esc 戻る", width, height, styles)
			cw, ch := ModalSize(width, height)
			if width <= 0 || height <= 0 {
				if view != "" || cw != 0 || ch != 0 {
					t.Fatalf("empty terminal produced content: %q, %dx%d", view, cw, ch)
				}
				return
			}
			rows := strings.Split(view, "\n")
			if len(rows) != height {
				t.Fatalf("got %d rows, want %d", len(rows), height)
			}
			for row, line := range rows {
				if got := ansi.StringWidth(line); got != width {
					t.Fatalf("row %d has %d cells, want %d: %q", row, got, width, line)
				}
			}
			if cw > 0 && ch > 0 && !strings.Contains(ansi.Strip(view), "…") {
				t.Fatal("overflow was silently clipped")
			}
			if height >= 3 && ansi.Strip(rows[0]) != ansi.Strip(ansi.Truncate(background[:strings.IndexByte(background, '\n')], width, ""))+strings.Repeat(" ", width%2) {
				t.Fatalf("top backdrop row changed: %q", rows[0])
			}
		})
	}
}

// A modal is about 80% of the terminal's columns at any width, with no
// fixed cap, and keeps the bordered chrome and tiny-terminal fallbacks.
func TestModalWidthIsEightyPercentWithNoCap(t *testing.T) {
	for _, tc := range []struct{ width, content int }{{80, 60}, {100, 76}, {160, 124}, {300, 236}, {7, 1}, {6, 4}, {3, 1}} {
		if cw, _ := ModalSize(tc.width, 24); cw != tc.content {
			t.Errorf("ModalSize(%d) content width %d, want %d", tc.width, cw, tc.content)
		}
	}
}

func TestModalSizeLeavesRoomForEveryBodyCell(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "mono", Density: "compact"})
	for _, size := range [][2]int{{160, 40}, {80, 24}, {19, 9}, {9, 7}, {3, 3}, {1, 1}} {
		cw, ch := ModalSize(size[0], size[1])
		body := strings.TrimSuffix(strings.Repeat(strings.Repeat("x", cw)+"\n", ch), "\n")
		view := ansi.Strip(ComposeModal("", "T", body, "F", size[0], size[1], styles))
		if strings.Count(view, "x") != cw*ch || strings.Contains(view, "…") {
			t.Fatalf("advertised %dx%d body does not fit %dx%d terminal:\n%s", cw, ch, size[0], size[1], view)
		}
	}
}

func TestModalSanitizesRawLabelsButPreservesTrustedBodyStyles(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "mono"})
	view := ComposeModal("page", "\x1b[2JChoose\nName\x1b]0;injected\a", "\x1b[31mtrusted body\x1b[0m", "Esc\tback\r\x1b[99B", 80, 24, styles)
	if strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x1b]") || strings.Contains(view, "\x1b[99B") || strings.Contains(view, "\r") {
		t.Fatalf("raw label injected terminal controls: %q", view)
	}
	if !strings.Contains(ansi.Strip(view), "Choose Name") || !strings.Contains(ansi.Strip(view), "Esc back") || !strings.Contains(view, "\x1b[31mtrusted body") {
		t.Fatalf("labels or trusted body style were lost: %q", view)
	}
}

func TestNoColorKeepsVisibleFocusCues(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	styles := NewStyles(Appearance{Theme: "dark"})
	panel := ansi.Strip(styles.Panel.Render("editor"))
	focused := ansi.Strip(styles.FocusedPanel.Render("editor"))
	tab := ansi.Strip(styles.Tab.Render("files"))
	active := ansi.Strip(styles.ActiveTab.Render("files"))
	if panel == focused || tab == active || !strings.Contains(focused, "editor") || !strings.Contains(active, "files") {
		t.Fatalf("focus depends only on color: panel=%q focused=%q tab=%q active=%q", panel, focused, tab, active)
	}
}

// A modal's cursor is the body's cursor moved by the panel; the backdrop's is dropped.
func TestModalBlockOffsetsBodyCursor(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "mono"})
	in := NewInput("field", "ab\ncd", true)
	in.Cursor = 4
	for _, size := range [][2]int{{56, 18}, {80, 24}, {9, 7}, {19, 9}} {
		width, height := size[0], size[1]
		cw, ch := ModalSize(width, height)
		body := InputBlock(in, cw, ch)
		view := ComposeModalBlock(strings.Repeat(strings.Repeat("b", width)+"\n", height), "Draft", body, "Esc close", width, height, styles)
		rows := strings.Split(ansi.Strip(view.Text), "\n")
		if len(rows) != height {
			t.Fatalf("%dx%d: got %d rows", width, height, len(rows))
		}
		for i, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatalf("%dx%d: row %d has %d cells", width, height, i, ansi.StringWidth(row))
			}
		}
		if view.Cursor == nil {
			t.Fatalf("%dx%d: modal lost the body cursor", width, height)
		}
		if cell := cellIndex(rows[view.Cursor.Y], "cd"); cell < 0 || cell+1 != view.Cursor.X {
			t.Fatalf("%dx%d: cursor %+v is not on the d of cd:\n%s", width, height, view.Cursor, ansi.Strip(view.Text))
		}
		if ComposeModal(strings.Repeat("b", width), "Draft", body.Text, "Esc close", width, height, styles) == "" {
			t.Fatal("string wrapper rendered nothing")
		}
	}
	if ComposeModalBlock("page", "T", InputBlock(in, 4, 2), "F", 2, 2, styles).Cursor != nil {
		t.Fatal("tiny terminal reported a cursor")
	}
}
