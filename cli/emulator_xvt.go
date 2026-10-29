package cli

import (
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// The x-vt engine is charmbracelet/x/vt v0.1.0, the emulator ochazuke used
// before tuios.dev/vt: kept so the two can be compared.
func init() {
	_ = RegisterTerminalEngine(TerminalEngine{
		Name: "x-vt", Description: "charmbracelet/x/vt, the earlier emulator",
		New: func(cols, rows int) Emulator { return &xvtEmulator{e: vt.NewEmulator(cols, rows)} },
	})
}

// xvtEmulator adapts x/vt: it encodes keys and pastes itself.
type xvtEmulator struct{ e *vt.Emulator }

func (x *xvtEmulator) Write(p []byte) (int, error) { return x.e.Write(p) }
func (x *xvtEmulator) Read(p []byte) (int, error)  { return x.e.Read(p) }
func (x *xvtEmulator) Resize(cols, rows int)       { x.e.Resize(cols, rows) }
func (x *xvtEmulator) Render() string              { return x.e.Render() }
func (x *xvtEmulator) SendKey(k uv.KeyPressEvent)  { x.e.SendKey(k) }
func (x *xvtEmulator) Paste(text string)           { x.e.Paste(text) }

func (x *xvtEmulator) Cursor() EmulatorCursor {
	pos := x.e.CursorPosition()
	style, _ := x.e.CursorStyle()
	return EmulatorCursor{X: pos.X, Y: pos.Y, Shape: xvtShapes[style], Hidden: x.e.CursorHidden()}
}

// xvtShapes names x/vt's cursor styles; the block is "".
var xvtShapes = map[vt.CursorStyle]string{vt.CursorBar: "bar", vt.CursorUnderline: "underline"}

// Close ends the reply pipe, which ends a blocked Read; x/vt's own Close
// sets a flag its Read checks without a lock.
func (x *xvtEmulator) Close() error {
	if pw, ok := x.e.InputPipe().(*io.PipeWriter); ok {
		_ = pw.CloseWithError(io.EOF)
	}
	return nil
}
