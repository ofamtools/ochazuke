package ochazuke

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke/retain"
)

// Retention (data-retention plan, lanes R2 and R5): an application or tool
// declares its retention rows as data (DeclareRetention); each row reads
// its limits from two settings; the framework noun ochazuke.retention-rule
// lists them with what they would remove now (the dry run), and its apply
// verb removes it. A file row is a retain.Rule over globs; a record row
// names a noun, a where condition and an existing verb, and runs that
// verb, through Do, on each thing the noun's list verb answers whose
// updated prop is older than the row's days. A store row has an Expire
// function instead: the program's own store removes what the row's limits
// say, its own way (a database table's old rows, in batches), and answers
// what it holds and what the cut removes. Nothing here names a tool, a
// folder or a noun: every row is the declaring program's data.

const (
	// RetentionNoun is the framework noun of the declared rows.
	RetentionNoun = "ochazuke.retention-rule"
	// RetentionAutoSetting turns the automatic runs (at start, the daily
	// tick, RetentionDue) on or off; the verbs always work.
	RetentionAutoSetting = "retention.auto"
	// RetentionFirstRunSetting decides what an automatic run does when a
	// row nobody has applied yet would remove more than its keep (at least
	// one) in one go: on (the default) holds the removal, records it and
	// tells the owner, and the row removes by itself only after someone
	// applied it once (retention-rule apply NAME); off removes at once.
	RetentionFirstRunSetting = "retention.first-run-ask"
	// RetentionStateFile holds each row's last run day, whether it was
	// applied by hand once and what it holds; RetentionLogFile
	// one JSON line per removal. Both live in the state directory.
	RetentionStateFile = "retention.json"
	RetentionLogFile   = "retention.log"
	// RetentionTick is how often a running Model asks whether a row is due
	// (a row runs once per UTC day).
	RetentionTick = time.Hour
	// retentionSource is the history source of the requests a rule runs.
	retentionSource = "hook:retention"
)

// RetentionRow is one declared retention rule.
//
// A file row has Paths: filepath.Glob patterns, absolute or starting with
// a placeholder, {data} (Config.DataDirectory), {state}
// (Config.StateDirectory), {config} (Config.Dir) or {home}; Group, Unit,
// Keep, Days and MinKeep mean what they mean in retain.Rule. A record row
// has Noun, Where (the list verb's where, prop=value pairs, may be empty)
// and Verb (an existing verb of the noun, such as archive or delete): the
// same rule (retain.Reasons) runs over the things the list answers, newest
// first by their updated prop, and each thing it removes gets the verb. A
// store row has Expire and neither Paths nor Noun: the program's store
// applies the cut itself (RetentionCut, in its own unit: rows of a table)
// and answers what it holds and what the cut removes (RetentionCount).
// Keep and Days here are the defaults of the row's settings, KeepSetting
// and DaysSetting (default retention.NAME.keep and retention.NAME.days; a
// row may name a setting the program already declares, such as
// backup.keep, so nothing is renamed). Both 0 turn the row off.
//
// Approved, when set, is the owner's recorded decision that approves the
// row's removals from its declaration (the decision's words, its date):
// an automatic run removes what the row says at once, as after a first
// apply by hand, instead of holding a first large removal
// (retention.first-run-ask, which still holds every other row).
type RetentionRow struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Group       string   `json:"group,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Keep        int      `json:"keep,omitempty"`
	Days        int      `json:"days,omitempty"`
	MinKeep     int      `json:"min_keep,omitempty"`
	KeepSetting string   `json:"keep_setting,omitempty"`
	DaysSetting string   `json:"days_setting,omitempty"`
	Noun        string   `json:"noun,omitempty"`
	Where       string   `json:"where,omitempty"`
	Verb        string   `json:"verb,omitempty"`
	Approved    string   `json:"approved,omitempty"`
	// Expire is a store row's cut (see RetentionCut).
	Expire func(RetentionCut) (RetentionCount, error) `json:"-"`
}

// RetentionCut is what a store row's Expire is asked: the row's limits
// now (Keep, Days, MinKeep; Before is now minus Days, zero with Days 0)
// and whether to remove (DryRun false) or only count.
type RetentionCut struct {
	Keep    int
	Days    int
	MinKeep int
	Before  time.Time
	DryRun  bool
}

// RetentionCount is a store row's answer: what the store holds (Count,
// Bytes), what the cut removes or removed (Remove, RemoveBytes: an
// estimate where the store cannot say exactly), and a line naming it for
// a person (What: "62,684 history rows from 2026-10-19 to 2026-11-10").
type RetentionCount struct {
	Count       int
	Bytes       int64
	Remove      int
	RemoveBytes int64
	What        string
}

// records reports whether the row rules a noun's things (not files).
func (row RetentionRow) records() bool { return row.Noun != "" }

// what names what the row removes, for its settings' descriptions.
func (row RetentionRow) what() string {
	switch {
	case row.records():
		return row.Noun + " things (" + row.Verb + ")"
	case row.stored():
		return "entries"
	}
	return "copies"
}

// stored reports whether the row is a store row (its store's own cut).
func (row RetentionRow) stored() bool { return row.Expire != nil }

// kind is the row's kind as its thing shows it.
func (row RetentionRow) kind() string {
	switch {
	case row.records():
		return "records"
	case row.stored():
		return "store"
	}
	return "files"
}

// retention is the declared rows and the runtime's retention state, held on
// the config component (the Runtime holds no component fields).
type retention struct {
	rows []RetentionRow
	// remove removes one path; nil is os.RemoveAll behind removeGuard.
	remove func(string) error
	// now is the clock; nil is time.Now.
	now func() time.Time
	// state is each row's state when there is no state directory.
	state map[string]retentionState
}

// retentionState is one row's line of retention.json: the UTC day it last
// ran automatically, when someone first applied it by hand (from then on a
// large automatic removal is not held), and the removal it holds.
type retentionState struct {
	Ran      string         `json:"ran,omitempty"`
	Approved string         `json:"approved,omitempty"`
	Held     *RetentionHeld `json:"held,omitempty"`
}

// RetentionHeld is a first large automatic removal a row holds for the
// owner (retention.first-run-ask): when, how many copies or things, how
// many bytes.
type RetentionHeld struct {
	At    string `json:"at"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes,omitempty"`
}

func (r *Runtime) retention() *retention {
	c := r.Config()
	if c.retention == nil {
		c.retention = &retention{state: map[string]retentionState{}}
	}
	return c.retention
}

func (st *retention) clock() time.Time {
	if st.now != nil {
		return st.now()
	}
	return time.Now()
}

// SetRetentionRemover replaces the function retention removes a path with
// (tests pass a fake; nil restores the default, os.RemoveAll behind a
// guard); it answers a function that puts the previous one back.
func (r *Runtime) SetRetentionRemover(remove func(string) error) (restore func()) {
	st := r.retention()
	before := st.remove
	st.remove = remove
	return func() { st.remove = before }
}

// SetRetentionClock replaces the clock retention reads (tests); nil is
// time.Now. It answers a function that puts the previous one back.
func (r *Runtime) SetRetentionClock(now func() time.Time) (restore func()) {
	st := r.retention()
	before := st.now
	st.now = now
	return func() { st.now = before }
}

// DeclareRetention declares retention rows. Each row's settings are
// declared (integer, section data) unless a setting of that name exists
// already: a program that names its own setting declares it first. A
// row's name is one word, unique; declaring a name again replaces the row.
func (r *Runtime) DeclareRetention(rows ...RetentionRow) error {
	st := r.retention()
	for _, row := range rows {
		if err := validRetentionRow(row); err != nil {
			return err
		}
		row.KeepSetting = cmp.Or(row.KeepSetting, "retention."+row.Name+".keep")
		row.DaysSetting = cmp.Or(row.DaysSetting, "retention."+row.Name+".days")
		if err := r.declareRetentionSetting(row.KeepSetting, row.Keep, "Retention rule "+row.Name+": keep at most this many newest "+row.what()+" (0: no count limit; with days 0 too the rule is off)"); err != nil {
			return err
		}
		if err := r.declareRetentionSetting(row.DaysSetting, row.Days, "Retention rule "+row.Name+": remove "+row.what()+" older than this many days (0: no age limit; with keep 0 too the rule is off)"); err != nil {
			return err
		}
		if i := slices.IndexFunc(st.rows, func(o RetentionRow) bool { return o.Name == row.Name }); i >= 0 {
			st.rows[i] = row
		} else {
			st.rows = append(st.rows, row)
		}
	}
	r.Changed(RetentionNoun)
	return nil
}

// RetentionRows are the declared rows, in declaration order (a copy).
func (r *Runtime) RetentionRows() []RetentionRow { return slices.Clone(r.retention().rows) }

func validRetentionRow(row RetentionRow) error {
	if !oneWord(row.Name) {
		return fmt.Errorf("retention row name %q must be one word", row.Name)
	}
	if row.Keep < 0 || row.Days < 0 || row.MinKeep < 0 {
		return fmt.Errorf("retention row %s: keep, days and min_keep are not negative", row.Name)
	}
	if row.stored() {
		return nil // a store row's Expire is its cut: paths and a noun are not read
	}
	if row.records() {
		if row.Verb == "" || len(row.Paths) > 0 {
			return fmt.Errorf("retention row %s: a record row names a verb and no paths", row.Name)
		}
		return nil
	}
	if len(row.Paths) == 0 {
		return fmt.Errorf("retention row %s: a file row names paths (or a noun for a record row)", row.Name)
	}
	for _, p := range row.Paths {
		if !filepath.IsAbs(p) && !slices.ContainsFunc(retentionPlaceholders, func(ph string) bool { return strings.HasPrefix(p, ph) }) {
			return fmt.Errorf("retention row %s: path %q is neither absolute nor starts with {data}, {state}, {config} or {home}", row.Name, p)
		}
	}
	return retain.Rule{Name: row.Name, Paths: []string{"/"}, Group: row.Group, Unit: row.Unit}.Validate()
}

func (r *Runtime) declareRetentionSetting(name string, def int, description string) error {
	if r.Settings().find(name) != nil {
		return nil
	}
	return r.DeclareSetting(Setting{Name: name, Type: SettingInteger, Default: def, Section: "data", Description: description})
}

// limits are the row's keep and days from its settings (a negative value
// reads as 0).
func (r *Runtime) retentionLimits(row RetentionRow) (keep, days int) {
	s := r.Settings()
	return max(s.Int(row.KeepSetting), 0), max(s.Int(row.DaysSetting), 0)
}

// retentionPlaceholders are the folders a row's path may start with.
var retentionPlaceholders = []string{"{data}", "{state}", "{config}", "{home}"}

// retentionDirs are the placeholders a row's paths may start with.
func (r *Runtime) retentionDirs() map[string]string {
	c := r.Config()
	home, _ := os.UserHomeDir()
	return map[string]string{retentionPlaceholders[0]: c.DataDirectory(), retentionPlaceholders[1]: c.StateDirectory(), retentionPlaceholders[2]: c.Dir, retentionPlaceholders[3]: home}
}

// expandRetention expands a row's paths; a placeholder with no folder
// here answers why, and the row removes nothing.
func (r *Runtime) expandRetention(row RetentionRow) ([]string, string) {
	dirs := r.retentionDirs()
	out := []string{}
	for _, p := range row.Paths {
		for ph, dir := range dirs {
			rest, ok := strings.CutPrefix(p, ph)
			if !ok {
				continue
			}
			if dir == "" {
				return nil, fmt.Sprintf("retention row %s: %s is no folder here, so the row removes nothing", row.Name, ph)
			}
			p = filepath.Join(dir, rest)
			break
		}
		out = append(out, p)
	}
	return out, ""
}

// RetentionItem is one copy or thing a rule removes (or removed): a file
// copy's path, or a thing's {noun, key} with its label.
type RetentionItem struct {
	Path  string   `json:"path,omitempty"`
	Paths []string `json:"paths,omitempty"`
	Noun  string   `json:"noun,omitempty"`
	Key   string   `json:"key,omitempty"`
	Label string   `json:"label,omitempty"`
	// Count is how many entries a store row's one item stands for.
	Count  int    `json:"count,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Reason string `json:"reason"`
	Error  string `json:"error,omitempty"`
}

// line is the item as one line of text: what, how big, why.
func (it RetentionItem) line() string {
	switch {
	case it.Noun != "":
		return fmt.Sprintf("%s %s %q: %s", it.Noun, it.Key, it.Label, it.Reason)
	case it.Path == "":
		return fmt.Sprintf("%s (about %s): %s", it.Label, retain.Size(it.Bytes), it.Reason)
	}
	return fmt.Sprintf("%s (%d bytes): %s", it.Path, it.Bytes, it.Reason)
}

// RetentionRun is one row's plan, or after apply what it did.
type RetentionRun struct {
	Name   string          `json:"name"`
	Kind   string          `json:"kind"`
	Keep   int             `json:"keep"`
	Days   int             `json:"days"`
	Off    bool            `json:"off,omitempty"`
	Count  int             `json:"count"`
	Bytes  int64           `json:"bytes"`
	Items  []RetentionItem `json:"removals"`
	Remove int             `json:"would_remove"`
	// RemoveBytes is what the removals hold; Freed what apply freed.
	RemoveBytes int64 `json:"would_free"`
	Applied     bool  `json:"applied,omitempty"`
	// Held is an automatic run that removed nothing because the row's
	// first large removal waits for the owner (retention.first-run-ask).
	Held    bool   `json:"held,omitempty"`
	Removed int    `json:"removed,omitempty"`
	Freed   int64  `json:"freed,omitempty"`
	Error   string `json:"error,omitempty"`
}

// planRetention is what the row removes now.
func (r *Runtime) planRetention(c Context, row RetentionRow) RetentionRun {
	keep, days := r.retentionLimits(row)
	run := RetentionRun{Name: row.Name, Kind: row.kind(), Keep: keep, Days: days, Off: keep == 0 && days == 0, Items: []RetentionItem{}}
	switch {
	case row.records():
		return r.planRecords(c, row, run)
	case row.stored():
		return r.planStore(c, row, run)
	}
	paths, problem := r.expandRetention(row)
	if problem != "" {
		run.Error = problem
		return run
	}
	res := retain.Plan([]retain.Rule{{Name: row.Name, Paths: paths, Group: row.Group, Unit: row.Unit, Keep: keep, Days: days, MinKeep: row.MinKeep}}, r.retention().clock())[0]
	run.Count, run.Bytes, run.Error = res.Copies, res.Bytes, res.Error
	for _, rm := range res.Remove {
		run.Items = append(run.Items, RetentionItem{Path: rm.Path, Paths: rm.Paths, Bytes: rm.Bytes, Reason: rm.Reason})
		run.RemoveBytes += rm.Bytes
	}
	run.Remove = len(run.Items)
	return run
}

// planRecords lists the row's noun through its list verb (the where, the
// archived things included) and rules the things by their updated prop.
func (r *Runtime) planRecords(c Context, row RetentionRow, run RetentionRun) RetentionRun {
	n := r.Noun(row.Noun)
	switch {
	case n == nil:
		run.Error = fmt.Sprintf("retention row %s: no noun %s here", row.Name, row.Noun)
		return run
	case n.roleProp(RoleUpdated) == nil:
		run.Error = fmt.Sprintf("retention row %s: %s has no updated prop", row.Name, r.DisplayNoun(n.Name))
		return run
	}
	if _, ok := r.Operation(n.Name + "." + row.Verb); !ok {
		run.Error = fmt.Sprintf("retention row %s: %s has no verb %s", row.Name, r.DisplayNoun(n.Name), row.Verb)
		return run
	}
	args := Arguments{"archived": true}
	if row.Where != "" {
		args["where"] = row.Where
	}
	value, err := r.retentionRequest(c, row, Request{Noun: n.Name, Verb: "list", Args: args})
	if err != nil {
		run.Error = err.Error()
		return run
	}
	var page ListAnswer
	if data, err := json.Marshal(value); err != nil || json.Unmarshal(data, &page) != nil {
		run.Error = fmt.Sprintf("retention row %s: %s list answered no rows", row.Name, r.DisplayNoun(n.Name))
		return run
	}
	// Newest first by updated, as retain rules copies; a thing with no
	// time is never removed.
	updated := n.roleProp(RoleUpdated).Name
	type dated struct {
		at   time.Time
		item map[string]any
	}
	things := []dated{}
	for _, item := range page.Items {
		run.Count++
		if at, ok := ParseTime(fmt.Sprint(item[updated])); ok {
			things = append(things, dated{at, item})
		}
	}
	slices.SortStableFunc(things, func(a, b dated) int { return b.at.Compare(a.at) })
	times := make([]time.Time, len(things))
	for i, t := range things {
		times[i] = t.at
	}
	for i, reason := range retain.Reasons(times, run.Keep, run.Days, row.MinKeep, r.retention().clock()) {
		if reason == "" {
			continue
		}
		label, _ := things[i].item["label"].(string)
		run.Items = append(run.Items, RetentionItem{Noun: n.Name, Key: fmt.Sprint(things[i].item["id"]), Label: label, Reason: reason})
	}
	run.Remove = len(run.Items)
	return run
}

// planStore asks a store row's Expire to count what its cut removes now
// (one item standing for every entry, with its reason).
func (r *Runtime) planStore(_ Context, row RetentionRow, run RetentionRun) RetentionRun {
	if run.Off {
		return run
	}
	got, err := row.Expire(r.retentionCut(row, run, true))
	if err != nil {
		run.Error = fmt.Sprintf("retention row %s: %v", row.Name, err)
		return run
	}
	run.Count, run.Bytes = got.Count, got.Bytes
	if got.Remove > 0 {
		run.Items = append(run.Items, RetentionItem{Label: got.What, Count: got.Remove, Bytes: got.RemoveBytes, Reason: storeReason(run)})
		run.Remove, run.RemoveBytes = got.Remove, got.RemoveBytes
	}
	return run
}

// applyStoreRow runs a store row's cut for its item i (the one the plan
// counted) and answers the log line: the count it removed and the plan's
// byte estimate for that share.
func (r *Runtime) applyStoreRow(row RetentionRow, run *RetentionRun, i int, now time.Time, who, source string) retain.Entry {
	item := &run.Items[i]
	got, err := row.Expire(r.retentionCut(row, *run, false))
	if err != nil {
		item.Error = err.Error()
	}
	var freed int64
	if item.Count > 0 {
		freed = item.Bytes * int64(min(got.Remove, item.Count)) / int64(item.Count)
	}
	run.Removed += got.Remove
	run.Freed += freed
	item.Label = cmp.Or(got.What, item.Label)
	return retain.Entry{Time: now, Rule: row.Name, Label: item.Label, Count: got.Remove, Bytes: freed, Reason: item.Reason, Error: item.Error, Actor: who, Source: source}
}

// retentionCut is the cut a store row's Expire is asked for now.
func (r *Runtime) retentionCut(row RetentionRow, run RetentionRun, dry bool) RetentionCut {
	cut := RetentionCut{Keep: run.Keep, Days: run.Days, MinKeep: row.MinKeep, DryRun: dry}
	if run.Days > 0 {
		cut.Before = r.retention().clock().Add(-time.Duration(run.Days) * 24 * time.Hour)
	}
	return cut
}

// storeReason is why a store row's entries go, in retain.Reasons' words.
func storeReason(run RetentionRun) string {
	var why []string
	if run.Days > 0 {
		why = append(why, fmt.Sprintf("older than %d days", run.Days))
	}
	if run.Keep > 0 {
		why = append(why, fmt.Sprintf("beyond the newest %d", run.Keep))
	}
	return strings.Join(why, " or ")
}

// retentionRequest runs a request a rule makes, through the one public
// trigger, with the rule as its history source. The actor is the caller's:
// an agent's call reaches the verb under the agent's rights.
func (r *Runtime) retentionRequest(c Context, row RetentionRow, q Request) (any, error) {
	q.source = retentionSource + ":" + row.Name
	return r.ExecuteAt(c, q)
}

// applyRetention removes what the row's plan lists: files through the
// remover, things through the row's verb (Confirmed: the owner's rule asks
// no question; the verb's agent right still decides for an agent). Every
// removal, done or failed, is one line of the retention log. An automatic
// run (auto) holds a first large removal (retentionHolds) and removes
// nothing; a run by hand (auto false) marks the row applied, so later
// automatic runs remove by themselves.
func (r *Runtime) applyRetention(c Context, row RetentionRow, auto bool) RetentionRun {
	run := r.planRetention(c, row)
	if run.Error != "" {
		return run
	}
	if auto && r.retentionHolds(row, run) {
		r.holdRetention(row, run)
		run.Held = true
		return run
	}
	if !auto {
		r.approveRetention(row)
	}
	if run.Off {
		return run
	}
	st := r.retention()
	now := st.clock()
	var entries []retain.Entry
	who, source := r.Actor().String(), retentionSource+":"+row.Name
	for i, item := range run.Items {
		if row.stored() {
			entries = append(entries, r.applyStoreRow(row, &run, i, now, who, source))
			continue
		}
		if row.records() {
			_, err := r.retentionRequest(c, row, Request{Noun: item.Noun, Verb: row.Verb, Args: Arguments{r.keyParam(item.Noun): item.Key}, Confirmed: true})
			if err != nil {
				run.Items[i].Error = err.Error()
			} else {
				run.Removed++
			}
		} else {
			res := retain.Apply([]retain.Result{{Rule: row.Name, Remove: []retain.Removal{{Rule: row.Name, Path: item.Path, Paths: item.Paths, Bytes: item.Bytes, Reason: item.Reason}}}}, r.retentionRemover(), now, nil)[0]
			if res.Remove[0].Error != "" {
				run.Items[i].Error = res.Remove[0].Error
			} else {
				run.Removed++
				run.Freed += item.Bytes
			}
		}
		entries = append(entries, retain.Entry{Time: now, Rule: row.Name, Path: item.Path, Paths: item.Paths, Noun: item.Noun, Key: item.Key, Bytes: item.Bytes, Reason: item.Reason, Error: run.Items[i].Error, Actor: who, Source: source})
	}
	run.Applied = true
	if path := r.retentionPath(RetentionLogFile); path != "" {
		if err := retain.AppendLog(path, entries...); err != nil {
			r.UI().Notify("retention log: " + err.Error())
		}
	}
	if len(entries) > 0 {
		r.Changed(RetentionNoun)
	}
	return run
}

// keyParam is the name of the noun's subject parameter (its key prop).
func (r *Runtime) keyParam(noun string) string {
	if p := r.Noun(noun).keyProp(); p != nil {
		return p.Name
	}
	return "id"
}

// retentionRemover is the injected remover, else os.RemoveAll behind
// removeGuard.
func (r *Runtime) retentionRemover() func(string) error {
	if fn := r.retention().remove; fn != nil {
		return fn
	}
	dirs := r.retentionDirs()
	return func(path string) error {
		if err := removeGuard(path, dirs); err != nil {
			return err
		}
		return os.RemoveAll(path)
	}
}

// removeGuard refuses a path that is a filesystem root, the home folder or
// one of the placeholder folders itself (a glob gone wrong).
func removeGuard(path string, dirs map[string]string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean == filepath.Dir(clean) {
		return fmt.Errorf("retention never removes %s", path)
	}
	for _, dir := range dirs {
		if dir != "" && clean == filepath.Clean(dir) {
			return fmt.Errorf("retention never removes %s itself", path)
		}
	}
	return nil
}

// retentionPath is a file of the state directory, "" when there is none.
func (r *Runtime) retentionPath(name string) string {
	dir := r.Config().StateDirectory()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// retentionStates reads each row's state (retention.json, else memory). A
// row whose value is a bare day string (the first form of the file) reads
// as its last run day.
func (r *Runtime) retentionStates() map[string]retentionState {
	path := r.retentionPath(RetentionStateFile)
	if path == "" {
		return maps.Clone(r.retention().state)
	}
	out := map[string]retentionState{}
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &raw)
	}
	for name, value := range raw {
		var s retentionState
		if json.Unmarshal(value, &s.Ran) != nil {
			_ = json.Unmarshal(value, &s)
		}
		out[name] = s
	}
	return out
}

// updateRetentionStates changes the named rows' states and writes them back
// (temp file + rename).
func (r *Runtime) updateRetentionStates(names []string, change func(*retentionState)) {
	states := r.retentionStates()
	for _, name := range names {
		s := states[name]
		change(&s)
		states[name] = s
	}
	path := r.retentionPath(RetentionStateFile)
	if path == "" {
		r.retention().state = states
		return
	}
	data, _ := json.MarshalIndent(states, "", "  ")
	if err := errors.Join(os.MkdirAll(filepath.Dir(path), 0o700), writeFileAtomic(path, data)); err != nil {
		r.UI().Notify("retention: " + err.Error())
	}
}

// markRetentionRan records the rows' run day.
func (r *Runtime) markRetentionRan(names []string, day string) {
	r.updateRetentionStates(names, func(s *retentionState) { s.Ran = day })
}

// retentionHolds reports whether an automatic run of the row holds its
// removal: retention.first-run-ask is on, nobody has applied the row by hand
// yet, and the run would remove more than the row's keep (at least one).
func (r *Runtime) retentionHolds(row RetentionRow, run RetentionRun) bool {
	if !r.Settings().Bool(RetentionFirstRunSetting) || run.Off {
		return false
	}
	return row.Approved == "" && r.retentionStates()[row.Name].Approved == "" && run.Remove > max(run.Keep, 1)
}

// holdRetention records the held removal (apply tells the owner).
func (r *Runtime) holdRetention(row RetentionRow, run RetentionRun) {
	held := &RetentionHeld{At: r.retention().clock().UTC().Format(time.RFC3339), Count: run.Remove, Bytes: run.RemoveBytes}
	r.updateRetentionStates([]string{row.Name}, func(s *retentionState) { s.Held = held })
	r.Changed(RetentionNoun)
}

// approveRetention marks the row applied by hand: its held removal is gone
// and later automatic runs remove by themselves.
func (r *Runtime) approveRetention(row RetentionRow) {
	if s := r.retentionStates()[row.Name]; s.Approved != "" && s.Held == nil {
		return
	}
	at := r.retention().clock().UTC().Format(time.RFC3339)
	r.updateRetentionStates([]string{row.Name}, func(s *retentionState) {
		s.Approved = cmp.Or(s.Approved, at)
		s.Held = nil
	})
	r.Changed(RetentionNoun)
}

// retentionHeldText is the line the owner reads about a held removal.
func retentionHeldText(name string, held RetentionHeld) string {
	what := fmt.Sprintf("%d", held.Count)
	if held.Bytes > 0 {
		what += " (" + retain.Size(held.Bytes) + ")"
	}
	return fmt.Sprintf("retention: %s would remove %s on its first run and waits for you: retention-rule show %s lists them, retention-rule apply %s removes them", name, what, name, name)
}

// notifyRetentionHeld notifies the rows whose first large removal waits
// for the owner, except those a run just told about: a program's start
// shows what a run in another process (mcp serve, a tool) held.
func (r *Runtime) notifyRetentionHeld(told []RetentionRun) {
	states := r.retentionStates()
	for _, row := range r.retention().rows {
		held := states[row.Name].Held
		if held == nil || slices.ContainsFunc(told, func(run RetentionRun) bool { return run.Name == row.Name && run.Held }) {
			continue
		}
		r.UI().Notify(retentionHeldText(row.Name, *held))
	}
}

// RetentionDue runs every row not run yet this UTC day, when retention.auto
// is on: at a program's start (Model.Init, mcp serve), from a running
// Model's hourly tick, and from a one-shot tool before its first write of
// the day (the hook a tool calls where it makes its daily backup). It runs
// as ochazuke.retention-rule.apply {due: true} under the caller's actor,
// and answers the rows it ran (none when nothing is due or it is off).
func (r *Runtime) RetentionDue() []RetentionRun {
	if len(r.retention().rows) == 0 || !r.Settings().Bool(RetentionAutoSetting) {
		return nil
	}
	defer r.forceSource(retentionSource)()
	value, err := r.ExecuteAt(r.Context(), Request{Noun: RetentionNoun, Verb: "apply", Args: Arguments{"due": true}})
	if err != nil {
		r.UI().Notify("retention: " + err.Error())
		return nil
	}
	runs, _ := value.([]RetentionRun)
	return runs
}

// RetentionTickMsg is a running Model's hourly retention check.
type RetentionTickMsg struct{}

// retentionTick schedules the next check, nil while no row is declared
// (RetentionDue reads retention.auto at each tick).
func (m *Model) retentionTick() tea.Cmd {
	if len(m.Runtime.retention().rows) == 0 {
		return nil
	}
	return tea.Tick(RetentionTick, func(time.Time) tea.Msg { return RetentionTickMsg{} })
}

// retentionTickFor answers a retention tick: run what is due and schedule
// the next one; nil for any other message.
func (m *Model) retentionTickFor(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(RetentionTickMsg); !ok {
		return nil
	}
	m.Runtime.RetentionDue()
	return m.retentionTick()
}

// retentionThing is a row as a thing of ochazuke.retention-rule, with its
// plan now.
func (r *Runtime) retentionThing(c Context, row RetentionRow) Thing {
	run := r.planRetention(c, row)
	items := make([]string, 0, len(run.Items))
	for _, it := range run.Items {
		items = append(items, it.line())
	}
	state := r.retentionStates()[row.Name]
	held := ""
	if state.Held != nil {
		held = retentionHeldText(row.Name, *state.Held)
	}
	paths := slices.Clone(row.Paths)
	if paths == nil {
		paths = []string{}
	}
	return Thing{
		"name": row.Name, "description": row.Description, "kind": row.kind(), "paths": paths,
		"noun": row.Noun, "where": row.Where, "verb": row.Verb,
		"keep": run.Keep, "days": run.Days, "min_keep": row.MinKeep,
		"keep_setting": row.KeepSetting, "days_setting": row.DaysSetting,
		"count": run.Count, "bytes": run.Bytes, "would_remove": run.Remove, "would_free": run.RemoveBytes,
		"removals": items, "error": run.Error, "last_run": state.Ran,
		"held": held, "approved": cmp.Or(state.Approved, row.Approved),
	}
}

func (r *Runtime) retentionRow(name string) (RetentionRow, error) {
	rows := r.retention().rows
	if i := slices.IndexFunc(rows, func(o RetentionRow) bool { return o.Name == name }); i >= 0 {
		return rows[i], nil
	}
	return RetentionRow{}, &NotFoundError{Noun: "retention-rule", ID: name}
}

var retentionProps = []PropSpec{
	{Name: "name", Key: true, Parameter: Parameter{Description: "The row's name, as the program declared it"}},
	{Name: "description", Role: RoleLabel, ReadOnly: true, Parameter: Parameter{Description: "What the row keeps"}},
	{Name: "kind", ReadOnly: true, Parameter: Parameter{Enum: []string{"files", "records", "store"}, Description: "files: globs of files; records: a noun's things; store: entries the program's own store removes by age (a table's old rows)"}},
	{Name: "paths", ReadOnly: true, Parameter: Parameter{Type: SettingList, Description: "A file row's globs ({data}, {state}, {config}, {home} expand)"}},
	{Name: "noun", ReadOnly: true, Parameter: Parameter{Description: "A record row's noun"}},
	{Name: "where", ReadOnly: true, Parameter: Parameter{Description: "A record row's condition (the list verb's where)"}},
	{Name: "verb", ReadOnly: true, Parameter: Parameter{Description: "A record row's verb, run on each old thing"}},
	{Name: "keep", Parameter: Parameter{Type: SettingInteger, Description: "Keep at most this many newest copies (0: no count limit); writes the row's keep setting"}},
	{Name: "days", Parameter: Parameter{Type: SettingInteger, Description: "Remove what is older than this many days (0: no age limit); writes the row's days setting"}},
	{Name: "min_keep", ReadOnly: true, Parameter: Parameter{Type: SettingInteger, Description: "Copies per group never removed"}},
	{Name: "keep_setting", ReadOnly: true, Parameter: Parameter{Description: "The setting keep reads"}},
	{Name: "days_setting", ReadOnly: true, Parameter: Parameter{Description: "The setting days reads"}},
	{Name: "count", ReadOnly: true, Parameter: Parameter{Type: SettingInteger, Description: "Copies or things the row matches now"}},
	{Name: "bytes", ReadOnly: true, Parameter: Parameter{Type: SettingInteger, Description: "Bytes the row's files hold now"}},
	{Name: "would_remove", ReadOnly: true, Parameter: Parameter{Type: SettingInteger, Description: "Copies or things the row would remove now (the dry run)"}},
	{Name: "would_free", ReadOnly: true, Parameter: Parameter{Type: SettingInteger, Description: "Bytes the row would free now"}},
	{Name: "removals", ReadOnly: true, Parameter: Parameter{Type: SettingList, Description: "What the row would remove now, a line each with its reason (apply --dry-run answers them as data)"}},
	{Name: "error", ReadOnly: true, Parameter: Parameter{Description: "Why the row cannot run here, when it cannot"}},
	{Name: "last_run", ReadOnly: true, Parameter: Parameter{Description: "The UTC day the row last ran automatically"}},
	{Name: "held", ReadOnly: true, Parameter: Parameter{Description: "A first large removal an automatic run held for you (retention.first-run-ask), until the row is applied by hand"}},
	{Name: "approved", ReadOnly: true, Parameter: Parameter{Description: "When the row was first applied by hand, or the owner's decision the program declared it with; from then on automatic runs remove what it says"}},
}

// registerRetention declares the retention-rule noun: a collection of the
// declared rows (get, list, show and set are the standard verbs; set of
// keep or days writes the row's settings through setting.set), plus apply
// and log.
func registerRetention(r *Runtime) {
	r.Define(RetentionNoun).About("The program's retention rules: what each keeps of its backups, history and old things, and what it would remove now").DefaultKeys(false).Props(retentionProps...).Store(Store{
		Kind: KindCollection,
		Get: func(c Ctx, name string) (Thing, error) {
			row, err := c.Runtime.retentionRow(name)
			if err != nil {
				return nil, err
			}
			return c.Runtime.retentionThing(c.Context, row), nil
		},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			out := []Thing{}
			for _, row := range c.Runtime.retention().rows {
				out = append(out, c.Runtime.retentionThing(c.Context, row))
			}
			return out, nil
		},
		Patch: patchRetention,
	}).
		Verb("apply", AppHandler(runRetentionApply), "Apply the retention rules now: one row (name) or every row; dry-run lists what would go and removes nothing; due runs only the rows not run yet today (the automatic run); due or after-write holds the first large removal of a row nobody applied by hand yet (retention.first-run-ask), and a run by hand marks the row applied. Files go through the remover, things through the row's own verb under the caller's rights; every removal is a line of the retention log").
		Params(map[string]Parameter{
			"name":        {Type: "string", Noun: RetentionNoun, Description: "The row to apply; every row when omitted"},
			"dry-run":     {Type: "boolean", Description: "List what would be removed; remove nothing"},
			"due":         {Type: "boolean", Description: "Only the rows not run yet this UTC day, and record that they ran (an automatic run)"},
			"after-write": {Type: "boolean", Description: "Run as the automatic run does, without the day check: a program pruning after its own write (a backup); a first large removal is held for the owner"},
		}).Positional("name").History(HistoryChange).
		Verb("log", AppHandler(runRetentionLog), "The retention log: one line per removal (time, row, path or thing, bytes, reason, who), newest first").
		Params(map[string]Parameter{
			"name":  {Type: "string", Noun: RetentionNoun, Description: "Only this row's lines"},
			"limit": {Type: "integer", Description: "At most this many lines (0: all)"},
		}).Positional("name").Fetch()
}

// patchRetention writes keep and days through setting.set (one request
// each, the row's own setting names).
func patchRetention(c Ctx, name string, patch Thing) (Thing, error) {
	r := c.Runtime
	row, err := r.retentionRow(name)
	if err != nil {
		return nil, err
	}
	for prop, setting := range map[string]string{"keep": row.KeepSetting, "days": row.DaysSetting} {
		v, ok := patch[prop]
		if !ok {
			continue
		}
		if _, err := r.ExecuteAt(c.Context, Request{Noun: "ochazuke.setting", Verb: "set", Args: Arguments{"name": setting, "value": fmt.Sprint(v)}}); err != nil {
			return nil, err
		}
	}
	return r.retentionThing(c.Context, row), nil
}

func runRetentionApply(c Ctx) Result {
	r := c.Runtime
	rows := r.retention().rows
	if name := StringArg(c.Args, "name", ""); name != "" {
		rows = slices.DeleteFunc(slices.Clone(rows), func(o RetentionRow) bool { return o.Name != name })
		if len(rows) == 0 {
			return Nothing(CauseEmpty, "no retention row "+name+" here (retention-rule list shows them)")
		}
	}
	due := boolArg(c.Args, "due")
	auto := due || boolArg(c.Args, "after-write")
	dry := boolArg(c.Args, "dry-run")
	day := r.retention().clock().UTC().Format(time.DateOnly)
	states := map[string]retentionState{}
	if due {
		states = r.retentionStates()
	}
	runs := []RetentionRun{}
	ranNow := []string{}
	for _, row := range rows {
		if due && states[row.Name].Ran == day {
			continue
		}
		if dry {
			runs = append(runs, r.planRetention(c.Context, row))
			continue
		}
		run := r.applyRetention(c.Context, row, auto)
		if run.Held {
			r.UI().Notify(retentionHeldText(row.Name, RetentionHeld{Count: run.Remove, Bytes: run.RemoveBytes}))
		}
		runs = append(runs, run)
		ranNow = append(ranNow, row.Name)
	}
	if due && len(ranNow) > 0 {
		r.markRetentionRan(ranNow, day)
	}
	if len(runs) == 0 {
		return Nothing(CauseEmpty, "no retention row is due")
	}
	return Done(runs)
}

func runRetentionLog(c Ctx) Result {
	r := c.Runtime
	path := r.retentionPath(RetentionLogFile)
	if path == "" {
		return Done([]retain.Entry{})
	}
	name := StringArg(c.Args, "name", "")
	limit := IntArg(c.Args, "limit", 0)
	read := limit
	if name != "" {
		read = 0
	}
	entries, err := retain.ReadLog(path, read)
	if err != nil {
		return Failed(err)
	}
	if name != "" {
		entries = slices.DeleteFunc(entries, func(e retain.Entry) bool { return e.Rule != name })
		if limit > 0 && len(entries) > limit {
			entries = entries[:limit]
		}
	}
	return Done(entries)
}
