package traits

import (
	"math"
	"slices"
	"strings"
	"time"
)

// Default exit-dissolve animation parameters and sweep directions.
const (
	DefaultExitAnimDuration     = 2500 * time.Millisecond
	DefaultExitAnimTickInterval = 60 * time.Millisecond
	DefaultExitAnimFadePhase    = 0.45

	ExitAnimRTL = "rtl"
	ExitAnimLTR = "ltr"
)

var defaultExitAnimWaveTones = []string{"accent", "warning", "muted"}

// ExitAnimConfig controls the timing, direction, and wave palette of an
// exit-dissolve animation (phase 1: color-fade sweep to dim; phase 2:
// character-by-character erosion).
type ExitAnimConfig struct {
	Duration     time.Duration `json:"duration,omitempty"`     // default 2500ms
	TickInterval time.Duration `json:"tickInterval,omitempty"` // default 60ms
	FadePhase    float64       `json:"fadePhase,omitempty"`    // default 0.45
	Direction    string        `json:"direction,omitempty"`    // "rtl" (default) or "ltr"
	WaveTones    []string      `json:"waveTones,omitempty"`    // default []string{"accent", "warning", "muted"}
}

// DefaultExitAnimConfig returns an ExitAnimConfig populated with the default
// timing, direction, and semantic wave tones.
func DefaultExitAnimConfig() ExitAnimConfig {
	return ExitAnimConfig{
		Duration:     DefaultExitAnimDuration,
		TickInterval: DefaultExitAnimTickInterval,
		FadePhase:    DefaultExitAnimFadePhase,
		Direction:    ExitAnimRTL,
		WaveTones:    slices.Clone(defaultExitAnimWaveTones),
	}
}

// NormalizeExitAnimConfig fills any unset or out-of-range fields with defaults.
func NormalizeExitAnimConfig(cfg ExitAnimConfig) ExitAnimConfig {
	if cfg.Duration <= 0 {
		cfg.Duration = DefaultExitAnimDuration
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = DefaultExitAnimTickInterval
	}
	if cfg.FadePhase <= 0 || cfg.FadePhase >= 1 || math.IsNaN(cfg.FadePhase) {
		cfg.FadePhase = DefaultExitAnimFadePhase
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Direction)) {
	case ExitAnimLTR:
		cfg.Direction = ExitAnimLTR
	default:
		cfg.Direction = ExitAnimRTL
	}
	if len(cfg.WaveTones) == 0 {
		cfg.WaveTones = slices.Clone(defaultExitAnimWaveTones)
	}
	return cfg
}

// ExitSpan styles a rune range [From, To) of dissolved text with a semantic
// tone name. Callers map ExitSpan 1:1 onto ui.ToneSpan.
type ExitSpan struct {
	From int    `json:"from"`
	To   int    `json:"to"`
	Tone string `json:"tone"`
}

// ExitAnimSet tracks active exit-dissolve animations keyed by item ID using
// pure deterministic clock math (zero I/O, zero goroutines, zero idle ticks).
// The zero value is ready to use with DefaultExitAnimConfig.
type ExitAnimSet struct {
	Config ExitAnimConfig
	items  map[string]time.Time
}

// NewExitAnimSet returns an ExitAnimSet configured with cfg.
func NewExitAnimSet(cfg ExitAnimConfig) *ExitAnimSet {
	return &ExitAnimSet{Config: NormalizeExitAnimConfig(cfg)}
}

func (s *ExitAnimSet) cfg() ExitAnimConfig {
	return NormalizeExitAnimConfig(s.Config)
}

// Start begins (or restarts) an exit-dissolve animation for key at now.
func (s *ExitAnimSet) Start(key string, now time.Time) {
	if key == "" {
		return
	}
	if s.items == nil {
		s.items = make(map[string]time.Time)
	}
	s.items[key] = now
}

// Cancel stops and removes any active animation for key, reporting whether one
// was present.
func (s *ExitAnimSet) Cancel(key string) bool {
	if s.items == nil {
		return false
	}
	if _, ok := s.items[key]; ok {
		delete(s.items, key)
		return true
	}
	return false
}

// Len reports how many animations are currently tracked (prior to pruning).
func (s *ExitAnimSet) Len() int {
	return len(s.items)
}

// Active reports whether key is currently animating and has not yet expired at now.
func (s *ExitAnimSet) Active(key string, now time.Time) bool {
	if s.items == nil {
		return false
	}
	started, ok := s.items[key]
	if !ok {
		return false
	}
	return now.Sub(started) < s.cfg().Duration
}

// Progress returns the normalized progress in [0.0, 1.0] for key at now, and
// whether key is tracked in the set. Keys that have reached expiry return
// (1.0, true) until Prune removes them so callers never flicker back to full
// text on the final frame.
func (s *ExitAnimSet) Progress(key string, now time.Time) (float64, bool) {
	if s.items == nil {
		return 0, false
	}
	started, ok := s.items[key]
	if !ok {
		return 0, false
	}
	elapsed := now.Sub(started)
	if elapsed <= 0 {
		return 0, true
	}
	dur := s.cfg().Duration
	if elapsed >= dur {
		return 1, true
	}
	return float64(elapsed) / float64(dur), true
}

// Prune removes all animations whose duration has elapsed at now, returning
// the expired keys in sorted deterministic order and whether any animations
// remain active.
func (s *ExitAnimSet) Prune(now time.Time) (expired []string, stillActive bool) {
	if len(s.items) == 0 {
		return nil, false
	}
	dur := s.cfg().Duration
	for k, started := range s.items {
		if now.Sub(started) >= dur {
			expired = append(expired, k)
			delete(s.items, k)
		}
	}
	slices.Sort(expired)
	return expired, len(s.items) > 0
}

// NextTick returns the delay until the next animation frame and true, or
// (0, false) when no animations are tracked (zero idle CPU). The delay is
// clamped to the earliest remaining expiry so animations finish promptly.
func (s *ExitAnimSet) NextTick(now time.Time) (time.Duration, bool) {
	if len(s.items) == 0 {
		return 0, false
	}
	cfg := s.cfg()
	minRem := cfg.TickInterval
	first := true
	for _, started := range s.items {
		rem := started.Add(cfg.Duration).Sub(now)
		if rem < 0 {
			rem = 0
		}
		if first || rem < minRem {
			minRem = rem
			first = false
		}
	}
	if minRem > cfg.TickInterval {
		minRem = cfg.TickInterval
	}
	return minRem, true
}

// DissolveText projects text at normalized progress in [0.0, 1.0] into visible
// text and per-rune semantic ExitSpans:
//   - progress <= 0: full untouched text, nil spans.
//   - 0 < progress < FadePhase (Phase 1): full text visible while a wave of
//     WaveTones sweeps across runes, leaving "row.dim" behind it.
//   - FadePhase <= progress < 1 (Phase 2): dimmed text erodes character by
//     character ("rtl" from the right, "ltr" from the left) with a glowing
//     leading edge char (WaveTones[0]) at the erosion front.
//   - progress >= 1: empty string, nil spans.
func DissolveText(text string, progress float64, cfg ExitAnimConfig) (visible string, spans []ExitSpan) {
	rs := []rune(text)
	n := len(rs)
	if n == 0 || progress >= 1 {
		return "", nil
	}
	if progress <= 0 || math.IsNaN(progress) {
		return text, nil
	}
	cfg = NormalizeExitAnimConfig(cfg)
	wave := cfg.WaveTones
	wLen := len(wave)

	if progress < cfg.FadePhase {
		t := progress / cfg.FadePhase
		pos := int(math.Floor(t * float64(n+wLen)))
		var out []ExitSpan
		curTone := ""
		curFrom := 0
		flush := func(to int) {
			if curTone != "" && to > curFrom {
				out = append(out, ExitSpan{From: curFrom, To: to, Tone: curTone})
			}
		}
		for i := 0; i < n; i++ {
			var d int
			if cfg.Direction == ExitAnimLTR {
				d = pos - i
			} else {
				front := n - 1 - pos
				d = i - front
			}
			tone := ""
			switch {
			case d < 0:
				tone = ""
			case d < wLen:
				tone = wave[d]
			default:
				tone = "row.dim"
			}
			if tone != curTone {
				flush(i)
				curTone = tone
				curFrom = i
			}
		}
		flush(n)
		return text, out
	}

	t := (progress - cfg.FadePhase) / (1.0 - cfg.FadePhase)
	eroded := int(math.Floor(t * float64(n)))
	if eroded >= n {
		eroded = n - 1
	}
	rem := n - eroded
	edgeTone := wave[0]

	if cfg.Direction == ExitAnimLTR {
		vis := string(rs[eroded:])
		if rem == 1 {
			return vis, []ExitSpan{{From: 0, To: 1, Tone: edgeTone}}
		}
		return vis, []ExitSpan{
			{From: 0, To: 1, Tone: edgeTone},
			{From: 1, To: rem, Tone: "row.dim"},
		}
	}

	vis := string(rs[:rem])
	if rem == 1 {
		return vis, []ExitSpan{{From: 0, To: 1, Tone: edgeTone}}
	}
	return vis, []ExitSpan{
		{From: 0, To: rem - 1, Tone: "row.dim"},
		{From: rem - 1, To: rem, Tone: edgeTone},
	}
}
