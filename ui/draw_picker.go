package ui

import (
	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/traits"
)

type pickerItem struct{ label, desc, tone, badge string }

func pickerItems(n *ViewNode) []pickerItem {
	var out []pickerItem
	for _, e := range toSlice(n.Props["items"]) {
		switch x := e.(type) {
		case map[string]any:
			out = append(out, pickerItem{
				label: anyString(firstOf(x, "label", "title", "name", "id")),
				desc:  anyString(firstOf(x, "desc", "description")),
				tone:  anyString(x["tone"]),
				badge: anyString(x["badge"]),
			})
		default:
			out = append(out, pickerItem{label: anyString(e)})
		}
	}
	return out
}

func (c *ctx) pickerLines(rn *rnode, w, h int) ([]string, *buffer.Cursor) {
	n := rn.n
	query := n.propString("query")
	items := pickerItems(n)
	filtering, hasFiltering := n.Props["filtering"].(bool)
	if !hasFiltering {
		filtering = true
	}

	style := c.st.tone("input")
	if rn.onFocus && filtering {
		style = c.st.tone("input.focus")
	}
	promptTone := "accent"
	if !filtering {
		promptTone = "muted"
	}
	prompt := c.st.t(promptTone, " ❯ ") + c.st.paint(query, style)
	if query == "" && n.propString("placeholder") != "" {
		prompt = c.st.t(promptTone, " ❯ ") + c.st.t("placeholder", n.propString("placeholder"))
	}

	count := itoa(len(items)) + " "
	right := count
	if !filtering {
		hint := "[/ or i: filter] "
		if query != "" {
			hint = "[/ or i: edit] "
		}
		if strW(prompt)+strW(hint)+strW(count) <= w {
			right = hint + count
		}
	}
	gap := w - strW(prompt) - strW(right)
	head := prompt + c.st.paint(spaces(gap), style) + c.st.paint(right, mergeTone(style, c.st.tone("muted")))
	out := []string{padLine(head, w, c.st.sgr(style))}

	rowsH := h - 1
	sel := selectedOf(n)
	start := windowStart(n, len(items), rowsH, sel)
	mode := n.propString("matchMode")
	matcher, _ := traits.CompileFilter(query, traits.MatchMode(mode), traits.FilterConfig{})
	for i := start; i < min(len(items), start+rowsH); i++ {
		it := items[i]
		ts := c.rowStyle(rn, i == sel)
		hit := matcher.MatchRunes(it.label)
		var spans []rspan
		for k, ok := range hit {
			if ok {
				spans = append(spans, rspan{k, k + 1, mergeTone(ts, c.st.tone("match"))})
			}
		}
		rs := []rune(it.label)
		line := c.st.paint("  ", ts) + c.st.runs(rs, 0, len(rs), ts, spans)
		if it.desc != "" {
			ds := ts
			if i != sel {
				ds = c.st.tone("muted")
			} else {
				ds.Dim = true
			}
			line += c.st.paint("  "+it.desc, ds)
		}
		if it.badge != "" {
			line += c.st.paint(" "+it.badge, ts)
		}
		out = append(out, padLine(line, w, c.st.sgr(ts)))
	}

	if !filtering {
		return out, nil
	}
	curX := 3 + strW(query)
	if ci, ok := n.propInt("cursor"); ok {
		qrs := []rune(query)
		ci = clampInt(ci, 0, len(qrs))
		curX = 3 + runesW(qrs[:ci])
	}
	return out, c.cursorIf(rn, curX, 0, buffer.CursorBar)
}
