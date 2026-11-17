package ochazuke

import (
	"fmt"
	"testing"
)

// BenchmarkPaletteKeystroke is one key typed into the open palette over a
// registry of the size an app with its tools mounted has (2,400 verbs,
// each behind a guard): typing "noun042 ver" a key at a time, then erasing
// it. A key that only edits the line filters what the open computed, so
// guards/key stays 0; it was every verb's guard per key while typing
// moved the palette's change counter.
func BenchmarkPaletteKeystroke(b *testing.B) {
	r := NewRuntime()
	r.Config().Disable()
	guards := 0
	r.DefineGuard("bench-offered", func(*Runtime, KeyEvent) bool { guards++; return true })
	for i := range 150 {
		n := r.Define(fmt.Sprintf("noun%03d", i))
		for j := range 16 {
			n.Verb(fmt.Sprintf("verb%02d", j), func(Ctx) Result { return Done(nil) }).When("bench-offered")
		}
	}
	p := NewPalette(r)
	if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "open"}); err != nil {
		b.Fatal(err)
	}
	p.View()
	line := "noun042 ver"
	keys := make([]string, 0, 2*len(line))
	for _, c := range line {
		keys = append(keys, string(c))
	}
	for range line {
		keys = append(keys, "backspace")
	}
	guards = 0
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for b.Loop() {
		if _, err := p.HandleKey(keys[n%len(keys)]); err != nil {
			b.Fatal(err)
		}
		p.View()
		n++
	}
	b.ReportMetric(float64(guards)/float64(n), "guards/key")
}
