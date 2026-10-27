package storymatrix

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke/story"
)

// Coverage is one component's cells against the stories' covers.
type Coverage struct {
	Component string   `json:"component"`
	Cells     int      `json:"cells"`
	Covered   int      `json:"covered"`
	Missing   int      `json:"missing"`
	Stories   int      `json:"stories"`  // stories in the component's areas
	Untagged  []string `json:"untagged"` // of those, the ones with no covers (AREA/SLUG)
	// Typos are covers patterns (of the component's stories, or naming
	// the component first) that match no cell: "AREA/SLUG: PATTERN".
	Typos        []string `json:"typos"`
	MissingCells []string `json:"missingCells,omitempty"`
}

// Matches reports whether a covers pattern covers the cell id: the
// pattern matches the id, or matches its leading segments
// ("tables/list.move" or "tables/*" covers every cell under them).
func Matches(pattern, id string) bool {
	if ok, _ := path.Match(pattern, id); ok {
		return true
	}
	for i := len(id) - 1; i > 0; i-- {
		if id[i] == '/' {
			if ok, _ := path.Match(pattern, id[:i]); ok {
				return true
			}
		}
	}
	return false
}

// Coverage counts every component's cells covered by the stories (any
// story may cover any component's cells), its stories without covers,
// and the patterns that match no cell. withMissing lists the missing
// cell ids too.
func (mx *Matrix) Coverage(stories []*story.Story, withMissing bool) []Coverage {
	type tagged struct {
		where, pattern string
		area           string
		hit            bool
	}
	var patterns []*tagged
	for _, s := range stories {
		for _, p := range s.Covers {
			patterns = append(patterns, &tagged{where: s.Area + "/" + story.Slug(s.Name), pattern: p, area: s.Area})
		}
	}
	var out []Coverage
	for _, c := range mx.Components {
		cov := Coverage{Component: c.ID, Cells: len(c.Cells), Untagged: []string{}, Typos: []string{}}
		for _, cell := range c.Cells {
			covered := false
			for _, p := range patterns {
				if Matches(p.pattern, cell.ID) {
					p.hit, covered = true, true
				}
			}
			if covered {
				cov.Covered++
			} else if withMissing {
				cov.MissingCells = append(cov.MissingCells, cell.ID)
			}
		}
		cov.Missing = cov.Cells - cov.Covered
		for _, s := range stories {
			if slices.Contains(c.Areas, s.Area) {
				cov.Stories++
				if len(s.Covers) == 0 {
					cov.Untagged = append(cov.Untagged, s.Area+"/"+story.Slug(s.Name))
				}
			}
		}
		slices.Sort(cov.Untagged)
		out = append(out, cov)
	}
	// Typos are known only once every component has been matched.
	for i, c := range mx.Components {
		for _, p := range patterns {
			first, _, _ := strings.Cut(p.pattern, "/")
			if !p.hit && (slices.Contains(c.Areas, p.area) || first == c.ID) {
				out[i].Typos = append(out[i].Typos, p.where+": "+p.pattern)
			}
		}
		slices.Sort(out[i].Typos)
		out[i].Typos = slices.Compact(out[i].Typos)
	}
	slices.SortFunc(out, func(a, b Coverage) int { return strings.Compare(a.Component, b.Component) })
	return out
}

// Typos are every pattern matching no cell, whichever component it
// belongs to ("AREA/SLUG: PATTERN"), so a pattern of a story in no
// component's area is still reported.
func (mx *Matrix) Typos(stories []*story.Story) []string {
	var out []string
	for _, s := range stories {
		for _, p := range s.Covers {
			hit := false
			for _, c := range mx.Components {
				for _, cell := range c.Cells {
					if Matches(p, cell.ID) {
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
			if !hit {
				out = append(out, s.Area+"/"+story.Slug(s.Name)+": "+p)
			}
		}
	}
	slices.Sort(out)
	return out
}

// WriteCoverageText prints the summary table (sorted by component), the
// total, then each component's untagged stories and typos when there are
// few components, and the missing cells when they were asked for.
// The total row counts each story once (areas shared by components
// count a story in each of their rows): stories and untagged are the
// whole set's.
func WriteCoverageText(w io.Writer, rows []Coverage, typos []string, stories, untagged int) {
	fmt.Fprintf(w, "%-12s %7s %7s %7s %7s %8s %5s\n", "component", "cells", "covered", "missing", "stories", "untagged", "typos")
	var t Coverage
	for _, r := range rows {
		fmt.Fprintf(w, "%-12s %7d %7d %7d %7d %8d %5d\n", r.Component, r.Cells, r.Covered, r.Missing, r.Stories, len(r.Untagged), len(r.Typos))
		t.Cells += r.Cells
		t.Covered += r.Covered
		t.Missing += r.Missing
	}
	fmt.Fprintf(w, "%-12s %7d %7d %7d %7d %8d %5d\n", "total", t.Cells, t.Covered, t.Missing, stories, untagged, len(typos))
	if len(typos) > 0 {
		fmt.Fprintln(w, "\npatterns matching no cell:")
		for _, p := range typos {
			fmt.Fprintln(w, "  "+p)
		}
	}
	for _, r := range rows {
		if r.MissingCells == nil {
			continue
		}
		fmt.Fprintf(w, "\n%s: untagged stories:\n", r.Component)
		for _, s := range r.Untagged {
			fmt.Fprintln(w, "  "+s)
		}
		fmt.Fprintf(w, "\n%s: missing cells:\n", r.Component)
		for _, id := range r.MissingCells {
			fmt.Fprintln(w, "  "+id)
		}
	}
}

// WriteCoverageJSON prints the rows and the typos as indented JSON.
func WriteCoverageJSON(w io.Writer, rows []Coverage, typos []string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if typos == nil {
		typos = []string{}
	}
	return enc.Encode(struct {
		Components []Coverage `json:"components"`
		Typos      []string   `json:"typos"`
	}{rows, typos})
}
