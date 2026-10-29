//go:build ghostty

package cli

import "tuios.dev/vt"

// With -tags ghostty the ghostty engine is tuios.dev/vt backed by
// libghostty-vt through cgo, behind the same adapter as tuios-vt.
func init() {
	_ = RegisterTerminalEngine(TerminalEngine{
		Name: "ghostty", Description: "libghostty-vt through tuios.dev/vt, cgo",
		New: func(cols, rows int) Emulator { return &tuiosEmulator{t: vt.NewGhosttyTerminal(cols, rows)} },
	})
}
