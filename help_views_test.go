package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// sameAnswers checks that a hypothetical walk (nounRows) answers every key
// the live walk answers at the noun's level or app's (or the editor's) with
// the same row, and the other way round, wherever neither walk judges a
// guard: the live rows a guard decided and the keys a level between the
// noun and app (a page) takes are left out.
func sameAnswers(t *testing.T, r *Runtime, live, hypo []levelRow, levels ...string) {
	t.Helper()
	id := func(b Binding) string { return rowKey(b) }
	guarded := func(row levelRow) bool { return row.When != "" || r.VerbGuard(row.Request.Key()) != "" }
	same := func(a, b levelRow) bool {
		return id(a.Binding) == id(b.Binding) && a.Request.Key() == b.Request.Key() && a.level == b.level && a.Capture == b.Capture
	}
	for _, row := range live {
		if !slices.Contains(levels, row.level) || guarded(row) {
			continue
		}
		if !slices.ContainsFunc(hypo, func(h levelRow) bool { return h.guard == "" && same(row, h) }) {
			t.Errorf("live %s → %s at %s missing from the hypothetical walk", id(row.Binding), row.Request.Key(), row.level)
		}
	}
	firsts := map[string]bool{}
	for _, h := range hypo {
		key := id(h.Binding)
		if firsts[key] {
			continue
		}
		if h.guard != "" {
			firsts[key] = true // the live walk judged this key's guard
			continue
		}
		firsts[key] = true
		i := slices.IndexFunc(live, func(l levelRow) bool { return id(l.Binding) == key })
		if i >= 0 && (!slices.Contains(levels, live[i].level) || guarded(live[i])) {
			continue
		}
		if i < 0 || !same(live[i], h) {
			t.Errorf("hypothetical %s → %s at %s is not what the live walk answers", key, h.Request.Key(), h.level)
		}
	}
}

// The hypothetical walk for a noun and mode answers as the live walk does
// for a focus on that noun in that mode: a list, an input (the editor
// fallback), a terminal (a passthrough mode: no capture pass).
func TestNounRowsMatchTheLiveWalk(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		r := NewRuntime()
		NewModel(r)
		list, err := r.AddList("rows", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := list.Focus(); err != nil {
			t.Fatal(err)
		}
		sameAnswers(t, r, r.levelRows(r.Focus), r.nounRows(NounPlace{Noun: "list"}, r.Mode), "list", "app")
	})
	t.Run("input", func(t *testing.T) {
		r := NewRuntime()
		NewModel(r)
		if err := r.AddInput(NewInput("note", "", false), Kind("prompt")); err != nil {
			t.Fatal(err)
		}
		if err := r.SetFocus("note"); err != nil {
			t.Fatal(err)
		}
		if r.Mode.Name != "insert" {
			t.Fatalf("prompt focus in %q", r.Mode.Name)
		}
		hypo := r.nounRows(NounPlace{Noun: "prompt"}, r.Mode)
		if !slices.ContainsFunc(hypo, func(h levelRow) bool { return h.level == "editor" && h.guard == "" }) {
			t.Fatal("no editor fallback rows for an input noun")
		}
		sameAnswers(t, r, r.levelRows(r.Focus), hypo, "prompt", "app", "editor")
		if err := r.SetMode("normal"); err != nil {
			t.Fatal(err)
		}
		sameAnswers(t, r, r.levelRows(r.Focus), r.nounRows(NounPlace{Noun: "prompt"}, r.Mode), "prompt", "app", "editor")
	})
	t.Run("terminal", func(t *testing.T) {
		r, _, _ := terminalFixture(t)
		NewModel(r)
		if r.Mode.Name != "terminal" {
			t.Fatalf("terminal focus in %q", r.Mode.Name)
		}
		hypo := r.nounRows(NounPlace{Noun: "terminal"}, r.Mode)
		if slices.ContainsFunc(hypo, func(h levelRow) bool { return h.Capture }) {
			t.Fatal("a passthrough mode has no capture pass")
		}
		sameAnswers(t, r, r.levelRows(r.Focus), hypo, "terminal", "app")
	})
}

// A guarded row is kept, marked with its guard, and the rows answering
// when the guard is false follow it on the same key.
func TestNounHelpMarksGuardedRows(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	rows := NounHelp(r, "list", ModeState{Name: "normal"})
	i := slices.IndexFunc(rows, func(h HelpRow) bool { return h.Key == "space" })
	if i < 0 || rows[i].Guard != "list-marks" || rows[i].Operation != "list.mark ?" || !strings.HasPrefix(rows[i].Description, "when list-marks · ") {
		t.Fatalf("space on list: %+v", rows)
	}
	var dollar []string
	for _, h := range rows {
		if h.Key == "$" {
			dollar = append(dollar, h.Operation)
		}
	}
	if len(dollar) < 2 || dollar[0] != "list.column ?" || dollar[1] != "list.scroll-columns ?" {
		t.Fatalf("$ rows on list: %v", dollar)
	}
	// The editor's implicit input guard holds for an input noun.
	for _, h := range NounHelp(r, "prompt", ModeState{Name: "insert"}) {
		if h.Guard == ImplicitGuard {
			t.Fatalf("implicit guard shown on an input noun: %+v", h)
		}
	}
}

// An exclusive pending mode reads only its own table: no any-mode rows.
func TestNounRowsExclusivePendingMode(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	rows := r.nounRows(NounPlace{Noun: "app"}, ModeState{Name: "count_pending"})
	if len(rows) == 0 {
		t.Fatal("count_pending answers nothing")
	}
	for _, row := range rows {
		if row.Keys == "ctrl+space" || row.Keys == "pgdown" {
			t.Fatalf("any-mode row %s in an exclusive mode", row.Keys)
		}
	}
	if !slices.ContainsFunc(rows, func(row levelRow) bool { return strings.HasPrefix(row.Pattern, Digit) }) {
		t.Fatal("count_pending has no digit class row")
	}
	if !slices.ContainsFunc(r.nounRows(NounPlace{Noun: "app"}, ModeState{Name: "normal"}), func(row levelRow) bool { return row.Keys == "ctrl+space" }) {
		t.Fatal("normal lost its any-mode rows")
	}
}

// The table has a column per mode with rows for the noun or app, a key
// class in its short form, app's cells in parentheses and guarded ones
// marked; pending modes are columns only with help.pending-modes.
func TestHelpTableColumns(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	modes, keys := r.HelpTable(NounPlace{Noun: "list"}, false)
	if !slices.Contains(modes, "normal") || !slices.Contains(modes, ListFilterMode) || slices.Contains(modes, "insert") || slices.Contains(modes, "terminal") {
		t.Fatalf("list modes: %v", modes)
	}
	if slices.Contains(modes, "count_pending") {
		t.Fatalf("pending mode without the setting: %v", modes)
	}
	items := helpTableItems("list", modes, keys)
	cell := func(key, mode string) string {
		for _, item := range items {
			if item.Data.(HelpKey).Key == key {
				return item.Cells[slices.Index(modes, mode)+1]
			}
		}
		t.Fatalf("no row for %s", key)
		return ""
	}
	if got := cell("space", "normal"); got != ".mark?" {
		t.Errorf("space normal: %q", got)
	}
	if got := cell(":", "normal"); got != "(palette.open)" {
		t.Errorf(": normal: %q", got)
	}
	if got := cell(":", ListFilterMode); got != "" {
		t.Errorf(": in the filter: %q", got)
	}
	if got := cell("ctrl+c", "normal"); got != "(app.quit?)" {
		t.Errorf("ctrl+c normal: %q", got)
	}
	if !slices.ContainsFunc(items, func(item ListItem) bool { return item.Cells[0] == "1-9" }) {
		t.Error("no count row in its short form")
	}
	if own := items[0].Data.(HelpKey); own.Modes[0].Rows[0].Noun == "app" {
		t.Errorf("the noun's keys do not come first: %+v", own)
	}
	pending, _ := r.HelpTable(NounPlace{Noun: "app"}, true)
	if !slices.Contains(pending, "count_pending") || slices.Contains(pending, "macro_recording") {
		t.Fatalf("pending columns: %v", pending)
	}
	names := func(ms []ModeState) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.Name)
		}
		return out
	}
	if plain := names(r.HelpModes(NounPlace{Noun: "list"}, false)); slices.Contains(plain, "count_pending") || slices.Contains(plain, "normal g") {
		t.Fatalf("pick modes without the setting: %v", plain)
	}
	if all := names(r.HelpModes(NounPlace{Noun: "list"}, true)); !slices.Contains(all, "count_pending") || !slices.Contains(all, "normal g") {
		t.Fatalf("pick modes with the setting: %v", all)
	}
	if rows := NounHelp(r, "list", ModeState{Name: "normal g", Payload: Payload{Prefix: "g"}}); !slices.ContainsFunc(rows, func(h HelpRow) bool { return h.Key == "g g" }) || slices.ContainsFunc(rows, func(h HelpRow) bool { return h.Key == "j" }) {
		t.Fatalf("a prefix mode lists its sequences only: %+v", rows)
	}
}

// S cycles here → pick → table → here, skipping a view its setting turns
// off; with both off S is not offered and does nothing.
func TestHelpViewCycle(t *testing.T) {
	cycle := func(t *testing.T, pick, table bool) []string {
		t.Helper()
		r, m := helpFixture(t)
		r.EnsureConfigSettings()
		if err := r.Settings().Set(HelpPickSetting, pick); err != nil {
			t.Fatal(err)
		}
		if err := r.Settings().Set(HelpTableSetting, table); err != nil {
			t.Fatal(err)
		}
		keytest.Press(m, "?")
		views := []string{r.helpOpen().view}
		for range 3 {
			keytest.Press(m, "S")
			views = append(views, r.helpOpen().view)
		}
		hints := BindingHints(r, r.Focus)
		if offered := strings.Contains(hints, "S view"); offered != (pick || table) {
			t.Fatalf("S offered %v in %q", offered, hints)
		}
		return views
	}
	for _, tc := range []struct {
		pick, table bool
		want        string
	}{
		{true, true, "here pick table here"},
		{false, true, "here table here table"},
		{true, false, "here pick here pick"},
		{false, false, "here here here here"},
	} {
		if got := strings.Join(cycle(t, tc.pick, tc.table), " "); got != tc.want {
			t.Errorf("pick %v table %v: %s, want %s", tc.pick, tc.table, got, tc.want)
		}
	}
}

// Pick: n chooses the noun, m the mode; the header names both, the rows
// are what that noun answers in that mode.
func TestHelpPickChoosesNounAndMode(t *testing.T) {
	r, m := helpFixture(t)
	m.Width, m.Height = 120, 40
	pressAll(m, "?", "S")
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Keys · pick") || !strings.Contains(frame, "noun: widget · mode: normal") || !strings.Contains(frame, "widget.open") {
		t.Fatalf("pick at open:\n%s", frame)
	}
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "n noun") || !strings.Contains(hints, "m mode") {
		t.Fatalf("pick hints: %q", hints)
	}
	keytest.Press(m, "n")
	if r.TopModal() != helpNounKind {
		t.Fatalf("n opened %q", r.TopModal())
	}
	pressAll(m, "p", "r", "o", "m", "p", "t", "enter")
	keytest.Press(m, "m")
	pressAll(m, "i", "n", "s", "e", "r", "t", "enter")
	frame = ansi.Strip(m.View().Content)
	if r.TopModal() != helpID || !strings.Contains(frame, "noun: prompt · mode: insert") || !strings.Contains(frame, "input.kill") {
		t.Fatalf("pick after n and m (%s):\n%s", m.Status, frame)
	}
	keytest.Press(m, "S")
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "n noun") || strings.Contains(hints, "m mode") {
		t.Fatalf("table hints: %q", hints)
	}
	frame = ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Keys · table") || !strings.Contains(frame, "noun: prompt") || !strings.Contains(frame, "insert") {
		t.Fatalf("table keeps the noun:\n%s", frame)
	}
}

func pressAll(m *Model, keys ...string) {
	for _, key := range keys {
		keytest.Press(m, key)
	}
}
