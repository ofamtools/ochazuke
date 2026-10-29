package ochazuke

import "testing"

// mode-badge.key-case sets the case of the held keys only: upper by
// default (g and G both read NORMAL G), as-typed keeps them as typed; the
// mode's name stays upper case either way, whether the keys are part of
// the mode's name (normal g) or only its payload (leader_pending).
func TestModeBadgeKeyCase(t *testing.T) {
	r, m := focusDisplayApp(t)
	if got := r.ModeBadgeKeyCase(); got != ModeBadgeKeysUpper {
		t.Fatalf("default mode-badge.key-case = %q, want upper", got)
	}
	cases := []struct {
		mode, prefix, upper, typed string
	}{
		{"normal", "", "NORMAL", "NORMAL"},
		{"normal g", "g", "NORMAL G", "NORMAL g"},
		{"normal G", "G", "NORMAL G", "NORMAL G"},
		{"normal z R", "z R", "NORMAL Z R", "NORMAL z R"},
		{"leader_pending", "ctrl+a", "LEADER PENDING CTRL+A", "LEADER PENDING ctrl+a"},
		{"g", "g", "G", "g"},
	}
	for _, value := range ModeBadgeKeyCases {
		if err := r.Settings().Set(ModeBadgeKeyCaseSetting, value); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			r.Mode = ModeState{Name: c.mode, Payload: Payload{Prefix: c.prefix}}
			want := c.upper
			if value == ModeBadgeKeysAsTyped {
				want = c.typed
			}
			if got := m.modeBadge(); got != want {
				t.Errorf("%s: mode %q prefix %q: badge %q, want %q", value, c.mode, c.prefix, got, want)
			}
		}
	}
	if err := r.Settings().Set(ModeBadgeKeyCaseSetting, "lower"); err == nil {
		t.Fatal("mode-badge.key-case took a value outside its enum")
	}
}
