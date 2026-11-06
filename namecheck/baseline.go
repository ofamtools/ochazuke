package namecheck

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Row is one baseline row: a key, how many times the code does it (for a
// cyclo row, the function's complexity) and, for a legitimate use, why.
type Row struct {
	Key
	Count  int
	Reason string
}

// Baseline is the accepted uses.
type Baseline map[Key]Row

const baselineHeader = `# namecheck baseline: file, function, kind, literal, count, reason (tab separated).
# A row with a reason is a legitimate use that stays. A row without one is debt that may only shrink:
# NAMECHECK_UPDATE=1 removes rows and lowers counts, never adds them. Adding a row is a reviewed edit.
`

// ParseBaseline reads a baseline file's text.
func ParseBaseline(text string) (Baseline, error) {
	b := Baseline{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 || len(f) > 6 {
			return nil, fmt.Errorf("baseline line %d: want file, function, kind, literal, count[, reason]", n)
		}
		lit, err := strconv.Unquote(f[3])
		if err != nil {
			return nil, fmt.Errorf("baseline line %d: literal %s: %v", n, f[3], err)
		}
		count, err := strconv.Atoi(f[4])
		if err != nil || count < 1 {
			return nil, fmt.Errorf("baseline line %d: count %q", n, f[4])
		}
		r := Row{Key: Key{f[0], f[1], f[2], lit}, Count: count}
		if len(f) == 6 {
			r.Reason = f[5]
		}
		if _, dup := b[r.Key]; dup {
			return nil, fmt.Errorf("baseline line %d: duplicate row for %s", n, r.Key)
		}
		b[r.Key] = r
	}
	return b, sc.Err()
}

// Format writes the baseline sorted by file, function, kind and literal.
func (b Baseline) Format() string {
	rows := make([]Row, 0, len(b))
	for _, r := range b {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, c := rows[i].Key, rows[j].Key
		if a.File != c.File {
			return a.File < c.File
		}
		if a.Func != c.Func {
			return a.Func < c.Func
		}
		if a.Kind != c.Kind {
			return a.Kind < c.Kind
		}
		return a.Literal < c.Literal
	})
	var sb strings.Builder
	sb.WriteString(baselineHeader)
	for _, r := range rows {
		fmt.Fprintf(&sb, "%s\t%d", r.Key, r.Count)
		if r.Reason != "" {
			sb.WriteString("\t" + r.Reason)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// LoadBaseline reads the file; a missing file is an empty baseline.
func LoadBaseline(file string) (Baseline, error) {
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return Baseline{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseBaseline(string(data))
}

// Counts is what a scan found, by Key: hit counts, and the complexity of
// each function over the ceiling or named by the baseline.
func (r *Report) counts(ceiling int, base Baseline) map[Key]int {
	m := map[Key]int{}
	for _, h := range r.Hits {
		m[h.Key()]++
	}
	for k, c := range r.Cyclo {
		if _, listed := base[k]; listed || c > ceiling {
			m[k] = c
		}
	}
	return m
}

// Problem is one failure.
type Problem struct {
	Key
	Message string
}

// Result is a comparison of a scan with a baseline.
type Result struct {
	// New are uses the baseline does not cover; Stale are rows that no
	// longer match the code and must come out or go down.
	New, Stale []Problem
	// Shrunk is the baseline with the stale rows removed or lowered.
	Shrunk Baseline
	// Counts is the totals by kind and category, split into legitimate
	// (with a reason) and debt, of the uses the baseline covers.
	Summary Summary
}

// Summary counts the uses in the code now.
type Summary struct {
	ByKind     map[string]int
	ByCategory map[string]int
	Legit      int
	Debt       int
	OverCeil   int // functions over the ceiling
	MaxCyclo   int
}

func (s Summary) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "namecheck: %d legitimate, %d debt; ", s.Legit, s.Debt)
	sb.WriteString("by kind " + sortedCounts(s.ByKind) + "; by category " + sortedCounts(s.ByCategory))
	fmt.Fprintf(&sb, "; %d functions over the complexity ceiling (max %d)", s.OverCeil, s.MaxCyclo)
	return sb.String()
}

func sortedCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// Compare checks a scan against the baseline. The rule for every key is
// the same: the code may not do more than the baseline allows (New), and
// the baseline may not allow more than the code does (Stale). A cyclo row
// is the function's complexity; a function without one is held to ceiling.
func Compare(rep *Report, base Baseline, ceiling int) Result {
	if ceiling <= 0 {
		ceiling = DefaultCeiling
	}
	now := rep.counts(ceiling, base)
	res := Result{Shrunk: Baseline{}, Summary: Summary{ByKind: map[string]int{}, ByCategory: map[string]int{}}}
	lines := map[Key][]int{}
	cats := map[Key][]string{}
	for _, h := range rep.Hits {
		lines[h.Key()] = append(lines[h.Key()], h.Line)
		cats[h.Key()] = h.Categories
	}
	keys := make([]Key, 0, len(now)+len(base))
	for k := range now {
		keys = append(keys, k)
	}
	for k := range base {
		if _, ok := now[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, k := range keys {
		have, allowed := now[k], base[k].Count
		cyclo := k.Kind == KindCyclo
		switch {
		case cyclo && allowed > 0 && have <= ceiling:
			res.Stale = append(res.Stale, Problem{k, staleMessage(k, have, allowed, ceiling)})
		case have > allowed:
			res.New = append(res.New, Problem{k, newMessage(k, have, allowed, ceiling, lines[k], rep.Lines[k])})
		case have < allowed:
			res.Stale = append(res.Stale, Problem{k, staleMessage(k, have, allowed, ceiling)})
		}
		keep := min(have, allowed)
		if cyclo && keep <= ceiling {
			keep = 0 // under the ceiling a function needs no row
		}
		if keep > 0 {
			r := base[k]
			r.Count = keep
			res.Shrunk[k] = r
		}
		res.Summary.add(k, have, base[k].Reason != "", cats[k], ceiling)
	}
	return res
}

func (s *Summary) add(k Key, have int, legit bool, cats []string, ceiling int) {
	if k.Kind == KindCyclo {
		if have > ceiling {
			s.OverCeil++
		}
		s.MaxCyclo = max(s.MaxCyclo, have)
		return
	}
	s.ByKind[k.Kind] += have
	for _, c := range cats {
		s.ByCategory[c] += have
	}
	if legit {
		s.Legit += have
	} else {
		s.Debt += have
	}
}

func newMessage(k Key, have, allowed, ceiling int, lines []int, fnLine int) string {
	if k.Kind == KindCyclo {
		if allowed == 0 {
			return fmt.Sprintf("%s:%d %s has complexity %d; a function may not exceed %d. Split it, drive it from a table, or (reviewed, with a reason) add a baseline row", k.File, fnLine, k.Func, have, ceiling)
		}
		return fmt.Sprintf("%s:%d %s grew to complexity %d (baseline %d); a function may not grow", k.File, fnLine, k.Func, have, allowed)
	}
	return fmt.Sprintf("%s:%v %s: %s on %q (%d, baseline %d): declare it as data (a binding row, a verb, a setting, a table the runtime reads) instead of branching on a name; a legitimate use is a reviewed baseline row with a reason",
		k.File, lines, k.Func, k.Kind, k.Literal, have, allowed)
}

func staleMessage(k Key, have, allowed, ceiling int) string {
	if k.Kind == KindCyclo {
		return fmt.Sprintf("%s %s: complexity is now %d, baseline row says %d: lower it (NAMECHECK_UPDATE=1), or drop the row at %d or below", k.File, k.Func, have, allowed, ceiling)
	}
	return fmt.Sprintf("%s %s: %s on %q is %d now, baseline row says %d: lower or remove the row (NAMECHECK_UPDATE=1)", k.File, k.Func, k.Kind, k.Literal, have, allowed)
}
