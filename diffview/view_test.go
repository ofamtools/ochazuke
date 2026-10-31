package diffview

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

const sample = `diff --git a/demo.go b/demo.go
index 1234567..7654321 100644
--- a/demo.go
+++ b/demo.go
@@ -1,4 +1,4 @@ package demo
 package demo
-func oldName() string {
- return "old"
+func newName() string {
+ return "new"
 }
@@ -30,2 +30,3 @@ tail
 // tail
-var old = 1
+var next = 2
+// added
@@ -60 +61 @@ final
-old final
+new final
`

func fixture(t testing.TB, opts ...Option) (*ochazuke.Runtime, *View) {
	t.Helper()
	files, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Instance("p")
	v, err := New(r, "diff", "p", files[0], opts...)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.AddPage("p", "Diff", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("") }, ochazuke.Focus(v.ID()))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	return r, v
}
func mono() ochazuke.Styles { return ochazuke.NewStyles(ochazuke.Appearance{Theme: "mono"}) }

func TestDiffGolden(t *testing.T) {
	for _, layout := range []Layout{Unified, Split} {
		t.Run(string(layout), func(t *testing.T) {
			_, v := fixture(t, WithLayout(layout))
			frame := ansi.Strip(v.View(mono(), 80, 22))
			rows := strings.Split(frame, "\n")
			if len(rows) != 22 {
				t.Fatalf("height=%d", len(rows))
			}
			for i, row := range rows {
				if ansi.StringWidth(row) != 80 {
					t.Fatalf("row %d width=%d", i, ansi.StringWidth(row))
				}
				rows[i] = strings.TrimRight(row, " ")
			}
			got := strings.TrimRight(strings.Join(rows, "\n"), "\n") + "\n"
			path := filepath.Join("testdata", string(layout)+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Fatalf("frame differs from %s:\n%s", path, got)
			}
		})
	}
}
func TestDiffBindingsAndMotions(t *testing.T) {
	r, v := fixture(t)
	if problems := r.CheckDefaultBindings("diffview/bindings.json", defaultBindings); len(problems) > 0 {
		t.Fatal(problems)
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	v.View(mono(), 80, 7)
	keys := func(keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, err := r.Dispatch(ochazuke.MacroKey{Key: key}); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			v.View(mono(), 80, 7)
		}
	}
	keys("2", "]", "c")
	if v.cursor != v.hunks[2] {
		t.Fatalf("2]c row=%d hunks=%v", v.cursor, v.hunks)
	}
	keys("[", "c")
	if v.cursor != v.hunks[1] {
		t.Fatalf("[c row=%d", v.cursor)
	}
	keys("9", "]", "c")
	if v.cursor != v.hunks[2] {
		t.Fatalf("end clamp=%d", v.cursor)
	}
	keys("9", "[", "c")
	if v.cursor != 0 {
		t.Fatalf("start clamp=%d", v.cursor)
	}
	keys("j")
	if v.cursor != 1 {
		t.Fatalf("j row=%d", v.cursor)
	}
	keys("k")
	if v.cursor != 0 {
		t.Fatalf("k row=%d", v.cursor)
	}
	keys("J")
	if v.top != 1 {
		t.Fatalf("J top=%d", v.top)
	}
	keys("K")
	if v.top != 0 {
		t.Fatalf("K top=%d", v.top)
	}
	keys("ctrl+d")
	if v.top != v.height/2 {
		t.Fatalf("ctrl+d top=%d height=%d", v.top, v.height)
	}
	keys("ctrl+u")
	if v.top != 0 {
		t.Fatalf("ctrl+u top=%d", v.top)
	}
	// The framework's own scroller reaches this component.
	if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.view", Verb: "scroll", Args: ochazuke.Arguments{"lines": 2}}); err != nil {
		t.Fatal(err)
	}
	if v.top != 2 {
		t.Fatalf("view.scroll top=%d", v.top)
	}
	var events []int
	v.OnFileChange = func(n int) { events = append(events, n) }
	var hookEvents []int
	r.Register(ochazuke.Operation{Noun: "host", Verb: "file", Parameters: map[string]ochazuke.Parameter{"delta": {Type: "integer"}, "id": {Type: "string"}}, Run: func(_ *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) (any, error) {
		if a["id"] != "diff" {
			t.Errorf("event id=%v", a["id"])
		}
		hookEvents = append(hookEvents, ochazuke.IntArg(a, "delta", 0))
		return nil, nil
	}})
	if err := r.AddDefaultHooks(ochazuke.HookRow{On: EventFile, Do: "host.file", From: map[string]string{"delta": "delta", "id": "id"}}); err != nil {
		t.Fatal(err)
	}

	keys("3", "]", "f", "[", "f")
	if !reflect.DeepEqual(events, []int{3, -1}) {
		t.Fatalf("file requests=%v", events)
	}
	if !reflect.DeepEqual(hookEvents, events) {
		t.Fatalf("file events=%v callbacks=%v", hookEvents, events)
	}

}
func TestDiffResponsiveAndHighlight(t *testing.T) {
	_, v := fixture(t)
	v.View(mono(), 139, 12)
	if v.layout != Unified {
		t.Fatal(v.layout)
	}
	v.View(mono(), 140, 12)
	if v.layout != Split {
		t.Fatal(v.layout)
	}
	v.SplitWidth = 160
	v.View(mono(), 159, 12)
	if v.layout != Unified {
		t.Fatal(v.layout)
	}
	v.Hunk(1)
	hi := v.rows[v.cursor].hunk
	v.View(mono(), 160, 12)
	if v.layout != Split || v.rows[v.cursor].hunk != hi {
		t.Fatalf("resize lost hunk: %v", v.Snapshot())
	}
	v.SetFile(v.file)
	v.Highlight = false
	v.View(mono(), 80, 6)
	if len(v.spans) != 0 {
		t.Fatal("highlight off tokenised")
	}
	v.Highlight = true
	v.View(mono(), 80, 6)
	if len(v.spans) == 0 {
		t.Fatal("no visible line tokenised")
	}
	for key := range v.spans {
		if key.hunk != 0 || key.line > 3 {
			t.Fatalf("hidden line tokenised: %+v", key)
		}
	}
	if got := Highlight("test.go", []string{"func demo() string { return \"hello\" }"}); len(got) != 1 || len(got[0]) == 0 {
		t.Fatal("Go syntax missing")
	}
}
func TestDiffSpecialFilesAndSafety(t *testing.T) {
	_, v := fixture(t)
	v.SetFile(File{OldPath: "a.bin", NewPath: "a.bin", Binary: true, OldSize: 12, NewSize: 34, Truncated: true, Added: 100, Removed: 30})
	got := ansi.Strip(v.View(mono(), 80, 5))
	for _, want := range []string{"binary, 12 bytes → 34 bytes", "+100 -30", "truncated"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	f := File{NewPath: "a\x1b[31m.go", Hunks: []Hunk{{OldStart: 1, NewStart: 1, Lines: []Line{{Kind: Add, New: 1, Text: "\x1b[31m" + strings.Repeat("界", 300) + " tail"}}}}}
	v.SetFile(f)
	f.Hunks[0].Lines[0].Text = "mutated"
	for _, width := range []int{1, 2, 3, 8, 30, 80, 160} {
		got = v.View(mono(), width, 5)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at width %d", width)
		}
		for _, line := range strings.Split(got, "\n") {
			if ansi.StringWidth(line) != width {
				t.Fatalf("width %d line has %d: %q", width, ansi.StringWidth(line), line)
			}
		}
	}
	if strings.Contains(got, "\x1b[31m") || strings.Contains(got, "mutated") {
		t.Fatal("untrusted input escaped snapshot/sanitisation")
	}
	v.View(mono(), 30, 5)
	v.ScrollColumns(24)
	got = ansi.Strip(v.View(mono(), 30, 5))
	if !strings.Contains(got, "‹") {
		t.Fatal("missing left overflow marker")
	}
	v.ScrollColumns(-9999)
	if v.x != 0 {
		t.Fatal("negative offset")
	}
}
func TestDiffChangedAndPairing(t *testing.T) {
	pairs := Pairs([]Kind{Context, Delete, Delete, Add, Context, Add})
	want := []Pair{{0, 0}, {1, 3}, {2, -1}, {4, 4}, {-1, 5}}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("pairs=%v", pairs)
	}
	a, b := Changed("call(旧名, same)", "call(新名, same)")
	if "call(旧名, same)"[a.Start:a.End] != "旧名" || "call(新名, same)"[b.Start:b.End] != "新名" {
		t.Fatalf("changed=%v %v", a, b)
	}
	_, v := fixture(t)
	v.Highlight = false
	frame := v.View(mono(), 80, 22)
	if !strings.Contains(frame, "\x1b[4") && !strings.Contains(frame, ";4m") {
		t.Fatal("mono lost changed-word underline")
	}
}
func BenchmarkDiffVisibleRows(b *testing.B) {
	_, v := fixture(b)
	lines := make([]Line, 100000)
	for i := range lines {
		lines[i] = Line{Kind: Context, Text: "const answer = 42", Old: i + 1, New: i + 1}
	}
	v.SetFile(File{NewPath: "huge.go", Hunks: []Hunk{{Lines: lines}}})
	styles := mono()
	v.View(styles, 160, 30)
	b.ResetTimer()
	for b.Loop() {
		v.View(styles, 160, 30)
	}
}

func TestDiffThemeAndResize(t *testing.T) {
	_, v := fixture(t)
	t.Setenv("NO_COLOR", "")
	a := ochazuke.Appearance{Theme: "dark"}
	a.SetTone("diff.text", ochazuke.ToneStyle{Fg: ochazuke.SchemeColor{Light: "7", Dark: "7"}, Bg: ochazuke.SchemeColor{Light: "0", Dark: "0"}})
	for name, slot := range map[string]string{"diff.add": "2", "diff.remove": "1"} {
		a.SetTone(name, ochazuke.ToneStyle{Fg: ochazuke.SchemeColor{Light: slot, Dark: slot}})
	}
	for _, class := range ClassNames {
		a.SetTone("diff.syntax."+class, ochazuke.ToneStyle{Fg: ochazuke.SchemeColor{Light: "7", Dark: "7"}})
	}
	frame := v.View(ochazuke.NewStyles(a), 80, 20)
	// Source rows take their ANSI slot colours, signs and underline, without
	// adding truecolour backgrounds outside this small theme palette.
	line := ""
	for _, row := range strings.Split(frame, "\n") {
		if strings.Contains(ansi.Strip(row), "oldName") {
			line = row
		}
	}
	if line == "" || strings.Contains(line, "48;2;") || !strings.Contains(ansi.Strip(line), "oldName") {
		t.Fatalf("16-colour source=%q", line)
	}
	first := ansi.Strip(frame)
	a.SetTone("diff.add", ochazuke.ToneStyle{Fg: ochazuke.SchemeColor{Light: "5", Dark: "5"}})
	next := v.View(ochazuke.NewStyles(a), 80, 20)
	if frame == next || first != ansi.Strip(next) {
		t.Fatal("theme update did not recolour identical text")
	}
	v.Hunk(99)
	v.View(mono(), 80, 3)
	if v.cursor < v.top || v.cursor >= v.top+v.height {
		t.Fatalf("resize hid cursor: %+v", v.Snapshot())
	}
	t.Setenv("NO_COLOR", "1")
	plain := v.View(ochazuke.NewStyles(a), 80, 20)
	if strings.Contains(plain, "38;") || strings.Contains(plain, "48;") {
		t.Fatal("NO_COLOR emitted colour")
	}
}

func TestDiffLongLineKeepsChangedMark(t *testing.T) {
	_, v := fixture(t)
	old, newLine := "oldName() "+strings.Repeat("x ", 3000), "newName() "+strings.Repeat("x ", 3000)
	v.SetFile(File{OldPath: "long.go", NewPath: "long.go", Hunks: []Hunk{{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1, Lines: []Line{{Kind: Delete, Old: 1, Text: old}, {Kind: Add, New: 1, Text: newLine}}}}})
	frame := v.View(mono(), 80, 5)
	if !strings.Contains(ansi.Strip(frame), "oldName") || !strings.Contains(frame, ";4;") {
		t.Fatal("long line lost changed-word mark")
	}
	for _, spans := range v.spans {
		if len(spans) > 0 {
			t.Fatal("long line tokenised")
		}
	}
}
