package refdoc

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Diff is one way the committed pages differ from what Render draws now:
// a page that is missing, one that should not be there, or one whose body
// or header fingerprint changed. Detail shows the first lines that differ.
type Diff struct {
	File    string `json:"file"`
	Problem string `json:"problem"`
	Detail  string `json:"detail,omitempty"`
}

// The problems a Diff names.
const (
	Missing = "missing"
	Extra   = "extra"
	Changed = "changed"
)

func (d Diff) String() string {
	text := d.File + ": " + d.Problem
	if d.Detail != "" {
		text += "\n" + d.Detail
	}
	return text
}

// Check renders the model in memory and compares it with the pages in
// dir: each page below its header, and the header only on its catalog
// fingerprint, so a page generated on an older commit of the same catalog
// still matches. It reports missing, extra and changed pages, nothing
// when they match. An error is a directory that cannot be read.
func Check(m Model, opts Options, dir string) ([]Diff, error) {
	want := Render(m, opts)
	var out []Diff
	for _, name := range slices.Sorted(maps.Keys(want)) {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			out = append(out, Diff{File: name, Problem: Missing})
			continue
		}
		if err != nil {
			return nil, err
		}
		got := string(data)
		if Body(got) != Body(want[name]) {
			out = append(out, Diff{File: name, Problem: Changed, Detail: lineDiff(Body(got), Body(want[name]))})
		} else if gf, wf := HeaderFingerprint(got), m.Fingerprint(); gf != wf {
			out = append(out, Diff{File: name, Problem: Changed, Detail: fmt.Sprintf("header fingerprint %q, the catalog's is %q", gf, wf)})
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if _, ok := want[e.Name()]; !ok {
			out = append(out, Diff{File: e.Name(), Problem: Extra})
		}
	}
	return out, nil
}

// diffContext and diffShown bound lineDiff: lines of context before the
// first difference, and lines shown from each side after it.
const (
	diffContext = 3
	diffShown   = 8
)

// lineDiff shows where got and want first differ, in unified-diff
// markings: a few lines of context, then got's lines (-) and want's (+)
// up to where they agree again, at most diffShown of each.
func lineDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	first := 0
	for first < len(g) && first < len(w) && g[first] == w[first] {
		first++
	}
	// The common tail, so the shown hunk ends where they agree again.
	gEnd, wEnd := len(g), len(w)
	for gEnd > first && wEnd > first && g[gEnd-1] == w[wEnd-1] {
		gEnd, wEnd = gEnd-1, wEnd-1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "@@ committed line %d, generated line %d @@\n", first+1, first+1)
	for i := max(0, first-diffContext); i < first; i++ {
		b.WriteString("  " + g[i] + "\n")
	}
	side := func(lines []string, mark string) {
		for i, l := range lines {
			if i == diffShown {
				fmt.Fprintf(&b, "%s … %d more\n", mark, len(lines)-diffShown)
				break
			}
			b.WriteString(mark + " " + l + "\n")
		}
	}
	side(g[first:gEnd], "-")
	side(w[first:wEnd], "+")
	return strings.TrimRight(b.String(), "\n")
}
