package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"ochazuke"
)

const keyHistorySize = 200

type keybindingModel struct {
	width, height int
	history       [keyHistorySize]string
	results       [keyHistorySize]string
	runtime       *ochazuke.Runtime
	count         int
	sandbox       bool
	selected      int
	form          string
	fields        []textinput.Model
	field         int
	editing       int
	notice        string
	path          string
	picker        filepicker.Model
}

func (m *keybindingModel) Init() tea.Cmd { return nil }

func (m *keybindingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.resizeForm()
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if !m.sandbox {
		return m.updateSetup(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+g" {
		m.sandbox = false
		if m.runtime != nil {
			m.runtime.CancelPendingBinding()
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case ochazuke.BindingTimeoutMsg:
		m.handleTimeout(msg)
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey records one key and returns the commands (quit or timer) it needs.
// A multi-rune message is split into single keys whose commands are batched.
func (m *keybindingModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyRunes && !msg.Paste && !msg.Alt && len(msg.Runes) > 1 {
		cmds := make([]tea.Cmd, 0, len(msg.Runes))
		for i := range msg.Runes {
			key := msg
			key.Runes = msg.Runes[i : i+1]
			cmds = append(cmds, m.handleKey(key))
		}
		return tea.Batch(cmds...)
	}
	m.history[m.count%keyHistorySize] = msg.String()
	m.count++
	index := (m.count - 1) % keyHistorySize
	m.results[index] = "unmatched"
	if msg.Type == tea.KeyCtrlC {
		return tea.Quit
	}
	if msg.Paste {
		m.results[index] = "paste (not dispatched)"
		return nil
	}
	if m.runtime == nil {
		return nil
	}
	handled, err := m.runtime.HandleBinding(msg.String())
	switch {
	case err != nil:
		m.results[index] = "error: " + err.Error()
	case m.runtime.Pending != "":
		m.results[index] = "pending: " + m.runtime.Pending
	case handled && m.results[index] == "unmatched":
		m.results[index] = "sequence cancelled"
	}
	return m.runtime.BindingTimeoutCommand()
}

// handleTimeout resolves a deferred sequence in the last key row. Stale
// events leave history and outcomes untouched.
func (m *keybindingModel) handleTimeout(msg ochazuke.BindingTimeoutMsg) {
	if m.runtime == nil || m.count == 0 {
		return
	}
	index := (m.count - 1) % keyHistorySize
	handled, err := m.runtime.HandleBindingTimeout(msg)
	switch {
	case err != nil:
		m.results[index] = "error: " + err.Error()
	case handled && strings.HasPrefix(m.results[index], "pending: "):
		m.results[index] = "sequence timed out"
	}
}

// parseBindingTimeout parses a positive duration such as 400ms or 1s.
func parseBindingTimeout(value string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("timeout must be a duration such as 400ms or 1s")
	}
	if d <= 0 {
		return 0, fmt.Errorf("timeout must be greater than zero")
	}
	return d, nil
}

func (m *keybindingModel) View() string {
	if !m.sandbox {
		return m.setupView()
	}
	st := m.styles()
	layout := m.layout()
	_, bodyHeight := layout.BodySize()
	// outcome colors a result string by kind.
	outcome := func(result string) string {
		result = cleanLabel(result)
		switch {
		case strings.HasPrefix(result, "error"):
			return st.Error.Render(result)
		case strings.HasPrefix(result, "ACTION"):
			return st.Accent.Render(result)
		case strings.HasPrefix(result, "pending"):
			return st.Subtitle.Render(result)
		case result == "unmatched":
			return st.Muted.Render(result)
		}
		return result
	}
	var left []string
	if m.count == 0 {
		left = append(left, st.Muted.Render("Waiting for a key…"))
	} else {
		last := (m.count - 1) % keyHistorySize
		left = append(left, st.Title.Render(strconv.Quote(m.history[last])), outcome(m.results[last]))
	}
	if m.width >= 72 || bodyHeight*2/5 >= 6 {
		left = append(left, "", st.Muted.Render("Key names are quoted."))
		if m.runtime != nil {
			left = append(left, "Timeout: "+m.runtime.BindingTimeout.String(), "", st.Subtitle.Render("Defined bindings"))
			for _, binding := range m.runtime.EffectiveBindings(m.runtime.Context()) {
				left = append(left, cleanLabel(binding.Keys)+st.Muted.Render(" → "+cleanLabel(binding.Request.Verb)))
			}
		}
	}
	var right []string
	if m.count == 0 {
		right = append(right, st.Muted.Render("No keys yet."))
	}
	for i := range min(m.count, keyHistorySize, bodyHeight) {
		n := m.count - 1 - i
		right = append(right, fmt.Sprintf("%s  %s · %s", st.Muted.Render(strconv.Itoa(n+1)), strconv.Quote(m.history[n%keyHistorySize]), outcome(m.results[n%keyHistorySize])))
	}
	return layout.Page("Keybinding sandbox", "Stage 2 · Test keys",
		layout.Split("Latest key", strings.Join(left, "\n"), "History · newest first", strings.Join(right, "\n")),
		"^G setup · ^C exit · last 200 key messages")
}

func runKeybinding(args []string) error {
	m := &keybindingModel{width: 80, height: 24, runtime: ochazuke.NewRuntime(), editing: -1}
	flags := flag.NewFlagSet("keybinding", flag.ContinueOnError)
	config := flags.String("bindings", "", "JSON bindings file to load into setup")
	timeout := flags.Duration("timeout", 400*time.Millisecond, "Wait for a longer key sequence before running the longest completed binding (not saved in JSON)")
	var overrides []string
	flags.Func("bind", "Dummy binding KEYS=ACTION; overrides matching config keys", func(spec string) error {
		overrides = append(overrides, spec)
		return nil
	})
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: ochazuke keybinding [--bindings FILE] [--timeout DURATION] [--bind 'KEYS=ACTION']...")
	}
	if *timeout <= 0 {
		return fmt.Errorf("--timeout must be greater than zero")
	}
	m.runtime.BindingTimeout = *timeout
	if *config != "" {
		if err := m.importBindings(*config); err != nil {
			return fmt.Errorf("loading bindings: %w", err)
		}
		m.path = *config
	}
	for _, spec := range overrides {
		if err := m.addBinding(spec); err != nil {
			return err
		}
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *keybindingModel) addBinding(spec string) error {
	keys, action, ok := strings.Cut(spec, "=")
	keys, action = strings.TrimSpace(keys), strings.TrimSpace(action)
	if !ok || keys == "" || action == "" {
		return fmt.Errorf("binding must be KEYS=ACTION with non-empty keys and action")
	}
	binding := sandboxBinding{Keys: keys, Action: action}
	if err := validateSandboxBinding(binding); err != nil {
		return err
	}
	m.installBinding(binding)
	return nil
}

func (m *keybindingModel) installBinding(spec sandboxBinding) {
	keys, action := strings.Join(strings.Fields(spec.Keys), " "), strings.TrimSpace(spec.Action)
	op := ochazuke.Operation{
		Verb: action, Noun: "sandbox",
		Run: func(_ *ochazuke.Runtime, _ ochazuke.Context, _ ochazuke.Arguments) (any, error) {
			m.results[(m.count-1)%keyHistorySize] = "ACTION: " + action
			return action, nil
		},
	}
	if _, exists := m.runtime.Operations[op.Key()]; !exists {
		m.runtime.Register(op)
	}
	layer := m.runtime.Layers["app"]
	binding := ochazuke.Binding{Keys: keys, Request: ochazuke.Request{Verb: action, Noun: "sandbox"}}
	replaced := false
	for i, existing := range layer.Bindings {
		if existing.Keys == keys {
			layer.Bindings[i] = binding
			replaced = true
			break
		}
	}
	if !replaced {
		layer.Bindings = append(layer.Bindings, binding)
	}
	m.pruneOperations()
}

// pruneOperations drops sandbox operations no binding refers to anymore, so
// replacing a binding does not leave its old action registered.
func (m *keybindingModel) pruneOperations() {
	used := map[string]bool{}
	for _, b := range m.runtime.Layers["app"].Bindings {
		used[ochazuke.Operation{Verb: b.Request.Verb, Noun: b.Request.Noun}.Key()] = true
	}
	for key, op := range m.runtime.Operations {
		if op.Noun == "sandbox" && !used[key] {
			delete(m.runtime.Operations, key)
		}
	}
}
