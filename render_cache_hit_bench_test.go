package ochazuke

import "testing"

// The cached reads an idle frame repeats: the binding walk, the help rows
// and the footer hint cells. A hit must not grow with the number of rows.
func BenchmarkBindingWalkCached(b *testing.B) {
	m, _ := renderFixture(b)
	r := m.Runtime
	r.levelRows(r.Focus)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r.levelRows(r.Focus)
	}
}

func BenchmarkBindingHelpCached(b *testing.B) {
	m, _ := renderFixture(b)
	r := m.Runtime
	BindingHelp(r, r.Focus)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		BindingHelp(r, r.Focus)
	}
}

func BenchmarkHintCellsCached(b *testing.B) {
	m, _ := renderFixture(b)
	r := m.Runtime
	HintCells(r, r.Focus)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		HintCells(r, r.Focus)
	}
}
