package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/unixsuperhero/ochazuke"
)

// Emulator is one terminal emulator: what a program writes goes in
// (Write), the screen and cursor come out (Render, Cursor), and the bytes
// the program should read (its replies to queries, and the keys and
// pastes sent to it) come out of Read. A PtyStream and an application's
// live screen (mtm's) drive one. Every engine is an adapter behind this
// interface, registered as a TerminalEngine row; nothing outside the
// adapters knows which engine runs.
//
// An Emulator is not safe for concurrent use except that Read may run on
// its own goroutine while one other goroutine calls the rest.
type Emulator interface {
	// Write feeds the program's output to the screen.
	Write(p []byte) (int, error)
	// Read returns bytes for the program; it blocks until there are some
	// and returns io.EOF after Close.
	Read(p []byte) (int, error)
	// Resize sizes the screen to cols x rows cells.
	Resize(cols, rows int)
	// Render is the screen as lines joined by "\n", styles as SGR.
	Render() string
	// Cursor is the cursor cell, shape and visibility.
	Cursor() EmulatorCursor
	// SendKey queues a key press for the program, encoded for the modes it
	// set (application cursor keys, the kitty keyboard protocol when the
	// engine has it).
	SendKey(key uv.KeyPressEvent)
	// Paste queues text for the program, bracketed when it enabled
	// bracketed paste.
	Paste(text string)
	// Close stops the emulator and ends a blocked Read.
	Close() error
}

// EmulatorCursor is an emulator's cursor: its cell, its shape as
// ochazuke.TerminalFrame.Shape names it ("" block, "bar", "underline")
// and whether the program hid it.
type EmulatorCursor struct {
	X, Y   int
	Shape  string
	Hidden bool
}

// EmulatorFrame is the emulator's screen and cursor as a terminal frame.
func EmulatorFrame(e Emulator) ochazuke.TerminalFrame {
	lines := strings.Split(e.Render(), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	c := e.Cursor()
	return ochazuke.TerminalFrame{Lines: lines, Cursor: ochazuke.Pos{X: c.X, Y: c.Y}, Shape: c.Shape, Hidden: c.Hidden}
}

// TerminalEngine is one emulator implementation, a row of the engine
// registry: its name (the terminal.engine setting's value), a line for
// the Settings page, and New, which makes an emulator of cols x rows
// cells. Unavailable, when set, says why this build cannot run it (a
// build tag it needs); such a row is not offered and the setting refuses
// it with that reason.
type TerminalEngine struct {
	Name        string
	Description string
	New         func(cols, rows int) Emulator
	Unavailable string
}

// Engine names and the setting that picks one.
const (
	// TerminalEngineSetting names the emulator newly opened terminals
	// use. A terminal keeps the engine it opened with.
	TerminalEngineSetting = "terminal.engine"
	// DefaultTerminalEngine is tuios.dev/vt, pure Go.
	DefaultTerminalEngine = "tuios-vt"
)

var engines struct {
	mu   sync.Mutex
	rows []TerminalEngine
}

// RegisterTerminalEngine adds an engine row. A name is registered once;
// a later row of an unavailable name replaces it (a build tag's file
// supplies the engine its stub announced).
func RegisterTerminalEngine(e TerminalEngine) error {
	if e.Name == "" || strings.ContainsAny(e.Name, " \t\n") {
		return fmt.Errorf("terminal engine name %q must be one word", e.Name)
	}
	if e.New == nil && e.Unavailable == "" {
		return fmt.Errorf("terminal engine %s needs New or Unavailable", e.Name)
	}
	engines.mu.Lock()
	defer engines.mu.Unlock()
	i := slices.IndexFunc(engines.rows, func(r TerminalEngine) bool { return r.Name == e.Name })
	switch {
	case i < 0:
		engines.rows = append(engines.rows, e)
		slices.SortFunc(engines.rows, func(a, b TerminalEngine) int { return strings.Compare(a.Name, b.Name) })
	case engines.rows[i].Unavailable != "":
		engines.rows[i] = e
	default:
		return fmt.Errorf("terminal engine %s is already registered", e.Name)
	}
	return nil
}

// TerminalEngines lists the registered engines by name, unavailable ones
// included (their Unavailable says why).
func TerminalEngines() []TerminalEngine {
	engines.mu.Lock()
	defer engines.mu.Unlock()
	return slices.Clone(engines.rows)
}

// availableEngines names the engines this build can run.
func availableEngines() []string {
	var names []string
	for _, e := range TerminalEngines() {
		if e.Unavailable == "" {
			names = append(names, e.Name)
		}
	}
	return names
}

// engineRow finds a registered engine.
func engineRow(name string) (TerminalEngine, bool) {
	for _, e := range TerminalEngines() {
		if e.Name == name {
			return e, true
		}
	}
	return TerminalEngine{}, false
}

// checkEngine is nil for an engine this build runs, else why not.
func checkEngine(name string) error {
	e, ok := engineRow(name)
	switch {
	case !ok:
		return fmt.Errorf("unknown terminal engine %q (one of %s)", name, strings.Join(availableEngines(), ", "))
	case e.Unavailable != "":
		return fmt.Errorf("terminal engine %s is not in this build: %s", name, e.Unavailable)
	}
	return nil
}

// NewEmulator makes an emulator of the named engine ("" is
// DefaultTerminalEngine) with cols x rows cells (at least 1 x 1).
func NewEmulator(engine string, cols, rows int) (Emulator, error) {
	if engine == "" {
		engine = DefaultTerminalEngine
	}
	if err := checkEngine(engine); err != nil {
		return nil, err
	}
	e, _ := engineRow(engine)
	return e.New(max(1, cols), max(1, rows)), nil
}

// EnsureTerminalEngine declares the terminal.engine setting unless the
// application declared it: its values are the engines this build runs,
// its default DefaultTerminalEngine, and a registered engine this build
// lacks (ghostty without -tags ghostty) is refused with the reason. An
// application with terminals calls it at setup (AddCommands does); the
// value applies to terminals opened after it changes.
func EnsureTerminalEngine(r *ochazuke.Runtime) {
	if r == nil || slices.ContainsFunc(r.Settings().Declared(), func(s ochazuke.Setting) bool { return s.Name == TerminalEngineSetting }) {
		return
	}
	_ = r.DeclareSetting(ochazuke.Setting{
		Name: TerminalEngineSetting, Type: ochazuke.SettingString, Default: DefaultTerminalEngine,
		Description: "Terminal emulator for newly opened terminals: " + engineSummary(),
		Enum:        availableEngines(),
		Validate: func(v any) error {
			name, _ := v.(string)
			if e, ok := engineRow(name); ok && e.Unavailable != "" {
				return errors.New(e.Unavailable)
			}
			return nil
		},
	})
}

// TerminalEngineOf is the engine a runtime's terminals open with: the
// terminal.engine setting when declared, else the default.
func TerminalEngineOf(r *ochazuke.Runtime) string {
	if r != nil {
		if name := r.Settings().String(TerminalEngineSetting); name != "" {
			return name
		}
	}
	return DefaultTerminalEngine
}

// engineSummary is the available engines with their descriptions.
func engineSummary() string {
	var parts []string
	for _, e := range TerminalEngines() {
		if e.Unavailable == "" {
			parts = append(parts, e.Name+" ("+e.Description+")")
		}
	}
	return strings.Join(parts, ", ")
}
