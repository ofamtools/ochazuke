package traits

import (
	"strings"
	"time"
)

// Escape guard policy names controlling accidental overlay dismissal.
const (
	EscapeGuardOff     = "off"
	EscapeGuardDouble  = "double"
	EscapeGuardCooloff = "cooloff"
	EscapeGuardBoth    = "both"
)

const defaultEscapeGuardCooloff = time.Second

// EscapeGuardConfig configures escape-to-close guarding.
type EscapeGuardConfig struct {
	Policy  string        `json:"policy,omitempty"`
	Cooloff time.Duration `json:"cooloff,omitempty"`
}

// EscapeGuardState holds pure timestamp/arming state for EscapeGuard transitions.
type EscapeGuardState struct {
	Armed        bool
	CooloffUntil time.Time
}

// EscapeDecision reports how a key event should affect overlay dismissal.
type EscapeDecision uint8

// EscapeGuardOnKey outcomes.
const (
	EscapeAllowClose EscapeDecision = iota
	EscapeArm
	EscapeSwallowCooloff
)

// NormalizeEscapeGuardPolicy normalizes a policy string to one of the four
// known policy constants, falling back to EscapeGuardOff when unknown.
func NormalizeEscapeGuardPolicy(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case EscapeGuardOff:
		return EscapeGuardOff
	case EscapeGuardDouble:
		return EscapeGuardDouble
	case EscapeGuardCooloff:
		return EscapeGuardCooloff
	case EscapeGuardBoth:
		return EscapeGuardBoth
	default:
		return EscapeGuardOff
	}
}

func escapeGuardCooloff(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultEscapeGuardCooloff
	}
	return d
}

// EscapeGuardOnExitInsert transitions state when leaving insert/editing mode.
// It clears Armed and starts a cooloff window when the policy includes cooloff.
func EscapeGuardOnExitInsert(st EscapeGuardState, cfg EscapeGuardConfig, now time.Time) EscapeGuardState {
	st.Armed = false
	policy := NormalizeEscapeGuardPolicy(cfg.Policy)
	if policy == EscapeGuardCooloff || policy == EscapeGuardBoth {
		st.CooloffUntil = now.Add(escapeGuardCooloff(cfg.Cooloff))
	} else {
		st.CooloffUntil = time.Time{}
	}
	return st
}

// EscapeGuardOnKey advances state for a key event and returns whether an escape
// key may close the overlay, should arm a double-escape confirmation, or should
// be swallowed during cooloff.
func EscapeGuardOnKey(st EscapeGuardState, cfg EscapeGuardConfig, isEscape bool, now time.Time) (EscapeGuardState, EscapeDecision) {
	if !isEscape {
		st.Armed = false
		return st, EscapeAllowClose
	}
	policy := NormalizeEscapeGuardPolicy(cfg.Policy)
	if (policy == EscapeGuardCooloff || policy == EscapeGuardBoth) && !st.CooloffUntil.IsZero() && now.Before(st.CooloffUntil) {
		return st, EscapeSwallowCooloff
	}
	if (policy == EscapeGuardDouble || policy == EscapeGuardBoth) && !st.Armed {
		st.Armed = true
		return st, EscapeArm
	}
	st.Armed = false
	st.CooloffUntil = time.Time{}
	return st, EscapeAllowClose
}

// EscapeGuardStatus returns whether a guard indicator should be rendered on the
// overlay along with its semantic tone and badge label.
func EscapeGuardStatus(st EscapeGuardState, cfg EscapeGuardConfig, now time.Time) (active bool, tone string, badge string) {
	policy := NormalizeEscapeGuardPolicy(cfg.Policy)
	if (policy == EscapeGuardCooloff || policy == EscapeGuardBoth) && !st.CooloffUntil.IsZero() && now.Before(st.CooloffUntil) {
		return true, "warning", "esc locked"
	}
	if (policy == EscapeGuardDouble || policy == EscapeGuardBoth) && st.Armed {
		return true, "warning", "esc again to close"
	}
	return false, "", ""
}
