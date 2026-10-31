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
//     and a table's current cell draws in list.cell.cursor, out of the
//     bar (setting list.cell-cursor: background), or underlined inside
//     it (underline).
//   - A page list without the focus fades its tones with it
//     (dim-unfocused): the tone's colour is carried the same percent toward
//     the background.
//   - A tone with a background (ToneStyle.Bg; the built-in "error" tone)
//     fills its run's cells; a span's own Bg names the tone whose
//     background goes under it, whatever its Tone. mono and NO_COLOR draw
//     no background. On the selected row the background is dropped, as
//     the colour is (setting tones.selected-background: drop), or kept
//     with the run drawn out of the reverse bar (keep).

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
	// ToneSession marks a setting's session value on the Settings page
	// (set this session, not written): bold in every theme, mono too.
	ToneSession = "marker.session"
	// ToneTreeGuide draws a tree's branch lines, dim in every built-in theme.
	ToneTreeGuide = "tree.guide"
	// ToneError marks a cell that is wrong (a duplicate key, a value that
	// does not parse): bold on an error background in every built-in
	// theme, bold alone in mono.
	ToneError = "error"
	// ToneChip draws a chip (a query term, a tag) on a dim background;
	// ToneChipFocused is the chip the focus is on, bright: the scheme's
	// surface on its accent, bold. mono draws the focused chip bold.
	ToneChip        = "chip"
	ToneChipFocused = "chip.focused"
	// ToneRecordInverted marks an Invertible record field in its not state.
	ToneRecordInverted = "record.inverted"
	// The table parts: a data cell where two highlight layers cross
	// (ToneSpan.Layer), the header row, a row-number column
	// (Column.Part PartRowNumber), and each part's crossing tone (the
	// part's tone plus IntersectSuffix).
	ToneCellIntersect = "list.cell.intersect"
	// ToneCellCursor is a cell-mode table's current cell on the selected
	// row, drawn out of the reverse bar (setting list.cell-cursor
	// background, the default).
	ToneCellCursor         = "list.cell.cursor"
	ToneHeader             = "list.header"
	ToneHeaderIntersect    = "list.header.intersect"
	ToneRowNumber          = "list.rownum"
	ToneRowNumberIntersect = "list.rownum.intersect"
)

// IntersectSuffix names a part's crossing tone: the part's tone with it.
const IntersectSuffix = ".intersect"

// toneCell is the part tone of a data cell, whose crossing tone is
// list.cell.intersect; it draws nothing of its own.
const toneCell = "list.cell"

// LayerSelected is the layer the selected row counts as where layers
// cross.
const LayerSelected = "selected"

// ToneSpan gives a run of one cell a tone. Cell is the column index (in a
// list without columns, and a tree without them, 0 is the label); From and
// To are rune offsets into the cell's text as drawn (escape sequences,
// which a cell never draws, do not count), To 0 meaning the cell's end, so
// {Cell, Tone} alone tones the whole cell. A later span wins where two
// overlap; a span outside the text draws nothing. Bg names a tone (a theme
// token) whose background goes under the span while Tone keeps the
// foreground and attributes; Bg alone, with no Tone, only fills. Where
// spans overlap, the foreground comes from the last span with a Tone and
// the background from the last span that has one.
//
// Layer names the highlight the span belongs to ("column", "row",
// "search": any word the application picks); a span without one is a
// plain tone. Highlights compose: where two or more layers cover a cell
// (the selected row is the layer "selected", a non-data part such as the
// header or a row-number column is a layer named after its part), the
// cell takes the intersect tone of its part (list.cell.intersect for a
// data cell, list.header.intersect, list.rownum.intersect), so a
// crossing reads at a glance (setting list.intersect, default on). A
// whole-cell layer span (From and To 0) fills the column's whole width,
// not only its text.
type ToneSpan struct {
	Cell  int    `json:"cell"`
	Tone  string `json:"tone,omitempty"`
	Bg    string `json:"bg,omitempty"`
	From  int    `json:"from,omitempty"`
	To    int    `json:"to,omitempty"`
	Layer string `json:"layer,omitempty"`
	// part is the part tone a framework span draws a non-data part in
	// (list.header, list.rownum); "" for a span of the application's.
	part string
}

// LayerTone tones a whole cell as one layer of highlight (ToneSpan.Layer).
func LayerTone(cell int, tone, layer string) ToneSpan {
	return ToneSpan{Cell: cell, Tone: tone, Layer: layer}
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
// so the tone follows the scheme; empty keeps the text's colour. Bg is the
// background, a colour or role the same way; empty draws none. Bold and
// Dim (faint) are the attributes, the only part mono draws. In themes.json
// a tone is {"fg": ..., "bg": ..., "bold": true, "dim": true}, or a colour
// or role alone ("#5FAFD7", "error").
type ToneStyle struct {
	Fg   SchemeColor `json:"fg"`
	Bg   SchemeColor `json:"bg,omitzero"`
	Bold bool        `json:"bold,omitempty"`
	Dim  bool        `json:"dim,omitempty"`
}

// UnmarshalJSON reads a colour or role alone, or {"fg", "bg", "bold", "dim"}.
func (t *ToneStyle) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*t = ToneStyle{Fg: SchemeColor{one, one}}
		return nil
	}
	var full struct {
		Fg   *SchemeColor `json:"fg"`
		Bg   *SchemeColor `json:"bg"`
		Bold bool         `json:"bold"`
		Dim  bool         `json:"dim"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&full); err != nil {
		return errors.New(`a tone is a colour, a role, or {"fg": ..., "bg": ..., "bold": true, "dim": true}`)
	}
	*t = ToneStyle{Bold: full.Bold, Dim: full.Dim}
	if full.Fg != nil {
		t.Fg = *full.Fg
	}
	if full.Bg != nil {
		t.Bg = *full.Bg
	}
	return nil
}

// toneRoles are the palette roles a tone's Fg may name; mode is the
// current mode's badge colour (Styles.WithChrome), resolved per frame.
var toneRoles = []string{"text", "muted", "accent", "border", "surface", "error", "mode"}

// defaultTones are the tones every built-in theme draws; a user scheme
// starts from them. The state tones keep their meaning without colour:
// needs-input and errored are bold, exited is dim, so mono reads them too.
var defaultTones = map[string]ToneStyle{
	ToneTreeGuide:      {Fg: SchemeColor{"muted", "muted"}, Dim: true},
	ToneWorking:        {Fg: SchemeColor{"#1F6FB2", "#7CB7E8"}},
	ToneNeedsInput:     {Fg: SchemeColor{"#9A6700", "#E3B341"}, Bold: true},
	ToneDone:           {Fg: SchemeColor{"#2E7D32", "#8BC48A"}},
	ToneErrored:        {Fg: SchemeColor{"error", "error"}, Bold: true},
	ToneIdle:           {Fg: SchemeColor{"muted", "muted"}},
	ToneExited:         {Fg: SchemeColor{"muted", "muted"}, Dim: true},
	ToneStar:           {Fg: SchemeColor{"#B08800", "#F2CC60"}, Bold: true},
	ToneQueued:         {Fg: SchemeColor{"accent", "accent"}},
	ToneSession:        {Bold: true},
	ToneError:          {Bg: SchemeColor{"#F6D3CE", "#5E2722"}, Bold: true},
	ToneChip:           {Bg: SchemeColor{"#E2EAE5", "#26362F"}},
	ToneChipFocused:    {Fg: SchemeColor{"surface", "surface"}, Bg: SchemeColor{"accent", "accent"}, Bold: true},
	ToneRecordInverted: {Fg: SchemeColor{"#B54708", "#FF9B70"}},
	// Table parts and crossings (ToneSpan.Layer, Column.Part): a crossing
	// is a warm fill unlike any other tone, the header a band, the row
	// numbers muted. mono draws the attributes alone: a crossing is bold
	// out of the reverse bar.
	ToneCellIntersect: {Fg: SchemeColor{"#1A1A1A", "#1A1A1A"}, Bg: SchemeColor{"#F2C94C", "#E0B243"}, Bold: true},
	// The current cell: the scheme's surface on its accent, bold, so it
	// reads as the focus inside the bar; mono draws it bold out of the bar.
	ToneCellCursor:         {Fg: SchemeColor{"surface", "surface"}, Bg: SchemeColor{"accent", "accent"}, Bold: true},
	ToneHeader:             {Fg: SchemeColor{"text", "text"}, Bg: SchemeColor{"#E4E8EE", "#2B323B"}, Bold: true},
	ToneHeaderIntersect:    {Fg: SchemeColor{"#1A1A1A", "#1A1A1A"}, Bg: SchemeColor{"#E5B53A", "#C99A2E"}, Bold: true},
	ToneRowNumber:          {Fg: SchemeColor{"muted", "muted"}},
	ToneRowNumberIntersect: {Fg: SchemeColor{"surface", "surface"}, Bg: SchemeColor{"muted", "muted"}, Bold: true},
	// A binding's arguments in the ? help (help_args.go).
	ToneHelpArgKey:   {Bold: true},
	ToneHelpArgValue: {},
	// The markdown package's note boxes (markdown.NoteTone).
	"markdown.note": {Fg: SchemeColor{"#875f00", "#d7af5f"}},
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
		for _, c := range []string{t.Fg.Light, t.Fg.Dark, t.Bg.Light, t.Bg.Dark} {
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
	fg, bg    color.Color
	bold, dim bool
	// fgMode and bgMode: the side names the mode role, resolved per frame
	// to the mode's colour (Styles.moded).
	fgMode, bgMode bool
}

// resolveTones turns a scheme's tones into looks: each Fg side a colour or
// the named role's same side, picked as the scheme picks its roles. The
// mode role is left to Styles.moded; in a chrome row (chrome.*) an
// application's accent (appAccent, nil for none) stands for the accent
// and border roles, as NewStyles makes it stand for them.
func resolveTones(scheme Scheme, plain bool, pick func(SchemeColor) color.Color, appAccent color.Color) map[string]toneLook {
	if len(scheme.Tones) == 0 {
		return nil
	}
	tones := withModeChrome(scheme.Tones)
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
	colour := func(v SchemeColor, chrome bool) (color.Color, bool) {
		if plain || (v.Light == "" && v.Dark == "") {
			return nil, false
		}
		if v.Light == "mode" || v.Dark == "mode" {
			return nil, true
		}
		if chrome && appAccent != nil && (slices.Contains([]string{"accent", "border"}, v.Light) || slices.Contains([]string{"accent", "border"}, v.Dark)) {
			return appAccent, false
		}
		c := SchemeColor{side(v.Light, true), side(v.Dark, false)}
		if c.Light == "" {
			c.Light = c.Dark
		}
		if c.Dark == "" {
			c.Dark = c.Light
		}
		return pick(c), false
	}
	out := make(map[string]toneLook, len(tones))
	for name, t := range tones {
		look := toneLook{bold: t.Bold, dim: t.Dim}
		chrome := isChromeTone(name)
		look.fg, look.fgMode = colour(t.Fg, chrome)
		look.bg, look.bgMode = colour(t.Bg, chrome)
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
	look = s.moded(look)
	if look.fg != nil {
		st = st.Foreground(look.fg)
	}
	if look.bg != nil {
		st = st.Background(look.bg)
	}
	return st.Bold(look.bold).Faint(look.dim)
}

// hasTone reports whether the styles draw the tone name.
func (s Styles) hasTone(name string) bool {
	_, ok := s.tones[name]
	return ok
}

// tonedText draws text in base with each run spans tone in its tone's
// style (Styles.Tone): From and To are rune offsets (To 0: the end), Cell
// is not read, a later span wins, an unknown tone keeps base. The text is
// unchanged, only styled.
func (s Styles) tonedText(text string, spans []ToneSpan, base lipgloss.Style) string {
	if len(spans) == 0 {
		return base.Render(text)
	}
	runes := []rune(text)
	tones := make([]string, len(runes))
	for _, sp := range spans {
		to := sp.To
		if to <= 0 || to > len(runes) {
			to = len(runes)
		}
		for i := max(0, sp.From); i < to; i++ {
			if s.hasTone(sp.Tone) {
				tones[i] = sp.Tone
			}
		}
	}
	var b strings.Builder
	for i := 0; i < len(runes); {
		j := i + 1
		for j < len(runes) && tones[j] == tones[i] {
			j++
		}
		style := base
		if tones[i] != "" {
			style = s.Tone(tones[i])
		}
		b.WriteString(style.Render(string(runes[i:j])))
		i = j
	}
	return b.String()
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
	look = s.moded(look)
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

// toneSeg is a tone on a drawn line: from cell x, w cells; bg the tone
// whose background fills it ("" the tone's own).
type toneSeg struct {
	x, w     int
	tone, bg string
	// layer is the span's highlight layer ("" none); part the part tone
	// a framework span marks its cells with ("" a data cell); underline
	// draws the run underlined (a header's current column).
	layer, part string
	underline   bool
}

// CellCursorSetting says how a cell-mode table draws its current cell on
// the selected row: background (the default: the cell in the
// list.cell.cursor tone, out of the reverse bar) or underline (reversed
// and underlined inside the bar, as before).
const CellCursorSetting = "list.cell-cursor"

// The values of CellCursorSetting.
const (
	CellCursorBackground = "background"
	CellCursorUnderline  = "underline"
)

// CellCursor is the list.cell-cursor setting's value (default
// background).
func (r *Runtime) CellCursor() string {
	if v, _ := r.settingValue(CellCursorSetting); v == CellCursorUnderline {
		return CellCursorUnderline
	}
	return CellCursorBackground
}

// cellCursor is the SGR a cell-mode table's current cell draws in on the
// selected row: the list.cell.cursor tone's background behind its
// foreground, bold, faded with a faded list. It is written as reverse
// video with the two colours swapped, so the cell stays part of the
// selection's reverse run (a story reads it as the current cell: reverse
// with a background, which nothing else draws). nil, with
// list.cell-cursor underline, without the Model's styles, when the theme
// has no such tone or the tone has no background (mono, NO_COLOR), draws
// the cell reversed and underlined inside the bar.
func (r *Runtime) cellCursor(faded bool) ansi.Style {
	if r.CellCursor() != CellCursorBackground {
		return nil
	}
	s := r.toneStyles()
	if s == nil {
		return nil
	}
	look, ok := s.tones[ToneCellCursor]
	look = s.moded(look)
	if !ok || look.bg == nil {
		return nil
	}
	fade := func(c color.Color) color.Color {
		if faded && s.ground != nil && s.unfocused > 0 {
			return fadeRamp(c, s.ground)[min(s.unfocused, MaxDim)]
		}
		return c
	}
	var style ansi.Style
	style = style.Reverse(true)
	if look.bold {
		style = style.Bold()
	}
	style = style.ForegroundColor(fade(look.bg))
	if look.fg != nil {
		style = style.BackgroundColor(fade(look.fg))
	}
	return style
}

// bgSGR is the background a segment fills with: its Bg tone's, else its
// tone's; faded toward the ground in a faded list. ok is false for none.
func (s Styles) bgSGR(sg toneSeg, faded bool) (ansi.Style, bool) {
	name := sg.bg
	if name == "" {
		name = sg.tone
	}
	look, ok := s.tones[name]
	look = s.moded(look)
	if !ok || look.bg == nil {
		return nil, false
	}
	bg := look.bg
	if faded && s.ground != nil && s.unfocused > 0 {
		bg = fadeRamp(bg, s.ground)[min(s.unfocused, MaxDim)]
	}
	var st ansi.Style
	return st.BackgroundColor(bg), true
}

// spansOf is the row's tones of cell i.
func spansOf(tones []ToneSpan, i int) []ToneSpan {
	var out []ToneSpan
	for _, t := range tones {
		if t.Cell == i && (t.Tone != "" || t.Bg != "") {
			out = append(out, t)
		}
	}
	return out
}

// cellSegs places a cell's tones on its line: text is the cell's full
// drawn text, x where its first cell sits and shown how many of its cells
// the line shows (a cut cell's "…" counts as shown).
func cellSegs(spans []ToneSpan, text string, x, shown int) []toneSeg {
	return slotSegs(spans, text, x, shown, cellAt{x: x, w: shown, sx: x, sw: shown})
}

// slotSegs is cellSegs in a column's slot: a whole-cell layer or part
// span (From and To 0) fills the slot (sx, sw), the column's whole width,
// rather than only the text.
func slotSegs(spans []ToneSpan, text string, x, shown int, slot cellAt) []toneSeg {
	if len(spans) == 0 {
		return nil
	}
	runes := []rune(text)
	var out []toneSeg
	for _, sp := range spans {
		if (sp.Layer != "" || sp.part != "") && sp.From <= 0 && sp.To <= 0 && slot.sw > 0 {
			out = append(out, toneSeg{x: slot.sx, w: slot.sw, tone: sp.Tone, bg: sp.Bg, layer: sp.Layer, part: sp.part})
			continue
		}
		if shown <= 0 {
			continue
		}
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
		out = append(out, toneSeg{x: x + start, w: end - start, tone: sp.Tone, bg: sp.Bg, layer: sp.Layer, part: sp.part})
	}
	return out
}

// listTones is the styles a list's tones draw in, nil when there is none
// to draw (no row carries a tone, no column is a part, no header tones)
// or the list is drawn without the Model's styles.
func (l *List) listTones() *Styles {
	if !l.wantsTones() {
		return nil
	}
	return l.r.toneStyles()
}

// toneStyles is the Model's styles for drawing ToneSpans (a list's rows,
// a chooser's): nil without a Model's styles or with no tone in the
// theme; the selected background and intersect settings filled in.
func (r *Runtime) toneStyles() *Styles {
	styles := r.Settings().page.styles
	if styles == nil {
		return nil
	}
	s := styles()
	if len(s.tones) == 0 {
		return nil
	}
	s.selectedBg = r.SelectedBackground()
	s.intersect = r.settingOn(IntersectSetting)
	return &s
}

// wantsTones reports whether the list draws any tone: a row's, the
// header's (SetHeaderTones, or the header part while list.part-tones is
// on), or a part column's.
func (l *List) wantsTones() bool {
	if slices.ContainsFunc(l.items, func(it ListItem) bool { return len(it.Tones) > 0 }) || len(l.headerTones) > 0 {
		return true
	}
	return len(l.columns) > 0 && l.r.settingOn(PartTonesSetting)
}

// settingOn is a boolean setting's value, true when it is not declared.
func (r *Runtime) settingOn(name string) bool {
	v, ok := r.settingValue(name)
	if !ok {
		return true
	}
	on, isBool := v.(bool)
	return !isBool || on
}

// IntersectSetting: where two or more highlight layers cover a table
// cell (ToneSpan.Layer, the selected row, a non-data part) the cell
// takes its part's intersect tone; off draws the layers as before, the
// later span winning (default on).
const IntersectSetting = "list.intersect"

// PartTonesSetting: a table's header row and its part columns
// (Column.Part, a row-number column) draw in their part tones
// (list.header, list.rownum) so they never read as data; off draws the
// header bold and the part columns as data, as before (default on).
const PartTonesSetting = "list.part-tones"

// paintLine draws a plain line with its tones: on the selected row the
// line is padded to width and reversed, the current cell (cur) in
// Styles.cellCursor (list.cell.cursor, out of the bar) or, when that is
// nil, reversed and underlined; elsewhere trailing spaces are trimmed (kept under a
// layer or part span, which fills its slot) and only the toned runs are
// styled. Where two or more layers cover a run (the selected row counts
// as one, Styles.intersect on), the run draws in its part's intersect
// tone instead, out of the reverse bar. Each run is written as one SGR
// sequence and closed, so a tone never leaks into the next cell.
func paintLine(line string, width int, segs []toneSeg, st *Styles, selected, faded bool, cur span) string {
	if selected {
		line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	} else {
		line = strings.TrimRight(line, " ")
		fill := 0
		for _, sg := range segs {
			if sg.layer != "" || sg.part != "" {
				fill = max(fill, sg.x+sg.w)
			}
		}
		if fill = min(fill, width); fill > ansi.StringWidth(line) {
			line += strings.Repeat(" ", fill-ansi.StringWidth(line))
		}
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
		onCur := selected && cur.w > 0 && a >= cur.x && a < cur.x+cur.w
		if onCur && st != nil && len(st.cellCursor) > 0 {
			b.WriteString(st.cellCursor.String() + text + ansi.ResetStyle)
			continue
		}
		underline := onCur
		for _, sg := range segs {
			if sg.underline && a >= sg.x && a < sg.x+sg.w {
				underline = true
			}
		}
		if cross, ok := st.crossing(segs, a, selected, faded); ok {
			if underline {
				cross = cross.Underline(true)
			}
			b.WriteString(cross.String() + text + ansi.ResetStyle)
			continue
		}
		var style ansi.Style
		var fill ansi.Style
		for j := len(segs) - 1; j >= 0; j-- {
			if sg := segs[j]; a >= sg.x && a < sg.x+sg.w {
				if bg, ok := st.bgSGR(sg, faded); ok {
					fill = bg
					break
				}
			}
		}
		keep := fill != nil && selected && st.selectedBg == SelectedBackgroundKeep
		if underline {
			style = style.Underline(true)
		}
		if selected && !keep {
			style = style.Reverse(true)
		}
		if fill != nil && (!selected || keep) {
			style = append(style, fill...)
		}
		for j := len(segs) - 1; j >= 0; j-- {
			if sg := segs[j]; sg.tone != "" && a >= sg.x && a < sg.x+sg.w {
				if tone, ok := st.toneSGR(sg.tone, selected && !keep, faded); ok {
					style = append(style, tone...)
				}
				break
			}
		}
		if keep && !slices.Contains(style, "1") {
			style = style.Bold()
		}
		if len(style) == 0 {
			b.WriteString(text)
			continue
		}
		b.WriteString(style.String() + text + ansi.ResetStyle)
	}
	return b.String()
}

// crossing is the style of the run at cell a when two or more highlight
// layers cover it: the selected row (LayerSelected), each distinct
// ToneSpan.Layer, a part span's part. The run takes its part's intersect
// tone (list.cell.intersect for a data cell), its colours, background and
// attributes, out of the reverse bar. ok is false under one layer or
// none, with list.intersect off, or when the theme has no such tone.
func (s *Styles) crossing(segs []toneSeg, a int, selected, faded bool) (ansi.Style, bool) {
	if s == nil || !s.intersect {
		return nil, false
	}
	layers := map[string]bool{}
	if selected {
		layers[LayerSelected] = true
	}
	part := toneCell
	for _, sg := range segs {
		if a < sg.x || a >= sg.x+sg.w {
			continue
		}
		if sg.layer != "" {
			layers[sg.layer] = true
		}
		if sg.part != "" {
			part = sg.part
		}
	}
	if len(layers) < 2 {
		return nil, false
	}
	name := part + IntersectSuffix
	if !s.hasTone(name) {
		return nil, false
	}
	style, _ := s.toneSGR(name, false, faded)
	if bg, ok := s.bgSGR(toneSeg{tone: name}, faded); ok {
		style = append(style, bg...)
	}
	if !slices.Contains(style, "1") {
		style = style.Bold()
	}
	return style, true
}

// SelectedBackgroundSetting says how the selected row draws a run with a
// tone background: drop (the default: the row stays one reverse bar, the
// background left out as the colour is) or keep (the run keeps its
// background and colour, bold, out of the reverse bar, so an error cell
// still reads as one on the selected row).
const SelectedBackgroundSetting = "tones.selected-background"

// The values of SelectedBackgroundSetting.
const (
	SelectedBackgroundDrop = "drop"
	SelectedBackgroundKeep = "keep"
)

// SelectedBackground is the tones.selected-background setting's value
// (a row of settings/ochazuke.json, declared when the runtime is made,
// default drop).
func (r *Runtime) SelectedBackground() string {
	if v, _ := r.settingValue(SelectedBackgroundSetting); v == SelectedBackgroundKeep {
		return SelectedBackgroundKeep
	}
	return SelectedBackgroundDrop
}
