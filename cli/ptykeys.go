package cli

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

// keyCodes names the non-printing keys as bindings and Bubble Tea spell
// them. It is the stream's encoder table, the inverse of the key names a
// terminal instance forwards; the emulator turns each event into the bytes
// the program's modes ask for. Function keys are here so a pane can receive
// them; ochazuke binds none.
var keyCodes = map[string]rune{
	"enter": uv.KeyEnter, "tab": uv.KeyTab, "backspace": uv.KeyBackspace,
	"esc": uv.KeyEscape, "escape": uv.KeyEscape, "space": uv.KeySpace,
	"up": uv.KeyUp, "down": uv.KeyDown, "left": uv.KeyLeft, "right": uv.KeyRight,
	"insert": uv.KeyInsert, "delete": uv.KeyDelete, "home": uv.KeyHome, "end": uv.KeyEnd,
	"pgup": uv.KeyPgUp, "pgdown": uv.KeyPgDown,
	"f1": uv.KeyF1, "f2": uv.KeyF2, "f3": uv.KeyF3, "f4": uv.KeyF4, "f5": uv.KeyF5, "f6": uv.KeyF6,
	"f7": uv.KeyF7, "f8": uv.KeyF8, "f9": uv.KeyF9, "f10": uv.KeyF10, "f11": uv.KeyF11, "f12": uv.KeyF12,
}

// keyMods are the modifier words of a key name ("ctrl+alt+x").
var keyMods = map[string]uv.KeyMod{"ctrl": uv.ModCtrl, "alt": uv.ModAlt, "shift": uv.ModShift, "meta": uv.ModMeta}

// keyEvent parses one key name: modifiers joined by "+" before a named key
// or one character ("ctrl+a", "shift+tab", "alt+b", "A", "+", "é").
func keyEvent(name string) (uv.KeyPressEvent, error) {
	var ev uv.KeyPressEvent
	rest := name
	for {
		mod, tail, ok := strings.Cut(rest, "+")
		if !ok || tail == "" || keyMods[mod] == 0 {
			break
		}
		ev.Mod |= keyMods[mod]
		rest = tail
	}
	if code, ok := keyCodes[rest]; ok {
		ev.Code = code
		return ev, nil
	}
	r, size := utf8.DecodeRuneInString(rest)
	if rest == "" || size != len(rest) || r == utf8.RuneError {
		return ev, fmt.Errorf("unknown key %q", name)
	}
	if ev.Mod&uv.ModShift != 0 && ev.Mod&^uv.ModShift == 0 {
		r, ev.Mod = unicode.ToUpper(r), 0
	}
	ev.Code = unicode.ToLower(r)
	if ev.Mod&uv.ModCtrl == 0 {
		ev.Code = r
		if ev.Mod == 0 {
			ev.Text = string(r)
		}
	}
	return ev, nil
}
