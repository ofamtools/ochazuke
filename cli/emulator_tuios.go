package cli

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"tuios.dev/vt"
)

// The tuios-vt engine is tuios.dev/vt's pure-Go emulator, the one every
// tuios pane runs, and the default.
func init() {
	_ = RegisterTerminalEngine(TerminalEngine{
		Name: "tuios-vt", Description: "tuios.dev/vt, pure Go",
		New: func(cols, rows int) Emulator { return &tuiosEmulator{t: vt.NewEmulator(cols, rows)} },
	})
}

// tuiosEmulator adapts a tuios.dev/vt Terminal (the pure-Go emulator, or
// libghostty under -tags ghostty). The library leaves key and paste
// encoding to its host, so this adapter does it as tuios does: the kitty
// keyboard protocol or modifyOtherKeys when the program asked for one,
// otherwise the legacy xterm encoding (legacyKey); a paste is sanitized
// and bracketed when the program enabled bracketed paste.
type tuiosEmulator struct{ t vt.Terminal }

func (x *tuiosEmulator) Write(p []byte) (int, error) { return x.t.Write(p) }
func (x *tuiosEmulator) Read(p []byte) (int, error)  { return x.t.Read(p) }
func (x *tuiosEmulator) Resize(cols, rows int)       { x.t.Resize(cols, rows) }
func (x *tuiosEmulator) Render() string              { return x.t.Render() }
func (x *tuiosEmulator) Close() error                { return x.t.Close() }

func (x *tuiosEmulator) Cursor() EmulatorCursor {
	pos := x.t.CursorPosition()
	style, _ := x.t.CursorStyle()
	return EmulatorCursor{X: pos.X, Y: pos.Y, Shape: tuiosShapes[style], Hidden: x.t.IsCursorHidden()}
}

// tuiosShapes names tuios.dev/vt's cursor styles; the block is "".
var tuiosShapes = map[vt.CursorStyle]string{vt.CursorBar: "bar", vt.CursorUnderline: "underline"}

func (x *tuiosEmulator) SendKey(k uv.KeyPressEvent) {
	flags := x.t.KittyKeyboardFlags()
	out := vt.EncodePaneKey(k, flags, x.t.ModifyOtherKeys())
	if out == "" {
		out = legacyKey(k, x.t.ApplicationCursorKeys())
	}
	// A program that asked for event types gets the release too: no
	// release follows a sent key.
	if out != "" && flags&ansi.KittyReportEventTypes != 0 {
		out += vt.EncodeKeyReleaseCSIu(k, flags)
	}
	if out != "" {
		x.t.WriteResponse([]byte(out))
	}
}

func (x *tuiosEmulator) Paste(text string) {
	text = vt.SanitizePaste(text)
	if x.t.BracketedPasteEnabled() {
		text = ansi.BracketedPasteStart + text + ansi.BracketedPasteEnd
	}
	x.t.WriteResponse([]byte(text))
}
