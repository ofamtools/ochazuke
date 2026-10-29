//go:build !ghostty

package cli

// Without -tags ghostty the ghostty engine is a row that says how to get
// it, so the terminal.engine setting can refuse it with that reason.
func init() {
	_ = RegisterTerminalEngine(TerminalEngine{
		Name: "ghostty", Description: "libghostty-vt through tuios.dev/vt, cgo",
		Unavailable: "build with -tags ghostty (libghostty-vt through cgo, needs the Zig toolchain)",
	})
}
