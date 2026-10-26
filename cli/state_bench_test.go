package cli

import (
	"fmt"
	"strings"
	"testing"
)

// benchRows is a state answer the size of o-ai's: 8,292 skill rows of
// about 120 bytes each (~1 MB); variant changes one row's label.
func benchRows(variant int) []Row {
	rows := make([]Row, 8292)
	for i := range rows {
		label := fmt.Sprintf("skill-%05d [claude]", i)
		if i == 4321 {
			label = fmt.Sprintf("changed v%d [claude]", variant)
		}
		rows[i] = Row{Noun: "skill", ID: fmt.Sprintf("skill/claude:user:s%05d", i), Label: label,
			Data:  map[string]any{"name": fmt.Sprintf("skill-%05d", i), "harness": "claude", "path": fmt.Sprintf("~/.claude/skills/skill-%05d/SKILL.md", i)},
			Cells: []string{fmt.Sprintf("skill-%05d", i), "claude"}}
	}
	return rows
}

func benchDoc() CatalogDoc {
	doc := stateDoc(true)
	doc.Pages[0].Lists = doc.Pages[0].Lists[:1]
	return doc
}

// BenchmarkStatePoll is one poll of an 8k-row state, extension and host
// together: the extension's shell builds the answer (in a fresh runtime,
// as a new process would) and the host decodes and files it into a
// mounted page's list. before is the plain answer (every row, SetState);
// after is the delta answer (ApplyState). unchanged polls the same rows;
// one-row alternates a single changed row.
func BenchmarkStatePoll(b *testing.B) {
	for _, tc := range []struct {
		name  string
		delta bool
		flip  bool
		fp    bool
	}{
		{"unchanged/before", false, false, false},
		{"unchanged/after", true, false, false},
		{"unchanged/after-fingerprint", true, false, true},
		{"one-row/before", false, true, false},
		{"one-row/after", true, true, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			variants := [][]Row{benchRows(0), benchRows(1)}
			st := newStateTool(b, variants[0]...)
			if tc.fp {
				st.fp = "inputs"
			}
			_, v := mountedPage(b, benchDoc(), PageConfig{}, 100, 40)
			poll := func() int {
				var flags []string
				if tc.delta {
					flags = v.SinceFlags()
				}
				out, _ := st.run(b, flags...)
				a, err := DecodeState([]byte(out))
				if err != nil {
					b.Fatal(err)
				}
				if bad, err := v.ApplyState(a); err != nil || len(bad) > 0 {
					b.Fatalf("ApplyState = %v, %v", bad, err)
				}
				return len(out)
			}
			poll() // the first load
			var size, i int
			for b.Loop() {
				if tc.flip {
					i++
					st.set(variants[i%2]...)
				}
				size = poll()
			}
			b.ReportMetric(float64(size), "answer-bytes")
		})
	}
}

// BenchmarkStateApply is the host's half of a poll alone: decoding the
// answer and filing it into the page (the answers are made once).
func BenchmarkStateApply(b *testing.B) {
	for _, tc := range []struct {
		name  string
		delta bool
	}{{"unchanged/before", false}, {"unchanged/after", true}, {"one-row/before", false}, {"one-row/after", true}} {
		b.Run(tc.name, func(b *testing.B) {
			variants := [][]Row{benchRows(0), benchRows(1)}
			st := newStateTool(b, variants[0]...)
			_, v := mountedPage(b, benchDoc(), PageConfig{}, 100, 40)
			apply := func(out string) {
				a, err := DecodeState([]byte(out))
				if err != nil {
					b.Fatal(err)
				}
				if bad, err := v.ApplyState(a); err != nil || len(bad) > 0 {
					b.Fatalf("ApplyState = %v, %v", bad, err)
				}
			}
			flags := func() []string {
				if tc.delta {
					return v.SinceFlags()
				}
				return nil
			}
			apply(first(st.run(b, flags()...)))
			// Two answers that take the page from one variant to the other
			// and back (or the same rows twice).
			var answers [2]string
			for i := range answers {
				if strings.HasPrefix(tc.name, "one-row") {
					st.set(variants[(i+1)%2]...)
				}
				answers[i] = first(st.run(b, flags()...))
				apply(answers[i])
			}
			i := 0
			for b.Loop() {
				apply(answers[i%2])
				i++
			}
		})
	}
}
