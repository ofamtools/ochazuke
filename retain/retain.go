// Package retain applies retention rules to files: a rule is data (globs,
// an optional group and unit pattern, keep, days and min_keep), Plan says
// what the rule removes now with the bytes and a reason for each copy, and
// Apply removes it through an injectable remover, one log entry per copy.
//
// It is the rule o-df's trim (o-df trim.go, PlanTrim/Trim: grouping by the
// group pattern's first capture, newest first by modification time with the
// later name newer on a tie, min_keep before keep before days, the reason
// wording) and o-lane's backup retention (o-lane backup.go retain: removal
// only through an injected function, a missing file is not a failure, one
// failure never stops the rest) had each built on their own; both can call
// this package instead. No code here knows a folder, a tool or a program:
// everything a rule does comes from its fields.
package retain

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Rule is one retention rule over files.
//
// A copy stays when it is among the newest Keep of its group and younger
// than Days, or among the newest MinKeep of its group, whatever the other
// limits say. Keep 0 sets no count limit and Days 0 no age limit; with
// both 0 the rule is off and removes nothing.
type Rule struct {
	// Name names the rule in its plan and log.
	Name string `json:"name"`
	// Paths are filepath.Glob patterns (absolute, already expanded).
	Paths []string `json:"paths"`
	// Group, when set, is a regular expression matched against a file's
	// base name: its first capture names the group the file is ruled in
	// (one group per program), and a file it does not match is left
	// alone. Empty: one group.
	Group string `json:"group,omitempty"`
	// Unit, when set, is a regular expression matched against a file's
	// base name whose first capture joins files into one copy (a database
	// file with its -wal and -shm): a copy is kept or removed whole, its
	// time is its newest file's and its bytes the sum. Empty: every file
	// is a copy of its own.
	Unit string `json:"unit,omitempty"`
	// Keep is the most copies kept per group (0: no count limit).
	Keep int `json:"keep"`
	// Days removes copies older than this many days (0: no age limit).
	Days int `json:"days"`
	// MinKeep copies per group are never removed, whatever Days says.
	MinKeep int `json:"min_keep,omitempty"`
}

// Off reports whether the rule sets no limit (Keep and Days both 0).
func (r Rule) Off() bool { return r.Keep <= 0 && r.Days <= 0 }

// Validate checks the rule's fields.
func (r Rule) Validate() error {
	if r.Keep < 0 || r.Days < 0 || r.MinKeep < 0 {
		return fmt.Errorf("rule %q: keep, days and min_keep are not negative", r.Name)
	}
	for _, p := range r.Paths {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("rule %q: path %q is not absolute", r.Name, p)
		}
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("rule %q: path %q: %w", r.Name, p, err)
		}
	}
	for field, text := range map[string]string{"group": r.Group, "unit": r.Unit} {
		if text == "" {
			continue
		}
		re, err := regexp.Compile(text)
		if err != nil {
			return fmt.Errorf("rule %q: %s: %w", r.Name, field, err)
		}
		if re.NumSubexp() < 1 {
			return fmt.Errorf("rule %q: %s %q needs a capture group", r.Name, field, text)
		}
	}
	return nil
}

// Removal is one copy a rule removes (or removed).
type Removal struct {
	Rule string `json:"rule"`
	// Path is the copy's first file (its name sorts first); Paths every
	// file of the copy, Path included.
	Path    string    `json:"path"`
	Paths   []string  `json:"paths,omitempty"`
	Group   string    `json:"group,omitempty"`
	Bytes   int64     `json:"bytes"`
	ModTime time.Time `json:"mtime"`
	Reason  string    `json:"reason"`
	// Error is set by Apply when a file of the copy could not be removed.
	Error string `json:"error,omitempty"`
}

// Result is a rule's plan, and after Apply what it did.
type Result struct {
	Rule string `json:"rule"`
	// Copies and Bytes are everything the rule matches now.
	Copies int   `json:"copies"`
	Bytes  int64 `json:"bytes"`
	// Remove is what the rule removes; RemoveBytes their bytes.
	Remove      []Removal `json:"remove"`
	RemoveBytes int64     `json:"remove_bytes"`
	// Applied is set by Apply; Freed counts the copies removed whole.
	Applied bool  `json:"applied,omitempty"`
	Freed   int64 `json:"freed,omitempty"`
	// Error is a rule that could not be planned (an invalid field).
	Error string `json:"error,omitempty"`
}

type copyOf struct {
	key   string
	group string
	paths []string
	bytes int64
	mtime time.Time
}

// Plan says what each rule removes at now. It reads names, modification
// times and sizes, never contents, and removes nothing. An invalid rule's
// Result carries its Error and removes nothing.
func Plan(rules []Rule, now time.Time) []Result {
	out := make([]Result, 0, len(rules))
	for _, rule := range rules {
		out = append(out, planRule(rule, now))
	}
	return out
}

func planRule(rule Rule, now time.Time) Result {
	res := Result{Rule: rule.Name, Remove: []Removal{}}
	if err := rule.Validate(); err != nil {
		res.Error = err.Error()
		return res
	}
	groups := collect(rule)
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	slices.Sort(names)
	for _, g := range names {
		copies := groups[g]
		// Newest first by modification time; equal times: the later name is newer.
		slices.SortStableFunc(copies, func(a, b copyOf) int {
			if c := b.mtime.Compare(a.mtime); c != 0 {
				return c
			}
			return strings.Compare(b.key, a.key)
		})
		times := make([]time.Time, len(copies))
		for i, c := range copies {
			times[i] = c.mtime
		}
		reasons := Reasons(times, rule.Keep, rule.Days, rule.MinKeep, now)
		for i, c := range copies {
			res.Copies++
			res.Bytes += c.bytes
			if reasons[i] == "" {
				continue
			}
			res.Remove = append(res.Remove, Removal{Rule: rule.Name, Path: c.paths[0], Paths: c.paths, Group: g, Bytes: c.bytes, ModTime: c.mtime, Reason: reasons[i]})
			res.RemoveBytes += c.bytes
		}
	}
	return res
}

// Reasons is the rule itself, over anything with a time: times are one
// group's copies newest first, and each answer is why that copy goes ("beyond
// the newest N", "older than N days") or "" when it stays. A copy stays
// when it is among the newest keep and younger than days, or among the
// newest minKeep; keep 0 sets no count limit, days 0 no age limit, both 0
// keep everything. Plan uses it for files; a caller ruling records (things
// with an updated time) uses the same function.
func Reasons(times []time.Time, keep, days, minKeep int, now time.Time) []string {
	out := make([]string, len(times))
	limit := time.Duration(days) * 24 * time.Hour
	for i, at := range times {
		switch {
		case keep <= 0 && days <= 0, i < minKeep:
		case keep > 0 && i >= keep:
			out[i] = fmt.Sprintf("beyond the newest %d", keep)
		case days > 0 && now.Sub(at) > limit:
			out[i] = fmt.Sprintf("older than %d days", days)
		}
	}
	return out
}

// collect matches the rule's globs and joins the files into copies by
// group.
func collect(rule Rule) map[string][]copyOf {
	var group, unit *regexp.Regexp
	if rule.Group != "" {
		group = regexp.MustCompile(rule.Group)
	}
	if rule.Unit != "" {
		unit = regexp.MustCompile(rule.Unit)
	}
	byKey := map[string]*copyOf{}
	order := []string{}
	seen := map[string]bool{}
	for _, pattern := range rule.Paths {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			m = filepath.Clean(m)
			if seen[m] {
				continue
			}
			seen[m] = true
			base := filepath.Base(m)
			g := ""
			if group != nil {
				sub := group.FindStringSubmatch(base)
				if sub == nil {
					continue
				}
				g = sub[1]
			}
			info, err := os.Lstat(m)
			if err != nil {
				continue
			}
			key := m
			if unit != nil {
				if sub := unit.FindStringSubmatch(base); sub != nil {
					key = filepath.Join(filepath.Dir(m), sub[1])
				}
			}
			key = g + "\x00" + key
			c := byKey[key]
			if c == nil {
				c = &copyOf{key: key, group: g}
				byKey[key] = c
				order = append(order, key)
			}
			c.paths = append(c.paths, m)
			c.bytes += sizeOf(m, info)
			if info.ModTime().After(c.mtime) {
				c.mtime = info.ModTime()
			}
		}
	}
	out := map[string][]copyOf{}
	for _, key := range order {
		c := byKey[key]
		slices.Sort(c.paths)
		out[c.group] = append(out[c.group], *c)
	}
	return out
}

// sizeOf is a file's size, or the sum of a folder's files.
func sizeOf(path string, info fs.FileInfo) int64 {
	if !info.IsDir() {
		return info.Size()
	}
	var n int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			n += fi.Size()
		}
		return nil
	})
	return n
}

// Size is n bytes in binary units, one decimal under 10 ("5.4G", "512M"),
// for a line a person reads (the sysstat segment's form).
func Size(n int64) string {
	const unit = 1024
	units := []string{"B", "K", "M", "G", "T", "P"}
	v := float64(max(n, 0))
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if v < 10 && i > 0 {
		return fmt.Sprintf("%.1f%s", v, units[i])
	}
	return fmt.Sprintf("%.0f%s", v, units[i])
}

// Entry is one log line: a copy or thing removed (or that failed to be).
type Entry struct {
	Time  time.Time `json:"time"`
	Rule  string    `json:"rule"`
	Path  string    `json:"path"`
	Paths []string  `json:"paths,omitempty"`
	// Noun and Key name a thing a record rule removed ({noun, key}).
	Noun   string `json:"noun,omitempty"`
	Key    string `json:"key,omitempty"`
	Bytes  int64  `json:"bytes"`
	Reason string `json:"reason"`
	Error  string `json:"error,omitempty"`
	// Actor and Source say who ran the removal, when known.
	Actor  string `json:"actor,omitempty"`
	Source string `json:"source,omitempty"`
}

// Apply removes what the plans list through remove, every file of a copy;
// a file already gone counts as removed, and one failing file is recorded
// on its Removal and never stops the rest. log, when set, gets one Entry
// per copy, removed or failed, at now. It answers the results with Applied,
// Freed and each Removal's Error set.
func Apply(results []Result, remove func(string) error, now time.Time, log func(Entry)) []Result {
	out := slices.Clone(results)
	for i := range out {
		if out[i].Error != "" {
			continue
		}
		out[i].Applied = true
		out[i].Remove = slices.Clone(out[i].Remove)
		for j, rm := range out[i].Remove {
			paths := rm.Paths
			if len(paths) == 0 {
				paths = []string{rm.Path}
			}
			var errs []error
			for _, p := range paths {
				if remove == nil {
					errs = append(errs, errors.New("no remover"))
					break
				}
				if err := remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
					errs = append(errs, fmt.Errorf("%s: %w", p, err))
				}
			}
			if err := errors.Join(errs...); err != nil {
				out[i].Remove[j].Error = err.Error()
			} else {
				out[i].Freed += rm.Bytes
			}
			if log != nil {
				log(Entry{Time: now, Rule: rm.Rule, Path: rm.Path, Paths: rm.Paths, Bytes: rm.Bytes, Reason: rm.Reason, Error: out[i].Remove[j].Error})
			}
		}
	}
	return out
}

// AppendLog appends entries to the log file at path, one JSON line each,
// creating it and its folder when missing.
func AppendLog(path string, entries ...Entry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			_ = f.Close()
			return err
		}
	}
	return errors.Join(w.Flush(), f.Close())
}

// ReadLog reads the log file at path, newest first; limit > 0 keeps the
// newest limit entries. A missing file is an empty log; a line that does
// not parse is skipped.
func ReadLog(path string, limit int) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for line := range strings.SplitSeq(string(data), "\n") {
		var e Entry
		if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		out = append(out, e)
	}
	slices.Reverse(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
