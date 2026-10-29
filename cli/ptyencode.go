package cli

import (
	"strconv"

	uv "github.com/charmbracelet/ultraviolet"
)

// The legacy xterm key encoding, for an engine that leaves encoding to its
// host (tuios.dev/vt) when the program asked for neither the kitty
// keyboard protocol nor modifyOtherKeys. Adapted from tuios's
// internal/input getRawKeyBytesWithMode and charmbracelet/x/vt's SendKey
// as tables: a key is a CSI final letter, a CSI number with "~", or an SS3
// letter; a modifier makes it CSI 1;m X or CSI n;m ~, as xterm does.

// csiFinals are the keys that are CSI (or SS3 under application cursor
// keys) and one letter.
var csiFinals = map[rune]byte{
	uv.KeyUp: 'A', uv.KeyDown: 'B', uv.KeyRight: 'C', uv.KeyLeft: 'D',
	uv.KeyHome: 'H', uv.KeyEnd: 'F', uv.KeyBegin: 'E',
}

// ss3Finals are F1 to F4: SS3 and a letter, CSI 1;m letter with modifiers.
var ss3Finals = map[rune]byte{uv.KeyF1: 'P', uv.KeyF2: 'Q', uv.KeyF3: 'R', uv.KeyF4: 'S'}

// tildeKeys are CSI number "~".
var tildeKeys = map[rune]int{
	uv.KeyInsert: 2, uv.KeyDelete: 3, uv.KeyPgUp: 5, uv.KeyPgDown: 6,
	uv.KeyF5: 15, uv.KeyF6: 17, uv.KeyF7: 18, uv.KeyF8: 19, uv.KeyF9: 20,
	uv.KeyF10: 21, uv.KeyF11: 23, uv.KeyF12: 24,
}

// ctrlSymbols are the control codes of ctrl with a symbol.
var ctrlSymbols = map[rune]byte{
	' ': 0x00, '@': 0x00, '2': 0x00, '[': 0x1b, '3': 0x1b, '\\': 0x1c, '4': 0x1c,
	']': 0x1d, '5': 0x1d, '^': 0x1e, '6': 0x1e, '_': 0x1f, '7': 0x1f, '/': 0x1f,
	'?': 0x7f, '8': 0x7f,
	uv.KeyBackspace: 0x08, uv.KeyEnter: '\r', uv.KeyTab: '\t', uv.KeyEscape: 0x1b,
}

// keyModParam is xterm's modifier parameter: 1 + shift 1, alt 2, ctrl 4,
// meta 8.
func keyModParam(m uv.KeyMod) int {
	p := 1
	for bit, add := range map[uv.KeyMod]int{uv.ModShift: 1, uv.ModAlt: 2, uv.ModCtrl: 4, uv.ModMeta: 8} {
		if m&bit != 0 {
			p += add
		}
	}
	return p
}

// legacyKey is the bytes xterm sends for k; appCursor is DECCKM. A key it
// cannot encode is "".
func legacyKey(k uv.KeyPressEvent, appCursor bool) string {
	mod := k.Mod & (uv.ModShift | uv.ModAlt | uv.ModCtrl | uv.ModMeta)
	if f, ok := csiFinals[k.Code]; ok {
		switch {
		case mod != 0:
			return "\x1b[1;" + strconv.Itoa(keyModParam(mod)) + string(f)
		case appCursor:
			return "\x1bO" + string(f)
		}
		return "\x1b[" + string(f)
	}
	if f, ok := ss3Finals[k.Code]; ok {
		if mod != 0 {
			return "\x1b[1;" + strconv.Itoa(keyModParam(mod)) + string(f)
		}
		return "\x1bO" + string(f)
	}
	if n, ok := tildeKeys[k.Code]; ok {
		if mod != 0 {
			return "\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(keyModParam(mod)) + "~"
		}
		return "\x1b[" + strconv.Itoa(n) + "~"
	}
	if k.Code == uv.KeyTab && mod&uv.ModShift != 0 {
		return "\x1b[Z"
	}
	// The rest is one character or control code, ESC first with alt.
	prefix := ""
	if mod&uv.ModAlt != 0 {
		prefix, mod = "\x1b", mod&^uv.ModAlt
	}
	if mod&uv.ModCtrl != 0 {
		switch c := k.Code; {
		case c >= 'a' && c <= 'z':
			return prefix + string(rune(c-'a'+1))
		case c >= 'A' && c <= 'Z':
			return prefix + string(rune(c-'A'+1))
		}
		if b, ok := ctrlSymbols[k.Code]; ok {
			return prefix + string(rune(b))
		}
		return ""
	}
	switch {
	case k.Text != "":
		return prefix + k.Text
	case k.Code < uv.KeyExtended:
		return prefix + string(k.Code)
	}
	return ""
}
