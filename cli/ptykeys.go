package cli

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/vt"
)

// keyCodes names the non-printing keys as bindings and Bubble Tea spell
// them. It is the stream's encoder table, the inverse of the key names a
// terminal instance forwards; the emulator turns each event into the bytes
// the program's modes ask for. Function keys are here so a pane can receive
// them; ochazuke binds none.
var keyCodes = map[string]rune{
	"enter": vt.KeyEnter, "tab": vt.KeyTab, "backspace": vt.KeyBackspace,
	"esc": vt.KeyEscape, "escape": vt.KeyEscape, "space": vt.KeySpace,
	"up": vt.KeyUp, "down": vt.KeyDown, "left": vt.KeyLeft, "right": vt.KeyRight,
	"insert": vt.KeyInsert, "delete": vt.KeyDelete, "home": vt.KeyHome, "end": vt.KeyEnd,
	"pgup": vt.KeyPgUp, "pgdown": vt.KeyPgDown,
	"f1": vt.KeyF1, "f2": vt.KeyF2, "f3": vt.KeyF3, "f4": vt.KeyF4, "f5": vt.KeyF5, "f6": vt.KeyF6,
	"f7": vt.KeyF7, "f8": vt.KeyF8, "f9": vt.KeyF9, "f10": vt.KeyF10, "f11": vt.KeyF11, "f12": vt.KeyF12,
}

// keyMods are the modifier words of a key name ("ctrl+alt+x").
var keyMods = map[string]vt.KeyMod{"ctrl": vt.ModCtrl, "alt": vt.ModAlt, "shift": vt.ModShift, "meta": vt.ModMeta}

// keyEvent parses one key name: modifiers joined by "+" before a named key
// or one character ("ctrl+a", "shift+tab", "alt+b", "A", "+", "é").
func keyEvent(name string) (vt.KeyPressEvent, error) {
	var ev vt.KeyPressEvent
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
	if ev.Mod&vt.ModShift != 0 && ev.Mod&^vt.ModShift == 0 {
		r, ev.Mod = unicode.ToUpper(r), 0
	}
	ev.Code = unicode.ToLower(r)
	if ev.Mod&vt.ModCtrl == 0 {
		ev.Code = r
		if ev.Mod == 0 {
			ev.Text = string(r)
		}
	}
	return ev, nil
}
