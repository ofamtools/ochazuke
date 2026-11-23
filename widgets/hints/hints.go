// Package hints provides a reusable, deterministic letter-label hint overlay
// engine shared by focus-jump hints (over layout.Solved.FocusOrder) and
// application terminal link hints.
//
// It performs no I/O and imports only Tier 0/1 packages (buffer, core, layout,
// ui) — never Bubble Tea or Lip Gloss.
package hints

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
)

// DefaultAlphabet lists home-row keys first, followed by top and bottom rows,
// excluding 'q' so 'q' remains available to cancel hint mode.
const DefaultAlphabet = "asdfghjklwertuiopyzxcvbnm"

// Action classifies how a matched hint target should be acted upon.
type Action string

// Hint match actions determined by key modifiers.
const (
	ActionDefault   Action = "default"   // lowercase key (e.g. focus / open)
	ActionSecondary Action = "secondary" // uppercase / Shift key (e.g. yank / copy)
	ActionAlt       Action = "alt"       // ctrl+key (e.g. inspect / alternate action)
)

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

// Option configures a Session.
type Option func(*sessionConfig)

type sessionConfig struct {
	alphabet    string
	hasAlphabet bool
}

// WithAlphabet sets a custom hint label alphabet for the session.
func WithAlphabet(alphabet string) Option {
	return func(c *sessionConfig) {
		c.alphabet = alphabet
		c.hasAlphabet = true
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
	targets       []Target
	alphabet      string
	alphabetRunes map[rune]bool
	prefix        string
	active        bool
	pendingAction Action
	matched       *Target
	matchedAction Action
}

// NewSession starts a hint session for targets. If WithAlphabet is provided,
// labels are regenerated using that alphabet; otherwise existing unique
// non-empty labels are preserved (normalized to lowercase) and missing or
// colliding labels are assigned using DefaultAlphabet.
func NewSession(targets []Target, opts ...Option) *Session {
	cfg := sessionConfig{alphabet: DefaultAlphabet}
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

	return &Session{
		targets:       cloned,
		alphabet:      alphaStr,
		alphabetRunes: alphaSet,
		active:        len(cloned) > 0,
		pendingAction: ActionDefault,
	}
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

// Cancel deactivates the session and clears any typed prefix.
func (s *Session) Cancel() {
	if s == nil {
		return
	}
	s.active = false
	s.prefix = ""
	s.pendingAction = ActionDefault
}

// Reset reactivates the session from the initial state (if it has targets).
func (s *Session) Reset() {
	if s == nil {
		return
	}
	s.prefix = ""
	s.pendingAction = ActionDefault
	s.matched = nil
	s.matchedAction = ""
	s.active = len(s.targets) > 0
}

// HandleKey processes a normalized Ochazuke key string ("esc", "q", "backspace",
// "a", "A", "ctrl+a", "ctrl+c", etc.).
func (s *Session) HandleKey(key string) Result {
	if s == nil || !s.active {
		return Result{}
	}
	switch key {
	case "esc", "escape", "ctrl+c":
		s.Cancel()
		return Result{Status: StatusCancelled, Handled: true}
	case "backspace", "ctrl+h":
		s.backspace()
		return Result{Status: StatusActive, Handled: true}
	}
	if rest, ok := strings.CutPrefix(key, "ctrl+"); ok {
		r, size := utf8.DecodeRuneInString(rest)
		if rest != "" && size == len(rest) && r != utf8.RuneError {
			return s.HandleRune(r, true)
		}
		return Result{Status: StatusNoMatch, Handled: true}
	}
	r, size := utf8.DecodeRuneInString(key)
	if key != "" && size == len(key) && r != utf8.RuneError {
		return s.HandleRune(r, false)
	}
	return Result{Status: StatusNoMatch, Handled: true}
}

// HandleRune processes a single typed rune and optional ctrl modifier.
// Uppercase runes select ActionSecondary; ctrl selects ActionAlt; esc or 'q'
// (when 'q' is not in the active alphabet) cancels; backspace pops one rune.
func (s *Session) HandleRune(r rune, ctrl bool) Result {
	if s == nil || !s.active {
		return Result{}
	}
	if r == '\x1b' || (ctrl && (r == 'c' || r == 'C')) {
		s.Cancel()
		return Result{Status: StatusCancelled, Handled: true}
	}
	if r == '\b' || r == '\x7f' || (ctrl && (r == 'h' || r == 'H')) {
		s.backspace()
		return Result{Status: StatusActive, Handled: true}
	}

	lower := unicode.ToLower(r)
	if lower == 'q' && !s.alphabetRunes['q'] {
		s.Cancel()
		return Result{Status: StatusCancelled, Handled: true}
	}

	stepAction := s.pendingAction
	switch {
	case ctrl:
		stepAction = ActionAlt
	case unicode.IsUpper(r):
		stepAction = ActionSecondary
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
	s.pendingAction = stepAction
	return Result{Status: StatusActive, Handled: true}
}

func (s *Session) backspace() {
	if s.prefix == "" {
		s.pendingAction = ActionDefault
		return
	}
	rs := []rune(s.prefix)
	s.prefix = string(rs[:len(rs)-1])
	if s.prefix == "" {
		s.pendingAction = ActionDefault
	}
}

// HandleCommand processes core commands (esc, input.backspace, input.insert).
func (s *Session) HandleCommand(cmd core.Command) (Result, bool) {
	if s == nil || !s.active {
		return Result{}, false
	}
	switch cmd.ID {
	case "esc", "hints.cancel":
		return s.HandleKey("esc"), true
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

// OverlayStyle controls the ANSI SGR styling of hint badges painted by
// PaintOverlay and RenderOverlay.
type OverlayStyle struct {
	PrefixSGR string // SGR for already-typed prefix runes
	LabelSGR  string // SGR for remaining untyped label runes
	Bracketed bool   // wrap badge in [brackets] (default true in DefaultOverlayStyle)
}

// DefaultOverlayStyle returns a high-contrast terminal-portable badge style
// using standard ANSI bold/reverse SGR codes.
func DefaultOverlayStyle() OverlayStyle {
	return OverlayStyle{
		PrefixSGR: "\x1b[2;7m",
		LabelSGR:  "\x1b[1;7m",
		Bracketed: true,
	}
}

// OverlayStyleFromTheme derives an OverlayStyle from a ui.Theme without
// importing Lip Gloss.
func OverlayStyleFromTheme(th ui.Theme) OverlayStyle {
	st := DefaultOverlayStyle()
	if labelSGR := toneToSGR(th.Tone("badge.accent")); labelSGR != "" {
		st.LabelSGR = labelSGR
	}
	if prefixSGR := toneToSGR(th.Tone("badge")); prefixSGR != "" {
		st.PrefixSGR = prefixSGR
	}
	return st
}

// PaintOverlay composites active hint badges directly onto buf at each
// remaining target's (Rect.X, Rect.Y) coordinate.
func PaintOverlay(buf *buffer.Buffer, s *Session, style ...OverlayStyle) {
	if buf == nil || s == nil || !s.active || buf.Width() <= 0 || buf.Height() <= 0 {
		return
	}
	st := DefaultOverlayStyle()
	if len(style) > 0 {
		st = style[0]
	}
	w, h := buf.Width(), buf.Height()
	for _, p := range s.Placements() {
		if p.Y < 0 || p.Y >= h || p.X >= w {
			continue
		}
		var sb strings.Builder
		plainLen := 0
		if st.Bracketed {
			sb.WriteString(st.LabelSGR)
			sb.WriteByte('[')
			plainLen++
		}
		if p.MatchedPrefix != "" {
			sb.WriteString("\x1b[0m")
			sb.WriteString(st.PrefixSGR)
			sb.WriteString(p.MatchedPrefix)
			plainLen += utf8.RuneCountInString(p.MatchedPrefix)
		}
		if p.RemainingSuffix != "" {
			sb.WriteString("\x1b[0m")
			sb.WriteString(st.LabelSGR)
			sb.WriteString(p.RemainingSuffix)
			plainLen += utf8.RuneCountInString(p.RemainingSuffix)
		}
		if st.Bracketed {
			sb.WriteString("\x1b[0m")
			sb.WriteString(st.LabelSGR)
			sb.WriteByte(']')
			plainLen++
		}
		sb.WriteString("\x1b[0m")

		if plainLen <= 0 {
			continue
		}
		bx := max(0, p.X)
		if bx+plainLen > w {
			bx = max(0, w-plainLen)
		}
		bw := min(plainLen, w-bx)
		if bw <= 0 {
			continue
		}
		badge := buffer.New(bw, 1)
		badge.SetLine(0, sb.String())
		buf.Blit(badge, layout.Rect{X: bx, Y: p.Y, W: bw, H: 1})
	}
}

// RenderOverlay composites active hint badges onto an already-rendered ANSI
// frame of size w x h and returns the resulting ANSI string.
func RenderOverlay(baseANSI string, w, h int, s *Session, style ...OverlayStyle) string {
	if w <= 0 || h <= 0 || s == nil || !s.active {
		return baseANSI
	}
	buf := buffer.New(w, h)
	for y, line := range strings.Split(baseANSI, "\n") {
		if y >= h {
			break
		}
		buf.SetLine(y, line)
	}
	PaintOverlay(buf, s, style...)
	return buf.String()
}

// View returns a compact single-row ui.KindText status bar describing the
// active hint session (prefix, remaining count, modifier legend).
func (s *Session) View(id string) ui.ViewNode {
	if id == "" {
		id = "hints"
	}
	rem := s.Remaining()
	facts := map[string]any{
		"active":    s.Active(),
		"prefix":    s.Prefix(),
		"remaining": len(rem),
		"total":     len(s.Targets()),
	}
	if !s.Active() {
		return ui.ViewNode{
			ID:       id,
			Kind:     ui.KindText,
			Scope:    "hints",
			Box:      layout.Constraints{FixedH: 1},
			Props:    map[string]any{"text": ""},
			Facts:    facts,
			CacheKey: 1,
		}
	}

	prefixDisp := s.prefix + "_"
	head := "HINTS "
	mid := prefixDisp
	tail := fmt.Sprintf("  (%d)  [A-Z] secondary  [ctrl] alt  [esc] cancel", len(rem))
	text := head + mid + tail

	headR := utf8.RuneCountInString(head)
	midR := utf8.RuneCountInString(mid)
	spans := []ui.ToneSpan{
		{From: 0, To: headR, Tone: "badge.accent"},
		{From: headR, To: headR + midR, Tone: "match"},
		{From: headR + midR, To: utf8.RuneCountInString(text), Tone: "muted"},
	}

	return ui.ViewNode{
		ID:    id,
		Kind:  ui.KindText,
		Scope: "hints",
		Box:   layout.Constraints{FixedH: 1},
		Props: map[string]any{
			"text":  text,
			"spans": spans,
		},
		Facts:    facts,
		CacheKey: sessionCacheKey(s, rem),
	}
}

// ViewOverlay returns a floating ui.KindOverlay listing every remaining hint
// target and its label, suitable as a standalone hint legend or accessible
// overlay view.
func (s *Session) ViewOverlay(id string) ui.ViewNode {
	if id == "" {
		id = "hints.overlay"
	}
	rem := s.Remaining()
	if !s.Active() {
		return s.View(id)
	}
	kids := make([]ui.ViewNode, 0, len(rem))
	for _, p := range s.Placements() {
		badge := "[" + p.Target.Label + "]"
		desc := p.Target.Value
		if desc == "" {
			desc = p.Target.ID
		}
		if p.Target.Kind != "" {
			desc = p.Target.Kind + " · " + desc
		}
		line := badge + " " + desc
		bRunes := utf8.RuneCountInString(badge)
		kids = append(kids, ui.ViewNode{
			ID:   id + "." + p.Target.ID,
			Kind: ui.KindText,
			Props: map[string]any{
				"text": line,
				"spans": []ui.ToneSpan{
					{From: 0, To: bRunes, Tone: "match"},
					{From: bRunes, To: utf8.RuneCountInString(line), Tone: "text"},
				},
			},
		})
	}
	title := "Hints"
	if s.prefix != "" {
		title = "Hints: " + s.prefix
	}
	return ui.ViewNode{
		ID:       id,
		Kind:     ui.KindOverlay,
		Scope:    "hints",
		Title:    title,
		Badges:   []ui.Badge{{Text: strconv.Itoa(len(rem)), Tone: "accent"}},
		Children: kids,
		Facts: map[string]any{
			"active":    true,
			"prefix":    s.prefix,
			"remaining": len(rem),
			"total":     len(s.targets),
		},
		CacheKey: sessionCacheKey(s, rem),
	}
}

func sessionCacheKey(s *Session, rem []Target) uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%t|%s|%s|", s.active, s.prefix, s.pendingAction)
	for _, t := range rem {
		fmt.Fprintf(h, "%s:%s@%d,%d;", t.ID, t.Label, t.Rect.X, t.Rect.Y)
	}
	return h.Sum64() | 1
}

func toneToSGR(ts ui.ToneStyle) string {
	var parts []string
	if ts.Bold {
		parts = append(parts, "1")
	}
	if ts.Dim {
		parts = append(parts, "2")
	}
	if ts.Underline {
		parts = append(parts, "4")
	}
	if ts.Reverse {
		parts = append(parts, "7")
	}
	if fg := colorSGR(ts.Fg, false); fg != "" {
		parts = append(parts, fg)
	}
	if bg := colorSGR(ts.Bg, true); bg != "" {
		parts = append(parts, bg)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

func colorSGR(c string, bg bool) string {
	if c == "" {
		return ""
	}
	code := "38"
	if bg {
		code = "48"
	}
	if strings.HasPrefix(c, "#") && len(c) == 7 {
		r, err1 := strconv.ParseUint(c[1:3], 16, 8)
		g, err2 := strconv.ParseUint(c[3:5], 16, 8)
		b, err3 := strconv.ParseUint(c[5:7], 16, 8)
		if err1 == nil && err2 == nil && err3 == nil {
			return fmt.Sprintf("%s;2;%d;%d;%d", code, r, g, b)
		}
		return ""
	}
	if idx, err := strconv.Atoi(c); err == nil && idx >= 0 && idx <= 255 {
		return fmt.Sprintf("%s;5;%d", code, idx)
	}
	return ""
}
