package ochazuke

import (
	"encoding/json"
	"maps"
	"reflect"
	"sync"
	"testing"
)

func BenchmarkNewRuntime(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		NewRuntime()
	}
}

// RowsOf is DefaultBindings filtered to one table, row for row.
func TestRowsOfMatchesDefaultBindings(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	all := r.DefaultBindings()
	seen := map[[2]string]bool{}
	for _, d := range all {
		if d.Instance != "" || seen[[2]string{d.Noun, d.Mode}] {
			continue
		}
		seen[[2]string{d.Noun, d.Mode}] = true
		var want []DefaultBinding
		for _, x := range all {
			if x.Instance == "" && x.Noun == d.Noun && x.Mode == d.Mode {
				want = append(want, x)
			}
		}
		if got := r.RowsOf(d.Noun, d.Mode); !reflect.DeepEqual(got, want) {
			t.Fatalf("RowsOf(%q, %q) differs from DefaultBindings: %d rows, want %d", d.Noun, d.Mode, len(got), len(want))
		}
	}
	if len(seen) == 0 {
		t.Fatal("no tables")
	}
	if got := r.RowsOf("no-such-noun", ""); len(got) != 0 {
		t.Fatalf("unknown noun has rows: %v", got)
	}
}

// The shared parse of the embedded files is never changed by a runtime: a
// runtime that rewrites its rows (a setting change) leaves the next
// runtime's rows as the file wrote them. Run with -race.
func TestSharedBindingParseIsImmutable(t *testing.T) {
	t.Parallel()
	snapshot := map[string]BindingsDoc{}
	for name, doc := range frameworkBindingDocs() {
		c := BindingsDoc{Modes: maps.Clone(doc.Modes)}
		for _, g := range doc.Groups {
			rows := g.Rows
			g.Rows = nil
			for _, row := range rows {
				g.Rows = append(g.Rows, cloneBinding(row))
			}
			c.Groups = append(c.Groups, g)
		}
		snapshot[name] = c
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := NewRuntime()
			// Mutate everything the runtime holds of the defaults.
			for _, n := range r.Nouns {
				for _, rows := range n.Bindings {
					for j := range rows {
						rows[j].Hint += "x"
						rows[j].Keys += "x"
						for k := range rows[j].Request.Args {
							rows[j].Request.Args[k] = "mutated"
						}
						for k := range rows[j].From {
							rows[j].From[k] = "mutated"
						}
						for k := range rows[j].Aliases {
							rows[j].Aliases[k] = "mutated"
						}
						if rows[j].Present != nil {
							*rows[j].Present = !*rows[j].Present
						}
					}
				}
			}
			r.rebindSettingRows(TerminalLeaderSetting)
			_ = r.DefaultBindings()
		}()
	}
	wg.Wait()
	for name, doc := range frameworkBindingDocs() {
		if !sameDocs(map[string]BindingsDoc{name: doc}, map[string]BindingsDoc{name: snapshot[name]}) {
			x, _ := json.Marshal(doc)
			y, _ := json.Marshal(snapshot[name])
			t.Errorf("%s changed:\n now  %.300s\n was  %.300s", name, x, y)
		}
	}
	if !sameDocs(frameworkBindingDocs(), snapshot) {
		t.Fatal("a runtime changed the shared parse of the embedded binding files")
	}
	if again := NewRuntime(); len(again.DefaultBindings()) == 0 {
		t.Fatal("no default rows")
	}
}

// sameDocs compares parsed files by their JSON form, so a nil and an empty
// map or slice are alike.
func sameDocs(a, b map[string]BindingsDoc) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
