package buffer

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type sgrState struct {
	bold          bool
	dim           bool
	italic        bool
	underline     string // "", "4", "4:1".."4:5", "21"
	blink         uint8  // 0, 5, 6
	reverse       bool
	conceal       bool
	strikethrough bool
	overline      bool
	fg            string // "", "30".."37", "90".."97", "38;...", "38:..."
	bg            string // "", "40".."47", "100".."107", "48;...", "48:..."
	ulColor       string // "", "58;...", "58:..."
}

func (s *sgrState) reset() {
	*s = sgrState{}
}

func (s *sgrState) apply(paramStr string) {
	if paramStr == "" {
		s.reset()
		return
	}
	params := strings.Split(paramStr, ";")
	for k := 0; k < len(params); k++ {
		p := params[k]
		if p == "" || isAllZeros(p) {
			s.reset()
			continue
		}
		if p == "38" || p == "48" || p == "58" {
			k = s.applyExtendedSemicolon(p, params, k)
			continue
		}
		if strings.HasPrefix(p, "38:") || strings.HasPrefix(p, "48:") || strings.HasPrefix(p, "58:") {
			s.applyExtendedColon(p)
			continue
		}
		if strings.HasPrefix(p, "4:") {
			if p == "4:0" {
				s.underline = ""
			} else if isValidColonUnderline(p) {
				s.underline = strings.Clone(p)
			}
			continue
		}
		n, ok := parseSGRUint(p)
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
		case n == 4:
			s.underline = "4"
		case n == 5 || n == 6:
			s.blink = uint8(n)
		case n == 7:
			s.reverse = true
		case n == 8:
			s.conceal = true
		case n == 9:
			s.strikethrough = true
		case n == 21:
			s.underline = "21"
		case n == 22:
			s.bold = false
			s.dim = false
		case n == 23:
			s.italic = false
		case n == 24:
			s.underline = ""
		case n == 25:
			s.blink = 0
		case n == 27:
			s.reverse = false
		case n == 28:
			s.conceal = false
		case n == 29:
			s.strikethrough = false
		case (n >= 30 && n <= 37) || (n >= 90 && n <= 97):
			s.fg = strconv.Itoa(n)
		case n == 39:
			s.fg = ""
		case (n >= 40 && n <= 47) || (n >= 100 && n <= 107):
			s.bg = strconv.Itoa(n)
		case n == 49:
			s.bg = ""
		case n == 53:
			s.overline = true
		case n == 55:
			s.overline = false
		case n == 59:
			s.ulColor = ""
		}
	}
}

func (s *sgrState) setColorTarget(target, val string) {
	switch target {
	case "38":
		s.fg = val
	case "48":
		s.bg = val
	case "58":
		s.ulColor = val
	}
}

func (s *sgrState) applyExtendedSemicolon(target string, params []string, k int) int {
	if k+1 >= len(params) {
		return k
	}
	sub := params[k+1]
	switch sub {
	case "2":
		if k+4 >= len(params) {
			// Truncated truecolor sequence: consume remaining params so trailing 0s do not reset.
			return len(params) - 1
		}
		r, okR := parseSGRChannel(params[k+2])
		g, okG := parseSGRChannel(params[k+3])
		b, okB := parseSGRChannel(params[k+4])
		if okR && okG && okB {
			s.setColorTarget(target, target+";2;"+strconv.Itoa(r)+";"+strconv.Itoa(g)+";"+strconv.Itoa(b))
		}
		return k + 4
	case "5":
		if k+2 >= len(params) {
			// Truncated 256-color sequence: consume remaining params so trailing 0 does not reset.
			return len(params) - 1
		}
		idx, ok := parseSGRChannel(params[k+2])
		if ok {
			s.setColorTarget(target, target+";5;"+strconv.Itoa(idx))
		}
		return k + 2
	case "0", "1":
		// ITU implementation-defined or transparent: consume submode without triggering reset.
		return k + 1
	default:
		return k + 1
	}
}

func (s *sgrState) applyExtendedColon(p string) {
	parts := strings.Split(p, ":")
	if len(parts) < 2 {
		return
	}
	target := parts[0]
	switch parts[1] {
	case "2":
		// Accepts 38:2:R:G:B (len >= 5) or 38:2:CS:R:G:B (len >= 6)
		if len(parts) < 5 {
			return
		}
		if validColonChannels(parts[2:]) {
			s.setColorTarget(target, strings.Clone(p))
		}
	case "5":
		if len(parts) < 3 {
			return
		}
		if _, ok := parseSGRChannel(parts[2]); ok {
			s.setColorTarget(target, strings.Clone(p))
		}
	case "1":
		s.setColorTarget(target, strings.Clone(p))
	}
}

func validColonChannels(chans []string) bool {
	for _, c := range chans {
		if c == "" {
			continue
		}
		if _, ok := parseSGRUint(c); !ok {
			return false
		}
	}
	return true
}

func (s *sgrState) ansi() string {
	var parts [12]string
	n := 0
	if s.bold {
		parts[n] = "1"
		n++
	}
	if s.dim {
		parts[n] = "2"
		n++
	}
	if s.italic {
		parts[n] = "3"
		n++
	}
	if s.underline != "" {
		parts[n] = s.underline
		n++
	}
	if s.blink != 0 {
		parts[n] = strconv.Itoa(int(s.blink))
		n++
	}
	if s.reverse {
		parts[n] = "7"
		n++
	}
	if s.conceal {
		parts[n] = "8"
		n++
	}
	if s.strikethrough {
		parts[n] = "9"
		n++
	}
	if s.overline {
		parts[n] = "53"
		n++
	}
	if s.fg != "" {
		parts[n] = s.fg
		n++
	}
	if s.bg != "" {
		parts[n] = s.bg
		n++
	}
	if s.ulColor != "" {
		parts[n] = s.ulColor
		n++
	}
	if n == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts[:n], ";") + "m"
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

func isValidColonUnderline(p string) bool {
	if len(p) != 3 || p[0] != '4' || p[1] != ':' {
		return false
	}
	return p[2] >= '1' && p[2] <= '5'
}

func parseSGRUint(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		if v > 1000000 {
			continue
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

func parseSGRChannel(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	return parseSGRUint(s)
}

// parseEscape scans an ECMA-48 escape sequence starting at s[i] == '\x1b'.
// It updates sgr and link for SGR and OSC 8 sequences and returns the index
// immediately after the consumed sequence.
func parseEscape(s string, i int, sgr *sgrState, link *string) int {
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
			if s[j] == 'm' && sgrOnly {
				sgr.apply(s[i+2 : j])
			}
			return j + 1
		}
		return j

	case ']': // OSC
		j := i + 2
		for j < len(s) {
			switch s[j] {
			case '\x07': // BEL
				applyOSC(s[i+2:j], link)
				return j + 1
			case '\x1b':
				if j+1 < len(s) && s[j+1] == '\\' { // ST
					applyOSC(s[i+2:j], link)
					return j + 2
				}
				return j
			}
			j++
		}
		return len(s)

	case 'P', 'X', '^', '_': // DCS, SOS, PM, APC
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
		if b >= 0x20 && b <= 0x2F { // nF ESC
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
		if b >= 0x30 && b <= 0x7E { // 2-byte ESC
			return i + 2
		}
		return i + 1
	}
}

func applyOSC(payload string, link *string) {
	code, rest, ok := strings.Cut(payload, ";")
	if !ok || code != "8" {
		return
	}
	params, uri, ok2 := strings.Cut(rest, ";")
	if !ok2 {
		return
	}
	cleanURI := sanitizeOSCField(uri)
	if cleanURI == "" {
		*link = ""
		return
	}
	cleanParams := sanitizeOSCField(params)
	*link = "\x1b]8;" + cleanParams + ";" + cleanURI + "\x07"
}

func isForbiddenOSCRune(r rune) bool {
	return r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) || r == '\u2028' || r == '\u2029'
}

func sanitizeOSCField(s string) string {
	dirty := false
	for i := 0; i < len(s); {
		if s[i] < 0x80 {
			if s[i] < 0x20 || s[i] == 0x7F {
				dirty = true
				break
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size == 1) || isForbiddenOSCRune(r) {
			dirty = true
			break
		}
		i += size
	}
	if !dirty {
		return strings.Clone(s)
	}

	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] < 0x80 {
			if s[i] >= 0x20 && s[i] != 0x7F {
				sb.WriteByte(s[i])
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		if !isForbiddenOSCRune(r) {
			sb.WriteRune(r)
		}
		i += size
	}
	return sb.String()
}

func isBareSGRParams(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !((b >= '0' && b <= '9') || b == ';' || b == ':') {
			return false
		}
	}
	return true
}

func parseStyleANSI(styleANSI string) (style, link string) {
	if styleANSI == "" {
		return "", ""
	}
	var sgr sgrState
	if !strings.ContainsRune(styleANSI, '\x1b') && isBareSGRParams(styleANSI) {
		sgr.apply(styleANSI)
		return sgr.ansi(), ""
	}
	for i := 0; i < len(styleANSI); {
		if styleANSI[i] == '\x1b' {
			i = parseEscape(styleANSI, i, &sgr, &link)
			continue
		}
		i++
	}
	return sgr.ansi(), link
}
