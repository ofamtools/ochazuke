package inspect

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Annotations are notes about the catalogs that the catalogs cannot carry:
// per noun, the verbs someone suggests adding, from one dated source. The
// Gaps view draws them beside the live catalogs, marking each suggestion
// present, present without some of its parameters, or missing. A page
// without annotations hides the view.
//
// The JSON a file passes to catalog inspect --annotations:
//
//	{
//	  "source": "notes/verbs.md",          // where the notes came from (a path or URL)
//	  "title": "verb review",              // optional; the source's base name otherwise
//	  "date": "2026-10-27",                // when they were written
//	  "tools": [{"tool": "mtm", "version": "v0.9.0", "commit": "abc1234"}],  // optional: what was read
//	  "nouns": [{
//	    "tool": "mtm", "noun": "pane",
//	    "existing": [{"verb": "attach", "keys": "enter"}],                  // optional: the verbs then
//	    "suggestions": [
//	      {"verb": "show", "note": "one pane as JSON"},
//	      {"noun": "project", "verb": "associate", "params": ["pane"], "maybe": true, "group": "relations"}
//	    ],
//	    "notes": ["..."],
//	    "noneToAdd": true                                                     // optional: reviewed, nothing to add
//	  }]
//	}
//
// A suggestion's noun defaults to the annotated noun. A parameter written
// "a|b" is satisfied by either name. AuditAnnotations reads the family's
// verb-audit.md format into the same shape.
type Annotations struct {
	Source   string              `json:"source"`
	Title    string              `json:"title,omitempty"`
	Date     string              `json:"date,omitempty"`
	Tools    []AnnotatedTool     `json:"tools,omitempty"`
	Nouns    []*AnnotatedNoun    `json:"nouns"`
	Sections []AnnotationSection `json:"sections,omitempty"` // a parsed file's sections (AuditAnnotations)
	Skipped  []SkippedLine       `json:"skipped,omitempty"`  // lines a parser could not read
	Stats    map[string]int      `json:"stats,omitempty"`    // Normalize: nouns, suggestions, duplicates, skippedLines
	Live     map[string]int      `json:"live,omitempty"`     // Match: present, partial, missing
	byKey    map[string]*AnnotatedNoun
}

// AnnotatedTool is a tool the annotations were written against.
type AnnotatedTool struct {
	Tool    string `json:"tool"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// AnnotationSection is one section of a parsed source, and whether the
// parser read it (the page links the rest by line).
type AnnotationSection struct {
	Title  string `json:"title"`
	Line   int    `json:"line"`
	Parsed bool   `json:"parsed"`
}

// SkippedLine is a line of the source a parser did not read.
type SkippedLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// AnnotatedNoun is one tool's noun and the notes on it.
type AnnotatedNoun struct {
	Tool        string       `json:"tool"`
	Noun        string       `json:"noun"`
	Line        int          `json:"line,omitempty"`
	Existing    []Existing   `json:"existing,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
	Notes       []string     `json:"notes,omitempty"`
	NoneToAdd   bool         `json:"noneToAdd,omitempty"`
}

// Existing is a verb the noun had when the notes were written, and its keys.
type Existing struct {
	Verb string `json:"verb"`
	Keys string `json:"keys,omitempty"`
	TUI  bool   `json:"tui,omitempty"` // drawn by the TUI only, never in a catalog
}

// Suggestion is a verb to add: Noun.Verb with Params.
type Suggestion struct {
	Noun   string   `json:"noun,omitempty"`
	Verb   string   `json:"verb"`
	Params []string `json:"params,omitempty"`
	Maybe  bool     `json:"maybe,omitempty"`
	Note   string   `json:"note,omitempty"`
	Group  string   `json:"group,omitempty"`
	Line   int      `json:"line,omitempty"`
	// Filled by Match against the live catalogs.
	Present       bool     `json:"present"`
	MissingParams []string `json:"missingParams,omitempty"` // the verb exists without these
	LiveID        string   `json:"liveId,omitempty"`
}

// ReadAnnotations decodes an annotations file and normalizes it.
func ReadAnnotations(b []byte) (*Annotations, error) {
	var a Annotations
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("annotations: %w", err)
	}
	a.Normalize()
	return &a, nil
}

func (a *Annotations) noun(tool, noun string, line int) *AnnotatedNoun {
	if a.byKey == nil {
		a.byKey = map[string]*AnnotatedNoun{}
	}
	k := tool + "\x00" + noun
	if n, ok := a.byKey[k]; ok {
		return n
	}
	n := &AnnotatedNoun{Tool: tool, Noun: noun, Line: line}
	a.byKey[k] = n
	a.Nouns = append(a.Nouns, n)
	return n
}

// Normalize fills each suggestion's noun from its noun, drops repeated
// suggestions (the same noun, verb and parameters) and counts what is
// left. It is idempotent.
func (a *Annotations) Normalize() {
	stats := map[string]int{"duplicates": a.Stats["duplicates"]}
	for _, n := range a.Nouns {
		seen := map[string]bool{}
		kept := n.Suggestions[:0]
		for _, sg := range n.Suggestions {
			if sg.Noun == "" {
				sg.Noun = n.Noun
			}
			k := sg.Noun + "." + sg.Verb + " " + strings.Join(sg.Params, ",")
			if seen[k] {
				stats["duplicates"]++
				continue
			}
			seen[k] = true
			kept = append(kept, sg)
		}
		n.Suggestions = kept
		stats["suggestions"] += len(n.Suggestions)
	}
	stats["nouns"] = len(a.Nouns)
	stats["skippedLines"] = len(a.Skipped)
	a.Stats = stats
}

// Match marks each suggestion present when the live catalog of the
// annotated tool has the verb on that noun with every parameter, partial
// when the verb lacks some (MissingParams), and counts them in Live.
func (a *Annotations) Match(d *Data) {
	if a == nil {
		return
	}
	have := map[string]*Verb{} // tool:noun#verb
	for _, v := range d.Verbs {
		n := strings.SplitN(v.Noun, ":", 2)
		for _, t := range v.Tools {
			have[t+":"+n[1]+"#"+v.Name] = v
			have[t+":"+strings.TrimPrefix(n[1], fwPrefix)+"#"+v.Name] = v
		}
	}
	a.Live = map[string]int{}
	for _, n := range a.Nouns {
		for i := range n.Suggestions {
			s := &n.Suggestions[i]
			s.Present, s.MissingParams, s.LiveID = false, nil, ""
			v, ok := have[n.Tool+":"+s.Noun+"#"+s.Verb]
			if !ok {
				a.Live["missing"]++
				continue
			}
			s.LiveID = v.ID
			for _, want := range s.Params {
				found := false // "a|b" is satisfied by either
				for _, alt := range strings.Split(want, "|") {
					for _, p := range v.Params {
						if p.Name == strings.TrimSpace(alt) {
							found = true
						}
					}
				}
				if !found {
					s.MissingParams = append(s.MissingParams, want)
				}
			}
			if len(s.MissingParams) == 0 {
				s.Present = true
				a.Live["present"]++
			} else {
				a.Live["partial"]++
			}
		}
	}
}
