package ochazuke

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
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
// updated prop is older than the row's days. Nothing here names a tool, a
// folder or a noun: every row is the declaring program's data.

const (
	// RetentionNoun is the framework noun of the declared rows.
	RetentionNoun = "ochazuke.retention-rule"
	// RetentionAutoSetting turns the automatic runs (at start, the daily
	// tick, RetentionDue) on or off; the verbs always work.
	RetentionAutoSetting = "retention.auto"
	// RetentionStateFile holds each row's last run day; RetentionLogFile
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
// first by their updated prop, and each thing it removes gets the verb.
// Keep and Days here are the defaults of the row's settings, KeepSetting
// and DaysSetting (default retention.NAME.keep and retention.NAME.days; a
// row may name a setting the program already declares, such as
// backup.keep, so nothing is renamed). Both 0 turn the row off.
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
}

// records reports whether the row rules a noun's things (not files).
func (row RetentionRow) records() bool { return row.Noun != "" }

// kind is the row's kind as its thing shows it.
func (row RetentionRow) kind() string {
	if row.records() {
		return "records"
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
	// ran is each row's last run day when there is no state directory.
	ran map[string]string
}

func (r *Runtime) retention() *retention {
	c := r.Config()
	if c.retention == nil {
		c.retention = &retention{ran: map[string]string{}}
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
		what := "copies"
		if row.records() {
			what = row.Noun + " things (" + row.Verb + ")"
		}
		if err := r.declareRetentionSetting(row.KeepSetting, row.Keep, "Retention rule "+row.Name+": keep at most this many newest "+what+" (0: no count limit; with days 0 too the rule is off)"); err != nil {
			return err
		}
		if err := r.declareRetentionSetting(row.DaysSetting, row.Days, "Retention rule "+row.Name+": remove "+what+" older than this many days (0: no age limit; with keep 0 too the rule is off)"); err != nil {
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
	Path   string   `json:"path,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	Noun   string   `json:"noun,omitempty"`
	Key    string   `json:"key,omitempty"`
	Label  string   `json:"label,omitempty"`
	Bytes  int64    `json:"bytes,omitempty"`
	Reason string   `json:"reason"`
	Error  string   `json:"error,omitempty"`
}

// line is the item as one line of text: what, how big, why.
func (it RetentionItem) line() string {
	if it.Noun != "" {
		return fmt.Sprintf("%s %s %q: %s", it.Noun, it.Key, it.Label, it.Reason)
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
	RemoveBytes int64  `json:"would_free"`
	Applied     bool   `json:"applied,omitempty"`
	Removed     int    `json:"removed,omitempty"`
	Freed       int64  `json:"freed,omitempty"`
	Error       string `json:"error,omitempty"`
}

// planRetention is what the row removes now.
func (r *Runtime) planRetention(c Context, row RetentionRow) RetentionRun {
	keep, days := r.retentionLimits(row)
	run := RetentionRun{Name: row.Name, Kind: row.kind(), Keep: keep, Days: days, Off: keep == 0 && days == 0, Items: []RetentionItem{}}
	if row.records() {
		return r.planRecords(c, row, run)
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
// removal, done or failed, is one line of the retention log.
func (r *Runtime) applyRetention(c Context, row RetentionRow) RetentionRun {
	run := r.planRetention(c, row)
	if run.Error != "" || run.Off {
		return run
	}
	st := r.retention()
	now := st.clock()
	var entries []retain.Entry
	who, source := r.Actor().String(), retentionSource+":"+row.Name
	for i, item := range run.Items {
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

// retentionRan reads each row's last run day (retention.json, else memory).
func (r *Runtime) retentionRan() map[string]string {
	st := r.retention()
	path := r.retentionPath(RetentionStateFile)
	if path == "" {
		return st.ran
	}
	out := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

// markRetentionRan records the rows' run day (temp file + rename).
func (r *Runtime) markRetentionRan(names []string, day string) {
	ran := r.retentionRan()
	for _, name := range names {
		ran[name] = day
	}
	path := r.retentionPath(RetentionStateFile)
	if path == "" {
		r.retention().ran = ran
		return
	}
	data, _ := json.MarshalIndent(ran, "", "  ")
	if err := errors.Join(os.MkdirAll(filepath.Dir(path), 0o700), writeFileAtomic(path, data)); err != nil {
		r.UI().Notify("retention: " + err.Error())
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
		"removals": items, "error": run.Error, "last_run": r.retentionRan()[row.Name],
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
	{Name: "kind", ReadOnly: true, Parameter: Parameter{Enum: []string{"files", "records"}, Description: "files: globs of files; records: a noun's things"}},
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
		Verb("apply", AppHandler(runRetentionApply), "Apply the retention rules now: one row (name) or every row; dry-run lists what would go and removes nothing; due runs only the rows not run yet today (the automatic run). Files go through the remover, things through the row's own verb under the caller's rights; every removal is a line of the retention log").
		Params(map[string]Parameter{
			"name":    {Type: "string", Noun: RetentionNoun, Description: "The row to apply; every row when omitted"},
			"dry-run": {Type: "boolean", Description: "List what would be removed; remove nothing"},
			"due":     {Type: "boolean", Description: "Only the rows not run yet this UTC day, and record that they ran"},
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
	dry := boolArg(c.Args, "dry-run")
	day := r.retention().clock().UTC().Format(time.DateOnly)
	ran := map[string]string{}
	if due {
		ran = r.retentionRan()
	}
	runs := []RetentionRun{}
	ranNow := []string{}
	for _, row := range rows {
		if due && ran[row.Name] == day {
			continue
		}
		if dry {
			runs = append(runs, r.planRetention(c.Context, row))
			continue
		}
		runs = append(runs, r.applyRetention(c.Context, row))
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
