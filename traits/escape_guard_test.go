package traits_test

import (
	"testing"
	"time"

	"ofam.dev/ochazuke/traits"
)

func TestNormalizeEscapeGuardPolicy(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"off", traits.EscapeGuardOff},
		{"double", traits.EscapeGuardDouble},
		{"cooloff", traits.EscapeGuardCooloff},
		{"both", traits.EscapeGuardBoth},
		{"  DOUBLE ", traits.EscapeGuardDouble},
		{"CoolOff", traits.EscapeGuardCooloff},
		{"", traits.EscapeGuardOff},
		{"unknown", traits.EscapeGuardOff},
	}
	for _, tc := range cases {
		if got := traits.NormalizeEscapeGuardPolicy(tc.in); got != tc.want {
			t.Errorf("NormalizeEscapeGuardPolicy(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeGuardOff(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	cfg := traits.EscapeGuardConfig{Policy: traits.EscapeGuardOff}
	st := traits.EscapeGuardOnExitInsert(traits.EscapeGuardState{Armed: true}, cfg, now)
	if st.Armed || !st.CooloffUntil.IsZero() {
		t.Fatalf("off policy exit insert state = %+v", st)
	}
	st, dec := traits.EscapeGuardOnKey(st, cfg, true, now)
	if dec != traits.EscapeAllowClose {
		t.Fatalf("off policy decision = %v, want EscapeAllowClose", dec)
	}
	if active, tone, badge := traits.EscapeGuardStatus(st, cfg, now); active || tone != "" || badge != "" {
		t.Fatalf("off policy status = (%v, %q, %q), want inactive", active, tone, badge)
	}
}

func TestEscapeGuardDouble(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	cfg := traits.EscapeGuardConfig{Policy: traits.EscapeGuardDouble}
	var st traits.EscapeGuardState

	// First escape arms the guard.
	st, dec := traits.EscapeGuardOnKey(st, cfg, true, now)
	if dec != traits.EscapeArm || !st.Armed {
		t.Fatalf("first esc = (%+v, %v), want armed + EscapeArm", st, dec)
	}
	active, tone, badge := traits.EscapeGuardStatus(st, cfg, now)
	if !active || tone != "warning" || badge != "esc again to close" {
		t.Fatalf("armed status = (%v, %q, %q)", active, tone, badge)
	}

	// Any non-escape key disarms.
	st, dec = traits.EscapeGuardOnKey(st, cfg, false, now.Add(50*time.Millisecond))
	if dec != traits.EscapeAllowClose || st.Armed {
		t.Fatalf("non-esc = (%+v, %v), want disarmed", st, dec)
	}
	if active, _, _ := traits.EscapeGuardStatus(st, cfg, now.Add(50*time.Millisecond)); active {
		t.Fatal("status should be inactive after non-escape key")
	}

	// Re-arm and confirm with second escape.
	st, dec = traits.EscapeGuardOnKey(st, cfg, true, now.Add(100*time.Millisecond))
	if dec != traits.EscapeArm || !st.Armed {
		t.Fatalf("re-arm = (%+v, %v)", st, dec)
	}
	st, dec = traits.EscapeGuardOnKey(st, cfg, true, now.Add(200*time.Millisecond))
	if dec != traits.EscapeAllowClose || st.Armed {
		t.Fatalf("second esc = (%+v, %v), want allow close + disarmed", st, dec)
	}
}

func TestEscapeGuardCooloff(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	// Default cooloff (0 -> 1s).
	cfg := traits.EscapeGuardConfig{Policy: traits.EscapeGuardCooloff}
	st := traits.EscapeGuardOnExitInsert(traits.EscapeGuardState{Armed: true}, cfg, now)
	if st.Armed || !st.CooloffUntil.Equal(now.Add(time.Second)) {
		t.Fatalf("after exit insert = %+v", st)
	}

	// Inside cooloff window: swallowed, status reports locked.
	t500 := now.Add(500 * time.Millisecond)
	active, tone, badge := traits.EscapeGuardStatus(st, cfg, t500)
	if !active || tone != "warning" || badge != "esc locked" {
		t.Fatalf("cooloff status = (%v, %q, %q)", active, tone, badge)
	}
	st, dec := traits.EscapeGuardOnKey(st, cfg, true, t500)
	if dec != traits.EscapeSwallowCooloff {
		t.Fatalf("inside cooloff decision = %v, want EscapeSwallowCooloff", dec)
	}

	// At/after cooloff deadline: allowed to close.
	t1000 := now.Add(time.Second)
	if active, _, _ := traits.EscapeGuardStatus(st, cfg, t1000); active {
		t.Fatal("cooloff status should clear at deadline")
	}
	st, dec = traits.EscapeGuardOnKey(st, cfg, true, t1000)
	if dec != traits.EscapeAllowClose || !st.CooloffUntil.IsZero() {
		t.Fatalf("after cooloff = (%+v, %v)", st, dec)
	}

	// Custom cooloff duration respected.
	custom := traits.EscapeGuardConfig{Policy: traits.EscapeGuardCooloff, Cooloff: 250 * time.Millisecond}
	st = traits.EscapeGuardOnExitInsert(traits.EscapeGuardState{}, custom, now)
	if !st.CooloffUntil.Equal(now.Add(250 * time.Millisecond)) {
		t.Fatalf("custom cooloff deadline = %v", st.CooloffUntil)
	}
}

func TestEscapeGuardBoth(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	cfg := traits.EscapeGuardConfig{Policy: traits.EscapeGuardBoth, Cooloff: 400 * time.Millisecond}

	st := traits.EscapeGuardOnExitInsert(traits.EscapeGuardState{Armed: true}, cfg, now)
	// During cooloff: swallowed, does not arm.
	st, dec := traits.EscapeGuardOnKey(st, cfg, true, now.Add(200*time.Millisecond))
	if dec != traits.EscapeSwallowCooloff || st.Armed {
		t.Fatalf("during cooloff = (%+v, %v)", st, dec)
	}
	if active, tone, badge := traits.EscapeGuardStatus(st, cfg, now.Add(200*time.Millisecond)); !active || tone != "warning" || badge != "esc locked" {
		t.Fatalf("cooloff status = (%v, %q, %q)", active, tone, badge)
	}

	// After cooloff expires: first escape arms double-escape.
	after := now.Add(400 * time.Millisecond)
	st, dec = traits.EscapeGuardOnKey(st, cfg, true, after)
	if dec != traits.EscapeArm || !st.Armed {
		t.Fatalf("after cooloff first esc = (%+v, %v)", st, dec)
	}
	if active, tone, badge := traits.EscapeGuardStatus(st, cfg, after); !active || tone != "warning" || badge != "esc again to close" {
		t.Fatalf("armed status = (%v, %q, %q)", active, tone, badge)
	}

	// Second escape closes and clears state.
	st, dec = traits.EscapeGuardOnKey(st, cfg, true, after.Add(50*time.Millisecond))
	if dec != traits.EscapeAllowClose || st.Armed || !st.CooloffUntil.IsZero() {
		t.Fatalf("second esc = (%+v, %v)", st, dec)
	}
}
