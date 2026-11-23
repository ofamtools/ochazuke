package tutorial

import (
	"fmt"
	"html"
	"strings"
)

// Default terminal colors used for reverse-video swapping when fg or bg is unset.
const (
	defaultTermFG = "#e6edf3"
	defaultTermBG = "#0d1117"
)

// Standard 16-color terminal palette (0-7 normal, 8-15 bright), tuned for a
// dark terminal background.
var ansi16Palette = [16]string{
	"#21262d", // 0: black
	"#f85149", // 1: red
	"#3fb950", // 2: green
	"#d29922", // 3: yellow
	"#58a6ff", // 4: blue
	"#bc8cff", // 5: magenta
	"#39c5cf", // 6: cyan
	"#c9d1d9", // 7: white
	"#6e7681", // 8: bright black (gray)
	"#ff7b72", // 9: bright red
	"#56d364", // 10: bright green
	"#e3b341", // 11: bright yellow
	"#79c0ff", // 12: bright blue
	"#d2a8ff", // 13: bright magenta
	"#56d4dd", // 14: bright cyan
	"#f0f6fc", // 15: bright white
}

type ansiStyle struct {
	fg            string // CSS hex color or ""
	bg            string // CSS hex color or ""
	bold          bool
	dim           bool
	italic        bool
	underline     bool
	reverse       bool
	strikethrough bool
}

func (s *ansiStyle) reset() {
	*s = ansiStyle{}
}

func (s *ansiStyle) css() string {
	fg, bg := s.fg, s.bg
	if s.reverse {
		effFG := bg
		if effFG == "" {
			effFG = defaultTermBG
		}
		effBG := fg
		if effBG == "" {
			effBG = defaultTermFG
		}
		fg, bg = effFG, effBG
	}

	var parts [6]string
	n := 0
	if fg != "" {
		parts[n] = "color:" + fg
		n++
	}
	if bg != "" {
		parts[n] = "background-color:" + bg
		n++
	}
	if s.bold {
		parts[n] = "font-weight:700"
		n++
	}
	if s.dim {
		parts[n] = "opacity:0.65"
		n++
	}
	if s.italic {
		parts[n] = "font-style:italic"
		n++
	}
	switch {
	case s.underline && s.strikethrough:
		parts[n] = "text-decoration:underline line-through"
		n++
	case s.underline:
		parts[n] = "text-decoration:underline"
		n++
	case s.strikethrough:
		parts[n] = "text-decoration:line-through"
		n++
	}
	if n == 0 {
		return ""
	}
	return strings.Join(parts[:n], ";")
}

func (s *ansiStyle) applySGR(paramStr string) {
	if paramStr == "" {
		s.reset()
		return
	}
	params := strings.Split(paramStr, ";")
	for i := 0; i < len(params); i++ {
		p := params[i]
		if p == "" || isAllZeros(p) {
			s.reset()
			continue
		}
		if p == "38" || p == "48" {
			i = s.applyExtendedSemicolon(p == "38", params, i)
			continue
		}
		if strings.HasPrefix(p, "38:") || strings.HasPrefix(p, "48:") {
			s.applyExtendedColon(strings.HasPrefix(p, "38:"), p)
			continue
		}
		if strings.HasPrefix(p, "4:") {
			s.underline = p != "4:0"
			continue
		}
		n, ok := parseUint(p)
		if !ok {
			continue
		}
		switch {
		case n == 0:
			s.reset()
		case n == 1:
			s.bold = true
		case n == 2:
			s.dim = true
		case n == 3:
			s.italic = true
		case n == 4 || n == 21:
			s.underline = true
		case n == 7:
			s.reverse = true
		case n == 9:
			s.strikethrough = true
		case n == 22:
			s.bold = false
			s.dim = false
		case n == 23:
			s.italic = false
		case n == 24:
			s.underline = false
		case n == 27:
			s.reverse = false
		case n == 29:
			s.strikethrough = false
		case n >= 30 && n <= 37:
			s.fg = ansi16Palette[n-30]
		case n == 39:
			s.fg = ""
		case n >= 40 && n <= 47:
			s.bg = ansi16Palette[n-40]
		case n == 49:
			s.bg = ""
		case n >= 90 && n <= 97:
			s.fg = ansi16Palette[n-90+8]
		case n >= 100 && n <= 107:
			s.bg = ansi16Palette[n-100+8]
		}
	}
}

func (s *ansiStyle) setColor(fg bool, hex string) {
	if fg {
		s.fg = hex
	} else {
		s.bg = hex
	}
}

func (s *ansiStyle) applyExtendedSemicolon(fg bool, params []string, i int) int {
	if i+1 >= len(params) {
		return i
	}
	switch params[i+1] {
	case "5":
		if i+2 >= len(params) {
			return len(params) - 1
		}
		if idx, ok := parseChannel(params[i+2]); ok && idx <= 255 {
			s.setColor(fg, xterm256Hex(idx))
		}
		return i + 2
	case "2":
		if i+4 >= len(params) {
			return len(params) - 1
		}
		r, okR := parseChannel(params[i+2])
		g, okG := parseChannel(params[i+3])
		b, okB := parseChannel(params[i+4])
		if okR && okG && okB {
			s.setColor(fg, rgbHex(r, g, b))
		}
		return i + 4
	default:
		return i + 1
	}
}

func (s *ansiStyle) applyExtendedColon(fg bool, p string) {
	parts := strings.Split(p, ":")
	if len(parts) < 3 {
		return
	}
	switch parts[1] {
	case "5":
		if idx, ok := parseChannel(parts[2]); ok && idx <= 255 {
			s.setColor(fg, xterm256Hex(idx))
		}
	case "2":
		// Supports both 38:2:R:G:B (5 parts) and ITU 38:2:colorspace:R:G:B (6+ parts).
		offset := 2
		if len(parts) >= 6 {
			offset = 3
		}
		if len(parts) < offset+3 {
			return
		}
		r, okR := parseChannel(parts[offset])
		g, okG := parseChannel(parts[offset+1])
		b, okB := parseChannel(parts[offset+2])
		if okR && okG && okB {
			s.setColor(fg, rgbHex(r, g, b))
		}
	}
}

func xterm256Hex(n int) string {
	if n < 0 {
		n = 0
	}
	if n > 255 {
		n = 255
	}
	if n < 16 {
		return ansi16Palette[n]
	}
	if n >= 232 {
		v := 8 + (n-232)*10
		return rgbHex(v, v, v)
	}
	cube := [6]int{0, 95, 135, 175, 215, 255}
	idx := n - 16
	r := cube[idx/36]
	g := cube[(idx/6)%6]
	b := cube[idx%6]
	return rgbHex(r, g, b)
}

func rgbHex(r, g, b int) string {
	r = clampByte(r)
	g = clampByte(g)
	b = clampByte(b)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func clampByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func isAllZeros(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

func parseUint(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		if v < 1000000 {
			v = v*10 + int(c-'0')
		}
	}
	return v, true
}

func parseChannel(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	return parseUint(s)
}

// consumeEscape scans one ECMA-48 escape sequence starting at s[i] == '\x1b'.
// If st is non-nil and the sequence is a valid SGR CSI sequence, st is updated.
// All other CSI, OSC, DCS, SOS, PM, APC, and 2-byte ESC sequences are skipped.
func consumeEscape(s string, i int, st *ansiStyle) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI
		j := i + 2
		sgrOnly := true
		for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3F {
			b := s[j]
			if !((b >= '0' && b <= '9') || b == ';' || b == ':') {
				sgrOnly = false
			}
			j++
		}
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2F {
			sgrOnly = false
			j++
		}
		if j >= len(s) {
			return len(s)
		}
		if s[j] == '\x1b' {
			return j
		}
		if s[j] >= 0x40 && s[j] <= 0x7E {
			if s[j] == 'm' && sgrOnly && st != nil {
				st.applySGR(s[i+2 : j])
			}
			return j + 1
		}
		return j

	case ']': // OSC (terminated by BEL or ST)
		j := i + 2
		for j < len(s) {
			if s[j] == '\x07' {
				return j + 1
			}
			if s[j] == '\x1b' {
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2
				}
				return j
			}
			j++
		}
		return len(s)

	case 'P', 'X', '^', '_': // DCS, SOS, PM, APC (terminated by ST)
		j := i + 2
		for j < len(s) {
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return len(s)

	default:
		b := s[i+1]
		if b >= 0x20 && b <= 0x2F {
			j := i + 2
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2F {
				j++
			}
			if j >= len(s) {
				return len(s)
			}
			if s[j] == '\x1b' {
				return j
			}
			if s[j] >= 0x30 && s[j] <= 0x7E {
				return j + 1
			}
			return j
		}
		if b >= 0x30 && b <= 0x7E {
			return i + 2
		}
		return i + 1
	}
}

// ANSIToHTML converts ANSI terminal output into HTML-escaped spans wrapped in
// <pre class="term">...</pre>. SGR colors (16-color, bright, 256-color, and
// 24-bit RGB) and attributes (bold, dim, italic, underline, reverse,
// strikethrough) are preserved; non-SGR CSI and OSC sequences are stripped.
func ANSIToHTML(ansi string) string {
	var out strings.Builder
	out.Grow(len(ansi) + 64)
	out.WriteString(`<pre class="term">`)

	var st ansiStyle
	var textBuf strings.Builder
	activeCSS := ""

	flush := func() {
		if textBuf.Len() == 0 {
			return
		}
		escaped := html.EscapeString(textBuf.String())
		textBuf.Reset()
		if activeCSS != "" {
			out.WriteString(`<span style="`)
			out.WriteString(activeCSS)
			out.WriteString(`">`)
			out.WriteString(escaped)
			out.WriteString(`</span>`)
		} else {
			out.WriteString(escaped)
		}
	}

	for i := 0; i < len(ansi); {
		if ansi[i] == '\x1b' {
			next := consumeEscape(ansi, i, &st)
			newCSS := st.css()
			if newCSS != activeCSS {
				flush()
				activeCSS = newCSS
			}
			i = next
			continue
		}
		if ansi[i] == '\r' {
			// Normalize CRLF and drop stray carriage returns.
			if i+1 < len(ansi) && ansi[i+1] == '\n' {
				i++
				continue
			}
			i++
			continue
		}
		textBuf.WriteByte(ansi[i])
		i++
	}
	flush()

	out.WriteString(`</pre>`)
	return out.String()
}

// StripANSI removes all ANSI CSI, OSC, and ESC sequences from s.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, '\x1b') {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			i = consumeEscape(s, i, nil)
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
