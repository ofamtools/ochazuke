package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Tones: a cell, or a run of runes inside one, can carry a named tone
// (state.working, marker.star) that the theme maps to a colour and
// attributes. The text never changes: a tone is a span beside the text
// (ListItem.Tones, TreeNode.Tones, a catalog row's "tones"), so widths, truncation, the filter, yanks and the
// command line's --format read the cells exactly as without tones.
//
// How a tone draws:
//
//   - A known tone draws in its scheme's ToneStyle: the foreground (a
//     colour, or a palette role such as "error" so it follows the scheme),
//     bold, dim (faint).
//   - An unknown tone, or a list drawn without the Model's styles, draws
//     plain.
//   - mono and NO_COLOR draw the attributes alone (bold, faint), never a
//     colour.
//   - On the selected row (reverse video) a tone draws its bold alone: no
//     colour, no dim, so the row stays one bar that reads in every theme,
//     and a table's current cell keeps its underline.
//   - A page list without the focus fades its tones with it
//     (dim-unfocused): the tone's colour is carried the same percent toward
//     the background.

// The tones every built-in theme draws (defaultTones); applications and
// themes.json add their own names.
const (
	ToneWorking    = "state.working"
	ToneNeedsInput = "state.needs-input"
	ToneDone       = "state.done"
	ToneErrored    = "state.errored"
	ToneIdle       = "state.idle"
	ToneExited     = "state.exited"
	ToneStar       = "marker.star"
	ToneQueued     = "marker.queued"
)

// ToneSpan gives a run of one cell a tone. Cell is the column index (in a
// list without columns, and a tree without them, 0 is the label); From and
// To are rune offsets into the cell's text as drawn (escape sequences,
// which a cell never draws, do not count), To 0 meaning the cell's end, so
// {Cell, Tone} alone tones the whole cell. A later span wins where two
// overlap; a span outside the text draws nothing.
type ToneSpan struct {
	Cell int    `json:"cell"`
	Tone string `json:"tone"`
	From int    `json:"from,omitempty"`
	To   int    `json:"to,omitempty"`
}

// CellTone tones a whole cell.
func CellTone(cell int, tone string) ToneSpan { return ToneSpan{Cell: cell, Tone: tone} }

// SpanTone tones the runes [from, to) of a cell (to 0: to its end).
func SpanTone(cell, from, to int, tone string) ToneSpan {
	return ToneSpan{Cell: cell, Tone: tone, From: from, To: to}
}

// ToneStyle is how a scheme draws one tone: Fg is a colour as a scheme
// role takes it ("#RRGGBB", "#RGB", an ANSI index, {"light", "dark"}) or
// the name of a palette role (text, muted, accent, border, surface, error)
// so the tone follows the scheme; empty keeps the text's colour. Bold and
// Dim (faint) are the attributes, the only part mono draws. In themes.json
// a tone is {"fg": ..., "bold": true, "dim": true}, or a colour or role
// alone ("#5FAFD7", "error").
type ToneStyle struct {
	Fg   SchemeColor `json:"fg"`
	Bold bool        `json:"bold,omitempty"`
	Dim  bool        `json:"dim,omitempty"`
}

// UnmarshalJSON reads a colour or role alone, or {"fg", "bold", "dim"}.
func (t *ToneStyle) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*t = ToneStyle{Fg: SchemeColor{one, one}}
		return nil
	}
	var full struct {
		Fg   *SchemeColor `json:"fg"`
		Bold bool         `json:"bold"`
		Dim  bool         `json:"dim"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&full); err != nil {
		return errors.New(`a tone is a colour, a role, or {"fg": ..., "bold": true, "dim": true}`)
	}
	*t = ToneStyle{Bold: full.Bold, Dim: full.Dim}
	if full.Fg != nil {
		t.Fg = *full.Fg
	}
	return nil
}

// toneRoles are the palette roles a tone's Fg may name.
var toneRoles = []string{"text", "muted", "accent", "border", "surface", "error"}

// defaultTones are the tones every built-in theme draws; a user scheme
// starts from them. The state tones keep their meaning without colour:
// needs-input and errored are bold, exited is dim, so mono reads them too.
var defaultTones = map[string]ToneStyle{
	ToneWorking:    {Fg: SchemeColor{"#1F6FB2", "#7CB7E8"}},
	ToneNeedsInput: {Fg: SchemeColor{"#9A6700", "#E3B341"}, Bold: true},
	ToneDone:       {Fg: SchemeColor{"#2E7D32", "#8BC48A"}},
	ToneErrored:    {Fg: SchemeColor{"error", "error"}, Bold: true},
	ToneIdle:       {Fg: SchemeColor{"muted", "muted"}},
	ToneExited:     {Fg: SchemeColor{"muted", "muted"}, Dim: true},
	ToneStar:       {Fg: SchemeColor{"#B08800", "#F2CC60"}, Bold: true},
	ToneQueued:     {Fg: SchemeColor{"accent", "accent"}},
}

// DefaultTones are the built-in tones and how the built-in themes draw
// them, a copy.
func DefaultTones() map[string]ToneStyle { return maps.Clone(defaultTones) }

// parseTones reads a scheme's "tones": an object of tones by name, each
// validated, laid over base (a tone given replaces base's whole).
func parseTones(base map[string]ToneStyle, data json.RawMessage) (map[string]ToneStyle, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, errors.New("tones is an object of tones by name")
	}
	out := maps.Clone(base)
	if out == nil {
		out = map[string]ToneStyle{}
	}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		if name == "" || strings.ContainsAny(name, " \t\n") {
			return nil, fmt.Errorf("tone %q: a tone name is one word", name)
		}
		var t ToneStyle
		if err := json.Unmarshal(raw[name], &t); err != nil {
			return nil, fmt.Errorf("tone %s: %v", name, err)
		}
		for _, c := range []string{t.Fg.Light, t.Fg.Dark} {
			if c != "" && !validColor(c) && !slices.Contains(toneRoles, c) {
				return nil, fmt.Errorf("tone %s: %q is not #RRGGBB, #RGB, an ANSI index 0-255 or a role (%s)", name, c, strings.Join(toneRoles, ", "))
			}
		}
		out[name] = t
	}
	return out, nil
}

// toneLook is a tone resolved for one frame: its colour (nil: none) and
// attributes.
type toneLook struct {
	fg        color.Color
	bold, dim bool
}

// resolveTones turns a scheme's tones into looks: each Fg side a colour or
// the named role's same side, picked as the scheme picks its roles.
func resolveTones(scheme Scheme, plain bool, pick func(SchemeColor) color.Color) map[string]toneLook {
	if len(scheme.Tones) == 0 {
		return nil
	}
	roles := map[string]SchemeColor{"text": scheme.Text, "muted": scheme.Muted, "accent": scheme.Accent, "border": scheme.Border, "surface": scheme.Surface, "error": scheme.Error}
	side := func(v string, light bool) string {
		if role, ok := roles[v]; ok {
			if light {
				return role.Light
			}
			return role.Dark
		}
		return v
	}
	out := make(map[string]toneLook, len(scheme.Tones))
	for name, t := range scheme.Tones {
		look := toneLook{bold: t.Bold, dim: t.Dim}
		if !plain && (t.Fg.Light != "" || t.Fg.Dark != "") {
			c := SchemeColor{side(t.Fg.Light, true), side(t.Fg.Dark, false)}
			if c.Light == "" {
				c.Light = c.Dark
			}
			if c.Dark == "" {
				c.Dark = c.Light
			}
			look.fg = pick(c)
		}
		out[name] = look
	}
	return out
}

// Tone is the style a tone draws in outside a list (an application's own
// line, a footer): its colour and attributes, the empty style for an
// unknown tone; mono and NO_COLOR keep the attributes alone.
func (s Styles) Tone(name string) lipgloss.Style {
	st := lipgloss.NewStyle()
	look, ok := s.tones[name]
	if !ok {
		return st
	}
	if look.fg != nil {
		st = st.Foreground(look.fg)
	}
	return st.Bold(look.bold).Faint(look.dim)
}

// toneSGR is a tone's attributes as a list draws them: on the selected
// row its bold alone; in a faded list (dim-unfocused) its colour carried
// toward the background as the list's text is. ok is false for an unknown
// tone, which draws plain.
func (s Styles) toneSGR(name string, selected, faded bool) (ansi.Style, bool) {
	look, ok := s.tones[name]
	if !ok {
		return nil, false
	}
	var st ansi.Style
	if look.bold {
		st = st.Bold()
	}
	if selected {
		return st, true
	}
	if look.dim {
		st = st.Faint()
	}
	if fg := look.fg; fg != nil {
		if faded && s.ground != nil && s.unfocused > 0 {
			fg = fadeRamp(fg, s.ground)[min(s.unfocused, MaxDim)]
		}
		st = st.ForegroundColor(fg)
	}
	return st, true
}

// toneSeg is a tone on a drawn line: from cell x, w cells.
type toneSeg struct {
	x, w int
	tone string
}

// spansOf is the row's tones of cell i.
func spansOf(tones []ToneSpan, i int) []ToneSpan {
	var out []ToneSpan
	for _, t := range tones {
		if t.Cell == i && t.Tone != "" {
			out = append(out, t)
		}
	}
	return out
}

// cellSegs places a cell's tones on its line: text is the cell's full
// drawn text, x where its first cell sits and shown how many of its cells
// the line shows (a cut cell's "…" counts as shown).
func cellSegs(spans []ToneSpan, text string, x, shown int) []toneSeg {
	if len(spans) == 0 || shown <= 0 {
		return nil
	}
	runes := []rune(text)
	var out []toneSeg
	for _, sp := range spans {
		from, to := max(sp.From, 0), sp.To
		if to <= 0 || to > len(runes) {
			to = len(runes)
		}
		if from >= to {
			continue
		}
		start := ansi.StringWidth(string(runes[:from]))
		end := min(start+ansi.StringWidth(string(runes[from:to])), shown)
		if start >= end {
			continue
		}
		out = append(out, toneSeg{x: x + start, w: end - start, tone: sp.Tone})
	}
	return out
}

// listTones is the styles a list's tones draw in, nil when there is none
// to draw (no row carries a tone) or the list is drawn without the
// Model's styles.
func (l *List) listTones() *Styles {
	if !slices.ContainsFunc(l.items, func(it ListItem) bool { return len(it.Tones) > 0 }) {
		return nil
	}
	styles := l.r.Settings().page.styles
	if styles == nil {
		return nil
	}
	s := styles()
	if len(s.tones) == 0 {
		return nil
	}
	return &s
}

// paintLine draws a plain line with its tones: on the selected row the
// line is padded to width and reversed, the current cell (cur) reversed
// and underlined; elsewhere trailing spaces are trimmed and only the toned
// runs are styled. Each run is written as one SGR sequence and closed, so
// a tone never leaks into the next cell.
func paintLine(line string, width int, segs []toneSeg, st *Styles, selected, faded bool, cur span) string {
	if selected {
		line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	} else {
		line = strings.TrimRight(line, " ")
	}
	n := ansi.StringWidth(line)
	cuts := []int{0, n}
	for _, sg := range segs {
		cuts = append(cuts, sg.x, sg.x+sg.w)
	}
	if selected && cur.w > 0 {
		cuts = append(cuts, cur.x, cur.x+cur.w)
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	var b strings.Builder
	for k := 0; k+1 < len(cuts); k++ {
		a, z := cuts[k], cuts[k+1]
		if a >= n {
			break
		}
		z = min(z, n)
		text := ansi.Cut(line, a, z)
		var style ansi.Style
		if selected {
			if cur.w > 0 && a >= cur.x && a < cur.x+cur.w {
				style = style.Underline(true)
			}
			style = style.Reverse(true)
		}
		for j := len(segs) - 1; j >= 0; j-- {
			if sg := segs[j]; a >= sg.x && a < sg.x+sg.w {
				if tone, ok := st.toneSGR(sg.tone, selected, faded); ok {
					style = append(style, tone...)
				}
				break
			}
		}
		if len(style) == 0 {
			b.WriteString(text)
			continue
		}
		b.WriteString(style.String() + text + ansi.ResetStyle)
	}
	return b.String()
}
