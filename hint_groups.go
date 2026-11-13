package ochazuke

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// HintsGroupSetting says how a drawn hint line writes the nouns of its
// hints: noun (the default) writes consecutive hints that run verbs of one
// noun under that noun once, "pane: e edit · r rename │ / list.filter";
// off writes every hint as "key noun.verb" (a row's own Hint as it
// reads), the line before v0.132.
const HintsGroupSetting = "hints.group"

// The hints.group values.
const (
	HintsGroupNoun = "noun"
	HintsGroupOff  = "off"
)

// hintGroupSep is what a drawn hint line puts before and after a group of
// hints written under their noun, where it meets other hints: as wide as
// hintSep, so choosing a form never moves the hints around it.
const hintGroupSep = " │ "

// hintsGrouped reports whether hint cells carry their noun and verb for
// grouping (hints.group is not off; an app without the setting groups).
func (r *Runtime) hintsGrouped() bool {
	return r.Settings().String(HintsGroupSetting) != HintsGroupOff
}

// groupHintCell fills a cell's Noun and Verb from the row's request when
// the cell's text is that request (no Hint of its own), so a drawn line
// can write the noun once for a run of them. Text stays "noun.verb":
// JoinHints and String are unchanged. A row with its own Hint, or that
// runs nothing, names no noun and has none.
func groupHintCell(r *Runtime, b Binding, c HintCell) HintCell {
	if b.Hint != "" || b.Request.Empty() {
		return c
	}
	noun, verb, ok := CutKey(b.Request.Key())
	if !ok || verb == "" {
		return c
	}
	c.Noun, c.Verb = modalLabel(r.DisplayNoun(noun)), modalLabel(verb)
	return c
}

// hintPiece is a run of text on a drawn hint line and the chrome role it
// is drawn in ("" for the lead, drawn muted).
type hintPiece struct {
	text, role string
}

// hintRun is consecutive cells under one Noun (the first cell's). A cell
// with no Noun (a row's own Hint, s skip) names no noun, so it never
// breaks a run: it joins the run before it, written as its Text in either
// form.
type hintRun []HintCell

func hintRuns(cells []HintCell) []hintRun {
	var runs []hintRun
	for _, c := range cells {
		if n := len(runs); n > 0 && (c.Noun == "" || runs[n-1][0].Noun == c.Noun) {
			runs[n-1] = append(runs[n-1], c)
			continue
		}
		runs = append(runs, hintRun{c})
	}
	return runs
}

// pieces is the run written grouped (its noun once, then each key and
// verb) or each cell as "key noun.verb", hintSep between its cells.
func (run hintRun) pieces(grouped bool) [][]hintPiece {
	out := make([][]hintPiece, len(run))
	for i, c := range run {
		var ps []hintPiece
		if i > 0 {
			ps = append(ps, hintPiece{hintSep, "hint.sep"})
		} else if grouped {
			ps = append(ps, hintPiece{c.Noun + ":", "hint.group"}, hintPiece{" ", "hint.sep"})
		}
		ps = append(ps, hintPiece{modalLabel(c.Key), "hint.key"})
		text := c.Text
		if grouped && c.Noun != "" {
			text = c.Verb
		}
		if text != "" {
			ps = append(ps, hintPiece{" " + modalLabel(text), "hint.text"})
		}
		out[i] = ps
	}
	return out
}

func piecesWidth(ps []hintPiece) int {
	w := 0
	for _, p := range ps {
		w += chromeWidth(p.text)
	}
	return w
}

// fit is how many of the run's cells end within room cells in a form, and
// the form's whole width.
func (run hintRun) fit(grouped bool, room int) (whole, width int) {
	for _, ps := range run.pieces(grouped) {
		width += piecesWidth(ps)
		if width <= room {
			whole++
		}
	}
	return whole, width
}

// hintPieces lays a hint line out: lead (a status or notice, "" for none),
// then the cells, each run of one noun in the form that shows more of its
// hints whole in the room left, else the narrower one (written under its
// noun once a second hint pays for the label). A run in the grouped form
// has hintGroupSep where it meets other hints, hintSep elsewhere. width 0
// or less is no limit. As every run may keep the "key noun.verb" form,
// the line never shows fewer whole hints than the ungrouped line would;
// the cut itself is the caller's (width-1 cells, then "…").
func hintPieces(lead string, cells []HintCell, width int) []hintPiece {
	runs := hintRuns(cells)
	grouped := make([]bool, len(runs))
	total := 0
	if lead != "" {
		total = chromeWidth(modalLabel(lead))
	}
	for i, run := range runs {
		if total > 0 || i > 0 {
			total += chromeWidth(hintSep)
		}
		_, plain := run.fit(false, 0)
		_, short := run.fit(true, 0)
		grouped[i] = run[0].Noun != "" && short < plain
		total += min(plain, short)
	}
	if width > 0 && total > width {
		// Cut: decide each run by the hints it shows whole before the
		// cut, the cut falling at width-1 cells.
		used := 0
		if lead != "" {
			used = chromeWidth(modalLabel(lead))
		}
		for i, run := range runs {
			if used > 0 || i > 0 {
				used += chromeWidth(hintSep)
			}
			room := width - 1 - used
			plainWhole, plain := run.fit(false, room)
			shortWhole, short := run.fit(true, room)
			if run[0].Noun != "" && shortWhole != plainWhole {
				grouped[i] = shortWhole > plainWhole
			}
			if grouped[i] {
				used += short
			} else {
				used += plain
			}
			if used >= width {
				break
			}
		}
	}
	var out []hintPiece
	if lead != "" {
		out = append(out, hintPiece{modalLabel(lead), ""})
	}
	for i, run := range runs {
		if len(out) > 0 {
			sep := hintSep
			if i > 0 && (grouped[i] || grouped[i-1]) {
				sep = hintGroupSep
			}
			out = append(out, hintPiece{sep, "hint.sep"})
		}
		for _, ps := range run.pieces(grouped[i]) {
			out = append(out, ps...)
		}
	}
	return out
}

// HintText is a hint line as it is drawn, as plain text in width cells
// (0: no limit): lead (a status; "" for none), " · ", then the cells,
// grouped under their nouns per hints.group (the cells carry Noun when it
// is on), cut with "…". With no cell carrying a Noun it is
// lead + " · " + JoinHints(cells), cut.
func HintText(lead string, cells []HintCell, width int) string {
	var b strings.Builder
	for _, p := range hintPieces(lead, cells, width) {
		b.WriteString(p.text)
	}
	if width > 0 {
		return ansi.Truncate(b.String(), width, "…")
	}
	return b.String()
}

// hintStyle is the style of a piece's role: the lead muted, the noun of
// a group in hint.group (muted when the level has no row for it).
func (s Styles) hintStyle(role string) lipgloss.Style {
	if role == "" {
		return s.Muted
	}
	return s.Chrome(role, s.Muted)
}
