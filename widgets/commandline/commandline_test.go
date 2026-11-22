package commandline

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func cmd(id string, kv ...any) core.Command {
	c := core.Command{ID: id}
	for i := 0; i+1 < len(kv); i += 2 {
		if c.Args == nil {
			c.Args = map[string]any{}
		}
		c.Args[kv[i].(string)] = kv[i+1]
	}
	return c
}

func typ(m *Model, s string) {
	for _, r := range s {
		m.HandleCommand(cmd(CmdInsert, "text", string(r)))
	}
}

func TestEditingAndCursor(t *testing.T) {
	m := New(PrefixCommand)
	typ(m, "foo bar baz")
	m.HandleCommand(cmd("motion.word.prev"))
	m.HandleCommand(cmd("motion.word.prev"))
	if m.CursorX != 4 {
		t.Fatalf("cursor %d", m.CursorX)
	}
	m.HandleCommand(cmd("motion.word.next"))
	if m.CursorX != 8 {
		t.Fatalf("cursor %d", m.CursorX)
	}
	m.HandleCommand(cmd("motion.start"))
	m.HandleCommand(cmd("motion.right"))
	m.HandleCommand(cmd(CmdInsert, "text", "XY"))
	if m.Value() != "fXYoo bar baz" {
		t.Fatal(m.Value())
	}
	m.HandleCommand(cmd(CmdBackspace))
	m.HandleCommand(cmd("motion.end"))
	m.HandleCommand(cmd(CmdDeleteWord))
	if m.Value() != "fXoo bar " {
		t.Fatalf("%q", m.Value())
	}
	m.HandleCommand(cmd("motion.left"))
	m.HandleCommand(cmd(CmdClear))
	if m.Value() != " " || m.CursorX != 0 {
		t.Fatalf("%q %d", m.Value(), m.CursorX)
	}
}

func TestRegisterPaste(t *testing.T) {
	m := New(PrefixCommand)
	m.RegisterLookup = func(r rune) (string, bool) {
		v, ok := map[rune]string{'%': "a.go", '"': "yanked", 'a': "reg a"}[r]
		return v, ok
	}
	typ(m, "e ")
	for _, c := range []struct{ reg, want string }{{"%", "e a.go"}, {`"`, "e a.goyanked"}, {"a", "e a.goyankedreg a"}, {"z", "e a.goyankedreg a"}} {
		m.HandleCommand(cmd(CmdRegister))
		if !m.PendingRegister {
			t.Fatal("not pending")
		}
		if v := m.View("c"); v.Title != `:"` {
			t.Fatalf("title %q", v.Title)
		}
		m.HandleCommand(cmd(CmdInsert, "text", c.reg))
		if m.Value() != c.want || m.PendingRegister {
			t.Fatalf("%q pending=%v", m.Value(), m.PendingRegister)
		}
	}
	m.Home()
	m.StartRegister()
	m.InsertRune('a')
	if !strings.HasPrefix(m.Value(), "reg ae") {
		t.Fatal(m.Value())
	}
}

func TestHistory(t *testing.T) {
	m := New(PrefixCommand)
	for _, s := range []string{"one", "one", "two"} {
		typ(m, s)
		m.HandleCommand(cmd(CmdSubmit))
		m.Open(PrefixCommand)
	}
	typ(m, "/x")
	m.Open(PrefixSearch)
	typ(m, "needle")
	m.HandleCommand(cmd(CmdSubmit))
	m.Open(PrefixSearch)
	if !slices.Equal(m.History[PrefixCommand], []string{"one", "two"}) {
		t.Fatal(m.History)
	}
	m.Open(PrefixCommand)
	typ(m, "dra")
	m.HandleCommand(cmd(CmdHistoryPrev))
	m.HandleCommand(cmd(CmdHistoryPrev))
	m.HandleCommand(cmd(CmdHistoryPrev))
	if m.Value() != "one" {
		t.Fatal(m.Value())
	}
	m.HandleCommand(cmd(CmdHistoryNext))
	if m.Value() != "two" {
		t.Fatal(m.Value())
	}
	m.HandleCommand(cmd(CmdHistoryNext))
	if m.Value() != "dra" {
		t.Fatal(m.Value())
	}
	m.Open(PrefixSearch)
	m.HistoryPrev()
	if m.Value() != "needle" {
		t.Fatal(m.Value())
	}
}

func TestCompletion(t *testing.T) {
	m := New(PrefixCommand)
	m.Completer = func(p Prefix, s string) []string {
		var out []string
		for _, c := range []string{"edit", "echo", "exit", "quit"} {
			if strings.HasPrefix(c, s) {
				out = append(out, c)
			}
		}
		return out
	}
	typ(m, "e")
	if m.Hint() != "edit" {
		t.Fatal(m.Hint())
	}
	var got []string
	for range 4 {
		m.CompleteNext()
		got = append(got, m.Value())
	}
	if !slices.Equal(got, []string{"edit", "echo", "exit", "edit"}) {
		t.Fatal(got)
	}
	m.CompletePrev()
	if m.Value() != "exit" {
		t.Fatal(m.Value())
	}
	typ(m, "x")
	if m.comp != nil {
		t.Fatal("completion not reset")
	}
}

func TestExpandVars(t *testing.T) {
	v := map[string]string{"FILE": "a b.go", "DIR": "/d", "REF": "task:1", "ID": "1", "TITLE": "it's", "VAR": "v"}
	if g := ExpandVars("$FILE ${VAR} $NOPE ${NOPE} $ $$ $DIR/x", v); g != "a b.go v $NOPE ${NOPE} $ $ /d/x" {
		t.Fatal(g)
	}
	if g := ExpandVarsShell("cat $FILE $TITLE $FOO", v); g != `cat 'a b.go' 'it'\''s' $FOO` {
		t.Fatal(g)
	}
}

func TestParseSubmission(t *testing.T) {
	v := map[string]string{"FILE": "a b", "ID": "7"}
	s := ParseSubmission(PrefixCommand, "open $ID  x", v)
	if s.Command != "open" || !slices.Equal(s.Args, []string{"7", "x"}) || s.Raw != "open $ID  x" {
		t.Fatalf("%+v", s)
	}
	if s := ParseSubmission(PrefixSearch, "foo $ID", v); s.Command != "" || s.Expanded != "foo $ID" {
		t.Fatalf("%+v", s)
	}
	if s := ParseSubmission(PrefixShell, "ls $FILE", v); s.ShellCmd != "ls 'a b'" || s.PipeCmd != "" {
		t.Fatalf("%+v", s)
	}
	if s := ParseSubmission(PrefixPipe, "sort", v); s.PipeCmd != "sort" || s.ShellCmd != "" {
		t.Fatalf("%+v", s)
	}
	if s := ParseSubmission(PrefixReadShell, "date", v); s.ShellCmd != "date" || s.Prefix != PrefixReadShell {
		t.Fatalf("%+v", s)
	}
	m := New(PrefixShell)
	m.Vars = v
	typ(m, "echo $ID")
	if s, ok := m.HandleCommand(cmd(CmdSubmit)); !ok || s.ShellCmd != "echo '7'" {
		t.Fatalf("%+v", s)
	}
	if _, ok := m.HandleCommand(cmd("nope")); ok {
		t.Fatal("handled unknown")
	}
}

func TestRender(t *testing.T) {
	m := New(PrefixReadShell)
	typ(m, "ls")
	m.Left()
	n := m.View("cl")
	if n.Kind != ui.KindInput || n.Scope != "commandline" || !n.Focusable || n.CacheKey == 0 || n.Facts["prefix"] != "r!" {
		t.Fatalf("%+v", n)
	}
	f, _ := ui.Render(n, ui.DefaultDarkTheme(), "cl", 20, 1)
	if !strings.Contains(f.ANSI, "r!") || !strings.Contains(f.ANSI, "ls") {
		t.Fatalf("%q", f.ANSI)
	}
	if f.Cursor.X != 5 {
		t.Fatalf("cursor %+v", f.Cursor)
	}
	m.Right()
	if m.View("cl").CacheKey == n.CacheKey {
		t.Fatal("cache key unchanged")
	}
}
