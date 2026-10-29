package ochazuke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Status segments are live items in the header rule (the rule under the
// tabs): a CPU reading, free disk, a build state, anything that is one
// short line. A segment is data, never a branch per segment: an id, a
// label and format, one source, a refresh interval, a tone and an order.
// Sources:
//
//   - Provider: a Go function the application registered by name
//     (Runtime.SegmentProvider; sysstat registers cpu, ram and disk).
//   - Command: an argv run every Every, its first line shown, killed
//     after Timeout. Only the application's Go code and the user's own
//     segments.json may name a command, never a catalog (as for actions).
//   - Verb: a registered NOUN.VERB whose answer is the text: an
//     extension's catalog row names one of its verbs, and the host mounts
//     it against the verb it mounted (cli.CatalogSegments).
//
// Every refresh runs off the loop (a tea.Cmd, as a job's work does) with
// its own timeout, one run per segment at a time, so a slow source never
// blocks a key; the header keeps the last text until the next answer.
// Refreshes are not jobs: they would fill jobs.list and the history every
// few seconds.
//
// Opting out: the status.segments setting hides the whole area (and stops
// every refresh), status.segments.<id> each segment (its default is the
// row's Enabled), and status.segment-order holds the order segment.move
// writes. All three are ordinary settings: the Settings page, settings set
// and settings.json reach them.

// The segment settings.
const (
	// SegmentsSetting shows (true, the default) or hides the whole
	// segments area.
	SegmentsSetting = "status.segments"
	// SegmentOrderSetting is the order the user gave the segments
	// (segment.move): ids first to last; segments it leaves out follow in
	// their rows' order.
	SegmentOrderSetting = "status.segment-order"
	// SegmentsFile is the user's segments file in the config directory:
	// a JSON array of Segment rows. A row naming an existing id changes
	// that segment's fields; a new id adds a segment.
	SegmentsFile = "segments.json"
)

// SegmentSetting is the setting that shows or hides one segment.
func SegmentSetting(id string) string { return SegmentsSetting + "." + id }

// Segment defaults and bounds.
const (
	DefaultSegmentEvery   = 5 * time.Second
	MinSegmentEvery       = 250 * time.Millisecond
	DefaultSegmentTimeout = 2 * time.Second
	// SegmentMaxWidth is the most cells one segment's text takes.
	SegmentMaxWidth = 32
)

// Segment is one status segment row. Format is a template over {{label}}
// and {{value}} (default "{{label}} {{value}}", or "{{value}}" without a
// label). Exactly one of Provider, Command and Verb is the source; Args
// are the verb's arguments. Every is the refresh interval (default
// DefaultSegmentEvery, at least MinSegmentEvery) and Timeout the most one
// refresh may take (default DefaultSegmentTimeout, at most Every). Tone is
// a named tone (tone.go) the text draws in, unless the answer brings its
// own (SegmentValue.Tone); none draws muted like the rule. Order sorts
// the rows (lower first, then the order they were added) before
// status.segment-order. Enabled is the default of the segment's setting
// (nil: on). Path is the provider's path (disk free reads it).
//
// In segments.json the durations are strings ("2s"), as settings.json
// writes them.
type Segment struct {
	ID       string        `json:"id"`
	Label    string        `json:"label,omitempty"`
	Format   string        `json:"format,omitempty"`
	Provider string        `json:"provider,omitempty"`
	Command  Argv          `json:"command,omitempty"`
	Verb     string        `json:"verb,omitempty"`
	Args     Arguments     `json:"args,omitempty"`
	Every    time.Duration `json:"-"`
	Timeout  time.Duration `json:"-"`
	Tone     string        `json:"tone,omitempty"`
	Order    int           `json:"order,omitempty"`
	Enabled  *bool         `json:"enabled,omitempty"`
	Path     string        `json:"path,omitempty"`
	// Description is the line segment list and the segment's setting show.
	Description string `json:"description,omitempty"`
}

type segmentJSON struct {
	ID          string    `json:"id"`
	Label       string    `json:"label,omitempty"`
	Format      string    `json:"format,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Command     Argv      `json:"command,omitempty"`
	Verb        string    `json:"verb,omitempty"`
	Args        Arguments `json:"args,omitempty"`
	Every       string    `json:"every,omitempty"`
	Timeout     string    `json:"timeout,omitempty"`
	Tone        string    `json:"tone,omitempty"`
	Order       int       `json:"order,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
	Path        string    `json:"path,omitempty"`
	Description string    `json:"description,omitempty"`
}

// MarshalJSON writes the durations as strings ("2s").
func (s Segment) MarshalJSON() ([]byte, error) {
	j := segmentJSON{ID: s.ID, Label: s.Label, Format: s.Format, Provider: s.Provider, Command: s.Command, Verb: s.Verb, Args: s.Args, Tone: s.Tone, Order: s.Order, Enabled: s.Enabled, Path: s.Path, Description: s.Description}
	if s.Every != 0 {
		j.Every = s.Every.String()
	}
	if s.Timeout != 0 {
		j.Timeout = s.Timeout.String()
	}
	return json.Marshal(j)
}

// UnmarshalJSON reads the durations as strings ("2s", "500ms"); an
// unknown field is an error.
func (s *Segment) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var j segmentJSON
	if err := dec.Decode(&j); err != nil {
		return err
	}
	out := Segment{ID: j.ID, Label: j.Label, Format: j.Format, Provider: j.Provider, Command: j.Command, Verb: j.Verb, Args: j.Args, Tone: j.Tone, Order: j.Order, Enabled: j.Enabled, Path: j.Path, Description: j.Description}
	for _, d := range []struct {
		name, text string
		into       *time.Duration
	}{{"every", j.Every, &out.Every}, {"timeout", j.Timeout, &out.Timeout}} {
		if d.text == "" {
			continue
		}
		v, err := time.ParseDuration(d.text)
		if err != nil || v < 0 {
			return fmt.Errorf("%s %q is not a duration such as 2s", d.name, d.text)
		}
		*d.into = v
	}
	*s = out
	return nil
}

// On returns a pointer to b, for Segment.Enabled.
func On(b bool) *bool { return &b }

// SegmentValue is one answer of a source: the text and, optionally, the
// tone it draws in (a disk nearly full answers state.errored).
type SegmentValue struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// SegmentProvider is a Go source: it returns the segment's value, off the
// loop, within ctx's deadline (the segment's Timeout).
type SegmentProvider func(ctx context.Context, s Segment) (SegmentValue, error)

// SegmentState is a segment as segment.list and segment.show answer: the
// row as merged, where it came from, whether it shows, its position and
// its last answer.
type SegmentState struct {
	Segment
	Source   string    `json:"source"` // provider, command or verb
	From     string    `json:"from"`   // app, or segments.json
	Shown    bool      `json:"shown"`  // its setting is on (and the area is)
	Position int       `json:"position"`
	Text     string    `json:"text,omitempty"`
	Error    string    `json:"error,omitempty"`
	At       time.Time `json:"at,omitzero"`
}

// segmentRow is one registered segment with its run state.
type segmentRow struct {
	seq      int
	base     Segment  // the application's row (AddSegment, merged)
	patch    *Segment // the user's segments.json row for this id, nil for none
	user     bool     // the row exists only in segments.json
	value    SegmentValue
	err      string
	at       time.Time // the last answer
	started  time.Time // the last refresh started
	inflight bool
	due      bool // refresh at the next chance (segment.refresh)
}

// segments is the runtime's segment registry.
type segments struct {
	rows      []*segmentRow
	providers map[string]SegmentProvider
	seq       int
	loaded    bool // segments.json was read
	problems  []string
	declared  bool // the area's settings are declared
}

// segs is the registry, kept with the config component beside the
// settings that show each segment.
func (r *Runtime) segs() *segments {
	c := r.Config()
	if c.segments == nil {
		c.segments = &segments{providers: map[string]SegmentProvider{}}
	}
	return c.segments
}

func (g *segments) find(id string) *segmentRow {
	for _, row := range g.rows {
		if row.base.ID == id {
			return row
		}
	}
	return nil
}

// SegmentProvider registers a Go source under name; a segment names it in
// its Provider field. A later registration of the name replaces it.
func (r *Runtime) SegmentProvider(name string, fn SegmentProvider) {
	r.segs().providers[name] = fn
}

// AddSegment adds status segments. A row whose id is already registered
// changes that segment: its non-zero fields replace the old ones, and a
// row naming a source replaces the old source, so an application turns a
// library row on with AddSegment(Segment{ID: "cpu", Enabled: On(true)}).
// Each segment's setting (SegmentSetting) is declared with the row's
// Enabled as its default.
func (r *Runtime) AddSegment(rows ...Segment) error {
	g := r.segs()
	for _, s := range rows {
		if err := checkSegmentID(s.ID); err != nil {
			return err
		}
		if row := g.find(s.ID); row != nil {
			row.base = mergeSegment(row.base, s)
			row.user = false
		} else {
			g.seq++
			g.rows = append(g.rows, &segmentRow{seq: g.seq, base: s})
		}
		r.declareSegmentSetting(g.find(s.ID))
	}
	return nil
}

func checkSegmentID(id string) error {
	if id == "" || strings.ContainsAny(id, " \t\n.") {
		return fmt.Errorf("segment id %q must be one word without dots", id)
	}
	return nil
}

// mergeSegment is base with patch's set fields over it; a patch naming a
// source replaces the source.
func mergeSegment(base, patch Segment) Segment {
	out := base
	if patch.Provider != "" || len(patch.Command) > 0 || patch.Verb != "" {
		out.Provider, out.Command, out.Verb, out.Args = patch.Provider, slices.Clone(patch.Command), patch.Verb, patch.Args
	}
	if patch.Args != nil {
		out.Args = patch.Args
	}
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&out.Label, patch.Label)
	set(&out.Format, patch.Format)
	set(&out.Tone, patch.Tone)
	set(&out.Path, patch.Path)
	set(&out.Description, patch.Description)
	if patch.Every != 0 {
		out.Every = patch.Every
	}
	if patch.Timeout != 0 {
		out.Timeout = patch.Timeout
	}
	if patch.Order != 0 {
		out.Order = patch.Order
	}
	if patch.Enabled != nil {
		out.Enabled = patch.Enabled
	}
	return out
}

// row is the segment as it runs: the application's row with the user's
// patch over it.
func (s *segmentRow) row() Segment {
	if s.patch == nil {
		return s.base
	}
	return mergeSegment(s.base, *s.patch)
}

// every and timeout are the row's refresh interval and run bound.
func (s Segment) every() time.Duration {
	if s.Every <= 0 {
		return DefaultSegmentEvery
	}
	return max(s.Every, MinSegmentEvery)
}

func (s Segment) timeout() time.Duration {
	t := s.Timeout
	if t <= 0 {
		t = DefaultSegmentTimeout
	}
	return min(t, s.every())
}

// sourceKind names the row's source, "" when it has none.
func (s Segment) sourceKind() string {
	switch {
	case s.Provider != "":
		return "provider"
	case len(s.Command) > 0:
		return "command"
	case s.Verb != "":
		return "verb"
	}
	return ""
}

// check reports why the merged row cannot run.
func (r *Runtime) checkSegment(s Segment) error {
	n := 0
	for _, set := range []bool{s.Provider != "", len(s.Command) > 0, s.Verb != ""} {
		if set {
			n++
		}
	}
	switch {
	case n == 0:
		return errors.New("needs a provider, a command or a verb")
	case n > 1:
		return errors.New("names more than one of provider, command and verb")
	case s.Provider != "":
		if r.segs().providers[s.Provider] == nil {
			return fmt.Errorf("no provider %q is registered", s.Provider)
		}
	case s.Verb != "":
		if _, ok := r.Operation(s.Verb); !ok {
			return fmt.Errorf("unknown verb %q", s.Verb)
		}
	}
	return nil
}

// declareSegmentSetting declares (or updates the default of) a segment's
// setting.
func (r *Runtime) declareSegmentSetting(row *segmentRow) {
	s := r.Settings()
	if s == nil {
		return
	}
	seg := row.row()
	def := seg.Enabled == nil || *seg.Enabled
	desc := seg.Description
	if desc == "" {
		label := seg.Label
		if label == "" {
			label = seg.ID
		}
		desc = "Show the " + label + " segment in the header rule"
	}
	name := SegmentSetting(seg.ID)
	if one := s.find(name); one != nil {
		old := s.current(*one)
		one.Default, one.Description = def, desc
		s.changed(name, old)
		return
	}
	_ = r.DeclareSetting(Setting{Name: name, Type: SettingBoolean, Default: def, Description: desc})
}

// EnsureSegments declares the segments area's settings and reads the
// user's segments.json, once. EnsureConfigSettings calls it (Model.Init
// and the CLI shell), so settings set reaches every segment.
func (r *Runtime) EnsureSegments() []string {
	g := r.segs()
	if !g.declared {
		g.declared = true
	}
	if g.loaded {
		return nil
	}
	return r.ReloadUserSegments()
}

// ReloadUserSegments reads segments.json again: the rows it held before
// are taken back (a user-only segment goes, a patched one keeps the
// application's row), then each valid row patches or adds its segment.
// A row that does not validate is skipped and reported; a missing file is
// no rows and no problem.
func (r *Runtime) ReloadUserSegments() []string {
	g := r.segs()
	c := r.Config()
	g.loaded = true
	if c != nil && c.Withdraw != nil {
		for _, p := range g.problems {
			c.Withdraw(p)
		}
	}
	g.problems = nil
	g.rows = slices.DeleteFunc(g.rows, func(s *segmentRow) bool { return s.user })
	for _, s := range g.rows {
		s.patch = nil
	}
	defer func() {
		for _, s := range g.rows {
			r.declareSegmentSetting(s)
		}
	}()
	if c == nil {
		return nil
	}
	path := c.Path(SegmentsFile)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	problem := func(p string) {
		g.problems = append(g.problems, p)
		c.report(p)
	}
	if err != nil {
		problem(SegmentsFile + ": " + err.Error())
		return slices.Clone(g.problems)
	}
	var raw []json.RawMessage
	if err := DecodeJSONC(data, &raw); err != nil {
		problem(SegmentsFile + ": not a JSON array of segments: " + err.Error())
		return slices.Clone(g.problems)
	}
	for i, one := range raw {
		var s Segment
		if err := json.Unmarshal(one, &s); err != nil {
			problem(fmt.Sprintf("%s: segment %d: %v (skipped)", SegmentsFile, i+1, err))
			continue
		}
		if err := checkSegmentID(s.ID); err != nil {
			problem(fmt.Sprintf("%s: segment %d: %v (skipped)", SegmentsFile, i+1, err))
			continue
		}
		if row := g.find(s.ID); row != nil {
			merged := mergeSegment(row.base, s)
			if err := r.checkSegment(merged); err != nil {
				problem(fmt.Sprintf("%s: segment %s: %v (skipped)", SegmentsFile, s.ID, err))
				continue
			}
			patch := s
			row.patch = &patch
			continue
		}
		if err := r.checkSegment(s); err != nil {
			problem(fmt.Sprintf("%s: segment %s: %v (skipped)", SegmentsFile, s.ID, err))
			continue
		}
		g.seq++
		g.rows = append(g.rows, &segmentRow{seq: g.seq, base: s, user: true})
	}
	return slices.Clone(g.problems)
}

// SegmentsShown reports whether the segments area is on (its setting
// undeclared reads on).
func (r *Runtime) SegmentsShown() bool {
	v, ok := r.Settings().Value(SegmentsSetting)
	b, isBool := v.(bool)
	return !ok || !isBool || b
}

// segmentOn reports whether one segment's setting is on.
func (r *Runtime) segmentOn(row *segmentRow) bool {
	v, ok := r.Settings().Value(SegmentSetting(row.base.ID))
	if b, isBool := v.(bool); ok && isBool {
		return b
	}
	seg := row.row()
	return seg.Enabled == nil || *seg.Enabled
}

// ordered is every segment row in drawing order: the ids the order
// setting names first, in its order, then the rest by Order and then by
// when they were added.
func (r *Runtime) ordered() []*segmentRow {
	rows := slices.Clone(r.segs().rows)
	slices.SortStableFunc(rows, func(a, b *segmentRow) int {
		if d := a.row().Order - b.row().Order; d != 0 {
			return d
		}
		return a.seq - b.seq
	})
	order := r.Settings().Strings(SegmentOrderSetting)
	rank := func(s *segmentRow) int {
		if i := slices.Index(order, s.base.ID); i >= 0 {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(rows, func(a, b *segmentRow) int { return rank(a) - rank(b) })
	return rows
}

// active are the rows that show and refresh: the area on, the segment's
// setting on and its row valid, in drawing order.
func (r *Runtime) active() []*segmentRow {
	if len(r.segs().rows) == 0 || !r.SegmentsShown() {
		return nil
	}
	var out []*segmentRow
	for _, row := range r.ordered() {
		if r.segmentOn(row) && r.checkSegment(row.row()) == nil {
			out = append(out, row)
		}
	}
	return out
}

// Segments lists every segment in drawing order, with whether it shows
// and its last answer.
func (r *Runtime) Segments() []SegmentState {
	area := r.SegmentsShown()
	out := []SegmentState{}
	for i, row := range r.ordered() {
		seg := row.row()
		from := "app"
		if row.user {
			from = SegmentsFile
		} else if row.patch != nil {
			from = "app+" + SegmentsFile
		}
		st := SegmentState{Segment: seg, Source: seg.sourceKind(), From: from, Shown: area && r.segmentOn(row), Position: i + 1, Text: row.value.Text, Error: row.err, At: row.at}
		if err := r.checkSegment(seg); err != nil && st.Error == "" {
			st.Error = err.Error()
		}
		out = append(out, st)
	}
	return out
}

// segmentText is the drawn text of a row's last answer: the format with
// {{label}} and {{value}}, one line, at most SegmentMaxWidth cells.
func segmentText(seg Segment, v SegmentValue) string {
	format := seg.Format
	if format == "" {
		format = "{{value}}"
		if seg.Label != "" {
			format = "{{label}} {{value}}"
		}
	}
	text := expand(format, func(name string) string {
		switch name {
		case "label":
			return seg.Label
		case "value":
			return firstLine(v.Text)
		}
		return ""
	})
	return ansi.Truncate(safeLine(strings.TrimSpace(text)), SegmentMaxWidth, "…")
}

func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// SegmentCell is one drawn segment: its text and tone ("" muted).
type SegmentCell struct {
	ID   string
	Text string
	Tone string
}

// SegmentCells are the segments the header rule draws now: the active
// rows that have answered, in order. A failed refresh keeps the last text
// and draws it in state.errored; one that never answered draws its label
// and "!" in state.errored.
func (r *Runtime) SegmentCells() []SegmentCell {
	var out []SegmentCell
	for _, row := range r.active() {
		seg := row.row()
		switch {
		case row.err != "" && row.value.Text == "":
			label := seg.Label
			if label == "" {
				label = seg.ID
			}
			out = append(out, SegmentCell{ID: seg.ID, Text: label + " !", Tone: ToneErrored})
		case row.at.IsZero():
			continue
		default:
			tone := row.value.Tone
			if tone == "" {
				tone = seg.Tone
			}
			if row.err != "" {
				tone = ToneErrored
			}
			if text := segmentText(seg, row.value); text != "" {
				out = append(out, SegmentCell{ID: seg.ID, Text: text, Tone: tone})
			}
		}
	}
	return out
}

// segmentValueOf turns a source's answer into a value: a SegmentValue, a
// string, JSON (a string, or an object with text and tone), else its
// printed form.
func segmentValueOf(v any) SegmentValue {
	switch t := v.(type) {
	case nil:
		return SegmentValue{}
	case SegmentValue:
		return t
	case string:
		return segmentValueOfText(t)
	case []byte:
		return segmentValueOfText(string(t))
	case json.RawMessage:
		return segmentValueOfText(string(t))
	case fmt.Stringer:
		return SegmentValue{Text: t.String()}
	}
	if b, err := json.Marshal(v); err == nil {
		return segmentValueOfText(string(b))
	}
	return SegmentValue{Text: fmt.Sprint(v)}
}

// segmentValueOfText reads a text answer: a JSON string or {text, tone}
// object on its first line, else the first line itself.
func segmentValueOfText(s string) SegmentValue {
	line := firstLine(s)
	if strings.HasPrefix(line, "{") || strings.HasPrefix(line, "\"") {
		var v SegmentValue
		if json.Unmarshal([]byte(line), &v) == nil && strings.HasPrefix(line, "{") {
			return v
		}
		var str string
		if json.Unmarshal([]byte(line), &str) == nil {
			return SegmentValue{Text: str}
		}
	}
	return SegmentValue{Text: line}
}

// segmentRun is one refresh: the work, run off the loop, and the row it
// answers.
type segmentRun struct {
	id    string
	work  func(context.Context) (any, error)
	bound time.Duration
}

// SegmentMsg is a refresh's answer, delivered on the loop.
type SegmentMsg struct {
	ID    string
	Value SegmentValue
	Err   error
}

// startSegment prepares one refresh of row: a provider or command runs
// entirely off the loop; a verb's handler runs here, on the loop, and
// only its pending work (an extension's process) runs off it. The verb
// runs outside Execute, so a refresh never enters the history.
func (r *Runtime) startSegment(row *segmentRow, now time.Time) *segmentRun {
	seg := row.row()
	row.inflight, row.due, row.started = true, false, now
	run := &segmentRun{id: seg.ID, bound: seg.timeout()}
	switch seg.sourceKind() {
	case "provider":
		fn := r.segs().providers[seg.Provider]
		run.work = func(ctx context.Context) (any, error) { return fn(ctx, seg) }
	case "command":
		argv := slices.Clone(seg.Command)
		env := append(os.Environ(), "OCHAZUKE_SEGMENT="+seg.ID)
		bound := run.bound
		run.work = func(ctx context.Context) (any, error) { return runAction(ctx, argv, "", env, bound) }
	case "verb":
		value, work, err := r.segmentVerb(seg)
		run.work = func(ctx context.Context) (any, error) {
			if err != nil || work == nil {
				return value, err
			}
			return work(ctx)
		}
	}
	return run
}

// segmentVerb runs a verb source's handler on the loop: its value, or the
// pending work to run off it.
func (r *Runtime) segmentVerb(seg Segment) (any, func(context.Context) (any, error), error) {
	op, ok := r.Operation(seg.Verb)
	if !ok {
		return nil, nil, fmt.Errorf("unknown verb %q", seg.Verb)
	}
	args := Arguments{}
	for k, v := range seg.Args {
		args[k] = v
	}
	if err := r.checkArguments(op, args); err != nil {
		return nil, nil, err
	}
	c := r.contextOf("app")
	if op.App != nil {
		res := op.App(Ctx{Runtime: r, Context: c, Args: args, Instance: r.Instances["app"]})
		return res.value, res.work, res.err
	}
	if op.Run == nil {
		return nil, nil, fmt.Errorf("%s has no handler", seg.Verb)
	}
	v, err := op.Run(r, c, args)
	return v, nil, err
}

// do runs the work under the bound and answers its message.
func (run *segmentRun) do() SegmentMsg {
	ctx, cancel := context.WithTimeout(context.Background(), run.bound)
	defer cancel()
	type answer struct {
		v   any
		err error
	}
	done := make(chan answer, 1)
	go func() {
		v, err := run.work(ctx)
		done <- answer{v, err}
	}()
	select {
	case a := <-done:
		if a.err != nil {
			return SegmentMsg{ID: run.id, Err: a.err}
		}
		return SegmentMsg{ID: run.id, Value: segmentValueOf(a.v)}
	case <-ctx.Done():
		return SegmentMsg{ID: run.id, Err: fmt.Errorf("timed out after %s", run.bound)}
	}
}

// settleSegment records an answer on the loop.
func (r *Runtime) settleSegment(msg SegmentMsg, now time.Time) {
	row := r.segs().find(msg.ID)
	if row == nil {
		return
	}
	row.inflight = false
	row.at = now
	if msg.Err != nil {
		row.err = msg.Err.Error()
		return
	}
	row.value, row.err = msg.Value, ""
}

// segmentMove is the order after moving id to a position: "first",
// "last", "up", "down", "+N"/"-N" (that many places) or N (1-based).
func segmentMove(order []string, id, to string) ([]string, error) {
	i := slices.Index(order, id)
	if i < 0 {
		return nil, fmt.Errorf("unknown segment %q", id)
	}
	j := i
	switch to {
	case "first":
		j = 0
	case "last":
		j = len(order) - 1
	case "up":
		j = i - 1
	case "down":
		j = i + 1
	default:
		n, err := strconv.Atoi(to)
		switch {
		case err != nil:
			return nil, fmt.Errorf("to %q is not first, last, up, down, +N, -N or a position", to)
		case strings.HasPrefix(to, "+") || strings.HasPrefix(to, "-"):
			j = i + n
		default:
			j = n - 1
		}
	}
	j = min(max(j, 0), len(order)-1)
	out := slices.Delete(slices.Clone(order), i, i+1)
	return slices.Insert(out, j, id), nil
}

func registerSegments(r *Runtime) {
	id := Parameter{Type: "string", Required: true, Description: "Segment id", Source: "ochazuke.segment.names"}
	find := func(r *Runtime, a Arguments) (SegmentState, error) {
		want := StringArg(a, "id", "")
		for _, s := range r.Segments() {
			if s.ID == want {
				return s, nil
			}
		}
		return SegmentState{}, fmt.Errorf("unknown segment %q", want)
	}
	set := func(on bool) Handler {
		return func(r *Runtime, c Context, a Arguments) (any, error) {
			r.EnsureSegments()
			s, err := find(r, a)
			if err != nil {
				return nil, err
			}
			if err := r.Settings().Set(SegmentSetting(s.ID), on); err != nil {
				return nil, err
			}
			return find(r, a)
		}
	}
	r.Register(Operation{Verb: "names", Noun: "ochazuke.segment", Description: "List the status segment ids in drawing order", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names := []string{}
		for _, s := range r.Segments() {
			names = append(names, s.ID)
		}
		return names, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "ochazuke.segment", Description: "List the status segments in drawing order: source, whether each shows, its last text or error", Result: "segment[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.EnsureSegments()
		return r.Segments(), nil
	}})
	r.Register(Operation{Verb: "show", Noun: "ochazuke.segment", Description: "Show one status segment: its row, source, setting and last answer", Parameters: map[string]Parameter{"id": id}, Positional: []string{"id"}, Result: "segment", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.EnsureSegments()
		return find(r, a)
	}})
	r.Register(Operation{Verb: "enable", Noun: "ochazuke.segment", Description: "Show a status segment (its status.segments.<id> setting on)", Parameters: map[string]Parameter{"id": id}, Positional: []string{"id"}, Result: "segment", Run: set(true)})
	r.Register(Operation{Verb: "disable", Noun: "ochazuke.segment", Description: "Hide a status segment and stop its refresh (its status.segments.<id> setting off)", Parameters: map[string]Parameter{"id": id}, Positional: []string{"id"}, Result: "segment", Run: set(false)})
	r.Register(Operation{Verb: "toggle", Noun: "ochazuke.segment", Description: "Show a hidden status segment or hide a shown one", Parameters: map[string]Parameter{"id": id}, Positional: []string{"id"}, Result: "segment", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.EnsureSegments()
		s, err := find(r, a)
		if err != nil {
			return nil, err
		}
		row := r.segs().find(s.ID)
		return set(!r.segmentOn(row))(r, c, a)
	}})
	r.Register(Operation{Verb: "move", Noun: "ochazuke.segment", Description: "Move a status segment: to first, last, up, down, +N, -N or a position; saved in status.segment-order", Parameters: map[string]Parameter{"id": id, "to": {Type: "string", Required: true, Description: "first, last, up, down, +N, -N or a 1-based position"}}, Positional: []string{"id", "to"}, Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.EnsureSegments()
		var order []string
		for _, s := range r.Segments() {
			order = append(order, s.ID)
		}
		out, err := segmentMove(order, StringArg(a, "id", ""), StringArg(a, "to", ""))
		if err != nil {
			return nil, err
		}
		if err := r.Settings().Set(SegmentOrderSetting, out); err != nil {
			return nil, err
		}
		return out, nil
	}})
	r.Register(Operation{Verb: "refresh", Noun: "ochazuke.segment", Description: "Refresh a status segment now, or every shown one with no id", Parameters: map[string]Parameter{"id": {Type: "string", Description: "Segment id (default: every shown segment)", Source: "ochazuke.segment.names"}}, Positional: []string{"id"}, Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		want := StringArg(a, "id", "")
		out := []string{}
		for _, row := range r.segs().rows {
			if want == "" || row.base.ID == want {
				row.due = true
				out = append(out, row.base.ID)
			}
		}
		if want != "" && len(out) == 0 {
			return nil, fmt.Errorf("unknown segment %q", want)
		}
		return out, nil
	}})
}
