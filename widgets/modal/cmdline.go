package modal

import (
	"fmt"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/commandline"
)

// CommandLineOptions configures a CommandLineModal.
type CommandLineOptions struct {
	ID          string
	Scope       string
	Mode        string
	FocusID     string
	Title       string
	TitleSuffix string
	Tone        string
	WidthPct    int
	HeightPct   int
	VarHintKeys []string
	VarHintMax  int
	Badges      []ui.Badge
	Facts       map[string]any
	OnSubmit    func(sub commandline.Submission) Result
	OnCancel    func() Result
	OnCommand   func(cl *commandline.Model, cmd core.Command) (Result, bool)
	OnClose     func(reason CloseReason)
}

// CommandLineModal adapts commandline.Model to the Modal interface.
type CommandLineModal struct {
	cl      *commandline.Model
	opts    CommandLineOptions
	id      string
	scope   string
	mode    string
	focusID string
}

// NewCommandLineModal wraps cl in a CommandLineModal using opts.
func NewCommandLineModal(cl *commandline.Model, opts CommandLineOptions) *CommandLineModal {
	if cl == nil {
		cl = commandline.New(commandline.PrefixCommand)
	}
	id := opts.ID
	if id == "" {
		id = "commandline"
	}
	scope := opts.Scope
	if scope == "" {
		scope = "commandline"
	}
	mode := opts.Mode
	if mode == "" {
		mode = ModeCommandLine
	}
	focusID := opts.FocusID
	if focusID == "" {
		focusID = id + ".input"
	}
	return &CommandLineModal{
		cl:      cl,
		opts:    opts,
		id:      id,
		scope:   scope,
		mode:    mode,
		focusID: focusID,
	}
}

// Model returns the underlying command-line model.
func (m *CommandLineModal) Model() *commandline.Model { return m.cl }

// ID returns the modal identifier.
func (m *CommandLineModal) ID() string { return m.id }

// Mode returns the modal input mode.
func (m *CommandLineModal) Mode() string { return m.mode }

// Scope returns the modal overlay scope.
func (m *CommandLineModal) Scope() string { return m.scope }

// FocusID returns the focused input ViewNode ID.
func (m *CommandLineModal) FocusID() string { return m.focusID }

// View renders the command line (and optional variable hints) inside an Overlay.
func (m *CommandLineModal) View(w, h int) ui.ViewNode {
	title := m.opts.Title
	if title == "" {
		title = defaultCommandLineTitle(m.cl.Prefix)
	}
	if m.opts.TitleSuffix != "" {
		title += " · " + m.opts.TitleSuffix
	}
	wp := m.opts.WidthPct
	if wp <= 0 {
		wp = 78
	}

	children := []ui.ViewNode{m.cl.View(m.focusID)}
	if hints := m.varHints(); len(hints) > 0 {
		children = append(children, ui.ViewNode{
			ID:    m.id + ".vars",
			Kind:  ui.KindText,
			Tone:  "muted",
			Box:   layout.Constraints{FixedH: 1},
			Props: map[string]any{"text": " " + strings.Join(hints, "  ")},
		})
	}

	return Overlay(OverlaySpec{
		ID:        m.id,
		Scope:     m.scope,
		Title:     title,
		Tone:      m.opts.Tone,
		WidthPct:  wp,
		HeightPct: m.opts.HeightPct,
		Badges:    m.opts.Badges,
		Facts:     m.opts.Facts,
		Children:  children,
	})
}

// Handle dispatches a command to the command-line modal.
func (m *CommandLineModal) Handle(cmd core.Command) Result {
	if m.opts.OnCommand != nil {
		if res, ok := m.opts.OnCommand(m.cl, cmd); ok {
			return res
		}
	}
	if cmd.ID == CmdCancel {
		_, _ = m.cl.HandleCommand(core.Command{ID: commandline.CmdCancel})
		return m.cancelResult()
	}

	sub, handled := m.cl.HandleCommand(cmd)
	if !handled {
		return Result{}
	}
	switch cmd.ID {
	case commandline.CmdCancel:
		return m.cancelResult()
	case commandline.CmdSubmit:
		if m.opts.OnSubmit != nil {
			res := m.opts.OnSubmit(sub)
			res.Handled = true
			return res
		}
		return Result{Handled: true, Close: true, CloseReason: CloseSubmit}
	}
	return Result{Handled: true}
}

// OnClose notifies the configured close hook.
func (m *CommandLineModal) OnClose(reason CloseReason) {
	if m.opts.OnClose != nil {
		m.opts.OnClose(reason)
	}
}

func (m *CommandLineModal) cancelResult() Result {
	if m.opts.OnCancel != nil {
		res := m.opts.OnCancel()
		res.Handled = true
		res.Close = true
		res.CloseReason = CloseCancel
		return res
	}
	return Result{
		Handled:     true,
		Close:       true,
		CloseReason: CloseCancel,
		SetStatus:   true,
	}
}

func (m *CommandLineModal) varHints() []string {
	if len(m.opts.VarHintKeys) == 0 || len(m.cl.Vars) == 0 {
		return nil
	}
	maxLen := m.opts.VarHintMax
	if maxLen <= 0 {
		maxLen = 28
	}
	var hints []string
	for _, k := range m.opts.VarHintKeys {
		if v := m.cl.Vars[k]; v != "" {
			hints = append(hints, fmt.Sprintf("$%s=%s", k, clipRunes(v, maxLen)))
		}
	}
	return hints
}

func defaultCommandLineTitle(p commandline.Prefix) string {
	switch p {
	case commandline.PrefixShell:
		return "Shell (!)"
	case commandline.PrefixPipe:
		return "Pipe (|)"
	case commandline.PrefixReadShell:
		return "Read shell (r!)"
	case commandline.PrefixSearch:
		return "Search (/)"
	case commandline.PrefixSearchBack:
		return "Search backward (?)"
	case commandline.PrefixCommand:
		return "Command (:)"
	default:
		if p == "" {
			return "Command"
		}
		return fmt.Sprintf("Command (%s)", p)
	}
}

func clipRunes(s string, maxLen int) string {
	rs := []rune(s)
	if len(rs) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(rs[:maxLen-1]) + "…"
}
