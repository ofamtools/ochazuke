package refusecheck

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Row is one baseline row: a key, how many sites the code has and, for a
// legitimate refusal, the exception reason.
type Row struct {
	Key
	Count  int
	Reason string
}

// Baseline is the accepted sites.
type Baseline map[Key]Row

// Reasons are the exception reasons a row may carry: E1 to E5 of the plan
// and their names.
var Reasons = map[string]bool{
	"E1": true, "E2": true, "E3": true, "E4": true, "E5": true,
	"rights": true, "loses": true, "failure": true, "declaration": true, "cancel": true,
}

const baselineHeader = `# refusecheck baseline: file, function, class, template, count, reason (tab separated).
# A row with a reason (E1..E5: rights, loses, failure, declaration, cancel) is a legitimate refusal that stays.
# A row without one is debt that may only shrink: REFUSECHECK_UPDATE=1 removes rows and lowers counts, never adds.
# Adding a row is a reviewed edit.
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
			return nil, fmt.Errorf("baseline line %d: want file, function, class, template, count[, reason]", n)
		}
		tmpl, err := strconv.Unquote(f[3])
		if err != nil {
			return nil, fmt.Errorf("baseline line %d: template %s: %v", n, f[3], err)
		}
		count, err := strconv.Atoi(f[4])
		if err != nil || count < 1 {
			return nil, fmt.Errorf("baseline line %d: count %q", n, f[4])
		}
		r := Row{Key: Key{f[0], f[1], f[2], tmpl}, Count: count}
		if len(f) == 6 {
			r.Reason = f[5]
			if !Reasons[r.Reason] {
				return nil, fmt.Errorf("baseline line %d: unknown exception reason %q (E1..E5, rights, loses, failure, declaration, cancel)", n, r.Reason)
			}
		}
		if _, dup := b[r.Key]; dup {
			return nil, fmt.Errorf("baseline line %d: duplicate row for %s", n, r.Key)
		}
		b[r.Key] = r
	}
	return b, sc.Err()
}

// Format writes the baseline sorted by file, function, class and template.
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
		if a.Class != c.Class {
			return a.Class < c.Class
		}
		return a.Template < c.Template
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

// Problem is one failure.
type Problem struct {
	Key
	Message string
}

// Summary counts the sites in the code now, by class.
type Summary struct {
	ByClass     map[string]int
	Legit, Debt int
}

func (s Summary) String() string {
	keys := make([]string, 0, len(s.ByClass))
	for k := range s.ByClass {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return classNum(keys[i]) < classNum(keys[j]) })
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, s.ByClass[k])
	}
	return fmt.Sprintf("refusecheck: %d exceptions, %d debt; by class {%s}", s.Legit, s.Debt, strings.Join(parts, " "))
}

func classNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "K"))
	return n
}

// Result is a comparison of a scan with a baseline.
type Result struct {
	// New are sites the baseline does not cover; Stale are rows that no
	// longer match the code and must come out or go down.
	New, Stale []Problem
	// Shrunk is the baseline with the stale rows removed or lowered.
	Shrunk  Baseline
	Summary Summary
}

// Compare checks a scan against the baseline: the code may not do more
// than the baseline allows (New), and the baseline may not allow more than
// the code does (Stale).
func Compare(sites []Site, base Baseline) Result {
	uses := classUse()
	now := map[Key]int{}
	lines := map[Key][]int{}
	for _, s := range sites {
		now[s.Key()]++
		lines[s.Key()] = append(lines[s.Key()], s.Line)
	}
	res := Result{Shrunk: Baseline{}, Summary: Summary{ByClass: map[string]int{}}}
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
		switch {
		case have > allowed:
			res.New = append(res.New, Problem{k, fmt.Sprintf("%s:%v %s: %s refusal %q (%d, baseline %d): %s; an exception is a reviewed baseline row with a reason",
				k.File, lines[k], k.Func, k.Class, k.Template, have, allowed, uses[k.Class])})
		case have < allowed:
			res.Stale = append(res.Stale, Problem{k, fmt.Sprintf("%s %s: %s refusal %q is %d now, baseline row says %d: lower or remove the row (REFUSECHECK_UPDATE=1)",
				k.File, k.Func, k.Class, k.Template, have, allowed)})
		}
		if keep := min(have, allowed); keep > 0 {
			r := base[k]
			r.Count = keep
			res.Shrunk[k] = r
		}
		res.Summary.ByClass[k.Class] += have
		if base[k].Reason != "" {
			res.Summary.Legit += have
		} else {
			res.Summary.Debt += have
		}
	}
	return res
}

func classUse() map[string]string {
	m := map[string]string{}
	for _, c := range Classes() {
		m[c.ID] = c.Title + ": " + c.Use
	}
	return m
}
