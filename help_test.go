package ochazuke

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/keytest"
)

// helpFixture is a page holding a list of widget rows; widget has open on
// enter and edit on i.
func helpFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("widget").
		Verb("open", noop, "Open the widget").
		Verb("edit", noop, "Edit the widget").
		Mode("normal").
		Bind("enter", Do("widget.open")).
		Bind("i", Do("widget.edit"))
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return Text(r.List("widgets").View(w, h)) }); err != nil {
		t.Fatal(err)
	}
	list, err := r.AddList("widgets", []ListItem{{ID: "w1", Label: "one", Noun: "widget"}, {ID: "w2", Label: "two", Noun: "widget"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 80, 24
	return r, m
}

// ? on a focused row opens a read-only list of the focus's bindings, the
// row's own verbs first and the framework (app) rows last; esc closes it
// and gives the focus back to the row.
func TestHelpListsTheFocusedRowsVerbsFirst(t *testing.T) {
	r, m := helpFixture(t)
	if r.Focus != "w1" {
		t.Fatalf("focus %q", r.Focus)
	}
	keytest.Press(m, "?")
	if r.TopModal() != helpID {
		t.Fatalf("top modal %q, status %q", r.TopModal(), m.Status)
	}
	items := r.List(helpListID).Items()
	if len(items) < 3 {
		t.Fatalf("items: %+v", items)
	}
	fields := func(i int) []string { return strings.Fields(items[i].Label) }
	if f := fields(0); f[0] != "enter" || f[1] != "widget.open" || strings.Join(f[2:], " ") != "Open the widget" {
		t.Fatalf("first row %q", items[0].Label)
	}
	if f := fields(1); f[0] != "i" || f[1] != "widget.edit" {
		t.Fatalf("second row %q", items[1].Label)
	}
	// The last row before the movement section is app's.
	var last HelpRow
	for _, it := range items {
		// A prefix's generated cancel (g esc) is shared by every level
		// binding the prefix and names the innermost one's noun.
		if row := it.Data.(HelpRow); row.Section != MovementSection && !row.cancel {
			last = row
		}
	}
	if last.Noun != "app" {
		t.Fatalf("last row is not an app row: %+v", last)
	}
	for _, it := range items {
		if row := it.Data.(HelpRow); row.Key == "ctrl+c" && row.Noun != "app" {
			t.Fatalf("ctrl+c grouped under %q", row.Noun)
		}
	}
	raw := m.View().Content
	frame := ansi.Strip(raw)
	if lit := highlightedLines(raw); !strings.Contains(frame, "Keys") || !slices.ContainsFunc(lit, func(l string) bool {
		return strings.HasPrefix(strings.Join(strings.Fields(l), " "), "enter widget.open Open the")
	}) {
		t.Fatalf("frame (highlighted %q):\n%s", lit, frame)
	}
	// Read-only: enter does not run widget.open beneath the modal.
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	if r.TopModal() != helpID {
		t.Fatalf("enter left the help: %q", r.TopModal())
	}
	keytest.Press(m, "esc")
	if r.TopModal() != "" || r.Focus != "w1" || r.Instances[helpListID] != nil {
		t.Fatalf("esc: modal %q focus %q", r.TopModal(), r.Focus)
	}
}

func TestHelpKeepsOwnerGroupVisibleWhileScrolling(t *testing.T) {
	r, m := helpFixture(t)
	widget := r.Define("widget").Mode("normal")
	for _, key := range []string{"a", "b", "c", "d", "e", "f"} {
		widget.Bind(key, Do("widget.open"))
	}
	widget.Bind("c", Do("ochazuke.mode.set", Arguments{"mode": "normal"}))
	keytest.Press(m, "?")
	for range 4 {
		keytest.Press(m, "j")
		bindingHelpView(r, nil, 80, 5)
	}
	raw := bindingHelpView(r, nil, 80, 5).Text
	frame := ansi.Strip(raw)
	lines := strings.Split(frame, "\n")
	if strings.Join(strings.Fields(lines[0]), " ") != "key operation arguments description" || strings.TrimSpace(lines[1]) != "widget" {
		t.Fatalf("scrolled help lost its header or binding owner:\n%s", frame)
	}
	if lit := highlightedLines(raw); len(lit) == 0 || !strings.HasPrefix(strings.Join(strings.Fields(lit[0]), " "), "c mode.set") {
		t.Fatalf("selected binding is not highlighted beneath its owner (%q):\n%s", lit, frame)
	}
	if len(lines) > 5 {
		t.Fatalf("help exceeds the viewport:\n%s", frame)
	}
}

// reverseSGR matches an SGR sequence that turns reverse video on.
var reverseSGR = regexp.MustCompile(`\x1b\[(?:[0-9;]*;)?7m`)

// highlightedLines is the text of every reverse-video line, stripped (and
// cut from a modal's border).
func highlightedLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if reverseSGR.MatchString(line) {
			out = append(out, strings.TrimSpace(strings.Trim(ansi.Strip(line), "│ ")))
		}
	}
	return out
}

// ? is a normal-mode row: in insert mode it is typed, and in a passthrough
// mode (an attached terminal) it reaches the pane.
func TestHelpDoesNotShadowInsertOrTerminal(t *testing.T) {
	r := NewRuntime()
	in := NewInput("field", "", false)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.SetFocus("field"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "i")
	keytest.Press(m, "?")
	if r.TopModal() != "" || string(in.Text) != "?" {
		t.Fatalf("insert ?: modal %q text %q", r.TopModal(), string(in.Text))
	}

	r, _, stream := terminalFixture(t)
	m = NewModel(r)
	keytest.Press(m, "?")
	if r.TopModal() != "" || len(stream.sent) == 0 || stream.sent[len(stream.sent)-1][0] != "?" {
		t.Fatalf("terminal ?: modal %q sent %v", r.TopModal(), stream.sent)
	}
}

var updateHelp = flag.Bool("update-help", false, "rewrite testdata/help-80.golden")

// The help modal at 80 columns for a task-like row: the key column is as
// wide as the longest literal key, pattern rows read short (1-9), the
// operation column is fixed, and every description reads in the rest.
func TestHelpGoldenAt80Columns(t *testing.T) {
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("task").
		Verb("open", noop, "Open the task's page").
		Verb("done", noop, "Mark the task done and move it to the archive").
		Verb("edit", noop, "Edit the task's title in a prompt").
		Verb("note", noop, "Add a note to the task").
		Verb("delete", noop, "Delete the task after a confirm").
		Mode("normal").
		Bind("enter", Do("task.open")).
		Bind("x", Do("task.done")).
		Bind("e", Do("task.edit")).
		Bind("n", Do("task.note")).
		Bind("d d", Do("task.delete"))
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return Text(r.List("tasks").View(w, h)) }); err != nil {
		t.Fatal(err)
	}
	list, err := r.AddList("tasks", []ListItem{{ID: "t1", Label: "write the brief", Noun: "task"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 80, 72
	keytest.Press(m, "?")
	if r.TopModal() != helpID {
		t.Fatalf("top modal %q, status %q", r.TopModal(), m.Status)
	}
	var lines []string
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	got := []byte(strings.Join(lines, "\n") + "\n")
	golden := filepath.Join("testdata", "help-80.golden")
	if *updateHelp {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("help differs from %s (go test -run TestHelpGoldenAt80Columns -update-help):\n%s", golden, got)
	}
}

// Pattern rows read short in the key column; literal keys stay as they are.
func TestHelpKeyShortensPatterns(t *testing.T) {
	for key, want := range map[string]string{"<digit>": "0-9", "<digit-0>": "1-9", "<alpha>": "a-z", "<alpha-q>": "a-z -q", "<printable>": "char", "<any>": "any", "<one-of:h l>": "h/l", "<": "<", "shift+tab": "shift+tab"} {
		got, literal := helpKey(key)
		if got != want || literal != (got == key) {
			t.Fatalf("helpKey(%q) = %q, %v; want %q", key, got, literal, want)
		}
	}
}

func TestHelpArgumentsPreviewAndTruncate(t *testing.T) {
	r, m := helpFixture(t)
	var received Arguments
	r.Define("widget").
		Verb("send", func(c Ctx) Result { received = c.Args; return Done(nil) }).
		Mode("normal").
		Bind("g z", Do("widget.send", Arguments{"text": strings.Repeat("界", 40)}).FromKey("key").FromPayload("steps", SourceCount)).
		Pattern(Digit, Do("widget.send").FromPayload("digit", SourceDigit))
	op := r.Operations["widget.send"]
	op.Parameters = map[string]Parameter{"text": {Type: "string"}, "key": {Type: "string"}, "steps": {Type: "integer"}, "digit": {Type: "integer"}}
	r.Operations["widget.send"] = op
	r.Mode.Payload.Count = 7
	if _, err := r.Execute(Do("ochazuke.bindings.help").Request); err != nil {
		t.Fatal(err)
	}
	var preview Arguments
	for _, item := range r.List(helpListID).Items() {
		row := item.Data.(HelpRow)
		if row.Key == "g z" {
			preview = row.Args
			if preview["key"] != "z" || preview["steps"] != 7 || preview["text"] != strings.Repeat("界", 40) {
				t.Fatalf("preview: %#v", preview)
			}
		}
		if row.Key == "<digit>" && row.Args["digit"] != "$digit" {
			t.Fatalf("unknown digit: %#v", row.Args)
		}
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, `{"key":"z"`) || !strings.Contains(frame, "…") || strings.Contains(frame, strings.Repeat("界", 40)) {
		t.Fatalf("arguments not shown and truncated:\n%s", frame)
	}
	for _, width := range []int{20, 40, 80} {
		for _, line := range strings.Split(ansi.Strip(bindingHelpView(r, nil, width, 24).Text), "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("line exceeds %d cells: %q", width, line)
			}
		}
	}
	keytest.Press(m, "esc")
	r.Mode.Payload.Count = 7
	keytest.Press(m, "g")
	keytest.Press(m, "z")
	if received["key"] != "z" || received["steps"] != 7 || received["text"] != preview["text"] {
		t.Fatalf("dispatched args differ from preview: %#v", received)
	}
}

// bindingHelpView draws the open help's body as the Model does.
func bindingHelpView(r *Runtime, _ *Instance, width, height int) Block {
	if h := r.helpOpen(); h != nil {
		return h.ModalBlock(Styles{}, width, height)
	}
	return Text("")
}

func TestSplitKeys(t *testing.T) {
	for keys, want := range map[string][2]string{"z l": {"z", "l"}, "ctrl+a v": {"ctrl+a", "v"}, "g c x": {"g c", "x"}, "enter": {"", "enter"}, "<digit>": {"", "<digit>"}, "": {"", ""}} {
		if p, f := SplitKeys(keys); p != want[0] || f != want[1] {
			t.Fatalf("SplitKeys(%q) = %q, %q; want %q", keys, p, f, want)
		}
	}
}

// Inside a section the single keys keep their order and come first; then
// every prefix has a row (made when nothing is bound on it alone, R6.10)
// with its sequences under it, sorted (characters case folded, named keys
// last), a longer prefix's row under the shorter one it extends with its
// own sequences under it; the sections keep their order. A row keeps its
// request beside its hint.
func TestHelpOrderGroupsPrefixes(t *testing.T) {
	row := func(section, key string) HelpRow {
		p, f := SplitKeys(key)
		return HelpRow{Key: key, Prefix: p, Final: f, Section: section}
	}
	rows := []HelpRow{row("a", "x"), row("a", "g c y"), row("a", "z l"), row("a", "enter"), row("a", "z esc"), row("a", "g esc"), row("a", "z h"), row("a", "z H"), row("a", "g c x"), row("b", "g g"), row("b", "j")}
	var got []string
	for _, r := range helpOrder(helpGroups(rows)) {
		got = append(got, r.Section+":"+r.Key+"<"+r.Under)
	}
	want := []string{"a:x<", "a:enter<", "a:g<", "a:g c<g", "a:g c x<g c", "a:g c y<g c", "a:g esc<g", "a:z<", "a:z h<z", "a:z H<z", "a:z l<z", "a:z esc<z", "b:j<", "b:g<", "b:g g<g"}
	if !slices.Equal(got, want) {
		t.Fatalf("order %q, want %q", got, want)
	}

	r, m := helpFixture(t)
	r.Define("widget").Verb("zoom", func(Ctx) Result { return Done(nil) }, "Zoom").Mode("normal").Bind("z z", Do("widget.zoom").Hinted("zoom in"))
	_ = m
	for _, h := range BindingHelp(r, r.Focus) {
		if h.Key == "z z" && (h.Operation != "zoom in" || h.Hint != "zoom in" || h.Request != "widget.zoom" || h.Prefix != "z" || h.Final != "z") {
			t.Fatalf("row %+v", h)
		}
	}
}
