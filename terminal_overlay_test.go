package ochazuke

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// An overlay is drawn over the frame, cell-exact around styles and wide
// runes, cut at the edge; the frame itself and later frames are untouched,
// and nil clears it.
func TestTerminalOverlayDrawsMarksOverTheFrame(t *testing.T) {
	r, term, stream := terminalFixture(t)
	frame := TerminalFrame{Lines: []string{
		"see \x1b[31mhttps://x.io\x1b[0m now",
		"界界界界",
		"tail",
	}}
	stream.frames <- frame
	deliverFrame(t, r)
	term.SetOverlay(&TerminalOverlay{Marks: []OverlayMark{
		{Row: 0, Col: 4, Text: "aa"},
		{Row: 1, Col: 1, Text: "b"},  // half covers the first 界, whose other cell blanks
		{Row: 2, Col: 9, Text: "cd"}, // cut at the edge
		{Row: 5, Col: 0, Text: "zz"}, // below the pane
	}})
	block := term.ViewBlock(10, 3)
	got := strings.Split(ansi.Strip(block.Text), "\n")
	want := []string{"see aatps:", " b界界界  ", "tail     c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overlay cells:\n%q\nwant\n%q", got, want)
	}
	for i, line := range strings.Split(block.Text, "\n") {
		if w := ansi.StringWidth(line); w != 10 {
			t.Fatalf("row %d is %d cells: %q", i, w, line)
		}
	}
	// The cells after a mark keep the pane's colour; the empty style is reverse.
	if row := strings.Split(block.Text, "\n")[0]; !strings.Contains(row, "7maa") || !strings.Contains(row[strings.Index(row, "aa"):], "\x1b[31m") {
		t.Fatalf("styles around the mark: %q", row)
	}
	if !reflect.DeepEqual(term.Frame(), frame) {
		t.Fatalf("the overlay changed the frame: %+v", term.Frame())
	}
	// A new frame keeps the overlay.
	stream.frames <- TerminalFrame{Lines: []string{"fresh line"}}
	deliverFrame(t, r)
	if got := ansi.Strip(strings.Split(term.ViewBlock(10, 3).Text, "\n")[0]); got != "fresaaline" {
		t.Fatalf("after a new frame: %q", got)
	}
	term.SetOverlay(nil)
	if got := ansi.Strip(strings.Split(term.ViewBlock(10, 3).Text, "\n")[0]); got != "fresh line" || term.Overlay() != nil {
		t.Fatalf("cleared: %q", got)
	}
}

// Dim fades every cell of the pane; a mark's own style is drawn as given.
func TestTerminalOverlayDims(t *testing.T) {
	r, term, stream := terminalFixture(t)
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	stream.frames <- TerminalFrame{Lines: []string{"plain text"}}
	deliverFrame(t, r)
	term.SetOverlay(&TerminalOverlay{Dim: true, Marks: []OverlayMark{{Col: 0, Text: "q", Style: lipgloss.NewStyle().Underline(true)}}})
	row := strings.Split(term.ViewBlock(10, 1).Text, "\n")[0]
	if !strings.HasPrefix(row, "\x1b[2m") || !strings.Contains(row, "4mq") || ansi.Strip(row) != "qlain text" {
		t.Fatalf("dimmed row: %q", row)
	}
}

func deliverFrame(t *testing.T, r *Runtime) {
	t.Helper()
	for _, msg := range runCommands(r.PendingCommands()) {
		if done, ok := msg.(Completion); ok {
			r.Deliver(done)
			return
		}
	}
	t.Fatal("no frame delivered")
}

// The hints recipe: a leader row runs an application verb that sets an
// overlay and enters an application base mode (passthrough, so every
// ctrl+letter reaches it); the mode's rows on the terminal noun answer
// letters, uppercase letters, ctrl+letters (the letter source reads the
// label letter of each) and esc, an any row swallows the rest, and nothing
// reaches the pane until the mode is left back to terminal with the focus
// unchanged.
func TestTerminalHintsModeKeepsKeysFromThePane(t *testing.T) {
	r, term, stream := terminalFixture(t)
	var picks []string
	hintsRecipe(r, &picks)
	press(t, r, "x", "ctrl+a", "f")
	if r.Mode.Name != "hints" || r.Focus != "pane" || term.Overlay() == nil || !reflect.DeepEqual(stream.sent, [][]string{{"x"}}) {
		t.Fatalf("enter: mode %q focus %q overlay %v sent %v", r.Mode.Name, r.Focus, term.Overlay(), stream.sent)
	}
	press(t, r, "a", "B", "ctrl+c", "ctrl+space", "enter", "1", "?")
	if _, err := r.Dispatch(MacroKey{Key: "[pasted]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"copy a", "type b", "open c", "miss ", "miss ", "miss ", "miss ", "miss "}
	if !reflect.DeepEqual(picks, want) || len(stream.sent) != 1 || len(stream.pasted) != 0 || r.Mode.Name != "hints" || r.Quit || r.TopModal() != "" {
		t.Fatalf("in hints: picks %q sent %v pasted %v mode %q quit %v modal %q", picks, stream.sent, stream.pasted, r.Mode.Name, r.Quit, r.TopModal())
	}
	hints := BindingHints(r, r.Focus)
	if !strings.Contains(hints, "<alpha> copy") || !strings.Contains(hints, "esc cancel") {
		t.Fatalf("footer hints: %q", hints)
	}
	press(t, r, "esc")
	if r.Mode.Name != "terminal" || r.Focus != "pane" || term.Overlay() != nil {
		t.Fatalf("leave: mode %q focus %q overlay %v", r.Mode.Name, r.Focus, term.Overlay())
	}
	press(t, r, "y")
	if !reflect.DeepEqual(stream.sent, [][]string{{"x"}, {"y"}}) {
		t.Fatalf("after leaving: %v", stream.sent)
	}
	// A focus change leaves the mode (the mode rule); the overlay is the
	// application's to clear.
	press(t, r, "ctrl+a", "f")
	if err := r.SetFocus("console"); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("focus change kept mode %q", r.Mode.Name)
	}
	if notes, err := r.ValidateBindings(); err != nil || len(notes) > 0 {
		t.Fatalf("rows: %v %v", notes, err)
	}
}

// hintsRecipe is the recipe AGENTS.md describes, with the picks recorded.
func hintsRecipe(r *Runtime, picks *[]string) {
	r.DefineMode("hints", false)
	_ = r.DefinePassthrough("hints")
	leave := func(c Ctx) {
		if term := c.Runtime.Terminal(c.Runtime.Focus); term != nil {
			term.SetOverlay(nil)
		}
		_ = c.Runtime.SetMode("terminal")
	}
	r.Define("ochazuke.terminal").
		Verb("hints", func(c Ctx) Result {
			term := c.Runtime.Terminal(c.Runtime.Focus)
			if term == nil {
				return Failed(errors.New("no terminal"))
			}
			term.SetOverlay(&TerminalOverlay{Dim: true, Marks: []OverlayMark{{Row: 0, Col: 0, Text: "a"}}})
			if err := c.Runtime.SetMode("hints"); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}, "Label the pane's links").
		Verb("hint", func(c Ctx) Result {
			*picks = append(*picks, StringArg(c.Args, "action", "")+" "+StringArg(c.Args, "label", ""))
			return Done(nil)
		}, "Act on a labelled link").
		Verb("hints-cancel", func(c Ctx) Result { leave(c); return Done(nil) }, "Leave hints")
	hint := r.Operations["ochazuke.terminal.hint"]
	hint.Parameters = map[string]Parameter{"label": {Type: "string"}, "action": {Type: "string"}}
	r.Operations["ochazuke.terminal.hint"] = hint
	r.Define("ochazuke.terminal").
		Mode("leader_pending").Bind("f", Do("ochazuke.terminal.hints")).
		Mode("hints").
		Pattern(Alpha, Do("ochazuke.terminal.hint", Arguments{"action": "copy"}).FromPayload("label", SourceLetter).Hinted("copy")).
		Pattern(Upper, Do("ochazuke.terminal.hint", Arguments{"action": "type"}).FromPayload("label", SourceLetter).Hinted("type")).
		Pattern(CtrlAlpha, Do("ochazuke.terminal.hint", Arguments{"action": "open"}).FromPayload("label", SourceLetter).Hinted("open")).
		Bind("esc", Do("ochazuke.terminal.hints-cancel").Hinted("cancel")).
		Pattern(Paste, Do("ochazuke.terminal.hint", Arguments{"action": "miss"}).Hide()).
		Pattern(Any, Do("ochazuke.terminal.hint", Arguments{"action": "miss"}).FromPayload("label", SourceLetter).Hide())
}

// upper answers before alpha; ctrl+alpha is one ctrl+letter and takes
// exclusions spelled in full; the letter source reads every spelling of a
// letter as the lowercase letter.
func TestLetterClassesAndLetterSource(t *testing.T) {
	for _, c := range []struct {
		pattern, key string
		want         bool
	}{
		{Upper, "G", true}, {Upper, "g", false}, {Alpha, "G", true},
		{CtrlAlpha, "ctrl+o", true}, {CtrlAlpha, "ctrl+O", false}, {CtrlAlpha, "ctrl+space", false},
		{CtrlAlpha, "o", false}, {CtrlAlpha, "ctrl+shift+o", false},
		{CtrlAlpha + "-ctrl+h-ctrl+m", "ctrl+h", false}, {CtrlAlpha + "-ctrl+h-ctrl+m", "ctrl+k", true},
	} {
		if got, _ := matchesPattern(c.pattern, MacroKey{Key: c.key}); got != c.want || !isPattern(c.pattern) {
			t.Errorf("%s on %q: %v, want %v", c.pattern, c.key, got, c.want)
		}
	}
	if _, upper := matchesPattern(Upper, MacroKey{Key: "G"}); upper != 0 {
		t.Errorf("upper ranks %d", upper)
	}
	for key, want := range map[string]string{"f": "f", "F": "f", "shift+f": "f", "ctrl+f": "f", "ctrl+space": "", "1": "", "enter": ""} {
		if got := keyLetter(key); got != want {
			t.Errorf("letter of %q: %q, want %q", key, got, want)
		}
	}
}
