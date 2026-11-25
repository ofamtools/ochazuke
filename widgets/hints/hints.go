// Package hints provides a reusable, deterministic letter-label hint overlay
// engine shared by focus-jump hints (over layout.Solved.FocusOrder) and
// application terminal link hints.
//
// It performs no I/O and imports only Tier 0/1 packages (buffer, core, layout,
// ui) — never Bubble Tea or Lip Gloss.
package hints

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
)

// DefaultAlphabet lists home-row keys first, followed by top and bottom rows,
// excluding 'q' so 'q' remains available to cancel hint mode.
const DefaultAlphabet = "asdfghjklwertuiopyzxcvbnm"

// Action classifies how a matched hint target should be acted upon.
type Action string

// Hint match actions determined by key modifiers or active action mode.
const (
	ActionDefault   Action = "default"   // lowercase key (e.g. focus / open)
	ActionSecondary Action = "secondary" // uppercase / Shift key (e.g. yank / copy)
	ActionAlt       Action = "alt"       // ctrl+key (e.g. inspect / alternate action)
	ActionMeta      Action = "meta"      // alt+key (host-defined meta action)
)

var defaultActionOrder = []Action{ActionDefault, ActionSecondary, ActionAlt, ActionMeta}

// Status describes the outcome of one input step on a Session.
type Status uint8

// Session step statuses.
const (
	StatusActive    Status = iota // session remains active; prefix may have narrowed
	StatusMatched                 // exact target matched; Target and Action are set
	StatusCancelled               // cancelled via esc, q, or ctrl+c
	StatusNoMatch                 // typed rune matched no remaining target (prefix unchanged)
)

// Target is one hintable screen region.
type Target struct {
	ID    string      `json:"id"`
	Label string      `json:"label"`
	Rect  layout.Rect `json:"rect"`
	Kind  string      `json:"kind,omitempty"`
	Value string      `json:"value,omitempty"`
}

// Result is returned by Session.HandleKey and Session.HandleRune.
type Result struct {
	Status  Status
	Target  *Target
	Action  Action
	Handled bool
}

// Placement describes a single rendered hint badge position and split label.
type Placement struct {
	Target          Target
	X, Y            int
	MatchedPrefix   string
	RemainingSuffix string
}

// ActionLegend describes one host-defined hint action or control entry shown in
// the session status bar.
type ActionLegend struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Action Action `json:"action,omitempty"`
}

// Option configures a Session.
type Option func(*sessionConfig)

type sessionConfig struct {
	alphabet      string
	hasAlphabet   bool
	defaultAction Action
	modifiers     bool
	hasModifiers  bool
	legend        []ActionLegend
}

// WithAlphabet sets a custom hint label alphabet for the session.
func WithAlphabet(alphabet string) Option {
	return func(c *sessionConfig) {
		c.alphabet = alphabet
		c.hasAlphabet = true
	}
}

// WithDefaultAction sets the initial active action resolved when an unmodified
// lowercase label completes a match. Defaults to ActionDefault when empty.
func WithDefaultAction(a Action) Option {
	return func(c *sessionConfig) {
		c.defaultAction = a
	}
}

// WithModifiers enables or disables Shift, ctrl+, and alt+ modifier overrides.
// Modifiers are enabled by default.
func WithModifiers(enabled bool) Option {
	return func(c *sessionConfig) {
		c.modifiers = enabled
		c.hasModifiers = true
	}
}

// WithActionLegend configures the host-defined legend rendered by Session.View.
func WithActionLegend(items []ActionLegend) Option {
	return func(c *sessionConfig) {
		c.legend = slices.Clone(items)
	}
}

func sanitizeAlphabet(alphabet string, minRunes int) []rune {
	seen := map[rune]bool{}
	var out []rune
	for _, r := range strings.ToLower(alphabet) {
		if unicode.IsSpace(r) || unicode.IsControl(r) || !unicode.IsPrint(r) {
			continue
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	if len(out) < minRunes {
		return []rune(DefaultAlphabet)
	}
	return out
}

// GenerateLabels produces count deterministic, prefix-free, minimal-length
// lowercase labels over alphabet (defaulting to DefaultAlphabet).
// No returned label is a prefix of any other returned label, and single-key
// labels from the front of the alphabet are preserved for as many targets as
// possible.
func GenerateLabels(count int, alphabet string) []string {
	if count <= 0 {
		return nil
	}
	minRunes := 1
	if count > 1 {
		minRunes = 2
	}
	alpha := sanitizeAlphabet(alphabet, minRunes)
	k := len(alpha)

	if count <= k {
		out := make([]string, count)
		for i := 0; i < count; i++ {
			out[i] = string(alpha[i])
		}
		return out
	}

	rank := make(map[rune]int, k)
	for i, r := range alpha {
		rank[r] = i
	}

	// Maintain the leaves of a k-ary prefix tree. Expanding a shortest leaf
	// from the tail preserves the highest-priority home-row keys as 1-char
	// labels while strictly preserving the prefix-free invariant.
	leaves := make([]string, k)
	for i, r := range alpha {
		leaves[i] = string(r)
	}

	for len(leaves) < count {
		minLen := utf8.RuneCountInString(leaves[0])
		expandIdx := 0
		for i := 1; i < len(leaves); i++ {
			l := utf8.RuneCountInString(leaves[i])
			if l < minLen {
				minLen = l
				expandIdx = i
			} else if l == minLen {
				expandIdx = i
			}
		}
		prefix := leaves[expandIdx]
		leaves = slices.Delete(leaves, expandIdx, expandIdx+1)
		for _, r := range alpha {
			leaves = append(leaves, prefix+string(r))
		}
	}

	slices.SortFunc(leaves, func(a, b string) int {
		ra, rb := []rune(a), []rune(b)
		if len(ra) != len(rb) {
			if len(ra) < len(rb) {
				return -1
			}
			return 1
		}
		for i := range ra {
			if rank[ra[i]] != rank[rb[i]] {
				if rank[ra[i]] < rank[rb[i]] {
					return -1
				}
				return 1
			}
		}
		return 0
	})

	return leaves[:count]
}

// AssignLabels returns a copy of targets with fresh prefix-free labels assigned
// using alphabet (defaulting to DefaultAlphabet).
func AssignLabels(targets []Target, alphabet string) []Target {
	if len(targets) == 0 {
		return nil
	}
	out := slices.Clone(targets)
	labels := GenerateLabels(len(out), alphabet)
	for i := range out {
		out[i].Label = labels[i]
	}
	return out
}

// TargetsFromFocusOrder constructs hint targets for every visible, non-synthetic
// focusable node in solved.FocusOrder with default prefix-free labels assigned.
func TargetsFromFocusOrder(solved layout.Solved) []Target {
	var out []Target
	for _, id := range solved.FocusOrder {
		if id == "" || ui.IsSyntheticID(id) {
			continue
		}
		r, ok := solved.Rects[id]
		if !ok || r.W <= 0 || r.H <= 0 {
			continue
		}
		out = append(out, Target{
			ID:    id,
			Rect:  r,
			Kind:  "focus",
			Value: id,
		})
	}
	return AssignLabels(out, DefaultAlphabet)
}

// Session is a pure state machine for one active hint selection overlay.
type Session struct {
	targets         []Target
	alphabet        string
	alphabetRunes   map[rune]bool
	prefix          string
	active          bool
	defaultAction   Action
	activeAction    Action
	pendingOverride Action
	modifiers       bool
	legend          []ActionLegend
	matched         *Target
	matchedAction   Action
}

// NewSession starts a hint session for targets. If WithAlphabet is provided,
// labels are regenerated using that alphabet; otherwise existing unique
// non-empty labels are preserved (normalized to lowercase) and missing or
// colliding labels are assigned using DefaultAlphabet.
func NewSession(targets []Target, opts ...Option) *Session {
	cfg := sessionConfig{
		alphabet:      DefaultAlphabet,
		defaultAction: ActionDefault,
		modifiers:     true,
	}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}

	minRunes := 1
	if len(targets) > 1 {
		minRunes = 2
	}
	alphaSlice := sanitizeAlphabet(cfg.alphabet, minRunes)
	alphaStr := string(alphaSlice)

	cloned := slices.Clone(targets)
	needAssign := cfg.hasAlphabet
	if !needAssign {
		seen := make(map[string]bool, len(cloned))
		for i := range cloned {
			lbl := strings.ToLower(strings.TrimSpace(cloned[i].Label))
			if lbl == "" || seen[lbl] {
				needAssign = true
				break
			}
			cloned[i].Label = lbl
			seen[lbl] = true
		}
	}
	if needAssign && len(cloned) > 0 {
		cloned = AssignLabels(cloned, alphaStr)
	}

	alphaSet := make(map[rune]bool, len(alphaSlice))
	for _, r := range alphaSlice {
		alphaSet[r] = true
	}
	for _, t := range cloned {
		for _, r := range t.Label {
			alphaSet[r] = true
		}
	}

	defAct := normalizeAction(cfg.defaultAction)
	mods := true
	if cfg.hasModifiers {
		mods = cfg.modifiers
	}

	return &Session{
		targets:       cloned,
		alphabet:      alphaStr,
		alphabetRunes: alphaSet,
		active:        len(cloned) > 0,
		defaultAction: defAct,
		activeAction:  defAct,
		modifiers:     mods,
		legend:        slices.Clone(cfg.legend),
	}
}

func normalizeAction(a Action) Action {
	if a == "" {
		return ActionDefault
	}
	return a
}

// Active reports whether the session is currently awaiting input.
func (s *Session) Active() bool { return s != nil && s.active }

// Alphabet returns the normalized alphabet used by the session.
func (s *Session) Alphabet() string {
	if s == nil {
		return DefaultAlphabet
	}
	return s.alphabet
}

// Prefix returns the currently typed lowercase label prefix.
func (s *Session) Prefix() string {
	if s == nil {
		return ""
	}
	return s.prefix
}

// Targets returns a copy of all targets in the session.
func (s *Session) Targets() []Target {
	if s == nil || len(s.targets) == 0 {
		return nil
	}
	return slices.Clone(s.targets)
}

// Remaining returns all targets whose Label still matches the current prefix.
func (s *Session) Remaining() []Target {
	if s == nil || !s.active {
		return nil
	}
	var out []Target
	for _, t := range s.targets {
		if strings.HasPrefix(t.Label, s.prefix) {
			out = append(out, t)
		}
	}
	return out
}

// Matched returns the matched target and action after StatusMatched.
func (s *Session) Matched() (*Target, Action, bool) {
	if s == nil || s.matched == nil {
		return nil, "", false
	}
	t := *s.matched
	return &t, s.matchedAction, true
}

// ActiveAction returns the currently active action resolved by unmodified
// lowercase label matches.
func (s *Session) ActiveAction() Action {
	if s == nil || s.activeAction == "" {
		return ActionDefault
	}
	return s.activeAction
}

// SetAction changes the session's active action for unmodified lowercase label
// matches and clears any pending mid-prefix modifier override.
func (s *Session) SetAction(a Action) {
	if s == nil {
		return
	}
	s.activeAction = normalizeAction(a)
	s.pendingOverride = ""
}

// CycleAction advances the session's active action to the next entry in order
// (wrapping around) and returns the new active action. When order is empty, it
// cycles through distinct actions from the configured ActionLegend, falling
// back to [ActionDefault, ActionSecondary, ActionAlt, ActionMeta].
func (s *Session) CycleAction(order []Action) Action {
	if s == nil {
		return ActionDefault
	}
	seq := uniqueNonEmptyActions(order)
	if len(seq) == 0 {
		for _, item := range s.legend {
			if item.Action != "" && !slices.Contains(seq, item.Action) {
				seq = append(seq, item.Action)
			}
		}
	}
	if len(seq) == 0 {
		seq = defaultActionOrder
	}
	cur := s.ActiveAction()
	next := seq[0]
	for i, a := range seq {
		if a == cur {
			next = seq[(i+1)%len(seq)]
			break
		}
	}
	s.SetAction(next)
	return next
}

func uniqueNonEmptyActions(in []Action) []Action {
	var out []Action
	for _, a := range in {
		if a != "" && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

// ActionLegend returns a copy of the host-configured action legend, or nil when
// unset.
func (s *Session) ActionLegend() []ActionLegend {
	if s == nil || len(s.legend) == 0 {
		return nil
	}
	return slices.Clone(s.legend)
}

// SetActionLegend replaces the host-configured action legend rendered by View.
func (s *Session) SetActionLegend(items []ActionLegend) {
	if s == nil {
		return
	}
	s.legend = slices.Clone(items)
}

// ModifiersEnabled reports whether Shift, ctrl+, and alt+ modifier overrides
// are currently enabled.
func (s *Session) ModifiersEnabled() bool {
	if s == nil {
		return true
	}
	return s.modifiers
}

// SetModifiers enables or disables Shift, ctrl+, and alt+ modifier overrides.
func (s *Session) SetModifiers(enabled bool) {
	if s == nil {
		return
	}
	s.modifiers = enabled
	if !enabled {
		s.pendingOverride = ""
	}
}

// Facts returns truthful read-only session state for declarative bindings and
// presenters: active, prefix, remaining, total, and action.
func (s *Session) Facts() map[string]any {
	if s == nil {
		return map[string]any{
			"active":    false,
			"prefix":    "",
			"remaining": 0,
			"total":     0,
			"action":    string(ActionDefault),
		}
	}
	act := s.effectiveAction()
	if !s.active && s.matchedAction != "" {
		act = s.matchedAction
	}
	return map[string]any{
		"active":    s.active,
		"prefix":    s.prefix,
		"remaining": len(s.Remaining()),
		"total":     len(s.targets),
		"action":    string(act),
	}
}

func (s *Session) effectiveAction() Action {
	if s == nil {
		return ActionDefault
	}
	if s.pendingOverride != "" {
		return s.pendingOverride
	}
	return s.ActiveAction()
}

func (s *Session) hasCandidatePrefix(prefix string) bool {
	for i := range s.targets {
		if strings.HasPrefix(s.targets[i].Label, prefix) {
			return true
		}
	}
	return false
}

// Cancel deactivates the session and clears any typed prefix.
func (s *Session) Cancel() {
	if s == nil {
		return
	}
	s.active = false
	s.prefix = ""
	s.pendingOverride = ""
	s.matched = nil
	s.matchedAction = ""
}

// Reset reactivates the session from the initial state (if it has targets).
func (s *Session) Reset() {
	if s == nil {
		return
	}
	s.prefix = ""
	s.activeAction = s.defaultAction
	s.pendingOverride = ""
	s.matched = nil
	s.matchedAction = ""
	s.active = len(s.targets) > 0
}

// HandleKey processes a normalized Ochazuke key string ("esc", "q", "backspace",
// "a", "A", "ctrl+a", "alt+a", "ctrl+c", etc.).
func (s *Session) HandleKey(key string) Result {
	if s == nil || !s.active {
		return Result{}
	}
	switch key {
	case "esc", "escape", "ctrl+c":
		s.Cancel()
		return Result{Status: StatusCancelled, Handled: true}
	case "backspace":
		s.backspace()
		return Result{Status: StatusActive, Handled: true}
	}
	if rest, ok := strings.CutPrefix(key, "ctrl+"); ok {
		r, size := utf8.DecodeRuneInString(rest)
		if rest != "" && size == len(rest) && r != utf8.RuneError {
			return s.handleRune(r, true, false)
		}
		return Result{Status: StatusNoMatch, Handled: true}
	}
	if rest, ok := strings.CutPrefix(key, "alt+"); ok {
		r, size := utf8.DecodeRuneInString(rest)
		if rest != "" && size == len(rest) && r != utf8.RuneError {
			return s.handleRune(r, false, true)
		}
		return Result{Status: StatusNoMatch, Handled: true}
	}
	r, size := utf8.DecodeRuneInString(key)
	if key != "" && size == len(key) && r != utf8.RuneError {
		return s.handleRune(r, false, false)
	}
	return Result{Status: StatusNoMatch, Handled: true}
}

// HandleRune processes a single typed rune and optional ctrl modifier.
// When modifier actions are enabled, uppercase runes select ActionSecondary and
// ctrl selects ActionAlt; otherwise lowercase matches resolve to ActiveAction.
// esc or 'q' (when 'q' is not in the active alphabet) cancels; backspace pops
// one rune.
func (s *Session) HandleRune(r rune, ctrl bool) Result {
	return s.handleRune(r, ctrl, false)
}

func (s *Session) handleRune(r rune, ctrl, alt bool) Result {
	if s == nil || !s.active {
		return Result{}
	}
	if r == '\x1b' || (ctrl && (r == 'c' || r == 'C')) {
		s.Cancel()
		return Result{Status: StatusCancelled, Handled: true}
	}
	if r == '\b' || r == '\x7f' {
		s.backspace()
		return Result{Status: StatusActive, Handled: true}
	}
	if ctrl && (r == 'h' || r == 'H') {
		if !s.modifiers || !s.hasCandidatePrefix(s.prefix+"h") {
			s.backspace()
			return Result{Status: StatusActive, Handled: true}
		}
	}

	lower := unicode.ToLower(r)
	if lower == 'q' && !s.alphabetRunes['q'] {
		s.Cancel()
		return Result{Status: StatusCancelled, Handled: true}
	}

	override := s.pendingOverride
	if s.modifiers {
		switch {
		case alt:
			override = ActionMeta
		case ctrl:
			override = ActionAlt
		case unicode.IsUpper(r):
			override = ActionSecondary
		}
	}
	stepAction := s.ActiveAction()
	if override != "" {
		stepAction = override
	}

	next := s.prefix + string(lower)
	var exact *Target
	matches := 0
	for i := range s.targets {
		lbl := s.targets[i].Label
		if lbl == next {
			t := s.targets[i]
			exact = &t
			matches++
		} else if strings.HasPrefix(lbl, next) {
			matches++
		}
	}

	if matches == 0 {
		return Result{Status: StatusNoMatch, Handled: true}
	}
	if exact != nil && matches == 1 {
		s.prefix = next
		s.active = false
		s.matched = exact
		s.matchedAction = stepAction
		out := *exact
		return Result{
			Status:  StatusMatched,
			Target:  &out,
			Action:  stepAction,
			Handled: true,
		}
	}

	s.prefix = next
	s.pendingOverride = override
	return Result{Status: StatusActive, Handled: true}
}

func (s *Session) backspace() {
	if s.prefix == "" {
		s.pendingOverride = ""
		return
	}
	rs := []rune(s.prefix)
	s.prefix = string(rs[:len(rs)-1])
	if s.prefix == "" {
		s.pendingOverride = ""
	}
}

// HandleCommand processes core commands (esc, hints.cancel, hints.cycle_action,
// hints.set_action, input.backspace, input.insert).
func (s *Session) HandleCommand(cmd core.Command) (Result, bool) {
	if s == nil || !s.active {
		return Result{}, false
	}
	switch cmd.ID {
	case "esc", "hints.cancel":
		return s.HandleKey("esc"), true
	case "hints.cycle_action":
		s.CycleAction(nil)
		return Result{Status: StatusActive, Handled: true}, true
	case "hints.set_action":
		if act := Action(cmd.Arg("action")); act != "" {
			s.SetAction(act)
		}
		return Result{Status: StatusActive, Handled: true}, true
	case "input.backspace":
		return s.HandleKey("backspace"), true
	case "input.insert":
		t := cmd.Arg("text")
		r, size := utf8.DecodeRuneInString(t)
		if t != "" && size == len(t) && r != utf8.RuneError {
			return s.HandleRune(r, false), true
		}
		return Result{Status: StatusNoMatch, Handled: true}, true
	}
	return Result{}, false
}

// Placements computes badge coordinates and matched/remaining label splits for
// all currently remaining targets.
func (s *Session) Placements() []Placement {
	rem := s.Remaining()
	if len(rem) == 0 {
		return nil
	}
	plen := len(s.prefix)
	out := make([]Placement, len(rem))
	for i, t := range rem {
		out[i] = Placement{
			Target:          t,
			X:               t.Rect.X,
			Y:               t.Rect.Y,
			MatchedPrefix:   t.Label[:min(plen, len(t.Label))],
			RemainingSuffix: t.Label[min(plen, len(t.Label)):],
		}
	}
	return out
}
