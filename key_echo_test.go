package ochazuke

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// keyEchoApp is a page with a ten-row list, a plain input and a secret
// one, under a Model on a fake clock with keys.echo in the footer.
func keyEchoApp(t *testing.T) (*Runtime, *Model, *time.Time) {
	t.Helper()
	r := NewRuntime()
	r.Define("file")
	if _, err := r.AddPage("work", "Work", func(r *Runtime, w, h int) Block {
		return Text(r.List("files").View(w, 10))
	}); err != nil {
		t.Fatal(err)
	}
	var items []ListItem
	for i := range 10 {
		items = append(items, ListItem{ID: fmt.Sprintf("f%d", i), Label: fmt.Sprintf("file %d", i), Noun: "file"})
	}
	if _, err := r.AddList("files", items, Parent("work")); err != nil {
		t.Fatal(err)
	}
	if err := r.AddInput(NewInput("name", "", false), Parent("work")); err != nil {
		t.Fatal(err)
	}
	pin := NewInput("pin", "", false)
	pin.Secret = true
	if err := r.AddInput(pin, Parent("work")); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return now }
	m.Init()
	if err := r.OpenPage("work"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("f0"); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(KeyEchoSetting, KeyEchoFooter); err != nil {
		t.Fatal(err)
	}
	return r, m, &now
}

// echoPress sends keys through Update, as the program does, so the echo
// prunes and schedules its timer as it would live.
func echoPress(m *Model, keys ...string) {
	for _, key := range keys {
		m.handleKey(MacroKey{Key: key})
		m.Update(nil)
	}
}

func wantEcho(t *testing.T, m *Model, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	got := m.KeyEchoLabels()
	if got == nil {
		got = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("key echo = %q, want %q", got, want)
	}
}

// Off (the default) echoes nothing and keeps no keys.
func TestKeyEchoOffShowsNothing(t *testing.T) {
	r, m, _ := keyEchoApp(t)
	if err := r.Settings().Reset(KeyEchoSetting); err != nil {
		t.Fatal(err)
	}
	if got := r.KeyEcho(); got != KeyEchoOff {
		t.Fatalf("default keys.echo = %q, want off", got)
	}
	echoPress(m, "5", "j")
	if got := m.KeyEchoLabels(); got != nil {
		t.Fatalf("echo while off = %q", got)
	}
	if err := r.Settings().Set(KeyEchoSetting, KeyEchoFooter); err != nil {
		t.Fatal(err)
	}
	wantEcho(t, m) // nothing was kept while off
}

// Counts join their motion, a sequence waits with … and reads as typed,
// and a lone key pressed again counts up.
func TestKeyEchoGroupsCountsAndSequences(t *testing.T) {
	_, m, _ := keyEchoApp(t)
	echoPress(m, "5")
	wantEcho(t, m, "5…")
	echoPress(m, "j")
	wantEcho(t, m, "5j")
	echoPress(m, "1", "2", "k")
	wantEcho(t, m, "5j", "12k")
	echoPress(m, "g")
	wantEcho(t, m, "5j", "12k", "g…")
	echoPress(m, "g")
	wantEcho(t, m, "5j", "12k", "g g")
	echoPress(m, "j", "j", "j")
	wantEcho(t, m, "5j", "12k", "g g", "j ×3")
}

// keys.echo-count keeps the newest entries.
func TestKeyEchoCountDropsTheOldest(t *testing.T) {
	r, m, _ := keyEchoApp(t)
	if err := r.Settings().Set(KeyEchoCountSetting, 2); err != nil {
		t.Fatal(err)
	}
	echoPress(m, "j", "k", "G")
	wantEcho(t, m, "k", "G")
}

// Text typed into an input collapses into one entry, as text or as a
// count (keys.echo-typing); a key that is not text ends the run.
func TestKeyEchoCollapsesTyping(t *testing.T) {
	r, m, _ := keyEchoApp(t)
	if err := r.SetFocus("name"); err != nil {
		t.Fatal(err)
	}
	echoPress(m, "i", "h", "i", "space", "y", "o")
	wantEcho(t, m, "i", `"hi yo"`)
	echoPress(m, "esc")
	wantEcho(t, m, "i", `"hi yo"`, "esc")
	echoPress(m, "c", "c")
	wantEcho(t, m, "i", `"hi yo"`, "esc", "c c")
	echoPress(m, "esc")
	if err := r.Settings().Set(KeyEchoTypingSetting, KeyEchoTypingCount); err != nil {
		t.Fatal(err)
	}
	wantEcho(t, m, "i", "typed 5", "esc", "c c", "esc")
}

// A secret input shows nothing at all: not the text, not how much.
func TestKeyEchoSecretInputShowsNothing(t *testing.T) {
	r, m, _ := keyEchoApp(t)
	echoPress(m, "j")
	if err := r.SetFocus("pin"); err != nil {
		t.Fatal(err)
	}
	echoPress(m, "i", "h", "u", "n", "t", "e", "r", "esc")
	wantEcho(t, m, "j")
	if got := r.Input("pin").Text; string(got) != "hunter" {
		t.Fatalf("the secret input got %q: the keys must still reach it", string(got))
	}
}

// A secret prop's record field is a secret input.
func TestPropFieldCarriesSecret(t *testing.T) {
	if f := PropField(PropSpec{Name: "token", Secret: true}); !f.Secret {
		t.Fatal("PropField dropped Secret")
	}
}

// A palette line shows as the command it ran, in place of the : that
// opened it and the keys that typed it; keys.echo-commands off hides it.
func TestKeyEchoPaletteCommand(t *testing.T) {
	r, m, _ := keyEchoApp(t)
	echoPress(m, "j", ":")
	for _, c := range "notice clear" {
		key := string(c)
		if c == ' ' {
			key = "space"
		}
		echoPress(m, key)
	}
	echoPress(m, "enter")
	got := m.KeyEchoLabels()
	if len(got) != 2 || got[0] != "j" || !strings.HasPrefix(got[1], ":") || !strings.Contains(got[1], "notice clear") {
		t.Fatalf("key echo = %q, want j then the :notice clear command", got)
	}
	if err := r.Settings().Set(KeyEchoCommandsSetting, false); err != nil {
		t.Fatal(err)
	}
	wantEcho(t, m, "j", ":…")
}

// An entry goes keys.echo-linger after its last key, on the Model's one
// linger timer, so a fake clock moves it; 0s keeps it.
func TestKeyEchoLingerExpires(t *testing.T) {
	r, m, now := keyEchoApp(t)
	echoPress(m, "j")
	tick, ok := m.JobLingerTick()
	if !ok || !tick.At.Equal(now.Add(3*time.Second)) {
		t.Fatalf("linger timer = %v, %v; want %v", tick.At, ok, now.Add(3*time.Second))
	}
	*now = now.Add(time.Second)
	echoPress(m, "k")
	*now = tick.At
	m.Update(tick)
	wantEcho(t, m, "k")
	tick, ok = m.JobLingerTick()
	if !ok {
		t.Fatal("no linger timer for the newer entry")
	}
	*now = tick.At
	m.Update(tick)
	wantEcho(t, m)
	if _, ok := m.JobLingerTick(); ok {
		t.Fatal("a linger timer with nothing to drop")
	}
	if err := r.Settings().Set(KeyEchoLingerSetting, "0s"); err != nil {
		t.Fatal(err)
	}
	echoPress(m, "j")
	*now = now.Add(time.Hour)
	m.Update(nil)
	wantEcho(t, m, "j")
}

// The echo keeps its cells whether it holds keys or not, so the footer
// never reflows; it drops the oldest entries with … to fit.
func TestKeyEchoReservesItsWidth(t *testing.T) {
	l := Layout{Width: 80, Height: 24, Styles: NewStyles(Appearance{Theme: "mono"})}
	for _, labels := range [][]string{nil, {"j"}, {"5j", "g g", `"a long line of typed text here"`}, {strings.Repeat("x", 60)}} {
		cell := l.echoCell(labels, 20)
		if w := ansi.StringWidth(cell); w != 20 {
			t.Errorf("echoCell(%q) is %d cells, want 20: %q", labels, w, ansi.Strip(cell))
		}
	}
	if got := ansi.Strip(l.echoCell([]string{"5j", "g g", "dd", "j ×3", "k", "G", "x"}, 14)); !strings.HasPrefix(strings.TrimLeft(got, " "), "…") || !strings.HasSuffix(got, "x") {
		t.Errorf("an overflowing echo = %q, want … then the newest", got)
	}
	_, m, _ := keyEchoApp(t)
	footerLine := func() string {
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		return lines[len(lines)-2]
	}
	empty := footerLine()
	echoPress(m, "5", "j")
	typed := footerLine()
	if !strings.HasSuffix(strings.TrimRight(typed, " "), "5j") {
		t.Fatalf("footer = %q, want the echo at its right end", typed)
	}
	if ansi.StringWidth(empty) != ansi.StringWidth(typed) || hintsPart(empty) != hintsPart(typed) {
		t.Fatalf("the footer reflowed:\n%q\n%q", empty, typed)
	}
}

// hintsPart is a footer line without its right-hand echo cell.
func hintsPart(line string) string { return strings.TrimRight(line[:len(line)-30], " ") }

// badge puts the echo on the title row, before the mode badge.
func TestKeyEchoBadgePlacement(t *testing.T) {
	r, m, _ := keyEchoApp(t)
	if err := r.Settings().Set(KeyEchoSetting, KeyEchoBadge); err != nil {
		t.Fatal(err)
	}
	echoPress(m, "g", "g")
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.Contains(lines[0], "g g NORMAL") && !strings.Contains(lines[0], "g g  NORMAL") {
		t.Fatalf("title row = %q, want the echo before the badge", lines[0])
	}
	if strings.Contains(lines[len(lines)-2], "g g") {
		t.Fatalf("footer = %q: badge placement drew it there too", lines[len(lines)-2])
	}
}

// key.echo sets or cycles keys.echo.
func TestKeyEchoVerbCycles(t *testing.T) {
	r, _, _ := keyEchoApp(t)
	for _, want := range []string{KeyEchoBadge, KeyEchoOff, KeyEchoFooter} {
		got, err := r.Execute(Request{Noun: "ochazuke.key", Verb: "echo"})
		if err != nil || got != want {
			t.Fatalf("key.echo = %v, %v; want %s", got, err, want)
		}
	}
	if got, err := r.Execute(Request{Noun: "ochazuke.key", Verb: "echo", Args: Arguments{"placement": "off"}}); err != nil || got != KeyEchoOff {
		t.Fatalf("key.echo off = %v, %v", got, err)
	}
}

// Keys forwarded to an attached pane stay out of the echo unless
// keys.echo-terminal is on; the leader and what follows it always show.
func TestKeyEchoTerminalPassthrough(t *testing.T) {
	r, _, stream := terminalFixture(t)
	r.EnsureTerminalLeader()
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	m.Init()
	if err := r.SetFocus("pane"); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(KeyEchoSetting, KeyEchoFooter); err != nil {
		t.Fatal(err)
	}
	echoPress(m, "l", "s", "enter", "ctrl+a", "ctrl+a")
	wantEcho(t, m, "ctrl+a ctrl+a")
	if len(stream.sent) == 0 {
		t.Fatal("the pane got no keys")
	}
	if err := r.Settings().Set(KeyEchoTerminalSetting, true); err != nil {
		t.Fatal(err)
	}
	echoPress(m, "l", "s", "enter")
	wantEcho(t, m, "ctrl+a ctrl+a", `"ls"`, "enter")
}
