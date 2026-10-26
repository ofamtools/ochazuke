package story

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// reverse is the escape that starts reverse video: the one highlight.
const reverse = "\x1b[7m"

// Golden is a frame as a golden file holds it: each row stripped of
// escapes behind a gutter that reads "R|" when the row has reverse video
// and " |" otherwise, so goldens pin the highlight without colour.
func Golden(frame string) string {
	var b strings.Builder
	for _, row := range strings.Split(frame, "\n") {
		if strings.Contains(row, reverse) {
			b.WriteString("R|")
		} else {
			b.WriteString(" |")
		}
		b.WriteString(strings.TrimRight(ansi.Strip(row), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// Check reports what in e does not hold for the session's current frame.
func Check(ss *Session, e Expect) []string {
	frame := ss.Frame()
	screen := ansi.Strip(frame)
	r := ss.R
	var bad []string
	failf := func(format string, a ...any) { bad = append(bad, fmt.Sprintf(format, a...)) }
	if e.Focus != "" && r.Focus != e.Focus {
		failf("focus %q, want %q", r.Focus, e.Focus)
	}
	if e.Mode != "" && r.Mode.Name != e.Mode {
		failf("mode %q, want %q", r.Mode.Name, e.Mode)
	}
	if e.Modal != nil && r.TopModal() != *e.Modal {
		failf("top modal %q, want %q", r.TopModal(), *e.Modal)
	}
	if e.Page != "" && r.Page != e.Page {
		failf("page %q, want %q", r.Page, e.Page)
	}
	if e.Visible != nil {
		lbl := label(r.Instances[r.Focus])
		if on := strings.Contains(screen, lbl); on != *e.Visible {
			failf("focused %q on screen = %v, want %v", lbl, on, *e.Visible)
		}
	}
	if e.Highlighted != nil {
		n := 0
		for _, row := range strings.Split(frame, "\n") {
			if strings.Contains(row, reverse) {
				n++
			}
		}
		if n != *e.Highlighted {
			failf("%d highlighted rows, want %d", n, *e.Highlighted)
		}
	}
	if e.HighlightedText != "" {
		n := 0
		for _, row := range strings.Split(frame, "\n") {
			if strings.Contains(row, reverse) {
				n++
				if !strings.Contains(ansi.Strip(row), e.HighlightedText) {
					failf("highlighted row %q lacks %q", strings.TrimSpace(ansi.Strip(row)), e.HighlightedText)
				}
			}
		}
		if n == 0 {
			failf("no highlighted row, want one holding %q", e.HighlightedText)
		}
	}
	for _, s := range e.Contains {
		if !strings.Contains(screen, s) {
			failf("screen lacks %q", s)
		}
	}
	for _, s := range e.Lacks {
		if strings.Contains(screen, s) {
			failf("screen shows %q", s)
		}
	}
	at := 0
	for _, s := range e.Order {
		i := strings.Index(screen[at:], s)
		if i < 0 {
			failf("order: %q not after offset %d", s, at)
			break
		}
		at += i + len(s)
	}
	w, _ := ss.Story.Size()
	ticker := ss.M.JobsTicker(w)
	if (e.Ticker != nil || e.NoTicker) && !slices.Equal(ticker, e.Ticker) {
		failf("ticker %q, want %q", ticker, e.Ticker)
	}
	if (e.Notices != nil || e.NoNotices) && !slices.Equal(ss.M.Notices, e.Notices) {
		failf("notices %q, want %q", ss.M.Notices, e.Notices)
	}
	rows := strings.Split(frame, "\n")
	for text, hex := range e.Colored {
		sgr, err := foreground(hex)
		if err != nil {
			failf("colored %q: %v", text, err)
			continue
		}
		found, drawn := false, false
		for _, row := range rows {
			if strings.Contains(ansi.Strip(row), text) {
				found = true
				drawn = drawn || strings.Contains(row, sgr)
			}
		}
		switch {
		case !found:
			failf("colored: screen lacks %q", text)
		case !drawn:
			failf("colored: no row with %q is drawn in %s", text, hex)
		}
	}
	for _, hex := range e.NoColor {
		if sgr, err := foreground(hex); err != nil {
			failf("noColor %q: %v", hex, err)
		} else if strings.Contains(frame, sgr) {
			failf("noColor: the frame draws in %s", hex)
		}
	}
	if e.Writes != nil && !slices.Equal(ss.Writes, *e.Writes) && (len(ss.Writes) > 0 || len(*e.Writes) > 0) {
		failf("writes %q, want %q", ss.Writes, *e.Writes)
	}
	if len(bad) > 0 {
		bad = append(bad, "frame:\n"+Golden(frame))
	}
	return bad
}

// foreground is the SGR parameters of a "#RRGGBB" foreground as Lip Gloss
// writes it ("38;2;R;G;B").
func foreground(hex string) (string, error) {
	var r, g, b uint8
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil || len(hex) != 7 {
		return "", fmt.Errorf("want #RRGGBB, got %q", hex)
	}
	return fmt.Sprintf("38;2;%d;%d;%d", r, g, b), nil
}

// Library is every story a run can name (embedded and the user's) and the
// recordings store a trimmed story's base may name.
type Library struct {
	Stories []*Story
	Store   Store
}

// Resolve turns a trimmed story into the base's initial story with this
// story's script and expectations, plus the base events to replay first.
// A base is "story:NAME" (its script) or "recording:NAME" (a saved
// recording's events).
func (lib Library) Resolve(s *Story) (*Story, []Event, error) {
	if s.Base == "" {
		return s, nil, nil
	}
	kind, name, _ := strings.Cut(s.Base, ":")
	var initial *Story
	var events []Event
	switch kind {
	case "story":
		base := Find(lib.Stories, name)
		if base == nil {
			return nil, nil, fmt.Errorf("base story %q not found", name)
		}
		b, prefix, err := lib.Resolve(base)
		if err != nil {
			return nil, nil, err
		}
		initial, events = b, append(slices.Clone(prefix), base.Script...)
	case "recording":
		e, err := lib.Store.Load(name)
		if err != nil {
			return nil, nil, err
		}
		initial, events = e.Rec.Initial, e.Rec.Events
	default:
		return nil, nil, fmt.Errorf("base %q: want story:NAME or recording:NAME", s.Base)
	}
	if s.Start > len(events) {
		return nil, nil, fmt.Errorf("start %d past the base's %d events", s.Start, len(events))
	}
	merged := *initial
	merged.Name, merged.Area, merged.Skip = s.Name, s.Area, s.Skip
	merged.Base, merged.Start = "", 0
	merged.Script, merged.Expect = s.Script, s.Expect
	merged.Path, merged.fsys, merged.rel, merged.disk = s.Path, s.fsys, s.rel, s.disk
	return &merged, events[:s.Start], nil
}

// Result is one story's outcome.
type Result struct {
	Story   *Story
	Failed  []string
	Err     error
	Skipped bool   // the story carries a skip ("gap: ..."): it ran but cannot fail
	Updated bool   // --update wrote the golden
	Trace   []Step // the window's steps (the base's prefix excluded)
}

// OK reports a pass (or a skip).
func (r Result) OK() bool { return r.Skipped || (r.Err == nil && len(r.Failed) == 0) }

// RunOptions control one run.
type RunOptions struct {
	Update bool // rewrite goldens
	Golden bool // compare the final frame to a golden when the story has no final expect
}

// Run plays a story headless and checks it: each per-step expect after its
// step, the final expect at the end, else the golden frame.
func (lib Library) Run(s *Story, opt RunOptions) (res Result) {
	res = Result{Story: s, Skipped: s.Skip != ""}
	story, prefix, err := lib.Resolve(s)
	if err != nil {
		res.Err = err
		return res
	}
	ss, err := Build(story, Options{Mono: true})
	if err != nil {
		res.Err = err
		return res
	}
	d := NewDriver(ss, Options{Mono: true})
	defer d.Close()
	if err := d.Play(prefix, nil); err != nil {
		res.Err = fmt.Errorf("base: %w", err)
		return res
	}
	d.Trace = nil
	defer func() { res.Trace = d.Trace }()
	var final []Expect
	steps := map[int][]Expect{}
	for _, e := range story.Expect {
		if e.After == 0 {
			final = append(final, e)
		} else {
			steps[e.After] = append(steps[e.After], e)
		}
	}
	err = d.Play(story.Script, func(n int) error {
		for _, e := range steps[n] {
			for _, f := range Check(d.S, e) {
				res.Failed = append(res.Failed, fmt.Sprintf("after %d: %s", n, f))
			}
		}
		return nil
	})
	if err != nil {
		res.Err = err
		return res
	}
	for _, e := range final {
		res.Failed = append(res.Failed, Check(d.S, e)...)
	}
	if len(final) > 0 || !opt.Golden {
		return res
	}
	got := Golden(d.S.Frame())
	if opt.Update {
		res.Err = story.WriteGolden(got)
		res.Updated = res.Err == nil
		return res
	}
	want, err := story.ReadGolden()
	if errors.Is(err, fs.ErrNotExist) {
		res.Err = fmt.Errorf("no golden %s (run with --update)", story.GoldenPath())
		return res
	}
	if err != nil {
		res.Err = err
		return res
	}
	if string(want) != got {
		res.Failed = append(res.Failed, "frame differs from "+story.GoldenPath()+"\ngot:\n"+got+"want:\n"+string(want))
	}
	return res
}

// Replay plays a recording headless: its events are the script, and only
// expect (the recording's own when nil) is asserted.
func Replay(rec *Recording, expect Expects) Result {
	s := *rec.Initial
	s.Base, s.Start, s.Script = "", 0, rec.Events
	s.Expect = rec.Expect
	if expect != nil {
		s.Expect = expect
	}
	return Library{}.Run(&s, RunOptions{})
}
