package ochazuke

import "testing"

// Each Normalize kind adapts the typed value before the check; nothing is
// refused for its shape.
func TestNormalizeKinds(t *testing.T) {
	r := NewRuntime()
	echo := func(_ *Runtime, _ Context, a Arguments) (any, error) { return a["v"], nil }
	for _, tc := range []struct {
		kind, typ string
		in, want  any
	}{
		{NormalizeTrim, "string", "  a b  ", "a b"},
		{NormalizeSlug, "string", "My Ext!", "my-ext"},
		{NormalizeEscape, "string", "a\nb", `a\nb`},
		{NormalizeCoerce, SettingInteger, "3", 3},
	} {
		r.Register(Operation{Key: KeyOf("norm", tc.kind), Parameters: map[string]Parameter{"v": {Type: tc.typ, Normalize: []string{tc.kind}}}, Run: echo})
		got, err := r.Execute(Request{Noun: "norm", Verb: tc.kind, Args: Arguments{"v": tc.in}})
		if err != nil || got != tc.want {
			t.Fatalf("%s: %#v %v, want %#v", tc.kind, got, err, tc.want)
		}
	}
}
