package retain

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// fixture writes files named by their age in days (name -> days old).
func fixture(t *testing.T, files map[string]int) string {
	t.Helper()
	dir := t.TempDir()
	for name, days := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-time.Duration(days)*24*time.Hour - time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func removed(r Result) []string {
	out := []string{}
	for _, rm := range r.Remove {
		out = append(out, filepath.Base(rm.Path))
	}
	slices.Sort(out)
	return out
}

// TestPlan covers each field once: keep, days, both 0 (off), min_keep,
// group and unit.
func TestPlan(t *testing.T) {
	daily := map[string]int{"d1": 1, "d2": 2, "d3": 3, "d9": 9, "d20": 20}
	cases := []struct {
		name  string
		files map[string]int
		rule  Rule
		want  []string
	}{
		{"keep", daily, Rule{Keep: 3}, []string{"d20", "d9"}},
		{"days", daily, Rule{Days: 5}, []string{"d20", "d9"}},
		{"off", daily, Rule{}, []string{}},
		{"min_keep", map[string]int{"old1": 30, "old2": 40}, Rule{Days: 14, MinKeep: 1}, []string{"old2"}},
		{"group", map[string]int{"a.bak-1": 1, "a.bak-2": 2, "b.bak-1": 5, "notes.txt": 50}, Rule{Keep: 1, Group: `^(\w+)\.bak-`}, []string{"a.bak-2"}},
		{"unit", map[string]int{"db.bak-1": 1, "db.bak-1-wal": 1, "db.bak-2": 3, "db.bak-2-wal": 3, "db.bak-2-shm": 3}, Rule{Keep: 1, Unit: `^(db\.bak-\d+)`}, []string{"db.bak-2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, tc.files)
			tc.rule.Name, tc.rule.Paths = tc.name, []string{filepath.Join(dir, "*")}
			got := Plan([]Rule{tc.rule}, now)[0]
			if got.Error != "" {
				t.Fatal(got.Error)
			}
			if r := removed(got); !slices.Equal(r, tc.want) {
				t.Fatalf("removes %v, want %v", r, tc.want)
			}
			if tc.name == "unit" && len(got.Remove[0].Paths) != 3 {
				t.Fatalf("a copy is removed whole: %v", got.Remove[0].Paths)
			}
			for _, rm := range got.Remove {
				if rm.Reason == "" || rm.Bytes == 0 {
					t.Fatalf("each removal says why and how much: %+v", rm)
				}
			}
		})
	}
}

// TestApply removes through the injected remover only; one failing file is
// recorded and the rest still go; every copy is one log entry.
func TestApply(t *testing.T) {
	dir := fixture(t, map[string]int{"a": 10, "b": 11, "c": 12})
	plan := Plan([]Rule{{Name: "r", Paths: []string{filepath.Join(dir, "*")}, Days: 5}}, now)
	var asked []string
	remove := func(p string) error {
		asked = append(asked, filepath.Base(p))
		if filepath.Base(p) == "b" {
			return errors.New("busy")
		}
		return nil
	}
	var log []Entry
	got := Apply(plan, remove, now, func(e Entry) { log = append(log, e) })[0]
	if len(asked) != 3 || len(log) != 3 {
		t.Fatalf("asked %v, logged %d", asked, len(log))
	}
	if got.Freed != 10 {
		t.Fatalf("freed %d, want the two copies that went", got.Freed)
	}
	for _, rm := range got.Remove {
		if (filepath.Base(rm.Path) == "b") != (rm.Error != "") {
			t.Fatalf("error recorded on the wrong copy: %+v", rm)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); err != nil {
		t.Fatal("the fake remover removes nothing on disk")
	}
	logPath := filepath.Join(t.TempDir(), "retention.log")
	if err := AppendLog(logPath, log...); err != nil {
		t.Fatal(err)
	}
	back, err := ReadLog(logPath, 2)
	if err != nil || len(back) != 2 || back[0].Path != log[2].Path {
		t.Fatalf("read back newest first: %+v %v", back, err)
	}
}
