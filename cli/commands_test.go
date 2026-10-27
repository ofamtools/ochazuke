//go:build !windows

package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
	"github.com/unixsuperhero/oden"
)

// fakeCommands is a CommandSource with an inline-looking command whose
// help is known, an external whose help must be fetched, and processes
// that are real (printf, cat) so runs go through a pty.
type fakeCommands struct {
	argv map[string][]string
	asks int
}

func (f *fakeCommands) Commands() []CommandEntry {
	return []CommandEntry{
		{Name: "greet", Kind: "inline", Summary: "Say hello", Source: "main.go:10", Help: "usage: tool greet [NAME]\n\nSay hello"},
		{Name: "echo", Kind: "external", Summary: "Echo keys", Source: "/bin/tool-echo", Shadowed: []string{"external /usr/bin/tool-echo"}},
	}
}

func (f *fakeCommands) Help(ctx context.Context, name string) (string, error) {
	f.asks++
	if name == "echo" {
		return "usage: tool-echo\n\n\x1b[1mcat\x1b[0m under a pty", nil
	}
	return "", errors.New("no help")
}

func (f *fakeCommands) Command(name string, args []string) (*exec.Cmd, error) {
	argv, ok := f.argv[name]
	if !ok {
		return nil, errors.New("unknown")
	}
	return exec.Command(argv[0], append(slices.Clone(argv[1:]), args...)...), nil
}

// driver pumps a model headless: commands run on goroutines and their
// messages are delivered, in arrival order, until a condition holds.
type driver struct {
	t    *testing.T
	m    *ochazuke.Model
	msgs chan tea.Msg
}

func newDriver(t *testing.T, m *ochazuke.Model) *driver {
	d := &driver{t: t, m: m, msgs: make(chan tea.Msg, 1024)}
	m.Width, m.Height = 110, 20
	m.Appearance.Theme = "mono"
	d.run(m.Runtime.PendingCommands())
	return d
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd != nil {
		go func() { d.push(cmd()) }()
	}
}

func (d *driver) push(msg tea.Msg) {
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			d.run(c)
		}
		return
	}
	if msg != nil {
		d.msgs <- msg
	}
}

func (d *driver) update(msg tea.Msg) {
	_, cmd := d.m.Update(msg)
	d.run(cmd)
}

func (d *driver) key(names ...string) {
	for _, name := range names {
		d.update(keytest.Msg(name, false, false))
	}
}

// exec runs a request as the palette or the CLI would, then schedules the
// work it queued, as the model does after every message.
func (d *driver) exec(q ochazuke.Request) (any, error) {
	v, err := d.m.Runtime.Execute(q)
	d.run(d.m.Runtime.PendingCommands())
	return v, err
}

func (d *driver) frame() string { return ansi.Strip(d.m.View().Content) }

// until delivers messages, rendering between them, until ok holds.
func (d *driver) until(what string, ok func(frame string) bool) string {
	d.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		frame := d.frame()
		if ok(frame) {
			return frame
		}
		select {
		case msg := <-d.msgs:
			d.update(msg)
		case <-deadline:
			d.t.Fatalf("%s: never happened; frame:\n%s", what, frame)
		}
	}
}

func contains(s ...string) func(string) bool {
	return func(frame string) bool {
		for _, want := range s {
			if !strings.Contains(frame, want) {
				return false
			}
		}
		return true
	}
}

func commandsModel(t *testing.T, src CommandSource) (*ochazuke.Model, *Commands) {
	t.Helper()
	m := ochazuke.NewModel(ochazuke.NewRuntime())
	c, err := AddCommands(m, src, CommandsConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Runtime.OpenPage(c.Page()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, name := range c.Names() {
			_ = c.endRun(name)
		}
	})
	return m, c
}

func TestCommandsPageListsAndShowsHelp(t *testing.T) {
	src := &fakeCommands{}
	m, _ := commandsModel(t, src)
	if _, err := m.Runtime.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	d := newDriver(t, m)
	frame := d.until("the table and the inline help", contains("Name", "Kind", "greet", "inline", "echo", "external", "greet · help", "usage: tool greet [NAME]"))
	if m.Runtime.Focus != rowID("greet") {
		t.Fatalf("focus = %q, want the first row", m.Runtime.Focus)
	}
	if !strings.Contains(frame, "enter command.run") || !strings.Contains(frame, "i command.help") || strings.Contains(frame, "x command.stop") {
		t.Fatalf("footer lacks the row keys:\n%s", frame)
	}
	// The external's help is fetched off the loop, once, escapes stripped.
	d.key("j")
	d.until("the external's placeholder", contains("echo · help", "shadows: external /usr/bin/tool-echo", "i shows its help"))
	d.key("i")
	frame = d.until("the external's help", contains("usage: tool-echo", "cat under a pty"))
	if strings.Contains(frame, "\x1b") || src.asks != 1 {
		t.Fatalf("asks = %d, frame:\n%q", src.asks, frame)
	}
	d.key("i")
	if src.asks != 1 {
		t.Fatalf("help fetched again: asks = %d", src.asks)
	}
}

func TestCommandsRunAttachesDetachesAndReruns(t *testing.T) {
	src := &fakeCommands{argv: map[string][]string{"echo": {"cat"}, "greet": {"printf", "hello %s\\n"}}}
	m, c := commandsModel(t, src)
	r := m.Runtime
	d := newDriver(t, m)
	d.key("j", "enter")
	term := c.Terminal("echo")
	if term == nil || r.Focus != term.ID() || r.Mode.Name != "terminal" {
		t.Fatalf("after enter: focus %q mode %q", r.Focus, r.Mode.Name)
	}
	if r.Instances[term.ID()].Parent != rowID("echo") {
		t.Fatalf("terminal parent = %q, want the row", r.Instances[term.ID()].Parent)
	}
	d.until("the attached pane", contains("echo · running · attached", "ctrl+a leader"))
	// Keys go to the pane: cat echoes the line back through the pty.
	d.key("h", "i", "enter")
	d.until("cat's echo", func(f string) bool { return strings.Count(f, "hi") >= 2 })
	if d.m.View().Cursor == nil {
		t.Fatal("the attached pane shows no cursor")
	}
	d.key("ctrl+a", "d")
	if r.Focus != rowID("echo") || r.Mode.Name != "normal" {
		t.Fatalf("after detach: focus %q mode %q", r.Focus, r.Mode.Name)
	}
	d.until("the detached pane", contains("echo · running · enter attaches"))
	// enter on a live run attaches again rather than starting another.
	d.key("enter")
	if c.Terminal("echo") != term || r.Focus != term.ID() {
		t.Fatal("enter on a live run did not reattach")
	}
	d.key("ctrl+d")
	d.until("cat exits", contains("echo · exited 0 · attached"))
	d.key("ctrl+a", "d")
	// After the process exits, enter runs it again in a new terminal.
	d.key("enter")
	if again := c.Terminal("echo"); again == nil || again == term || r.Instances[term.ID()] != nil {
		t.Fatal("enter after exit did not replace the run")
	}
	d.key("ctrl+a", "d", "k")
	// Arguments come from the palette or the CLI: command run greet world.
	if _, err := d.exec(ochazuke.Request{Noun: "app.command", Verb: "run", Args: ochazuke.Arguments{"name": "greet", "args": "world"}}); err != nil {
		t.Fatal(err)
	}
	d.until("greet's output", contains("hello world", "greet · exited 0"))
}

func TestCommandsStopReleasesTheRun(t *testing.T) {
	src := &fakeCommands{argv: map[string][]string{"greet": {"sleep", "60"}}}
	m, c := commandsModel(t, src)
	r := m.Runtime
	d := newDriver(t, m)
	d.key("enter")
	term := c.Terminal("greet")
	stream := c.runs["greet"].stream.(*PtyStream)
	d.key("ctrl+a", "d")
	d.until("x in the footer once there is a run", contains("x command.stop"))
	d.key("x")
	d.until("the confirm", contains("Stop greet?"))
	d.key("y")
	if r.Instances[term.ID()] != nil || c.Terminal("greet") != nil {
		t.Fatal("stop left the terminal")
	}
	select {
	case <-stream.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("stop left the process running")
	}
	frame := d.until("the help again", contains("greet · help"))
	if strings.Contains(frame, "x command.stop") {
		t.Fatalf("x still offered without a run:\n%s", frame)
	}
	if _, err := d.exec(ochazuke.Request{Noun: "app.command", Verb: "stop", Args: ochazuke.Arguments{"name": "greet"}, Confirmed: true}); err == nil {
		t.Fatal("stop without a run succeeded")
	}
	got, err := d.exec(ochazuke.Request{Noun: "app.command", Verb: "which", Args: ochazuke.Arguments{"name": "echo"}})
	if err != nil || got.(map[string]any)["source"] != "/bin/tool-echo" {
		t.Fatalf("which = %v, %v", got, err)
	}
}

// A session on the Commands page leaves only the runs in history.jsonl:
// a command's arguments (they can carry a secret) are dropped, as the
// history file drops text-like arguments, and nothing typed into the pty
// (the terminal noun's entries, the key that started the run aside) is
// written.
func TestCommandsHistoryFileKeepsNoArgsOrPtyKeys(t *testing.T) {
	src := &fakeCommands{argv: map[string][]string{"echo": {"cat"}, "greet": {"printf", "hello %s\\n"}}}
	m, _ := commandsModel(t, src)
	r := m.Runtime
	state := t.TempDir()
	r.Config().Dir, r.Config().StateDir = t.TempDir(), state
	m.Init()
	t.Cleanup(func() { _ = r.Config().Flush() })
	d := newDriver(t, m)
	d.key("j", "enter")
	d.until("the attached pane", contains("echo · running · attached"))
	d.key("p", "w", "9", "z", "enter")
	d.until("cat's echo", func(f string) bool { return strings.Count(f, "pw9z") >= 2 })
	d.key("ctrl+a", "d", "k")
	restore := r.Sourced("palette")
	_, err := d.exec(ochazuke.Request{Noun: "app.command", Verb: "run", Args: ochazuke.Arguments{"name": "greet", "args": "--token=s3cret"}})
	restore()
	if err != nil {
		t.Fatal(err)
	}
	d.until("greet's output", contains("hello --token=s3cret"))
	if err := r.Config().Flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(state, ochazuke.HistoryFile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"name":"greet"`) || !strings.Contains(text, `"verb":"run"`) {
		t.Fatalf("the runs are missing:\n%s", text)
	}
	for _, leak := range []string{"s3cret", "pw9z", `"noun":"terminal"`, `"key:p"`, `"key:9"`} {
		if strings.Contains(text, leak) {
			t.Fatalf("the file holds %q:\n%s", leak, text)
		}
	}
}

// odenApp is an oden tool with an inline command, a group and an external
// tool-ext on a PATH of its own.
func odenApp(t *testing.T) *oden.App {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --help ]; then echo 'usage: tool-ext [WORD]'; exit 0; fi\necho \"ext $ODEN_PARENT $ODEN_ROOT $*\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tool-ext"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	app := oden.New("tool", nil, oden.WithPath(dir))
	app.Add(
		oden.Command{Name: "greet", Aliases: []string{"g"}, Summary: "Say hello", Usage: "[NAME]", Parameters: []oden.Parameter{{Name: "loud", Type: "boolean"}}, Run: func(context.Context, []string) error { return nil }},
		oden.Command{Name: "repo", Summary: "Repositories", Children: []oden.Command{{Name: "list", Summary: "List them"}}},
	)
	return app
}

func TestOdenCommandsListsRunsAndHelps(t *testing.T) {
	app := odenApp(t)
	src := OdenCommands(app, "/bin/echo", "self")
	entries := src.Commands()
	byName := map[string]CommandEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if ext := byName["ext"]; ext.Kind != "external" || !strings.HasSuffix(ext.Source, "/tool-ext") || ext.Summary != "tool-ext on PATH" || ext.Help != "" {
		t.Fatalf("ext = %+v", ext)
	}
	greet := byName["greet"]
	if greet.Kind != "inline" || !slices.Equal(greet.Aliases, []string{"g"}) || !strings.Contains(greet.Help, "usage: tool greet [NAME]") || !strings.Contains(greet.Help, "--loud <boolean> (optional)") {
		t.Fatalf("greet = %+v", greet)
	}
	if !strings.Contains(byName["repo"].Help, "list         List them") {
		t.Fatalf("repo help = %q", byName["repo"].Help)
	}
	help, err := src.Help(context.Background(), "ext")
	if err != nil || help != "usage: tool-ext [WORD]\n" {
		t.Fatalf("ext help = %q, %v", help, err)
	}
	cmd, err := src.Command("greet", []string{"a"})
	if err != nil || !slices.Equal(cmd.Args, []string{"/bin/echo", "self", "greet", "a"}) {
		t.Fatalf("greet argv = %v, %v", cmd.Args, err)
	}
	if _, err := src.Command("nope", nil); err == nil {
		t.Fatal("unknown command ran")
	}
	// The external runs under a pty with oden's environment.
	m, c := commandsModel(t, src)
	d := newDriver(t, m)
	if err := c.List().Select(rowID("ext")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.exec(ochazuke.Request{Noun: "app.command", Verb: "run", Args: ochazuke.Arguments{"args": "x y"}}); err != nil {
		t.Fatal(err)
	}
	d.until("the external's output", contains("ext tool tool x y", "ext · exited 0"))
}

func TestCommandsVerbsAreBuiltin(t *testing.T) {
	m, _ := commandsModel(t, &fakeCommands{})
	for _, verb := range []string{"list", "run", "help", "which", "stop"} {
		if !m.Runtime.Operations[CommandNoun+"."+verb].Builtin {
			t.Errorf("%s.%s not builtin", CommandNoun, verb)
		}
	}
}
