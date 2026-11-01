package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Chip fields and inverted fields of a record editor (RecordField.Chips,
// RecordField.Invertible).
//
// A chips field draws its choices as chips wrapped across the lines under
// its label, indented and in the chip tone (ToneChip; the one under the
// cursor in ToneChipFocused while the field has the focus), so they read
// apart from the field labels. space toggles the chip under the cursor;
// h and l walk a line, j and k move between lines and spill to the field
// above or below at the first and last line, as a list's rows spill. The
// four edges have one verb, form.chip-edge: the first and last chip, and
// the first and last chip of the cursor's line.
//
// An Invertible field has a normal and an inverted state, drawn after its
// label ("is" or "not") and turned over by form.invert; the record
// carries it under the field's name plus RecordInvertSuffix.

// RecordInvertSuffix ends the record key that holds an Invertible field's
// state ("status!" is "true" while status is inverted).
const RecordInvertSuffix = "!"

// FormsInvertedToneSetting draws inverted fields in ToneRecordInverted.
const FormsInvertedToneSetting = "forms.inverted-tone"

// fieldStyle keeps the value's focus and selection emphasis while the
// inverted tone supplies its colours and any additional emphasis.
func (e *RecordEditor) fieldStyle(f *recordField, base lipgloss.Style) lipgloss.Style {
	if !f.Invertible || !f.not || !e.r.helpFlag(FormsInvertedToneSetting, true) {
		return base
	}
	tone := e.Styles.Tone(ToneRecordInverted)
	return tone.Inherit(base).Bold(tone.GetBold() || base.GetBold()).Faint(tone.GetFaint() || base.GetFaint())
}

// FormChipsNoun is a chips field's instance noun.
const FormChipsNoun = "ochazuke.form-chips"

// Form guards for chips and inverted fields: the rows of form.json name
// them.
const (
	FormChipsGuard      = "form-chips"      // the focus is on a chips field with chips
	FormInvertibleGuard = "form-invertible" // the focused field is Invertible
	FormClearableGuard  = "form-clearable"  // the form is Clearable
)

// defaultChipWidth lays chips out before the form is first drawn.
const defaultChipWidth = 76

func invertText(not bool) string {
	if not {
		return "true"
	}
	return ""
}

// ParseChips reads a chips field's value: a JSON array of choice ids, ""
// for none.
func ParseChips(v string) ([]string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(v), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// ChipsValue is a chips field's value for ids ("" for none).
func ChipsValue(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	raw, _ := json.Marshal(ids)
	return string(raw)
}

func (e *RecordEditor) chipsProblem(f *recordField, v string) string {
	ids, err := ParseChips(v)
	if err != nil {
		return "a list of choices"
	}
	for _, id := range ids {
		if len(f.choices) > 0 && !hasChoice(f.choices, id) {
			return fmt.Sprintf("%q is not a choice", id)
		}
	}
	if f.Validate != nil {
		if err := f.Validate(v); err != nil {
			return err.Error()
		}
	}
	return ""
}

// chipOn reports whether the field's value holds choice id.
func (f *recordField) chipOn(id string) bool {
	ids, _ := ParseChips(f.value)
	return slices.Contains(ids, id)
}

func chipText(c Choice, on bool) string {
	mark := "○ "
	if on {
		mark = "● "
	}
	return " " + mark + modalLabel(choiceLabel(c)) + " "
}

// layoutChips wraps the chips into lines width cells wide (each line
// holds at least one chip).
func (f *recordField) layoutChips(width int) {
	f.lines = nil
	var line []int
	used := 0
	for i, c := range f.choices {
		w := ansi.StringWidth(chipText(c, true))
		if len(line) > 0 && used+1+w > width {
			f.lines = append(f.lines, line)
			line, used = nil, 0
		}
		if len(line) > 0 {
			used++
		}
		line = append(line, i)
		used += w
	}
	if len(line) > 0 {
		f.lines = append(f.lines, line)
	}
}

func (f *recordField) chipLines() [][]int {
	if f.lines == nil {
		f.layoutChips(defaultChipWidth)
	}
	return f.lines
}

// chipAt is the line and column of the cursor's chip.
func (f *recordField) chipAt() (int, int) {
	for li, line := range f.chipLines() {
		if col := slices.Index(line, f.chip); col >= 0 {
			return li, col
		}
	}
	return 0, 0
}

// chipBlock draws the chips' lines, indent cells in.
func (e *RecordEditor) chipBlock(f *recordField, width, indent int) []string {
	f.layoutChips(max(1, width-indent))
	s := e.Styles
	focused := e.r.Focus == f.id
	var out []string
	for _, line := range f.lines {
		parts := make([]string, 0, len(line))
		for _, i := range line {
			c := f.choices[i]
			on := f.chipOn(c.ID)
			st := s.Tone(ToneChip).Bold(on)
			if focused && i == f.chip {
				st = s.Tone(ToneChipFocused)
				if !s.hasTone(ToneChipFocused) {
					st = st.Inherit(s.Selection(true))
				}
			}
			parts = append(parts, e.fieldStyle(f, st).Render(chipText(c, on)))
		}
		out = append(out, ansi.Truncate(strings.Repeat(" ", indent)+strings.Join(parts, " "), width, "…"))
	}
	if len(f.choices) == 0 {
		out = append(out, strings.Repeat(" ", indent)+e.fieldStyle(f, s.Muted).Render(EmptyText("values")))
	}
	return out
}

// toggleChip turns the cursor's chip on or off and writes the field.
func (e *RecordEditor) toggleChip(f *recordField) error {
	if f == nil || !f.chips || f.chip >= len(f.choices) {
		return errors.New("no chip under the cursor")
	}
	ids, _ := ParseChips(f.value)
	id := f.choices[f.chip].ID
	if i := slices.Index(ids, id); i >= 0 {
		ids = slices.Delete(ids, i, i+1)
	} else {
		ids = append(ids, id)
		// Chips keep the choices' order, whatever order they were picked.
		order := func(a, b string) int {
			return slices.IndexFunc(f.choices, func(c Choice) bool { return c.ID == a }) - slices.IndexFunc(f.choices, func(c Choice) bool { return c.ID == b })
		}
		slices.SortStableFunc(ids, order)
	}
	f.value = ChipsValue(ids)
	e.seen = e.snapshot()
	return e.seal(f)
}

// moveChip walks the cursor's line by delta, clamped at its ends.
func (f *recordField) moveChip(delta int) {
	lines := f.chipLines()
	if len(lines) == 0 {
		return
	}
	li, col := f.chipAt()
	f.chip = lines[li][min(max(0, col+delta), len(lines[li])-1)]
}

// chipLine moves the cursor delta lines, keeping its column where the
// line is long enough; past the first or last line the focus spills to
// the field above or below.
func (e *RecordEditor) chipLine(f *recordField, delta int) error {
	lines := f.chipLines()
	li, col := f.chipAt()
	to := li + delta
	if to < 0 || to >= len(lines) {
		return e.move(delta, false)
	}
	f.chip = lines[to][min(col, len(lines[to])-1)]
	return nil
}

// chipEdge puts the cursor on an edge: the first or last chip, or the
// first or last chip of its line (SpillEdge names).
func (f *recordField) chipEdge(edge SpillEdge) error {
	lines := f.chipLines()
	if len(lines) == 0 {
		return errors.New("no chips")
	}
	li, _ := f.chipAt()
	switch edge {
	case EdgeFirst:
		f.chip = lines[0][0]
	case EdgeLast:
		last := lines[len(lines)-1]
		f.chip = last[len(last)-1]
	case EdgeFirstColumn:
		f.chip = lines[li][0]
	case EdgeLastColumn:
		f.chip = lines[li][len(lines[li])-1]
	default:
		return fmt.Errorf("unknown edge %q", edge)
	}
	return nil
}

// landChips puts the cursor of a chips field the focus moved into on the
// line it was entered from: the first line coming down, the last coming up.
func (e *RecordEditor) landChips(f *recordField, delta int) {
	if !f.chips {
		return
	}
	lines := f.chipLines()
	if len(lines) == 0 {
		return
	}
	_, col := f.chipAt()
	line := lines[0]
	if delta < 0 {
		line = lines[len(lines)-1]
	}
	f.chip = line[min(col, len(line)-1)]
}

// invert turns the field over and writes it; u undoes it.
func (e *RecordEditor) invert(f *recordField) error {
	if f == nil || !f.Invertible {
		return errors.New("the focused field cannot be inverted")
	}
	from := invertText(f.not)
	f.not = !f.not
	e.seen = e.snapshot()
	err := e.flush()
	e.undo = append(e.undo, recordStep{field: f.Name + RecordInvertSuffix, from: from, to: invertText(f.not)})
	e.redo = nil
	return err
}

// clear empties every field and turns every inverted one back.
func (e *RecordEditor) clear() error {
	if !e.spec.Clearable {
		return errors.New("this form has nothing to clear")
	}
	var errs []error
	for _, f := range e.fields {
		if f.not {
			errs = append(errs, e.invert(f))
		}
		if f.text() != "" {
			if err := e.set(f, ""); err != nil {
				errs = append(errs, err)
				continue
			}
			e.seen = e.snapshot()
			errs = append(errs, e.seal(f))
		}
	}
	return errors.Join(errs...)
}

// invertWord is what an Invertible field's label line ends with.
func invertWord(not bool) string {
	if not {
		return "not"
	}
	return "is"
}

func registerFormChips(r *Runtime) {
	on := func(run func(*RecordEditor, *recordField, Arguments) error) Handler {
		return func(r *Runtime, c Context, a Arguments) (any, error) {
			e := r.recordEditorOf(focusOf(c))
			if e == nil {
				return nil, errors.New("no focused record editor")
			}
			return nil, run(e, e.fieldOn(focusOf(c)), a)
		}
	}
	chips := func(run func(*RecordEditor, *recordField, Arguments) error) Handler {
		return on(func(e *RecordEditor, f *recordField, a Arguments) error {
			if f == nil || !f.chips {
				return errors.New("the focused field has no chips")
			}
			return run(e, f, a)
		})
	}
	r.Define(FormChipsNoun).Focusable()
	r.Define(FormNoun).
		Verb("invert", on(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.invert(f) }), "Turn the focused field over: is becomes not and back; written at once").When(FormInvertibleGuard).
		Verb("clear", on(func(e *RecordEditor, _ *recordField, _ Arguments) error { return e.clear() }), "Empty every field and turn every inverted field back; u undoes it a field at a time").When(FormClearableGuard).
		Verb("chip-toggle", chips(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.toggleChip(f) }), "Turn the chip under the cursor on or off; written at once").When(FormChipsGuard).
		Verb("chip-move", chips(func(_ *RecordEditor, f *recordField, a Arguments) error {
			f.moveChip(IntArg(a, "delta", 0))
			return nil
		}), "Move the chip cursor along its line, stopping at the line's ends").Motion().When(FormChipsGuard).
		Verb("chip-line", chips(func(e *RecordEditor, f *recordField, a Arguments) error {
			return e.chipLine(f, IntArg(a, "delta", 0))
		}), "Move the chip cursor a line down or up; past the first or last line the focus goes to the field above or below").Motion().When(FormChipsGuard).
		Verb("chip-edge", chips(func(_ *RecordEditor, f *recordField, a Arguments) error {
			return f.chipEdge(SpillEdge(StringArg(a, "edge", "")))
		}), "Put the chip cursor on an edge: first, last, first-col or last-col (the ends of its line)").Motion().When(FormChipsGuard)
	delta := map[string]Parameter{"delta": {Type: "integer", Required: true}}
	for verb, params := range map[string]map[string]Parameter{
		"chip-move": delta,
		"chip-line": delta,
		"chip-edge": {"edge": {Type: "string", Required: true, Enum: []string{string(EdgeFirst), string(EdgeLast), string(EdgeFirstColumn), string(EdgeLastColumn)}}},
	} {
		op := r.Operations[FormNoun+"."+verb]
		op.Parameters = params
		r.Operations[FormNoun+"."+verb] = op
	}
	r.DefineGuard(FormChipsGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		if e == nil {
			return false
		}
		f := e.fieldOn(ev.Focus)
		return f != nil && f.chips && len(f.choices) > 0
	})
	r.DefineGuard(FormInvertibleGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		if e == nil {
			return false
		}
		f := e.fieldOn(ev.Focus)
		return f != nil && f.Invertible
	})
	r.DefineGuard(FormClearableGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		return e != nil && e.spec.Clearable
	})
}
