package ochazuke

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const (
	ChromeBorderSetting = "chrome.border"
	ChromeGlyphsSetting = "chrome.glyphs"
)

// Border is the panel or focused border selected by chrome.border. The zero
// Styles value uses rounded panels and joined heavy focused corners.
func (s Styles) Border(focused bool) lipgloss.Border {
	row, ok := chromeBorders[s.borderSet]
	if !ok {
		row = chromeBorders["rounded"]
	}
	if focused {
		return row.focused
	}
	return row.panel
}

var chromeBorders = func() map[string]struct{ panel, focused lipgloss.Border } {
	heavy := lipgloss.ThickBorder()
	classic := lipgloss.RoundedBorder()
	classic.Top, classic.Bottom, classic.Left, classic.Right = "━", "━", "┃", "┃"
	return map[string]struct{ panel, focused lipgloss.Border }{
		"rounded": {lipgloss.RoundedBorder(), heavy},
		"square":  {lipgloss.NormalBorder(), heavy},
		"classic": {lipgloss.RoundedBorder(), classic},
	}
}()

// WithBorder selects a chrome.border row without changing border colours,
// padding or cell sizes. Unknown values use rounded.
func (s Styles) WithBorder(set string) Styles {
	s.borderSet = set
	s.Panel = s.Panel.Border(s.Border(false))
	s.FocusedPanel = s.FocusedPanel.Border(s.Border(true))
	return s
}

// GlyphRow gives a role its Unicode and opt-in Nerd Font marks. Both must be
// one cell. ASCII is a frame conversion, not another value per role.
type GlyphRow struct {
	Unicode string `json:"unicode"`
	Nerd    string `json:"nerd"`
}

// GlyphRows are application roles declared by Appearance.SetGlyph.
// A pointer keeps Appearance comparable.
type GlyphRows struct {
	rows       map[string]GlyphRow
	generation uint64
}

// SetGlyph declares an application's role. Marks must occupy one cell and
// the Unicode value may not require an emoji or private-use font.
func (a *Appearance) SetGlyph(role string, row GlyphRow) error {
	if role == "" || !unicodeGlyph(row.Unicode) || ansi.StringWidth(row.Unicode) != 1 || ansi.StringWidth(row.Nerd) != 1 || strings.ContainsAny(row.Nerd, "\x1b\n\r\t") {
		return fmt.Errorf("glyph %q needs one-cell Unicode and Nerd marks", role)
	}
	if a.Glyphs == nil {
		a.Glyphs = &GlyphRows{rows: map[string]GlyphRow{}}
	}
	a.Glyphs.rows[role] = row
	a.Glyphs.generation++
	return nil
}

func unicodeGlyph(mark string) bool {
	if mark == "" {
		return false
	}
	for _, r := range mark {
		if !(r >= 0x20 && r <= 0x7e || r >= 0x2000 && r <= 0x206f || r >= 0x2500 && r <= 0x25ff || r == '✓') {
			return false
		}
	}
	return true
}

// FontAwesome codepoints confirmed against tuios's go-nf/glyphs/fa:
// Gem, AngleRight, AngleLeft, ChevronDown, ChevronRight, SortAsc,
// SortDesc, Circle and CircleO. No font package is needed at runtime.
var chromeGlyphs = map[string]GlyphRow{
	"title.mark":     {"◆", "\uf219"},
	"tab.active":     {"›", "\uf105"},
	"tab.more.left":  {"‹", "\uf104"},
	"tab.more.right": {"›", "\uf105"},
	"fold.open":      {"▾", "\uf078"},
	"fold.shut":      {"▸", "\uf054"},
	"sort.asc":       {"▲", "\uf0de"},
	"sort.desc":      {"▼", "\uf0dd"},
	"check.on":       {"●", "\uf111"},
	"check.off":      {"○", "\uf10c"},
}

// Glyph reads a role in the chosen set. Auto and ASCII use the Unicode
// row; ASCII conversion happens once at Model.View's exit. Unknown roles
// return an empty string.
func (s Styles) Glyph(role string) string {
	row := chromeGlyphs[role]
	if s.glyphRows != nil {
		if own, ok := s.glyphRows.rows[role]; ok {
			row = own
		}
	}
	if s.glyphSet == "nerd" {
		return row.Nerd
	}
	return row.Unicode
}

func (r *Runtime) glyph(role string) string {
	return (Styles{glyphSet: r.Settings().String(ChromeGlyphsSetting)}).Glyph(role)
}

// DetectGlyphEnv follows LC_ALL, LC_CTYPE, LANG precedence. No locale is
// unknown, not C. Unlike tuios, TERM=linux also uses ASCII, never Nerd Font.
// Call only for a terminal; redirected output deliberately stays Unicode.
func DetectGlyphEnv(getenv func(string) string) (set, reason string) {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v := getenv(key)
		if v == "" {
			continue
		}
		lower := strings.ToLower(v)
		if !strings.Contains(lower, "utf-8") && !strings.Contains(lower, "utf8") {
			return "ascii", key + "=" + v + " is not a UTF-8 locale"
		}
		break
	}
	if getenv("TERM") == "linux" {
		return "ascii", "TERM=linux is the Linux console"
	}
	return "unicode", "UTF-8 or unspecified locale; Nerd Font is opt-in"
}

func (a Appearance) resolvedGlyphs(configured string) string {
	if configured != "" && configured != "auto" {
		return configured
	}
	if a.glyphEnv != "" {
		return a.glyphEnv
	}
	return "unicode"
}

func (a Appearance) glyphChoice(configured string) (string, string) {
	if configured != "" && configured != "auto" {
		return configured, "explicit chrome.glyphs=" + configured
	}
	if a.glyphEnv != "" {
		return a.glyphEnv, a.glyphReason
	}
	return "unicode", "stdout is not a terminal; locale detection skipped"
}

func registerGlyphs(r *Runtime, a *Appearance) {
	if term.IsTerminal(os.Stdout.Fd()) {
		a.glyphEnv, a.glyphReason = DetectGlyphEnv(os.Getenv)
	}
	// Keep direct NewStyles(*appearance) consumers on the same settings as
	// Model.Styles; shape must not depend on which style entry point an app uses.
	// Construction must not read settings.json: hosts finish their config
	// directory and declarations before Init. current reads only memory.
	settings := r.Settings()
	a.borderSet, _ = settings.current(*settings.find(ChromeBorderSetting)).(string)
	a.glyphSet, _ = settings.current(*settings.find(ChromeGlyphsSetting)).(string)
	r.Settings().Observe(ChromeBorderSetting, func(_ string, value any) { a.borderSet, _ = value.(string) })
	r.Settings().Observe(ChromeGlyphsSetting, func(_ string, value any) { a.glyphSet, _ = value.(string) })
	r.defineFacts(factsSpec{Noun: "ochazuke.glyph", Sample: glyphFact{}, Key: "name", Label: "name",
		Docs: map[string]string{"name": "The glyph set: auto, ascii, unicode or nerd", "active": "The set in effect", "resolved": "The set auto resolves to", "reason": "Why auto chose it"},
		List: func(Ctx, ListQuery) ([]Thing, error) {
			active, reason := a.glyphChoice(r.Settings().String(ChromeGlyphsSetting))
			var rows []glyphFact
			for _, set := range []string{"auto", "ascii", "unicode", "nerd"} {
				rows = append(rows, glyphFact{Name: set, Active: set == active, Resolved: active, Reason: reason})
			}
			return factThings(rows, nil)
		}})
	r.Register(Operation{Key: "ochazuke.glyph.show", HistoryKind: HistoryFetch, Description: "Show every glyph role and codepoint; nerd needs a Nerd Font Mono", Parameters: map[string]Parameter{"set": {Type: "string", Enum: []string{"auto", "ascii", "unicode", "nerd"}}}, Positional: []string{"set"}, Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
		set := StringArg(args, "set", "")
		if set == "" || set == "auto" {
			set, _ = a.glyphChoice(r.Settings().String(ChromeGlyphsSetting))
		}
		if !slices.Contains([]string{"ascii", "unicode", "nerd"}, set) {
			return nil, fmt.Errorf("unknown glyph set %q", set)
		}
		s := Styles{glyphSet: set, glyphRows: a.Glyphs}
		roles := make(map[string]GlyphRow, len(chromeGlyphs))
		for role, row := range chromeGlyphs {
			roles[role] = row
		}
		if a.Glyphs != nil {
			for role, row := range a.Glyphs.rows {
				roles[role] = row
			}
		}
		rows := make([]map[string]any, 0, len(roles))
		for _, role := range sortedKeys(roles) {
			value := s.Glyph(role)
			if set == "ascii" {
				value = asciiFrame(value)
			}
			var points []string
			for _, char := range value {
				points = append(points, fmt.Sprintf("U+%04X", char))
			}
			rows = append(rows, map[string]any{"role": role, "set": set, "value": value, "codepoints": points})
		}
		return rows, nil
	}})
}

var asciiGlyphs = func() map[string]string {
	out := map[string]string{}
	for _, group := range []struct{ chars, value string }{
		{"╭╮╰╯┌┐└┘┏┓┗┛├┤┣┫┼┬┴╋┳┻", "+"},
		{"─┄╌–—", "-"}, {"━", "="}, {"│┃▌", "|"}, {"┆", ":"},
		{"◆●★✦•", "*"}, {"○◦", "o"}, {"›▸→⤷", ">"}, {"‹←↵", "<"},
		{"▾▼", "v"}, {"▲", "^"}, {"·", "."}, {"…", "~"}, {"✓✗×", "x"}, {"📌🔒", "* "},
	} {
		for _, char := range group.chars {
			out[string(char)] = group.value
		}
	}
	return out
}()

// asciiFrame decodes graphemes and ANSI sequences together: an unsupported
// grapheme becomes one ? per display cell, not per rune. SGR stays intact.
func asciiFrame(text string) string {
	if !strings.ContainsFunc(text, func(r rune) bool { return r > 127 }) {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	state := byte(0)
	for len(text) > 0 {
		seq, width, n, next := ansi.DecodeSequence(text, state, nil)
		state = next
		if n == 0 {
			break
		}
		switch {
		case !strings.ContainsFunc(seq, func(r rune) bool { return r > 127 }):
			out.WriteString(seq)
		case asciiGlyphs[seq] != "" && len(asciiGlyphs[seq]) == width:
			out.WriteString(asciiGlyphs[seq])
		default:
			for range width {
				out.WriteByte('?')
			}
		}
		text = text[n:]
	}
	return out.String()
}

// glyphFact is a glyph set as a thing.
type glyphFact struct {
	Name     string `json:"name"`
	Active   bool   `json:"active"`
	Resolved string `json:"resolved"`
	Reason   string `json:"reason"`
}
