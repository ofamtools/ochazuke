package toast

import (
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func TestStoreLifecycleAndClock(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	var s *Store = NewWithClock(func() time.Time { return now })

	if got := s.Active(); got != nil {
		t.Fatalf("empty Active() = %v, want nil", got)
	}
	if _, ok := s.NextExpiry(); ok {
		t.Fatal("empty NextExpiry() should be false")
	}

	t1 := s.Push("Saved config", Ok)
	if t1.ID == "" || t1.Level != Ok || !t1.CreatedAt.Equal(now) || t1.TTL != DefaultTTL {
		t.Fatalf("unexpected toast t1: %+v", t1)
	}

	now = now.Add(time.Second)
	t2 := s.Push("Rate limited", Warn, 5*time.Second)
	t3 := s.Push("Sticky error", Err, -1) // sticky
	if t3.TTL != 0 {
		t.Fatalf("sticky toast TTL = %v, want 0", t3.TTL)
	}
	if s.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", s.Len())
	}

	// NextExpiry should be t1 at 10:00:03.
	exp, ok := s.NextExpiry()
	if !ok || !exp.Equal(t1.CreatedAt.Add(DefaultTTL)) {
		t.Fatalf("NextExpiry() = (%v, %v), want %v", exp, ok, t1.CreatedAt.Add(DefaultTTL))
	}

	// Before t1 expires, Tick returns false.
	if s.Tick(t1.CreatedAt.Add(DefaultTTL - time.Millisecond)) {
		t.Fatal("Tick before expiry should return false")
	}
	if s.Len() != 3 {
		t.Fatalf("Len() after early tick = %d, want 3", s.Len())
	}

	// At exact t1 expiry, t1 is removed; t2 and sticky t3 remain.
	if !s.Tick(t1.CreatedAt.Add(DefaultTTL)) {
		t.Fatal("Tick at t1 expiry should return true")
	}
	active := s.Active()
	if len(active) != 2 || active[0].ID != t2.ID || active[1].ID != t3.ID {
		t.Fatalf("Active after t1 expiry = %+v", active)
	}

	// Dismiss by ID.
	if !s.Dismiss(t2.ID) || s.Dismiss("nonexistent") {
		t.Fatal("Dismiss mismatch")
	}
	if s.Len() != 1 {
		t.Fatalf("Len after dismiss = %d, want 1", s.Len())
	}

	// Sticky toast survives far-future Tick.
	if s.Tick(now.Add(24 * time.Hour)) {
		t.Fatal("sticky toast should not expire")
	}
	if s.Len() != 1 {
		t.Fatalf("sticky toast vanished")
	}

	// DismissAll clears everything.
	s.DismissAll()
	if s.Len() != 0 || s.Active() != nil {
		t.Fatalf("after DismissAll: len=%d active=%v", s.Len(), s.Active())
	}
}

func TestStoreMaxActiveAndUpsert(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	m := NewWithClock(func() time.Time { return now })
	m.MaxActive = 2

	a := m.Push("one", Info)
	b := m.Push("two", Ok)
	c := m.Push("three", Err)

	active := m.Active()
	if len(active) != 2 || active[0].ID != b.ID || active[1].ID != c.ID {
		t.Fatalf("MaxActive eviction failed: got %+v (first was %s)", active, a.ID)
	}

	// Upsert same ID via PushToast replaces in place.
	updated := m.PushToast(Toast{ID: b.ID, Text: "two updated", Level: Warn})
	if updated.ID != b.ID || updated.Level != Warn {
		t.Fatalf("PushToast upsert = %+v", updated)
	}
	if got := m.Active(); len(got) != 2 || got[0].Text != "two updated" {
		t.Fatalf("after upsert: %+v", got)
	}
}

func TestStoreDisabledOptOut(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	m := NewWithClock(func() time.Time { return now })
	m.SetEnabled(false)
	if m.Enabled() {
		t.Fatal("expected Disabled")
	}

	if got := m.Push("ignored", Info); got.ID != "" {
		t.Fatalf("Push while disabled returned %+v", got)
	}
	if m.Len() != 0 || m.Active() != nil {
		t.Fatalf("expected empty while disabled")
	}

	v := m.View("toasts")
	if v.Facts["disabled"] != true || v.Facts["active"] != false || v.Props["text"] != "" {
		t.Fatalf("disabled View = %+v", v)
	}

	// Re-enabling works and disabling hides existing toasts.
	m.SetEnabled(true)
	m.Push("hello", Ok)
	if m.Len() != 1 {
		t.Fatalf("expected 1 toast after re-enable")
	}
	m.Disabled = true
	if m.Len() != 0 || m.Active() != nil {
		t.Fatal("Disabled=true should suppress Active and Len")
	}
	if m.View("toasts").Facts["active"] != false {
		t.Fatal("Disabled=true should suppress View")
	}
}

func TestStoreViewAndRender(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	m := NewWithClock(func() time.Time { return now })
	m.Push("Saved", Ok)
	m.Push("Disk 90%", Warn)

	node := m.View("strip")
	if node.Kind != ui.KindText || node.ID != "strip" || node.Facts["count"] != 2 || node.Facts["level"] != string(Warn) {
		t.Fatalf("unexpected strip node: %+v", node)
	}

	f, _ := ui.Render(node, ui.DefaultDarkTheme(), "", 40, 1)
	if !strings.Contains(f.ANSI, "▲ Disk 90%") || !strings.Contains(f.ANSI, "✓ Saved") {
		t.Fatalf("rendered strip missing text: %q", f.ANSI)
	}
	// Newest toast appears first.
	if strings.Index(f.ANSI, "Disk 90%") > strings.Index(f.ANSI, "Saved") {
		t.Fatalf("expected newest toast first in strip: %q", f.ANSI)
	}

	ov := m.ViewOverlay("toast-ov")
	if ov.Kind != ui.KindOverlay || len(ov.Children) != 2 {
		t.Fatalf("unexpected overlay node: %+v", ov)
	}
	fov, _ := ui.Render(ov, ui.DefaultDarkTheme(), "", 40, 8)
	if !strings.Contains(fov.ANSI, "Notifications") || !strings.Contains(fov.ANSI, "Disk 90%") || !strings.Contains(fov.ANSI, "Saved") {
		t.Fatalf("rendered overlay missing content: %q", fov.ANSI)
	}

	// Commands dismiss latest and all.
	if !m.HandleCommand(core.Command{ID: CmdDismiss}) || m.Len() != 1 {
		t.Fatalf("CmdDismiss failed: len=%d", m.Len())
	}
	if !m.HandleCommand(core.Command{ID: CmdDismissAll}) || m.Len() != 0 {
		t.Fatalf("CmdDismissAll failed: len=%d", m.Len())
	}
}
