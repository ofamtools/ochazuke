package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// forEachEngine runs fn once per engine this build runs.
func forEachEngine(t *testing.T, fn func(t *testing.T, engine string)) {
	t.Helper()
	names := availableEngines()
	if len(names) < 2 {
		t.Fatalf("engines %v: want tuios-vt and x-vt at least", names)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) { fn(t, name) })
	}
}

func newTestEmulator(t *testing.T, engine string, cols, rows int) Emulator {
	t.Helper()
	e, err := NewEmulator(engine, cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

// plainScreen is the frame's text, each row without trailing blanks.
func plainScreen(f ochazuke.TerminalFrame) string {
	rows := make([]string, len(f.Lines))
	for i, l := range f.Lines {
		rows[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return strings.Join(rows, "\n")
}

// vimLike is what an editor draws on 20x6 cells: the alternate screen,
// colours, a status line in reverse video, a scroll region scrolled up by a
// line feed and down by a reverse index, wide characters, the cursor moved,
// hidden then shown as a bar, application cursor keys and bracketed paste
// on (testdata/vimlike.vt); golden is the screen it leaves.
func vimLike(t *testing.T) (fixture []byte, golden string) {
	t.Helper()
	fixture, err := os.ReadFile("testdata/vimlike.vt")
	if err != nil {
		t.Fatal(err)
	}
	g, err := os.ReadFile("testdata/vimlike.golden")
	if err != nil {
		t.Fatal(err)
	}
	return fixture, strings.TrimSuffix(string(g), "\n")
}

// reply runs send and reads what the emulator queued for the program,
// want's length of it; the reader starts first, since x/vt's reply pipe
// blocks the sender until it is read, a write at a time.
func reply(t *testing.T, e Emulator, want string, send func()) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		var all []byte
		buf := make([]byte, 256)
		for len(all) < len(want) {
			n, err := e.Read(buf)
			all = append(all, buf[:n]...)
			if err != nil {
				break
			}
		}
		got <- string(all)
	}()
	send()
	select {
	case s := <-got:
		return s
	case <-time.After(2 * time.Second):
		t.Fatalf("the emulator queued less than %q", want)
	}
	return ""
}

// Every engine draws the vim-like fixture the same: the screen, the
// cursor, and the main screen back when the editor leaves.
func TestEnginesDrawTheVimLikeFixture(t *testing.T) {
	fixture, golden := vimLike(t)
	forEachEngine(t, func(t *testing.T, engine string) {
		e := newTestEmulator(t, engine, 20, 6)
		_, _ = e.Write([]byte("shell$ vim\r\n"))
		_, _ = e.Write(fixture)
		f := EmulatorFrame(e)
		if got := plainScreen(f); got != golden {
			t.Fatalf("screen:\n%s\nwant:\n%s", got, golden)
		}
		if f.Cursor != (ochazuke.Pos{X: 4, Y: 2}) || f.Shape != "bar" || f.Hidden {
			t.Fatalf("cursor %+v shape %q hidden %v", f.Cursor, f.Shape, f.Hidden)
		}
		if !strings.Contains(f.Lines[5], "\x1b[7m") {
			t.Fatalf("status line not in reverse video: %q", f.Lines[5])
		}
		if !strings.Contains(f.Lines[2], "32m") {
			t.Fatalf("wide characters lost their colour: %q", f.Lines[2])
		}
		// The keys follow the modes the editor set.
		if got := reply(t, e, "\x1bOA", func() { e.SendKey(uv.KeyPressEvent{Code: uv.KeyUp}) }); got != "\x1bOA" {
			t.Fatalf("up under application cursor keys = %q", got)
		}
		if got := reply(t, e, "\x1b[200~hi\x1b[201~", func() { e.Paste("hi") }); got != "\x1b[200~hi\x1b[201~" {
			t.Fatalf("paste = %q", got)
		}
		_, _ = e.Write([]byte("\x1b[?1049l"))
		if got := plainScreen(EmulatorFrame(e)); !strings.HasPrefix(got, "shell$ vim\n") {
			t.Fatalf("main screen after the alternate one:\n%s", got)
		}
	})
}

// The engines' styled renders of the fixture, compared row by row. They
// draw the same cells; the SGR they write for them may differ, which the
// test reports (go test -v) without failing.
func TestEnginesRenderTheFixtureAlike(t *testing.T) {
	fixture, _ := vimLike(t)
	renders := map[string][]string{}
	for _, name := range availableEngines() {
		e := newTestEmulator(t, name, 20, 6)
		_, _ = e.Write(fixture)
		renders[name] = EmulatorFrame(e).Lines
	}
	a, b := renders[DefaultTerminalEngine], renders["x-vt"]
	if len(a) != len(b) {
		t.Fatalf("tuios-vt %d rows, x-vt %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Logf("row %d differs:\n  tuios-vt %q\n  x-vt     %q", i, a[i], b[i])
		}
	}
}

// Legacy key encoding (tuios-vt without the kitty protocol): modifiers on
// cursor and function keys are xterm's CSI 1;m forms.
func TestLegacyKey(t *testing.T) {
	for name, want := range map[string]string{
		"a": "a", "A": "A", "ctrl+a": "\x01", "alt+b": "\x1bb", "ctrl+alt+c": "\x1b\x03",
		"enter": "\r", "tab": "\t", "shift+tab": "\x1b[Z", "backspace": "\x7f", "esc": "\x1b",
		"space": " ", "ctrl+space": "\x00", "up": "\x1b[A", "ctrl+up": "\x1b[1;5A",
		"shift+left": "\x1b[1;2D", "home": "\x1b[H", "delete": "\x1b[3~", "ctrl+delete": "\x1b[3;5~",
		"f1": "\x1bOP", "shift+f1": "\x1b[1;2P", "f5": "\x1b[15~", "pgdown": "\x1b[6~", "é": "é",
	} {
		ev, err := keyEvent(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := legacyKey(ev, false); got != want {
			t.Errorf("legacyKey(%s) = %q, want %q", name, got, want)
		}
	}
	ev, _ := keyEvent("up")
	if got := legacyKey(ev, true); got != "\x1bOA" {
		t.Errorf("up under DECCKM = %q", got)
	}
}

// A program that asks for the kitty keyboard protocol gets it from
// tuios-vt (x/vt has none).
func TestTuiosEngineSpeaksKittyKeys(t *testing.T) {
	e := newTestEmulator(t, "tuios-vt", 20, 3)
	_, _ = e.Write([]byte("\x1b[>1u"))
	ev, _ := keyEvent("ctrl+i")
	if got := reply(t, e, "\x1b[105;5u", func() { e.SendKey(ev) }); got != "\x1b[105;5u" {
		t.Fatalf("ctrl+i under kitty disambiguate = %q", got)
	}
}

func TestTerminalEngineRegistry(t *testing.T) {
	if _, err := NewEmulator("", 10, 2); err != nil {
		t.Fatalf("default engine: %v", err)
	}
	if _, err := NewEmulator("nope", 10, 2); err == nil || !strings.Contains(err.Error(), "tuios-vt") {
		t.Fatalf("unknown engine = %v", err)
	}
	if err := RegisterTerminalEngine(TerminalEngine{Name: "x-vt", New: func(int, int) Emulator { return nil }}); err == nil {
		t.Fatal("a second x-vt row was registered")
	}
}

// The setting offers the engines this build runs, defaults to tuios-vt,
// and refuses ghostty in a build without it with the reason.
func TestTerminalEngineSetting(t *testing.T) {
	r := ochazuke.NewRuntime()
	EnsureTerminalEngine(r)
	EnsureTerminalEngine(r) // twice is once
	if got := TerminalEngineOf(r); got != DefaultTerminalEngine {
		t.Fatalf("default engine %q", got)
	}
	if err := r.Settings().Set(TerminalEngineSetting, "x-vt"); err != nil {
		t.Fatal(err)
	}
	if got := TerminalEngineOf(r); got != "x-vt" {
		t.Fatalf("engine after set %q", got)
	}
	err := r.Settings().Set(TerminalEngineSetting, "nope")
	if err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("set nope = %v", err)
	}
	if e, _ := engineRow("ghostty"); e.Unavailable != "" {
		err = r.Settings().Set(TerminalEngineSetting, "ghostty")
		if err == nil || !strings.Contains(err.Error(), "-tags ghostty") {
			t.Fatalf("set ghostty = %v", err)
		}
	}
}

// Where the engines differ, pinned: tuios-vt draws what xterm does, x/vt
// v0.1.0 does not support left/right margins (DECSLRM), screen alignment
// (DECALN) or insert mode (IRM). Colours, 256 and true colour, underline
// styles, OSC 8 links, wide and combining characters, insert/delete line,
// ECH, REP, tabs, autowrap, DEC line drawing, DECSC/DECRC and synchronized
// output render the same in both.
func TestEngineDifferences(t *testing.T) {
	for _, c := range []struct{ name, in, tuios, xvt string }{
		{"left/right margins", "\x1b[?69h\x1b[2;5s\x1b[1;5H123456", "    1\n 2345\n 6\n", "    123456\n\n\n"},
		{"screen alignment", "\x1b#8", "EEEEEEEEEE\nEEEEEEEEEE\nEEEEEEEEEE\nEEEEEEEEEE", "\n\n\n"},
		{"insert mode", "abc\x1b[1;1H\x1b[4hX\x1b[4l", "Xabc\n\n\n", "Xbc\n\n\n"},
	} {
		for engine, want := range map[string]string{"tuios-vt": c.tuios, "x-vt": c.xvt} {
			e := newTestEmulator(t, engine, 10, 4)
			_, _ = e.Write([]byte(c.in))
			if got := plainScreen(EmulatorFrame(e)); got != want {
				t.Errorf("%s on %s:\n%q\nwant\n%q", c.name, engine, got, want)
			}
		}
	}
}
