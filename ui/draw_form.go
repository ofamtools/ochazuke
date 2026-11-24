package ui

import (
	"fmt"
	"strings"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/core"
)

func formFields(n *ViewNode) []core.ParamSpec {
	f, _ := propDecode[[]core.ParamSpec](n, "fields")
	return f
}

// formRows is the number of rows one form field takes at density d: compact
// fields are one row, balanced fields three (blank, control, note) and
// spacious fields four (separator, blank, control, note).
func formRows(d string) int {
	switch d {
	case DensityCompact:
		return 1
	case DensitySpacious:
		return 4
	}
	return 3
}

type formVisualRow struct {
	fieldIdx  int
	field     core.ParamSpec
	isItem    bool
	isAdd     bool
	itemIdx   int
	itemCount int
	value     any
}

func formRepeatableItems(f core.ParamSpec, values map[string]any) []string {
	raw, has := values[f.Name]
	if !has {
		raw = f.Default
	}
	switch t := raw.(type) {
	case nil:
		return nil
	case []string:
		return t
	case []any:
		out := make([]string, len(t))
		for i, e := range t {
			out[i] = anyString(e)
		}
		return out
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		var out []string
		for _, p := range strings.Split(t, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return toStrings(raw)
	}
}

func formVisualRows(fields []core.ParamSpec, values map[string]any) []formVisualRow {
	rows := make([]formVisualRow, 0, len(fields))
	for fi, f := range fields {
		if !f.Repeatable {
			val, has := values[f.Name]
			if !has {
				val = f.Default
			}
			rows = append(rows, formVisualRow{
				fieldIdx: fi,
				field:    f,
				itemIdx:  -1,
				value:    val,
			})
			continue
		}
		items := formRepeatableItems(f, values)
		for idx, item := range items {
			rows = append(rows, formVisualRow{
				fieldIdx:  fi,
				field:     f,
				isItem:    true,
				itemIdx:   idx,
				itemCount: len(items),
				value:     item,
			})
		}
		rows = append(rows, formVisualRow{
			fieldIdx:  fi,
			field:     f,
			isAdd:     true,
			itemIdx:   -1,
			itemCount: len(items),
		})
	}
	return rows
}

func formAddLabel(s core.ParamSpec) string {
	if lbl := strings.TrimSpace(s.AddLabel); lbl != "" {
		return lbl
	}
	name := strings.TrimSpace(s.Name)
	if len(name) > 2 && strings.HasSuffix(name, "s") {
		return name[:len(name)-1]
	}
	if name != "" {
		return name
	}
	return "item"
}

func formLabelWidth(fields []core.ParamSpec, values map[string]any) int {
	lw := 0
	for _, f := range fields {
		lw = max(lw, strW(fieldText(f.Name))+2)
		if f.Repeatable {
			nItems := len(formRepeatableItems(f, values))
			if nItems > 1 {
				lw = max(lw, strW(fmt.Sprintf("[%d]", nItems))+2)
			}
		}
	}
	return lw
}

func formTextInputWidth(aw int, inlineErr string) (iw int, tail string) {
	if inlineErr != "" {
		tail = " ✗ " + inlineErr
	}
	iw = aw
	if tail != "" {
		if aw-strW(tail) < 6 {
			tail = truncate(tail, max(0, aw-6))
		}
		iw = aw - strW(tail)
	}
	return iw, tail
}

func formWrappedLineCount(val any, aw int, inlineErr string) int {
	iw, _ := formTextInputWidth(aw, inlineErr)
	if iw <= 1 {
		return 1
	}
	rs := []rune(fieldTextMultiline(anyString(val)))
	return len(wrapRanges(rs, max(1, iw-1)))
}

// formHeight is a form's content height: every field block (including expanded
// repeatable item rows, wrapped text lines when wrap is enabled, and "+ Add"
// rows) plus the submit row.
func formHeight(n *ViewNode, w int, d string) int {
	fields := formFields(n)
	values, _ := propDecode[map[string]any](n, "values")
	errsRaw, _ := propDecode[map[string]any](n, "errors")
	wrap := n.propBool("wrap")
	per := formRows(d)
	aw := 0
	if wrap && w > 0 {
		aw = max(0, w-(formLabelWidth(fields, values)+2))
	}
	total := 1
	for _, f := range fields {
		isText := len(f.Enum) == 0 && f.Type != core.ParamBool
		if f.Repeatable {
			items := formRepeatableItems(f, values)
			total += (per - 1) + 1
			for idx, item := range items {
				if wrap && isText && aw > 0 {
					inline := ""
					if d == DensityCompact && idx == 0 {
						inline = fieldText(anyString(errsRaw[f.Name]))
					}
					total += formWrappedLineCount(item, aw, inline)
				} else {
					total++
				}
			}
		} else {
			extra := 0
			if wrap && isText && aw > 0 {
				val, has := values[f.Name]
				if !has {
					val = f.Default
				}
				inline := ""
				if d == DensityCompact {
					inline = fieldText(anyString(errsRaw[f.Name]))
				}
				extra = max(0, formWrappedLineCount(val, aw, inline)-1)
			}
			total += per + extra
		}
	}
	return total
}

func formActive(n *ViewNode, rows []formVisualRow) int {
	active := 0
	maxIdx := len(rows) - 1
	if n.propBool("submitRow") {
		maxIdx = len(rows)
	}
	if v, ok := n.prop("active"); ok {
		if i, isInt := anyInt(v); isInt {
			active = clampInt(i, 0, maxIdx)
		} else if s, isStr := v.(string); isStr {
			for i, r := range rows {
				if r.field.Name == s {
					active = i
					break
				}
			}
		}
	}
	return active
}

// formLines renders a form at width w and height h. Compact fields keep the
// value on the label row with any error inline after it; balanced and
// spacious fields put the error, else the muted description, on the row
// under the control. When h cannot hold the density's natural rows, fields
// fall back to compact rows so every control stays visible, and only the
// active field keeps its full block if the rows left over allow it. When even
// compact rows do not fit, they scroll to keep the active control visible and
// the submit row stays pinned at the bottom.
func (c *ctx) formLines(rn *rnode, w, h int) ([]string, *buffer.Cursor) {
	n := rn.n
	d := c.b.dens
	fields := formFields(n)
	values, _ := propDecode[map[string]any](n, "values")
	errsRaw, _ := propDecode[map[string]any](n, "errors")
	vrows := formVisualRows(fields, values)
	active := formActive(n, vrows)
	cursorPos, hasCursor := n.propInt("cursor")
	_, hasEditing := n.prop("editing")
	editing := n.propBool("editing")
	wrap := n.propBool("wrap")
	showCursor := editing || hasCursor || !hasEditing
	lw := formLabelWidth(fields, values)
	x0 := lw + 2 // value column: mark, space, name, required mark, space
	tx := x0 + 1 // text column inside the value's leading space

	// A field uses the density's block when the natural height fits; else one
	// compact row, except the active field when its extra rows still fit.
	per := formRows(d)
	natH := formHeight(n, w, d)
	allFull := h <= 0 || natH <= h
	compactH := formHeight(n, w, DensityCompact)
	activeFull := allFull || compactH+(per-1) <= h
	activeFieldIdx := -1
	if active >= 0 && active < len(vrows) {
		activeFieldIdx = vrows[active].fieldIdx
	}

	var out []string
	var cur *buffer.Cursor
	ctlStart, ctlEnd := 0, 0 // line range [ctlStart, ctlEnd] of the active row
	for i, vr := range vrows {
		f := vr.field
		fd := d
		if !allFull && !(activeFull && vr.fieldIdx == activeFieldIdx) {
			fd = DensityCompact
		}
		isFirst := !f.Repeatable || (vr.isItem && vr.itemIdx == 0) || (vr.isAdd && vr.itemCount == 0)
		isLast := !f.Repeatable || vr.isAdd
		on := rn.onFocus && i == active
		errMsg := fieldText(anyString(errsRaw[f.Name]))

		if isFirst {
			switch fd {
			case DensityCompact:
			case DensitySpacious:
				sep := ""
				if c.b.border != BorderNone {
					sep = c.st.t("border", strings.Repeat("─", max(0, w)))
				}
				out = append(out, sep, "")
			default:
				out = append(out, "")
			}
		}

		mark := " "
		ls := c.st.tone("muted")
		if on {
			mark, ls = c.st.t("accent", "▸"), mergeTone(c.st.tone("accent"), ToneStyle{Bold: true})
		}
		req := " "
		if f.Required {
			req = c.st.t("error", "*")
		}

		if i == active {
			ctlStart = len(out)
		}

		if vr.isAdd {
			var label string
			if vr.itemCount == 0 {
				label = mark + " " + c.st.paint(padPlain(fieldText(f.Name), lw-2), ls) + req + " "
			} else {
				label = mark + " " + spaces(lw)
			}
			addStyle := c.st.tone("muted")
			if on {
				addStyle = mergeTone(c.st.tone("accent"), ToneStyle{Bold: true})
			}
			body := c.st.paint("[+ Add "+fieldText(formAddLabel(f))+"]", addStyle)
			if fd == DensityCompact && vr.itemCount == 0 && errMsg != "" {
				body += c.st.t("error", " ✗ "+errMsg)
			}
			out = append(out, label+body)
		} else {
			var label string
			if !f.Repeatable || vr.itemIdx == 0 {
				label = mark + " " + c.st.paint(padPlain(fieldText(f.Name), lw-2), ls) + req + " "
			} else {
				idxLabel := fmt.Sprintf("[%d]", vr.itemIdx+1)
				label = mark + " " + spaces(max(0, lw-2-strW(idxLabel))) + c.st.paint(idxLabel, c.st.tone("muted")) + "  "
			}
			inline, placeholder := "", ""
			if fd == DensityCompact && isFirst {
				inline, placeholder = errMsg, fieldText(f.Description)
			}
			fieldCursor := -1
			if on && hasCursor {
				fieldCursor = cursorPos
			}
			baseY := len(out)
			bodyLines, cx, cy := c.formControl(f, vr.value, on, editing, showCursor, wrap, max(0, w-x0), placeholder, inline, fieldCursor)
			for li, body := range bodyLines {
				if li == 0 {
					out = append(out, label+body)
				} else {
					out = append(out, spaces(x0)+body)
				}
			}
			if on && rn.isFocus && cx >= 0 {
				cur = &buffer.Cursor{X: x0 + cx, Y: baseY + cy, Shape: buffer.CursorBar}
			}
		}

		if i == active {
			ctlEnd = len(out) - 1
		}

		if isLast && fd != DensityCompact {
			note := ""
			switch desc := fieldText(f.Description); {
			case errMsg != "":
				note = spaces(tx) + c.st.t("error", truncate("✗ "+errMsg, max(0, w-tx)))
			case desc != "":
				note = spaces(tx) + c.st.t("muted", truncate(desc, max(0, w-tx)))
			}
			out = append(out, note)
		}
	}

	submit := fieldText(n.propString("submitLabel"))
	if submit == "" {
		submit = "Submit"
	}
	if n.propBool("submitRow") && active == len(vrows) && rn.onFocus {
		ctlStart, ctlEnd = len(out), len(out)
		btnStyle := mergeTone(c.st.tone("selected"), ToneStyle{Fg: c.st.tone("accent").Fg, Bold: true})
		out = append(out, c.st.t("accent", "▸")+" "+c.st.paint("[ ⏎ "+submit+" ]", btnStyle))
	} else {
		out = append(out, "  "+c.st.t("key", "⏎")+" "+c.st.t("muted", strings.ToLower(submit)))
	}
	if h <= 0 || len(out) <= h {
		return out, cur
	}

	// Too short for all lines: window them around the active control (and cursor
	// line when wrapped) while keeping the submit row pinned at the bottom.
	rows, foot := out[:len(out)-1], out[len(out)-1:]
	avail := h - 1
	if h == 1 {
		avail, foot = 1, nil
	}
	anchor := ctlStart
	if ctlEnd-ctlStart+1 <= avail {
		anchor = ctlEnd
	}
	if cur != nil && (cur.Y > anchor || ctlEnd-ctlStart+1 > avail) {
		anchor = cur.Y
	}
	start := min(max(0, anchor+1-avail), max(0, len(rows)-avail))
	win := append(append([]string{}, rows[start:min(len(rows), start+avail)]...), foot...)
	if cur != nil {
		cur.Y -= start
		if cur.Y < 0 || cur.Y >= avail {
			cur = nil
		}
	}
	return win, cur
}

// formControl renders a field's value area, aw columns wide from the value
// column. When wrap is true, text fields soft-wrap and preserve explicit
// newlines so every character remains visible. Otherwise, text fields scroll
// horizontally to keep the cursor in view while focused. It returns the
// rendered lines and the cursor (col, line) relative to the value column, or
// cx = -1 when the field has no text cursor.
func (c *ctx) formControl(f core.ParamSpec, val any, on, editing, showCursor, wrap bool, aw int, placeholder, inlineErr string, cursor int) ([]string, int, int) {
	tail := ""
	if inlineErr != "" {
		tail = " ✗ " + inlineErr
	}
	// The active enum or toggle sits on the input.focus background and keeps
	// its selected (accent) versus unselected (muted) foreground.
	var hl ToneStyle
	if on {
		hl = ToneStyle{Bg: c.st.tone("input.focus").Bg}
	}
	switch {
	case len(f.Enum) > 0:
		parts := make([]string, len(f.Enum))
		for k, opt := range f.Enum {
			if anyString(val) == opt {
				parts[k] = c.st.paint("(•) "+fieldText(opt), mergeTone(mergeTone(c.st.tone("accent"), ToneStyle{Bold: true}), hl))
			} else {
				parts[k] = c.st.paint("( ) "+fieldText(opt), mergeTone(c.st.tone("muted"), hl))
			}
		}
		return []string{strings.Join(parts, c.st.paint("  ", hl)) + c.st.t("error", tail)}, -1, 0
	case f.Type == core.ParamBool:
		box := c.st.paint("[ ]", mergeTone(c.st.tone("muted"), hl))
		if b, _ := val.(bool); b || anyString(val) == "true" {
			box = c.st.paint("[x]", mergeTone(c.st.tone("accent"), hl))
		}
		return []string{box + c.st.t("error", tail)}, -1, 0
	}
	iw, tail := formTextInputWidth(aw, inlineErr)
	if iw <= 0 {
		return []string{c.st.t("error", tail)}, -1, 0
	}
	style := c.st.tone("input")
	if on {
		if editing {
			style = c.st.tone("input.edit")
		} else {
			style = c.st.tone("input.focus")
		}
	}
	room := iw - 1 // columns after the leading space
	fill := c.st.sgr(style)

	if wrap {
		rs := []rune(fieldTextMultiline(anyString(val)))
		if len(rs) == 0 && placeholder != "" {
			shown := c.st.t("placeholder", " "+truncate(placeholder, room))
			cx := -1
			if on && showCursor {
				cx = min(1, iw-1)
			}
			return []string{padLine(shown, iw, fill) + c.st.t("error", tail)}, cx, 0
		}
		rngs := wrapRanges(rs, max(1, room))
		lines := make([]string, len(rngs))
		for li, rng := range rngs {
			seg := string(rs[rng[0]:rng[1]])
			line := padLine(c.st.paint(" "+seg, style), iw, fill)
			if li == 0 && tail != "" {
				line += c.st.t("error", tail)
			}
			lines[li] = line
		}
		if !on || !showCursor {
			return lines, -1, 0
		}
		ci := len(rs)
		if cursor >= 0 {
			ci = clampInt(cursor, 0, len(rs))
		}
		cy, col := wrapCursor(rs, rngs, ci)
		return lines, min(1+col, iw-1), cy
	}

	rs := []rune(fieldText(anyString(val)))
	var shown string
	cx := 1
	switch {
	case len(rs) == 0 && placeholder != "":
		shown = c.st.t("placeholder", " "+truncate(placeholder, room))
	case on && cursor >= 0:
		ci := clampInt(cursor, 0, len(rs))
		startRune := 0
		for startRune < ci && runesW(rs[startRune:ci]) > max(0, room-1) {
			startRune++
		}
		vis := truncate(string(rs[startRune:]), room)
		shown, cx = c.st.paint(" "+vis, style), 1+runesW(rs[startRune:ci])
	case on && showCursor:
		vis := tailFit(rs, room-1) // keep one column for the cursor
		shown, cx = c.st.paint(" "+vis, style), 1+strW(vis)
	default:
		shown = c.st.paint(" "+truncate(string(rs), room), style)
	}
	if !on || !showCursor {
		cx = -1
	}
	return []string{padLine(shown, iw, fill) + c.st.t("error", tail)}, min(cx, iw-1), 0
}

// tailFit returns the longest suffix of rs whose display width is at most w.
// It walks back from the end, so the work is bounded by the visible width
// rather than the length of the text, and it never starts on a zero-width
// rune, so combining marks stay with their base.
func tailFit(rs []rune, w int) string {
	if w <= 0 {
		return ""
	}
	from, used := len(rs), 0
	for from > 0 && used+runeW(rs[from-1]) <= w {
		from--
		used += runeW(rs[from])
	}
	for from < len(rs) && runeW(rs[from]) == 0 {
		from++
	}
	vis := string(rs[from:])
	for strW(vis) > w && from < len(rs) { // grapheme widths can exceed the per-rune sum
		from++
		vis = string(rs[from:])
	}
	return vis
}
