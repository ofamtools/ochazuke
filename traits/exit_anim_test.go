package traits_test

import (
	"slices"
	"testing"
	"time"

	"ofam.dev/ochazuke/traits"
)

func TestExitAnimSetLifecycleAndZeroIdleTick(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	var set traits.ExitAnimSet

	// Empty set reports zero idle CPU (NextTick ok == false).
	if d, ok := set.NextTick(t0); ok || d != 0 {
		t.Fatalf("empty NextTick = (%v, %v), want (0, false)", d, ok)
	}
	if set.Active("a", t0) {
		t.Fatal("empty set must not report active")
	}
	if p, ok := set.Progress("a", t0); ok || p != 0 {
		t.Fatalf("empty Progress = (%v, %v), want (0, false)", p, ok)
	}
	if set.Cancel("a") {
		t.Fatal("Cancel on missing key should return false")
	}

	// Start animation with default 2500ms duration, 60ms tick.
	set.Start("item-1", t0)
	if !set.Active("item-1", t0) || set.Len() != 1 {
		t.Fatalf("item-1 should be active after Start")
	}
	if p, ok := set.Progress("item-1", t0); !ok || p != 0 {
		t.Fatalf("Progress at start = (%v, %v), want (0, true)", p, ok)
	}
	if d, ok := set.NextTick(t0); !ok || d != traits.DefaultExitAnimTickInterval {
		t.Fatalf("NextTick at start = (%v, %v), want (%v, true)", d, ok, traits.DefaultExitAnimTickInterval)
	}

	// Midpoint progress.
	mid := t0.Add(1250 * time.Millisecond)
	if p, ok := set.Progress("item-1", mid); !ok || p != 0.5 {
		t.Fatalf("Progress at mid = (%v, %v), want (0.5, true)", p, ok)
	}

	// Near expiry: NextTick clamps to remaining duration.
	nearEnd := t0.Add(2480 * time.Millisecond)
	if d, ok := set.NextTick(nearEnd); !ok || d != 20*time.Millisecond {
		t.Fatalf("NextTick near expiry = (%v, %v), want (20ms, true)", d, ok)
	}

	// At expiry (before Prune): Active is false, Progress clamps to 1.0 (no flicker).
	end := t0.Add(2500 * time.Millisecond)
	if set.Active("item-1", end) {
		t.Fatal("Active at expiry must be false")
	}
	if p, ok := set.Progress("item-1", end); !ok || p != 1.0 {
		t.Fatalf("Progress at expiry = (%v, %v), want (1.0, true)", p, ok)
	}

	// Prune removes expired key and restores zero-idle state.
	expired, still := set.Prune(end)
	if still || !slices.Equal(expired, []string{"item-1"}) {
		t.Fatalf("Prune(end) = (%v, %v), want ([item-1], false)", expired, still)
	}
	if d, ok := set.NextTick(end); ok || d != 0 {
		t.Fatalf("NextTick after Prune = (%v, %v), want (0, false)", d, ok)
	}

	// Cancel stops an active animation immediately.
	set.Start("item-2", end)
	if !set.Cancel("item-2") || set.Active("item-2", end) {
		t.Fatal("Cancel failed to remove item-2")
	}
	if _, ok := set.NextTick(end); ok {
		t.Fatal("NextTick should be false after cancelling last item")
	}
}

func TestExitAnimSetMultiItemIndependentProgress(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	set := traits.NewExitAnimSet(traits.ExitAnimConfig{
		Duration:     time.Second,
		TickInterval: 50 * time.Millisecond,
	})
	set.Start("b", t0)
	set.Start("a", t0)
	set.Start("c", t0.Add(500*time.Millisecond))

	// At t0+500ms: a & b are at 0.5, c is at 0.0.
	t500 := t0.Add(500 * time.Millisecond)
	pa, _ := set.Progress("a", t500)
	pc, _ := set.Progress("c", t500)
	if pa != 0.5 || pc != 0.0 {
		t.Fatalf("independent progress at 500ms: a=%v c=%v, want 0.5 and 0.0", pa, pc)
	}

	// At t0+980ms: NextTick clamps to a/b's 20ms remaining, not c's 520ms.
	if d, ok := set.NextTick(t0.Add(980 * time.Millisecond)); !ok || d != 20*time.Millisecond {
		t.Fatalf("NextTick = (%v, %v), want (20ms, true)", d, ok)
	}

	// At t0+1000ms: Prune expires a and b in sorted order while keeping c active.
	t1000 := t0.Add(time.Second)
	expired, still := set.Prune(t1000)
	if !still || !slices.Equal(expired, []string{"a", "b"}) {
		t.Fatalf("Prune(1000ms) = (%v, %v), want ([a b], true)", expired, still)
	}
	if !set.Active("c", t1000) {
		t.Fatal("c should still be active at 1000ms")
	}
	if pc2, ok := set.Progress("c", t1000); !ok || pc2 != 0.5 {
		t.Fatalf("c progress at 1000ms = (%v, %v), want (0.5, true)", pc2, ok)
	}

	// At t0+1500ms: c expires too.
	expired2, still2 := set.Prune(t0.Add(1500 * time.Millisecond))
	if still2 || !slices.Equal(expired2, []string{"c"}) {
		t.Fatalf("Prune(1500ms) = (%v, %v), want ([c], false)", expired2, still2)
	}
}

func TestDissolveTextPhase1ColorSweepRTLAndLTR(t *testing.T) {
	cfgRTL := traits.ExitAnimConfig{FadePhase: 0.5, Direction: traits.ExitAnimRTL}
	cfgLTR := traits.ExitAnimConfig{FadePhase: 0.5, Direction: traits.ExitAnimLTR}

	// Boundary checks.
	if vis, sp := traits.DissolveText("Hello", 0, cfgRTL); vis != "Hello" || sp != nil {
		t.Fatalf("p=0 => (%q, %+v)", vis, sp)
	}
	if vis, sp := traits.DissolveText("Hello", 1.0, cfgRTL); vis != "" || sp != nil {
		t.Fatalf("p=1 => (%q, %+v)", vis, sp)
	}
	if vis, sp := traits.DissolveText("", 0.3, cfgRTL); vis != "" || sp != nil {
		t.Fatalf("empty => (%q, %+v)", vis, sp)
	}

	// RTL early sweep: full text visible, rightmost char has wave[0] ("accent").
	vis, sp := traits.DissolveText("Hello", 0.01, cfgRTL)
	if vis != "Hello" {
		t.Fatalf("Phase 1 RTL must keep full text, got %q", vis)
	}
	if len(sp) != 1 || sp[0] != (traits.ExitSpan{From: 4, To: 5, Tone: "accent"}) {
		t.Fatalf("Phase 1 RTL early spans = %+v, want [{4 5 accent}]", sp)
	}

	// RTL mid-sweep: wave in middle, dimmed tail on right.
	vis, sp = traits.DissolveText("Hello", 0.25, cfgRTL) // t=0.5, pos=floor(0.5*8)=4, front=0 -> 0:accent, 1:warning, 2:muted, 3..5:row.dim
	if vis != "Hello" {
		t.Fatalf("Phase 1 RTL mid vis = %q", vis)
	}
	wantMid := []traits.ExitSpan{
		{From: 0, To: 1, Tone: "accent"},
		{From: 1, To: 2, Tone: "warning"},
		{From: 2, To: 3, Tone: "muted"},
		{From: 3, To: 5, Tone: "row.dim"},
	}
	if !slices.Equal(sp, wantMid) {
		t.Fatalf("Phase 1 RTL mid spans = %+v, want %+v", sp, wantMid)
	}

	// RTL end of Phase 1: entire text dimmed cleanly into one span.
	vis, sp = traits.DissolveText("Hello", 0.49, cfgRTL)
	if vis != "Hello" || !slices.Equal(sp, []traits.ExitSpan{{From: 0, To: 5, Tone: "row.dim"}}) {
		t.Fatalf("Phase 1 RTL end = (%q, %+v)", vis, sp)
	}

	// LTR early sweep: leftmost char has wave[0] ("accent").
	vis, sp = traits.DissolveText("Hello", 0.01, cfgLTR)
	if vis != "Hello" || !slices.Equal(sp, []traits.ExitSpan{{From: 0, To: 1, Tone: "accent"}}) {
		t.Fatalf("Phase 1 LTR early = (%q, %+v)", vis, sp)
	}

	// LTR mid-sweep: dimmed tail on left, wave advancing right.
	vis, sp = traits.DissolveText("Hello", 0.25, cfgLTR) // pos=4 -> 0..2:row.dim, 2:muted, 3:warning, 4:accent
	wantLTRMid := []traits.ExitSpan{
		{From: 0, To: 2, Tone: "row.dim"},
		{From: 2, To: 3, Tone: "muted"},
		{From: 3, To: 4, Tone: "warning"},
		{From: 4, To: 5, Tone: "accent"},
	}
	if vis != "Hello" || !slices.Equal(sp, wantLTRMid) {
		t.Fatalf("Phase 1 LTR mid = (%q, %+v), want %+v", vis, sp, wantLTRMid)
	}
}

func TestDissolveTextPhase2ErosionRTLAndLTR(t *testing.T) {
	cfgRTL := traits.ExitAnimConfig{FadePhase: 0.4, Direction: traits.ExitAnimRTL}
	cfgLTR := traits.ExitAnimConfig{FadePhase: 0.4, Direction: traits.ExitAnimLTR}

	// RTL trims from right, keeping glowing edge char at the right end.
	prevLen := 6
	for _, p := range []float64{0.40, 0.55, 0.70, 0.85, 0.99} {
		vis, sp := traits.DissolveText("ABCDEF", p, cfgRTL)
		n := len([]rune(vis))
		if n < 1 || n > prevLen {
			t.Fatalf("RTL p=%v len=%d (prev=%d) vis=%q", p, n, prevLen, vis)
		}
		prevLen = n
		if vis != "ABCDEF"[:n] {
			t.Fatalf("RTL p=%v vis=%q, want prefix of ABCDEF", p, vis)
		}
		last := sp[len(sp)-1]
		if last != (traits.ExitSpan{From: n - 1, To: n, Tone: "accent"}) {
			t.Fatalf("RTL p=%v missing glowing right edge span: %+v", p, sp)
		}
		if n > 1 && sp[0] != (traits.ExitSpan{From: 0, To: n - 1, Tone: "row.dim"}) {
			t.Fatalf("RTL p=%v expected dimmed body span: %+v", p, sp)
		}
	}

	// LTR trims from left, keeping glowing edge char at the left end.
	prevLen = 6
	for _, p := range []float64{0.40, 0.55, 0.70, 0.85, 0.99} {
		vis, sp := traits.DissolveText("ABCDEF", p, cfgLTR)
		n := len([]rune(vis))
		if n < 1 || n > prevLen {
			t.Fatalf("LTR p=%v len=%d (prev=%d) vis=%q", p, n, prevLen, vis)
		}
		prevLen = n
		if vis != "ABCDEF"[6-n:] {
			t.Fatalf("LTR p=%v vis=%q, want suffix of ABCDEF", p, vis)
		}
		if sp[0] != (traits.ExitSpan{From: 0, To: 1, Tone: "accent"}) {
			t.Fatalf("LTR p=%v missing glowing left edge span: %+v", p, sp)
		}
		if n > 1 && sp[1] != (traits.ExitSpan{From: 1, To: n, Tone: "row.dim"}) {
			t.Fatalf("LTR p=%v expected dimmed tail span: %+v", p, sp)
		}
	}

	// Unicode rune safety: multi-byte runes never split mid-character.
	cfgExact := traits.ExitAnimConfig{FadePhase: 0.5, Direction: traits.ExitAnimRTL}
	vis, sp := traits.DissolveText("日本語🚀", 0.75, cfgExact) // exact t=0.5 -> eroded=2, rem=2 -> "日本"
	if vis != "日本" {
		t.Fatalf("Unicode RTL dissolve = %q, want %q", vis, "日本")
	}
	if !slices.Equal(sp, []traits.ExitSpan{{From: 0, To: 1, Tone: "row.dim"}, {From: 1, To: 2, Tone: "accent"}}) {
		t.Fatalf("Unicode RTL spans = %+v", sp)
	}
}
