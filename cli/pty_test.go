//go:build !windows

package cli

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

var _ ochazuke.TerminalReleaser = (*PtyStream)(nil)

// waitFrame reads frames until one satisfies ok or the deadline passes.
func waitFrame(t *testing.T, s *PtyStream, ok func(ochazuke.TerminalFrame) bool) ochazuke.TerminalFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var last ochazuke.TerminalFrame
	for {
		f, err := s.Next(ctx)
		if err != nil {
			t.Fatalf("no matching frame (%v); last:\n%s", err, strings.Join(last.Lines, "\n"))
		}
		last = f
		if ok(f) {
			return f
		}
	}
}

func screen(f ochazuke.TerminalFrame) string { return ansi.Strip(strings.Join(f.Lines, "\n")) }

func startPty(t *testing.T, cols, rows int, name string, args ...string) *PtyStream {
	t.Helper()
	return startPtyEngine(t, "", cols, rows, name, args...)
}

func startPtyEngine(t *testing.T, engine string, cols, rows int, name string, args ...string) *PtyStream {
	t.Helper()
	s, err := StartPtyWith(exec.Command(name, args...), cols, rows, PtyOptions{Engine: engine})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Release() })
	return s
}

func TestPtyStreamPrintsAndExits(t *testing.T) {
	s := startPty(t, 40, 5, "printf", `one\ntwo\033[1mbold\033[0m`)
	f := waitFrame(t, s, func(f ochazuke.TerminalFrame) bool { return strings.Contains(screen(f), "twobold") })
	if !strings.HasPrefix(f.Lines[0], "one") || len(f.Lines) != 5 {
		t.Fatalf("frame lines = %q", f.Lines)
	}
	if f.Cursor != (ochazuke.Pos{X: 7, Y: 1}) {
		t.Fatalf("cursor = %+v, want after twobold", f.Cursor)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("printf never exited")
	}
	if got := s.Status(); got != "exited 0" {
		t.Fatalf("status = %q", got)
	}
}

func TestPtyStreamForwardsKeysAndPaste(t *testing.T) {
	forEachEngine(t, testPtyStreamForwardsKeysAndPaste)
}

func testPtyStreamForwardsKeysAndPaste(t *testing.T, engine string) {
	s := startPtyEngine(t, engine, 40, 6, "cat")
	if err := s.Send("h", "i", "enter"); err != nil {
		t.Fatal(err)
	}
	// The pty echoes the line and cat prints it back.
	waitFrame(t, s, func(f ochazuke.TerminalFrame) bool { return strings.Count(screen(f), "hi") == 2 })
	if err := s.Paste("pasted"); err != nil {
		t.Fatal(err)
	}
	if err := s.Send("enter"); err != nil {
		t.Fatal(err)
	}
	waitFrame(t, s, func(f ochazuke.TerminalFrame) bool { return strings.Count(screen(f), "pasted") == 2 })
	if err := s.Send("ctrl+d"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("ctrl+d did not end cat")
	}
	if err := s.Send("nosuchkey"); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("Send(nosuchkey) = %v", err)
	}
}

func TestPtyStreamResizeReachesTheProgram(t *testing.T) {
	s := startPty(t, 40, 6, "sh", "-c", "read x; stty size")
	if err := s.Resize(33, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.Send("enter"); err != nil {
		t.Fatal(err)
	}
	f := waitFrame(t, s, func(f ochazuke.TerminalFrame) bool { return strings.Contains(screen(f), "7 33") })
	if len(f.Lines) != 7 {
		t.Fatalf("frame has %d lines, want the 7 rows it was resized to", len(f.Lines))
	}
}

func TestPtyStreamReleaseKillsTheGroupOnce(t *testing.T) {
	// The shell starts a child in its group; both must go.
	s := startPty(t, 40, 5, "sh", "-c", "sleep 60 & echo started; wait")
	waitFrame(t, s, func(f ochazuke.TerminalFrame) bool { return strings.Contains(screen(f), "started") })
	pgid := s.Pid()
	if err := s.Release(); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(); err != nil {
		t.Fatalf("second Release = %v, want the first's nil", err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process outlived Release")
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(-pgid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatal("process group outlived Release")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := s.Next(context.Background()); err != ErrReleased {
		t.Fatalf("Next after Release = %v", err)
	}
	if err := s.Send("a"); err != ErrReleased {
		t.Fatalf("Send after Release = %v", err)
	}
}

func TestPtyStreamReportsCursorShapeAndVisibility(t *testing.T) {
	forEachEngine(t, testPtyStreamReportsCursorShapeAndVisibility)
}

func testPtyStreamReportsCursorShapeAndVisibility(t *testing.T, engine string) {
	s := startPtyEngine(t, engine, 20, 3, "printf", `\033[6 q\033[?25lx`)
	f := waitFrame(t, s, func(f ochazuke.TerminalFrame) bool { return f.Hidden && strings.Contains(screen(f), "x") })
	if f.Shape != "bar" {
		t.Fatalf("shape = %q, want bar", f.Shape)
	}
}

// The fixture through a real pty: the stream's last frame is the golden
// screen whichever engine it opened with.
func TestPtyStreamDrawsTheFixtureWithEachEngine(t *testing.T) {
	_, golden := vimLike(t)
	forEachEngine(t, func(t *testing.T, engine string) {
		s := startPtyEngine(t, engine, 20, 6, "sh", "-c", "cat testdata/vimlike.vt; sleep 30")
		f := waitFrame(t, s, func(f ochazuke.TerminalFrame) bool { return plainScreen(f) == golden })
		if f.Cursor != (ochazuke.Pos{X: 4, Y: 2}) || f.Shape != "bar" {
			t.Fatalf("cursor %+v shape %q", f.Cursor, f.Shape)
		}
	})
}

func TestPtyStreamRefusesAnEngineItLacks(t *testing.T) {
	if _, err := StartPtyWith(exec.Command("true"), 10, 2, PtyOptions{Engine: "nope"}); err == nil {
		t.Fatal("an unknown engine started")
	}
}

func TestKeyEvent(t *testing.T) {
	for name, want := range map[string]uv.KeyPressEvent{
		"a":         {Code: 'a', Text: "a"},
		"A":         {Code: 'A', Text: "A"},
		"+":         {Code: '+', Text: "+"},
		"ctrl+a":    {Code: 'a', Mod: uv.ModCtrl},
		"ctrl+A":    {Code: 'a', Mod: uv.ModCtrl},
		"alt+b":     {Code: 'b', Mod: uv.ModAlt},
		"shift+a":   {Code: 'A', Text: "A"},
		"shift+tab": {Code: uv.KeyTab, Mod: uv.ModShift},
		"enter":     {Code: uv.KeyEnter},
		"ctrl+up":   {Code: uv.KeyUp, Mod: uv.ModCtrl},
		"é":         {Code: 'é', Text: "é"},
	} {
		got, err := keyEvent(name)
		if err != nil || got != want {
			t.Errorf("keyEvent(%q) = %+v, %v; want %+v", name, got, err, want)
		}
	}
	for _, bad := range []string{"", "ctrl+", "bogus", "ctrl+bogus"} {
		if _, err := keyEvent(bad); err == nil {
			t.Errorf("keyEvent(%q) parsed", bad)
		}
	}
}
