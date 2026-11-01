package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/glamour/v2"
)

// trickyDoc mixes the blocks whose boundaries splitRuns must respect:
// loose and ordered lists across blank lines, nested lists, fences
// holding blank lines and column-0 lines, definition lists, quotes,
// setext headings, indented code, rules and notes.
const trickyDoc = "Title\n=====\n\nIntro *para*.\n\n- a\n\n- b\n\n  more of b\n\n  ```\ncol0 in a fence\n\nstill\n  ```\n\n1. one\n\n2. two\n\n3) three\n\nTerm\n\n: definition\n\n> quote\n\n> another\n\n    indented code\n\n    more\n\n---\n\n```go\nfunc f() {\n\n}\n```\n\n~~~~\n```\n\nx\n~~~~\n\n- [ ] task\n  > **owner** 2026-10-27: a note\n\n  > more\n\nend\n"

// TestRenderRunsMatchWhole draws documents run by run and whole and
// wants the same bytes, at several widths, styles and options.
func TestRenderRunsMatchWhole(t *testing.T) {
	docs := map[string]string{"tricky": trickyDoc, "bench": benchDocText()}
	marked := benchDocText()
	docs["bench-marked"] = markedSource(marked, Elements(marked), nil)
	docs["tricky-marked"] = markedSource(trickyDoc, Elements(trickyDoc), nil)
	cut, _ := cutTables(docs["bench-marked"])
	docs["bench-cut"] = cut
	// MARKDOWN_RUNS_CORPUS names a folder of .md files to check as well.
	corpus := map[string]bool{}
	if dir := os.Getenv("MARKDOWN_RUNS_CORPUS"); dir != "" {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err == nil {
				text := string(b)
				c, _ := cutTables(markedSource(text, Elements(text), nil))
				docs[p] = c
				corpus[p] = true
			}
		}
	}
	for name, doc := range docs {
		for _, style := range []string{StyleDark, StyleLight, StylePlain} {
			for _, width := range []int{12, 40, 100} {
				for _, nl := range []bool{false, true} {
					ropts := []glamour.TermRendererOption{glamour.WithStyles(styleConfig(style)), glamour.WithWordWrap(width)}
					if nl {
						ropts = append(ropts, glamour.WithPreservedNewLines())
					}
					tr, err := glamour.NewTermRenderer(ropts...)
					if err != nil {
						t.Fatal(err)
					}
					want, err := tr.Render(doc)
					if err != nil {
						t.Fatal(err)
					}
					flaky := corpus[name]
					cache.Lock()
					got, err := renderRuns(tr, rendererKey{width, style, options{newlines: nl}}, doc)
					cache.Unlock()
					if err != nil {
						t.Fatal(err)
					}
					if flaky {
						// Chroma colours a comment in a code block of a
						// guessed language one of two greys, differently
						// from one drawing to the next (whole or not).
						got = strings.ReplaceAll(got, "38;5;242m", "38;5;241m")
						want = strings.ReplaceAll(want, "38;5;242m", "38;5;241m")
					}
					if got != want {
						gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
						for i := range min(len(gl), len(wl)) {
							if gl[i] != wl[i] {
								t.Fatalf("%s %s w%d nl=%v: line %d\n got %q\nwant %q", name, style, width, nl, i, gl[i], wl[i])
							}
						}
						t.Fatalf("%s %s w%d nl=%v: %d lines, want %d", name, style, width, nl, len(gl), len(wl))
					}
				}
			}
		}
	}
}

// TestSplitRunsKeepsBlocksWhole cuts only where a top-level block starts.
func TestSplitRunsKeepsBlocksWhole(t *testing.T) {
	runs := splitRuns(trickyDoc)
	if strings.Join(runs, "") != trickyDoc {
		t.Fatal("runs do not concatenate to the text")
	}
	for _, r := range runs {
		for _, bad := range []string{"col0 in a fence", "\nx\n", "2. two", ": definition", "\n- b"} {
			if strings.HasPrefix(r, strings.TrimPrefix(bad, "\n")) && bad != "col0 in a fence" {
				t.Fatalf("a run starts inside a block: %q", r)
			}
		}
		if strings.HasPrefix(r, "col0") || strings.HasPrefix(r, "x\n") {
			t.Fatalf("a run starts inside a fence: %q", r)
		}
	}
	if got := splitRuns("a\n\n[x]: https://example.com\n\nb\n"); len(got) != 1 {
		t.Fatalf("a reference definition splits the text: %q", got)
	}
	if got := splitRuns("a\n\n<!--\n\nb\n-->\n"); len(got) != 1 {
		t.Fatalf("an HTML block splits the text: %q", got)
	}
}

// TestRenderRunsAfterInsert reuses the drawn runs below an inserted
// block although their markers were renumbered.
func TestRenderRunsAfterInsert(t *testing.T) {
	text := benchDocText()
	before := markedSource(text, Elements(text), nil)
	edited := "# New\n\n" + text
	after := markedSource(edited, Elements(edited), nil)
	tr, err := glamour.NewTermRenderer(glamour.WithStyles(styleConfig(StylePlain)), glamour.WithWordWrap(60))
	if err != nil {
		t.Fatal(err)
	}
	rk := rendererKey{60, StylePlain, options{}}
	cache.Lock()
	defer cache.Unlock()
	clear(chunkCache.runs)
	chunkCache.bytes = 0
	if _, err := renderRuns(tr, rk, before); err != nil {
		t.Fatal(err)
	}
	drawn := func() int {
		n := 0
		for k := range chunkCache.runs {
			if !k.given {
				n++
			}
		}
		return n
	}
	n := drawn()
	got, err := renderRuns(tr, rk, after)
	if err != nil {
		t.Fatal(err)
	}
	if added := drawn() - n; added > 2 {
		t.Fatalf("an inserted heading drew %d runs again", added)
	}
	want, _ := tr.Render(after)
	if got != want {
		t.Fatal("the drawing after the insert differs from the whole drawing")
	}
}
