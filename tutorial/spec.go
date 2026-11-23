// Package tutorial provides a deterministic, headless tutorial engine for
// Ochazuke applications: declarative tutorial specs, binding-aware key
// resolution, assertion-checked frame capture, ANSI-to-HTML rendering,
// standalone illustrated HTML guide generation, and Charm VHS tape + SRT
// subtitle output.
//
// tutorial is a Tier 2 package: it imports only the Go standard library and
// ofam.dev/ochazuke/core.
package tutorial

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
)

// Default viewport and pacing values applied when omitted in a Spec.
const (
	DefaultWidth   = 120
	DefaultHeight  = 36
	DefaultPauseMs = 1200
)

// Spec declaratively describes an interactive tutorial for an Ochazuke tool.
type Spec struct {
	Tool        string    `json:"tool"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Command     string    `json:"command,omitempty"`
	WorkDir     string    `json:"workDir,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	Theme       string    `json:"theme,omitempty"`
	Sections    []Section `json:"sections"`
}

// Section groups tutorial steps around one high-level user goal.
type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Goal  string `json:"goal,omitempty"`
	Steps []Step `json:"steps"`
}

// Step is a single user action, assertion set, and optional screen capture.
type Step struct {
	Caption string   `json:"caption"`
	Command string   `json:"command,omitempty"`
	Keys    []string `json:"keys,omitempty"`
	Mode    string   `json:"mode,omitempty"`
	Expect  []string `json:"expect,omitempty"`
	PauseMs int      `json:"pauseMs,omitempty"`
	Capture *bool    `json:"capture,omitempty"`
}

// ShouldCapture reports whether this step should be rendered in the guide.
// When Capture is nil, steps are captured by default.
func (s Step) ShouldCapture() bool {
	if s.Capture == nil {
		return true
	}
	return *s.Capture
}

func effectivePauseMs(ms int) int {
	if ms > 0 {
		return ms
	}
	return DefaultPauseMs
}

// LoadSpec reads and parses a JSON tutorial spec from disk.
func LoadSpec(path string) (Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("read tutorial spec %q: %w", path, err)
	}
	spec, err := ParseSpec(raw)
	if err != nil {
		return Spec{}, fmt.Errorf("parse tutorial spec %q: %w", path, err)
	}
	return spec, nil
}

// ParseSpec decodes raw JSON bytes into a validated Spec with defaults applied.
func ParseSpec(raw []byte) (Spec, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Spec{}, errors.New("tutorial spec is empty")
	}
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return Spec{}, fmt.Errorf("decode tutorial spec json: %w", err)
	}
	if err := validateAndDefaultSpec(&spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func validateAndDefaultSpec(spec *Spec) error {
	spec.Tool = strings.TrimSpace(spec.Tool)
	spec.Title = strings.TrimSpace(spec.Title)
	if spec.Tool == "" {
		return errors.New("tutorial spec: tool is required")
	}
	if spec.Title == "" {
		return errors.New("tutorial spec: title is required")
	}
	if spec.Width < 0 {
		return fmt.Errorf("tutorial spec: width must be >= 0 (got %d)", spec.Width)
	}
	if spec.Height < 0 {
		return fmt.Errorf("tutorial spec: height must be >= 0 (got %d)", spec.Height)
	}
	if spec.Width == 0 {
		spec.Width = DefaultWidth
	}
	if spec.Height == 0 {
		spec.Height = DefaultHeight
	}
	if len(spec.Sections) == 0 {
		return errors.New("tutorial spec: at least one section is required")
	}
	seenIDs := make(map[string]struct{}, len(spec.Sections))
	for i := range spec.Sections {
		sec := &spec.Sections[i]
		sec.ID = strings.TrimSpace(sec.ID)
		sec.Title = strings.TrimSpace(sec.Title)
		if sec.ID == "" {
			return fmt.Errorf("tutorial spec: section %d: id is required", i+1)
		}
		if _, dup := seenIDs[sec.ID]; dup {
			return fmt.Errorf("tutorial spec: duplicate section id %q", sec.ID)
		}
		seenIDs[sec.ID] = struct{}{}
		if sec.Title == "" {
			return fmt.Errorf("tutorial spec: section %q: title is required", sec.ID)
		}
		if len(sec.Steps) == 0 {
			return fmt.Errorf("tutorial spec: section %q: at least one step is required", sec.ID)
		}
		for j, st := range sec.Steps {
			if st.PauseMs < 0 {
				return fmt.Errorf("tutorial spec: section %q step %d: pauseMs must be >= 0 (got %d)", sec.ID, j+1, st.PauseMs)
			}
		}
	}
	return nil
}

// ResolveKeys computes the full key sequence and human-readable key badge for
// step against bindings.
//
// When step.Command is non-empty, ResolveKeys finds the primary default binding
// matching step.Command (preferring step.Mode when set, otherwise "normal",
// then mode-agnostic bindings, then the first match in declaration order) and
// prepends its key sequence before any explicit step.Keys.
func ResolveKeys(step Step, bindings []core.Binding) (keys []string, badge string) {
	if step.Command != "" {
		if b, ok := lookupPrimaryBinding(step.Command, step.Mode, bindings); ok {
			keys = append(keys, b.Keys...)
		}
	}
	if len(step.Keys) > 0 {
		keys = append(keys, step.Keys...)
	}
	if len(keys) == 0 {
		return nil, ""
	}
	return slices.Clone(keys), FormatKeyBadge(keys)
}

func lookupPrimaryBinding(command, mode string, bindings []core.Binding) (core.Binding, bool) {
	wantMode := mode
	if wantMode == "" {
		wantMode = "normal"
	}

	bestRank := 99
	var best core.Binding
	found := false

	for _, b := range bindings {
		if b.Unbind || b.Command != command || len(b.Keys) == 0 {
			continue
		}
		rank := bindingMatchRank(b, wantMode)
		if !found || rank < bestRank {
			best = b
			bestRank = rank
			found = true
			if rank == 0 {
				break
			}
		}
	}
	return best, found
}

func bindingMatchRank(b core.Binding, wantMode string) int {
	switch {
	case b.Mode == wantMode && !b.Hidden:
		return 0
	case b.Mode == wantMode:
		return 1
	case b.Mode == "" && !b.Hidden:
		return 2
	case b.Mode == "":
		return 3
	case !b.Hidden:
		return 4
	default:
		return 5
	}
}

// FormatKeyBadge joins formatted key tokens into a clean human-readable badge
// string (for example "/ status=open Enter" or "C").
func FormatKeyBadge(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		parts = append(parts, FormatKey(k))
	}
	return strings.Join(parts, " ")
}

// FormatKey converts an Ochazuke key name into a clean display label.
func FormatKey(key string) string {
	if key == "" {
		return ""
	}
	if key == " " {
		return "Space"
	}
	lower := strings.ToLower(key)
	if named, ok := displaySpecialKey(lower); ok {
		return named
	}
	if strings.Contains(lower, "+") && len(key) > 1 {
		if formatted, ok := formatModifierKey(key); ok {
			return formatted
		}
	}
	return key
}

func displaySpecialKey(lower string) (string, bool) {
	switch lower {
	case "enter", "return":
		return "Enter", true
	case "esc", "escape":
		return "Esc", true
	case "tab":
		return "Tab", true
	case "backtab":
		return "Shift+Tab", true
	case "backspace":
		return "Backspace", true
	case "delete", "del":
		return "Delete", true
	case "space":
		return "Space", true
	case "up":
		return "Up", true
	case "down":
		return "Down", true
	case "left":
		return "Left", true
	case "right":
		return "Right", true
	case "home":
		return "Home", true
	case "end":
		return "End", true
	case "pgup", "pageup":
		return "PgUp", true
	case "pgdown", "pgdn", "pagedown":
		return "PgDown", true
	case "insert", "ins":
		return "Insert", true
	}
	if strings.HasPrefix(lower, "f") && len(lower) >= 2 && len(lower) <= 3 {
		allDigits := true
		for i := 1; i < len(lower); i++ {
			if lower[i] < '0' || lower[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return strings.ToUpper(lower), true
		}
	}
	return "", false
}

func formatModifierKey(key string) (string, bool) {
	parts := strings.Split(key, "+")
	if len(parts) < 2 {
		return "", false
	}
	var mods []string
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			mods = append(mods, "Ctrl")
		case "alt", "opt", "option":
			mods = append(mods, "Alt")
		case "shift":
			mods = append(mods, "Shift")
		case "meta", "cmd", "super":
			mods = append(mods, "Meta")
		default:
			return "", false
		}
	}
	last := parts[len(parts)-1]
	if last == "" {
		return "", false
	}
	if named, ok := displaySpecialKey(strings.ToLower(last)); ok {
		last = named
	} else {
		r, size := utf8.DecodeRuneInString(last)
		if size == len(last) && unicode.IsLetter(r) {
			last = strings.ToUpper(last)
		}
	}
	return strings.Join(append(mods, last), "+"), true
}
