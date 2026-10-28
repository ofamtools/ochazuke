package inspect

import (
	"bufio"
	"regexp"
	"strings"
)

var (
	reAuditDate  = regexp.MustCompile(`Audited (\d{4}-\d{2}-\d{2})`)
	reToolHead   = regexp.MustCompile("^### (\\S+)\\s*$")
	reNounHead   = regexp.MustCompile("^#### (\\S+) `([^`]+)`")
	reTick       = regexp.MustCompile("`([^`]+)`")
	reExisting   = regexp.MustCompile("`([^`]+)`(?: \\[([^\\]]+)\\])?")
	reNoneEntry  = regexp.MustCompile("`([^`]+)` \\(")
	reCategory   = regexp.MustCompile(`^\*([^*]+)\*\s*$`)
	reVerbToken  = regexp.MustCompile(`^([a-z][\w-]*)\.([A-Za-z][\w-]*)(?:\s+(\{[^}]*\}))?`)
	reSectionTop = regexp.MustCompile(`^## (\d+)\. (.+)$`)
)

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func untick(s string) string {
	if m := reTick.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

// AuditAnnotations reads the o-family's verb audit (~/hub/o-plans/lists/
// verb-audit.md) into Annotations: the "Audited YYYY-MM-DD" date, the
// "Tools read" table, section 2 ("Per tool, per noun": "#### tool `noun`"
// headings, each with an "Existing" list, "*group*" lines, suggestion
// bullets "- `noun.verb {params}` / `noun.other` — note" (maybe when the
// note starts with or says "(maybe"), "Note:" lines and "No suggestions"
// lines), and section 3's one-way relation tables (the last column, the
// reverse verb to add; the "Maybe" tier marks maybe). It never fails: what
// it cannot read is listed in Skipped and Sections (parsed false). The
// caller sets Source (the file's path).
func AuditAnnotations(text string) *Annotations {
	a := &Annotations{Title: "verb-audit.md", Stats: map[string]int{}}
	if m := reAuditDate.FindStringSubmatch(text); m != nil {
		a.Date = m[1]
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var (
		lineNo                       int
		section                      string // "tools", "pernoun", "oneway", "other"
		tool, category, oneWayTier   string
		cur                          *AnnotatedNoun
		toolsHeaderSeen, inOneWayTab bool
	)
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		t := strings.TrimSpace(line)

		if m := reSectionTop.FindStringSubmatch(t); m != nil {
			title := m[1] + ". " + m[2]
			parsed := false
			switch {
			case strings.HasPrefix(m[2], "Per tool"):
				section, parsed = "pernoun", true
			case strings.HasPrefix(m[2], "One-way relations"):
				section, parsed = "oneway", true
			default:
				section = "other"
			}
			a.Sections = append(a.Sections, AnnotationSection{Title: title, Line: lineNo, Parsed: parsed})
			cur, category = nil, ""
			continue
		}
		if section == "" && strings.HasPrefix(t, "Tools read") {
			section = "tools"
			continue
		}

		switch section {
		case "tools":
			if !strings.HasPrefix(t, "|") {
				continue
			}
			cells := splitRow(t)
			if len(cells) < 3 || strings.HasPrefix(cells[0], "---") {
				continue
			}
			if !toolsHeaderSeen {
				toolsHeaderSeen = true
				continue
			}
			name := strings.Fields(cells[0])
			if len(name) == 0 {
				continue
			}
			a.Tools = append(a.Tools, AnnotatedTool{Tool: name[0], Version: untick(cells[1]), Commit: cells[2]})

		case "pernoun":
			if m := reToolHead.FindStringSubmatch(t); m != nil {
				tool, cur, category = m[1], nil, ""
				continue
			}
			if m := reNounHead.FindStringSubmatch(t); m != nil {
				tool = m[1]
				cur, category = a.noun(m[1], m[2], lineNo), ""
				continue
			}
			if strings.HasPrefix(t, "No suggestions") && tool != "" {
				body := t
				if i := strings.Index(t, "):"); i >= 0 {
					body = t[i+2:]
				}
				// "`cell` (`clear` [TUI], ...); `field` (...)"
				for _, part := range strings.Split(body, "); ") {
					m := reNoneEntry.FindStringSubmatch(strings.TrimSpace(part))
					if m == nil {
						continue
					}
					n := a.noun(tool, m[1], lineNo)
					n.NoneToAdd = true
					if i := strings.Index(part, "("); i >= 0 {
						n.Existing = parseExisting(part[i:])
					}
				}
				continue
			}
			if cur == nil {
				if t != "" && !strings.HasPrefix(t, "#") {
					a.Skipped = append(a.Skipped, SkippedLine{lineNo, clip(t)})
				}
				continue
			}
			if strings.HasPrefix(t, "Existing") {
				if i := strings.Index(t, ":"); i >= 0 {
					cur.Existing = append(cur.Existing, parseExisting(t[i+1:])...)
				}
				continue
			}
			if m := reCategory.FindStringSubmatch(t); m != nil {
				category = strings.TrimSpace(m[1])
				continue
			}
			if strings.HasPrefix(t, "Note:") {
				cur.Notes = append(cur.Notes, strings.TrimSpace(strings.TrimPrefix(t, "Note:")))
				continue
			}
			if strings.HasPrefix(t, "- ") {
				sugs := parseBullet(t[2:], category, lineNo)
				if len(sugs) == 0 {
					a.Skipped = append(a.Skipped, SkippedLine{lineNo, clip(t)})
				}
				cur.Suggestions = append(cur.Suggestions, sugs...)
				continue
			}
			if t != "" {
				a.Skipped = append(a.Skipped, SkippedLine{lineNo, clip(t)})
			}

		case "oneway":
			if strings.HasPrefix(t, "### ") {
				oneWayTier = strings.TrimPrefix(t, "### ")
				inOneWayTab = false
				continue
			}
			if !strings.HasPrefix(t, "|") {
				inOneWayTab = false
				continue
			}
			cells := splitRow(t)
			if len(cells) >= 5 && strings.Contains(strings.ToLower(cells[len(cells)-1]), "reverse") {
				inOneWayTab = true
				continue
			}
			if !inOneWayTab || strings.HasPrefix(cells[0], "---") || len(cells) < 5 {
				continue
			}
			rev := cells[len(cells)-1]
			sugs := parseTokens(rev, "One-way relation (section 3, "+strings.ToLower(oneWayTier)+")", lineNo)
			if len(sugs) == 0 {
				a.Skipped = append(a.Skipped, SkippedLine{lineNo, clip(t)})
				continue
			}
			for i := range sugs {
				sugs[i].Maybe = sugs[i].Maybe || strings.EqualFold(oneWayTier, "maybe")
				sugs[i].Note = "reverse of " + untick(cells[1]) + " (" + cells[3] + ")"
				n := a.noun(cells[0], sugs[i].Noun, lineNo)
				n.Suggestions = append(n.Suggestions, sugs[i])
			}
		}
	}
	// The relation tables repeat section 2's suggestions: Normalize folds them.
	a.Normalize()
	return a
}

func clip(s string) string {
	if len(s) > 160 {
		return s[:157] + "..."
	}
	return s
}

func parseExisting(s string) []Existing {
	var out []Existing
	for _, m := range reExisting.FindAllStringSubmatch(s, -1) {
		e := Existing{Verb: m[1], Keys: m[2]}
		if strings.HasPrefix(e.Keys, "TUI") {
			e.TUI = true
			e.Keys = strings.TrimPrefix(strings.TrimPrefix(e.Keys, "TUI"), ", ")
		}
		out = append(out, e)
	}
	return out
}

// parseBullet reads "`noun.verb {params}` / `noun.other` — text".
func parseBullet(s, category string, line int) []Suggestion {
	head, text := s, ""
	if i := strings.Index(s, " — "); i >= 0 {
		head, text = s[:i], strings.TrimSpace(s[i+len(" — "):])
	}
	sugs := parseTokens(head, category, line)
	maybe := strings.HasPrefix(strings.ToLower(text), "maybe") || strings.Contains(strings.ToLower(text), "(maybe")
	for i := range sugs {
		sugs[i].Note = text
		sugs[i].Maybe = sugs[i].Maybe || maybe
	}
	return sugs
}

func parseTokens(s, category string, line int) []Suggestion {
	var out []Suggestion
	for _, m := range reTick.FindAllStringSubmatch(s, -1) {
		v := reVerbToken.FindStringSubmatch(m[1])
		if v == nil {
			continue
		}
		out = append(out, Suggestion{Noun: v[1], Verb: v[2], Params: paramNames(v[3]), Group: category, Line: line,
			Maybe: strings.Contains(strings.ToLower(s), "maybe")})
	}
	return out
}

// paramNames reads "{from, to, story}" or "{action: type}" as parameter names.
func paramNames(braced string) []string {
	braced = strings.Trim(strings.TrimSpace(braced), "{}")
	var out []string
	for _, part := range strings.Split(braced, ",") {
		if i := strings.Index(part, ":"); i >= 0 {
			part = part[:i]
		}
		if part = strings.TrimSpace(part); part != "" && !strings.ContainsAny(part, " .") {
			out = append(out, part)
		}
	}
	return out
}
