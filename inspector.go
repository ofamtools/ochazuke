package ochazuke

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const inspectorID = "__inspector"

// InspectorEnabledSetting shows the optional state strip in Model's chrome.
const InspectorEnabledSetting = "inspector.enabled"

// Inspection is a detached, JSON-safe snapshot of live interaction state.
// Text values are bounded to 256 runes; Truncated identifies shortened values.
// Inspect does not run requests, read files or change the focus.
type Inspection struct {
	Mode          string                  `json:"mode"`
	DeclaredMode  string                  `json:"declaredMode"`
	Focus         []InspectFocus          `json:"focus"`
	Registers     []InspectRegister       `json:"registers"`
	YankHead      *InspectValue           `json:"yankHead,omitempty"`
	Message       *InspectMessage         `json:"message,omitempty"`
	LastRequest   *InspectRequest         `json:"lastRequest,omitempty"`
	Selection     int                     `json:"selection"`
	Marks         int                     `json:"marks"`
	Count         int                     `json:"count"`
	OperatorCount int                     `json:"operatorCount"`
	Pending       string                  `json:"pending,omitempty"`
	Modals        []string                `json:"modals"`
	Jobs          []JobInfo               `json:"jobs"`
	FilesWritten  []string                `json:"filesWritten"`
	Fields        map[string]InspectValue `json:"fields"`
}

// InspectMessage retains the last nonempty UI message, even after the
// status line clears. Tone is notice, info or error; Kind keeps the wire kind.
type InspectMessage struct {
	UIMessage
	Tone string `json:"tone"`
}

// InspectFocus is one level of the focus path, outermost first.
type InspectFocus struct {
	Instance string `json:"instance"`
	Noun     string `json:"noun"`
	Row      string `json:"row,omitempty"`
}

// InspectValue is bounded display text. Newlines are retained in JSON and
// escaped in Compact, so user text cannot introduce terminal control codes.
type InspectValue struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// InspectRegister is a named yank or macro register, sorted by name.
type InspectRegister struct {
	Name      string       `json:"name"`
	Value     InspectValue `json:"value"`
	Shape     string       `json:"shape"`
	MacroKeys int          `json:"macroKeys,omitempty"`
}

// InspectRequest describes the most recent nonquiet outer request and its outcome.
// Result is a bounded JSON summary, never a reference to application data.
type InspectRequest struct {
	Key     string                  `json:"key"`
	Args    map[string]InspectValue `json:"args,omitempty"`
	Result  InspectValue            `json:"result"`
	Error   string                  `json:"error,omitempty"`
	Job     uint64                  `json:"job,omitempty"`
	Pending bool                    `json:"pending,omitempty"`
}

// inspectorState is a component; the runtime keeps no inspector fields.
type inspectorState struct {
	last   *InspectRequest
	fields map[string]func() any
	files  []string
}

func (*inspectorState) HandleMessage(tea.Msg) bool { return false }
func (*inspectorState) PendingCommand() tea.Cmd    { return nil }
func (*inspectorState) Close()                     {}

func (r *Runtime) inspector() *inspectorState {
	if inst := r.Instances[inspectorID]; inst != nil {
		s, _ := inst.Component.(*inspectorState)
		return s
	}
	return nil
}

func registerInspector(r *Runtime) {
	r.Instance(inspectorID, Kind("ochazuke.state")).Component = &inspectorState{fields: map[string]func() any{}}
	r.Register(Operation{Noun: "state", Verb: "inspect", Quiet: true, Description: "Inspect live focus, registers, messages and the last request", HistoryKind: HistoryFetch, Result: "inspection", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) { return r.Inspect(), nil }})
	r.Register(Operation{Noun: "state", Verb: "toggle", Quiet: true, Description: "Toggle the live state inspector strip", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		enabled := !r.Settings().Bool(InspectorEnabledSetting)
		_, err := r.Settings().SetSession(InspectorEnabledSetting, enabled)
		return enabled, err
	}})
}

// AddInspectField registers an application field evaluated on the runtime's
// loop. A repeated name replaces it; nil removes it. Callbacks must be quick,
// read-only and must not call Inspect. The snapshot holds only bounded text.
func (r *Runtime) AddInspectField(name string, read func() any) {
	s := r.inspector()
	if read == nil {
		delete(s.fields, name)
		return
	}
	s.fields[name] = read
}

// RecordWrittenFile records a successful write on the runtime's loop.
// The inspector does not infer disk writes from change verbs. Apps and hosts
// that know which files were written can report them here (last 32 paths).
func (r *Runtime) RecordWrittenFile(path string) {
	s := r.inspector()
	if path == "" {
		return
	}
	s.files = slices.DeleteFunc(s.files, func(p string) bool { return p == path })
	s.files = append(s.files, path)
	if len(s.files) > 32 {
		s.files = s.files[len(s.files)-32:]
	}
}

func inspectText(text string) InspectValue {
	r := []rune(text)
	if len(r) > 256 {
		return InspectValue{Text: string(r[:256]) + "…", Truncated: true}
	}
	return InspectValue{Text: text}
}

func inspectInline(text string) string {
	quoted := strconv.QuoteToGraphic(text)
	return strings.ReplaceAll(quoted[1:len(quoted)-1], `\"`, `"`)
}

// inspectPreview bounds traversal before encoding, rather than encoding an
// entire component result and then cutting its text. In particular it never
// calls application MarshalJSON methods while observing a key or a yank.
func inspectPreview(v reflect.Value, depth int, shortened *bool) any {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		if depth < 4 {
			return inspectPreview(v.Elem(), depth+1, shortened)
		}
	case reflect.String:
		text := inspectText(v.String())
		*shortened = *shortened || text.Truncated
		return text.Text
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Array, reflect.Slice, reflect.Map:
		if v.Len() > 8 {
			*shortened = true
			return fmt.Sprintf("<%s: %d items>", v.Type(), v.Len())
		}
		if depth < 4 {
			if v.Kind() == reflect.Map {
				if v.Type().Key().Kind() == reflect.String {
					out := map[string]any{}
					for _, key := range v.MapKeys() {
						out[key.String()] = inspectPreview(v.MapIndex(key), depth+1, shortened)
					}
					return out
				}
			} else {
				out := make([]any, v.Len())
				for i := range out {
					out[i] = inspectPreview(v.Index(i), depth+1, shortened)
				}
				return out
			}
		}
	case reflect.Struct:
		if depth < 4 && v.NumField() <= 8 {
			out := map[string]any{}
			for i := 0; i < v.NumField(); i++ {
				field := v.Type().Field(i)
				if !field.IsExported() {
					continue
				}
				name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
				if name == "-" || (strings.Contains(options, "omitempty") && v.Field(i).IsZero()) {
					continue
				}
				if name == "" {
					name = field.Name
				}
				out[name] = inspectPreview(v.Field(i), depth+1, shortened)
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	*shortened = true
	return fmt.Sprintf("<%s: unavailable>", v.Type())
}

func inspectValue(v any) InspectValue {
	if text, ok := v.(string); ok {
		return inspectText(text)
	}
	shortened := false
	preview := inspectPreview(reflect.ValueOf(v), 0, &shortened)
	b, err := json.Marshal(preview)
	if err != nil {
		return InspectValue{Text: fmt.Sprintf("<%T: unavailable>", v), Truncated: true}
	}
	out := inspectText(string(b))
	out.Truncated = out.Truncated || shortened
	return out
}

func (r *Runtime) inspectRequest(q Request, value any, err error) {
	s := r.inspector()
	if s == nil || r.Operations[q.Key()].Quiet {
		return
	}
	// Motions return component snapshots for their callers, potentially a
	// whole list. Their observable result is the new place, not its rows.
	result := value
	if r.Operations[q.Key()].Motion {
		result = r.place()
	}
	out := &InspectRequest{Key: q.Key(), Result: inspectValue(result), Args: map[string]InspectValue{}}
	for k, v := range q.Args {
		out.Args[k] = inspectValue(v)
	}
	if err != nil {
		out.Error = inspectText(err.Error()).Text
	}
	if ticket, ok := value.(Ticket); ok {
		out.Job, out.Pending = ticket.Job, true
	}
	s.last = out
}

func (r *Runtime) inspectCompletion(done Completion, err error) {
	s := r.inspector()
	if s == nil || s.last == nil || s.last.Job != done.Job {
		return
	}
	s.last.Pending = false
	s.last.Result = inspectValue(done.Value)
	if err != nil {
		s.last.Error = inspectText(err.Error()).Text
	}
}

// Inspect returns the live snapshot on the runtime's event loop. Registers,
// focus levels, args, paths and fields are copied; modifying it is harmless.
func (r *Runtime) Inspect() Inspection {
	s := r.inspector()
	out := Inspection{Mode: r.Mode.Name, DeclaredMode: r.DeclaredMode(r.Focus),
		Focus: []InspectFocus{}, Registers: []InspectRegister{}, Modals: append([]string{}, r.modals...),
		Jobs: r.runningJobs(), FilesWritten: append([]string{}, s.files...), Fields: map[string]InspectValue{},
		Count: r.Mode.Payload.Count, OperatorCount: r.Mode.Payload.OpCount, Pending: r.Mode.Payload.Prefix}
	if out.Pending == "" {
		out.Pending = r.Pending
	}
	path := r.Ancestry(r.Focus)
	for i := len(path) - 1; i >= 0; i-- {
		inst := r.Instances[path[i]]
		f := InspectFocus{Instance: inst.ID, Noun: inst.Noun}
		if r.rows[inst.ID] != "" {
			f.Row = inst.ID
		}
		out.Focus = append(out.Focus, f)
	}
	regs := r.Registers()
	for name, reg := range regs.Named {
		out.Registers = append(out.Registers, InspectRegister{Name: name, Value: inspectText(reg.Yank.Text), Shape: reg.Yank.Shape, MacroKeys: len(reg.Keys)})
	}
	if _, ok := regs.Named[`"`]; !ok {
		out.Registers = append(out.Registers, InspectRegister{Name: `"`, Value: InspectValue{}})
	}
	sort.Slice(out.Registers, func(i, j int) bool { return out.Registers[i].Name < out.Registers[j].Name })
	if len(regs.History) > 0 {
		v := inspectText(regs.History[0].Text)
		out.YankHead = &v
	}
	if msg := r.UI().last; msg != nil {
		tone := msg.Kind
		if tone == UIError {
			tone = "error"
		}
		out.Message = &InspectMessage{UIMessage: *msg, Tone: tone}
	}
	if s.last != nil {
		v := *s.last
		if j := r.jobs[v.Job]; v.Pending && j != nil && j.State != JobRunning {
			v.Pending = false
			v.Error = inspectText(j.Error).Text
		}
		v.Args = map[string]InspectValue{}
		for k, a := range s.last.Args {
			v.Args[k] = a
		}
		out.LastRequest = &v
	}
	for _, id := range r.Ancestry(r.Focus) {
		if l := r.List(id); l != nil {
			out.Marks = len(l.Marked())
			if l.visual {
				out.Selection = len(l.Marked())
			}
			break
		}
	}
	if in := r.Input(r.Focus); in != nil {
		if start, end, ok := in.Selection(); ok {
			out.Selection = end - start
		}
	}
	for name, read := range s.fields {
		out.Fields[name] = inspectValue(read())
	}
	return out
}

// InspectPart is one compact field, with a stable key for change highlighting.
type InspectPart struct{ Key, Text string }

// Parts returns compact fields in display order. Values are quoted to keep
// newlines and terminal control characters inert in a terminal strip.
func (s Inspection) Parts() []InspectPart {
	var out []InspectPart
	for _, reg := range s.Registers {
		if reg.Value.Text != "" {
			out = append(out, InspectPart{"register:" + reg.Name, fmt.Sprintf("register %s = %q", inspectInline(reg.Name), reg.Value.Text)})
		}
	}
	if s.YankHead != nil {
		unnamed := ""
		for _, reg := range s.Registers {
			if reg.Name == `"` {
				unnamed = reg.Value.Text
				break
			}
		}
		if s.YankHead.Text != unnamed {
			out = append(out, InspectPart{"yankHead", fmt.Sprintf("yank head = %q", s.YankHead.Text)})
		}
	}
	if s.Message != nil {
		out = append(out, InspectPart{"message", fmt.Sprintf("%s: %q", s.Message.Tone, s.Message.Text)})
	}
	for _, path := range s.FilesWritten {
		out = append(out, InspectPart{"file:" + path, fmt.Sprintf("wrote %q", path)})
	}
	names := make([]string, 0, len(s.Fields))
	for name := range s.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, InspectPart{"field:" + name, fmt.Sprintf("%s = %q", inspectInline(name), s.Fields[name].Text)})
	}
	if s.LastRequest != nil {
		text := inspectInline(s.LastRequest.Key) + " → " + fmt.Sprintf("%q", s.LastRequest.Result.Text)
		if s.LastRequest.Pending {
			text += " (running)"
		}
		if s.LastRequest.Error != "" {
			text += " error: " + fmt.Sprintf("%q", s.LastRequest.Error)
		}
		out = append(out, InspectPart{"request", text})
	}
	out = append(out, InspectPart{"mode", "mode " + inspectInline(s.Mode) + " (declared " + inspectInline(s.DeclaredMode) + ")"})
	var path []string
	for _, level := range s.Focus {
		if level.Instance != "app" {
			path = append(path, inspectInline(level.Instance)+" ["+inspectInline(level.Noun)+"]")
		}
	}
	out = append(out, InspectPart{"focus", "focus " + strings.Join(path, " › ")})
	if s.Count != 0 || s.OperatorCount != 0 || s.Pending != "" {
		out = append(out, InspectPart{"pending", fmt.Sprintf("count %d/%d · pending %q", s.Count, s.OperatorCount, s.Pending)})
	}
	if s.Selection != 0 || s.Marks != 0 {
		out = append(out, InspectPart{"selection", fmt.Sprintf("selection %d · marks %d", s.Selection, s.Marks)})
	}
	if len(s.Modals) > 0 {
		out = append(out, InspectPart{"modals", fmt.Sprintf("modals %q", s.Modals)})
	}
	if len(s.Jobs) > 0 {
		out = append(out, InspectPart{"jobs", fmt.Sprintf("%d running jobs", len(s.Jobs))})
	}
	return out
}

// Compact is a one-line summary; callers clip it to their available cells.
func (s Inspection) Compact() string {
	var parts []string
	for _, p := range s.Parts() {
		parts = append(parts, p.Text)
	}
	return strings.Join(parts, " · ")
}
