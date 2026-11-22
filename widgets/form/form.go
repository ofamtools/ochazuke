// Package form is a schema-driven dynamic form. Field widgets are inferred
// from []core.ParamSpec (type and enum), and the model renders to a plain
// ui.KindForm ViewNode. It performs no I/O.
//
// Values holds the editable representation: bool for toggles, string for
// everything else. TypedValues/Submit convert them to int, float64, bool,
// time.Duration, []string, core.Ref, any (JSON) or string.
package form

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

// FieldKind is the widget inferred for a ParamSpec.
type FieldKind string

// Inferred field kinds.
const (
	KindBool     FieldKind = "bool"
	KindEnum     FieldKind = "enum"
	KindNumber   FieldKind = "number"
	KindPath     FieldKind = "path"
	KindRef      FieldKind = "ref"
	KindList     FieldKind = "list"
	KindDuration FieldKind = "duration"
	KindText     FieldKind = "text"
	KindJSON     FieldKind = "json"
)

// KindFor infers the widget for spec.
func KindFor(spec core.ParamSpec) FieldKind {
	switch {
	case spec.Type == core.ParamBool:
		return KindBool
	case len(spec.Enum) > 0:
		return KindEnum
	}
	switch spec.Type {
	case core.ParamInt, core.ParamFloat:
		return KindNumber
	case core.ParamPath:
		return KindPath
	case core.ParamRef:
		return KindRef
	case core.ParamList:
		return KindList
	case core.ParamDuration:
		return KindDuration
	case core.ParamObject:
		return KindJSON
	}
	return KindText
}

// Command IDs HandleCommand understands, besides motion.down/up/top/bottom.
const (
	CmdSubmit    = "form.submit"
	CmdCancel    = "form.cancel"
	CmdEdit      = "form.edit"   // start editing the active text field
	CmdStop      = "form.stop"   // leave editing; cancels when not editing
	CmdToggle    = "form.toggle" // toggle bool / cycle enum
	CmdCycle     = "form.cycle"  // Args dir: -1 or 1
	CmdInsert    = "form.insert" // Args text
	CmdBackspace = "form.backspace"
	CmdPick      = "form.pick" // request a picker for the active path/ref field
	CmdSet       = "form.set"  // Args field, value
)

// FormResult is a signal returned by HandleCommand.
type FormResult struct {
	Submitted   bool
	Cancelled   bool
	PickPath    *PathRequest
	PickRef     *RefRequest
	Values      map[string]any // typed values when Submitted
	ErrorFields []string       // fields failing validation on a rejected submit
}

// PathRequest asks the host to open a path picker for Field.
type PathRequest struct{ Field string }

// RefRequest asks the host to open a ref picker for Field.
type RefRequest struct {
	Field       string
	RefKinds    []string
	SourceQuery string
}

// Model is the form state.
type Model struct {
	Title       string
	Specs       []core.ParamSpec
	Values      map[string]any
	ActiveIdx   int
	Editing     bool
	Errors      map[string]string
	Dirty       bool
	SubmitLabel string

	// ChoiceResolver, when set, supplies an enum field's choices from the
	// current values, so one field's options can depend on another (the
	// models offered for the chosen harness). A nil or empty result falls
	// back to spec.Enum. A field whose spec has no Enum becomes an enum
	// field while the resolver returns choices for it. The resolver must
	// not modify values.
	ChoiceResolver func(spec core.ParamSpec, values map[string]any) []string
}

// EffectiveEnum returns the choices of field idx: the ChoiceResolver's
// result when non-empty, otherwise spec.Enum.
func (m *Model) EffectiveEnum(idx int) []string {
	if idx < 0 || idx >= len(m.Specs) {
		return nil
	}
	s := m.Specs[idx]
	if m.ChoiceResolver != nil {
		if choices := m.ChoiceResolver(s, m.Values); len(choices) > 0 {
			return choices
		}
	}
	return s.Enum
}

// spec returns field idx with Enum replaced by EffectiveEnum.
func (m *Model) spec(idx int) core.ParamSpec {
	s := m.Specs[idx]
	if m.ChoiceResolver != nil {
		s.Enum = m.EffectiveEnum(idx)
	}
	return s
}

// KindAt infers the widget for field idx from its effective choices.
func (m *Model) KindAt(idx int) FieldKind {
	if idx < 0 || idx >= len(m.Specs) {
		return ""
	}
	return KindFor(m.spec(idx))
}

// New builds a form. initial values win over ParamSpec.Default.
func New(title string, specs []core.ParamSpec, initial map[string]any) *Model {
	m := &Model{Title: title, Specs: specs, Values: map[string]any{}, Errors: map[string]string{}, SubmitLabel: "Submit"}
	for _, s := range specs {
		if v, ok := initial[s.Name]; ok {
			m.Values[s.Name] = display(s, v)
		} else if s.Default != nil {
			m.Values[s.Name] = display(s, s.Default)
		} else if KindFor(s) == KindBool {
			m.Values[s.Name] = false
		}
	}
	return m
}

func display(s core.ParamSpec, v any) any {
	k := KindFor(s)
	if k == KindBool {
		switch t := v.(type) {
		case bool:
			return t
		case string:
			return t == "true"
		}
		return false
	}
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case time.Duration:
		return t.String()
	case []string:
		return strings.Join(t, ", ")
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = fmt.Sprint(e)
		}
		return strings.Join(parts, ", ")
	case core.Ref:
		if t.Kind == "" {
			return t.ID
		}
		return t.Kind + ":" + t.ID
	case map[string]any:
		b, _ := json.Marshal(t)
		return string(b)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	}
	if k == KindJSON {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return fmt.Sprint(v)
}

func (m *Model) active() (core.ParamSpec, bool) {
	if m.ActiveIdx < 0 || m.ActiveIdx >= len(m.Specs) {
		return core.ParamSpec{}, false
	}
	return m.spec(m.ActiveIdx), true
}

func (m *Model) text(name string) string {
	s, _ := m.Values[name].(string)
	return s
}

func (m *Model) touch(name string) {
	m.Dirty = true
	delete(m.Errors, name)
	if m.ChoiceResolver == nil {
		return
	}
	// Dependent choices may have changed: re-check the other enum fields
	// so a value no longer offered is flagged at once.
	if m.Errors == nil {
		m.Errors = map[string]string{}
	}
	for i := range m.Specs {
		s := m.spec(i)
		if s.Name == name || KindFor(s) != KindEnum {
			continue
		}
		if _, err := m.typed(s); err != nil {
			m.Errors[s.Name] = err.Error()
		} else {
			delete(m.Errors, s.Name)
		}
	}
}

// Down, Up, Top and Bottom move the active field and stop editing.
func (m *Model) Down() { m.move(m.ActiveIdx + 1) }

// Up moves to the previous field.
func (m *Model) Up() { m.move(m.ActiveIdx - 1) }

// Top moves to the first field.
func (m *Model) Top() { m.move(0) }

// Bottom moves to the last field.
func (m *Model) Bottom() { m.move(len(m.Specs) - 1) }

func (m *Model) move(i int) {
	m.ActiveIdx = max(0, min(i, len(m.Specs)-1))
	m.Editing = false
}

// Editable reports whether the active field takes typed text.
func (m *Model) Editable() bool {
	s, ok := m.active()
	if !ok {
		return false
	}
	k := KindFor(s)
	return k != KindBool && k != KindEnum
}

// InsertRune appends r to the active text field.
func (m *Model) InsertRune(r rune) { m.insert(string(r)) }

func (m *Model) insert(text string) {
	s, ok := m.active()
	if !ok || !m.Editable() {
		return
	}
	m.Values[s.Name] = m.text(s.Name) + text
	m.touch(s.Name)
}

// Backspace removes the last rune of the active text field.
func (m *Model) Backspace() {
	s, ok := m.active()
	if !ok || !m.Editable() {
		return
	}
	r := []rune(m.text(s.Name))
	if len(r) > 0 {
		m.Values[s.Name] = string(r[:len(r)-1])
		m.touch(s.Name)
	}
}

// ToggleBool flips the active bool field (and cycles an enum forward).
func (m *Model) ToggleBool() {
	s, ok := m.active()
	if !ok {
		return
	}
	switch KindFor(s) {
	case KindBool:
		b, _ := m.Values[s.Name].(bool)
		m.Values[s.Name] = !b
		m.touch(s.Name)
	case KindEnum:
		m.CycleEnum(1)
	}
}

// CycleEnum selects the next (dir>0) or previous enum option of the active field.
func (m *Model) CycleEnum(dir int) {
	s, ok := m.active()
	if !ok || KindFor(s) != KindEnum {
		return
	}
	n := len(s.Enum)
	cur := -1
	for i, e := range s.Enum {
		if e == m.text(s.Name) {
			cur = i
		}
	}
	if dir >= 0 {
		cur = (cur + 1) % n
	} else if cur <= 0 {
		cur = n - 1
	} else {
		cur--
	}
	m.Values[s.Name] = s.Enum[cur]
	m.touch(s.Name)
}

// SetValue sets a field from any Go value (e.g. a picker result).
func (m *Model) SetValue(name string, v any) bool {
	for i := range m.Specs {
		if s := m.spec(i); s.Name == name {
			m.Values[name] = display(s, v)
			m.touch(name)
			return true
		}
	}
	return false
}

// Validate checks every field and records per-field Errors.
func (m *Model) Validate() bool {
	m.Errors = map[string]string{}
	for i := range m.Specs {
		s := m.spec(i)
		if _, err := m.typed(s); err != nil {
			m.Errors[s.Name] = err.Error()
		}
	}
	return len(m.Errors) == 0
}

// typed converts one field. A nil value with nil error means "omit".
func (m *Model) typed(s core.ParamSpec) (any, error) {
	k := KindFor(s)
	if k == KindBool {
		b, _ := m.Values[s.Name].(bool)
		return b, nil
	}
	txt := strings.TrimSpace(m.text(s.Name))
	if txt == "" {
		if s.Required {
			return nil, fmt.Errorf("required")
		}
		return nil, nil
	}
	if k == KindEnum {
		for _, e := range s.Enum {
			if e == txt {
				return txt, nil
			}
		}
		return nil, fmt.Errorf("must be one of %s", strings.Join(s.Enum, ", "))
	}
	switch k {
	case KindNumber:
		if s.Type == core.ParamInt {
			n, err := strconv.Atoi(txt)
			if err != nil {
				return nil, fmt.Errorf("must be an integer")
			}
			return n, nil
		}
		f, err := strconv.ParseFloat(txt, 64)
		if err != nil {
			return nil, fmt.Errorf("must be a number")
		}
		return f, nil
	case KindDuration:
		d, err := time.ParseDuration(txt)
		if err != nil {
			return nil, fmt.Errorf("must be a duration like 5m or 1h30m")
		}
		return d, nil
	case KindList:
		var out []string
		for _, p := range strings.Split(txt, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		if len(out) == 0 && s.Required {
			return nil, fmt.Errorf("required")
		}
		return out, nil
	case KindJSON:
		var v any
		if err := json.Unmarshal([]byte(txt), &v); err != nil {
			return nil, fmt.Errorf("invalid JSON")
		}
		return v, nil
	case KindRef:
		if kind, id, ok := strings.Cut(txt, ":"); ok && kind != "" && id != "" {
			return core.Ref{Kind: kind, ID: id}, nil
		}
		if len(s.RefKinds) == 1 {
			return core.Ref{Kind: s.RefKinds[0], ID: txt}, nil
		}
		return txt, nil
	}
	return txt, nil
}

// TypedValues converts the values that are valid; invalid and empty optional
// fields are omitted.
func (m *Model) TypedValues() map[string]any {
	out := map[string]any{}
	for i := range m.Specs {
		s := m.spec(i)
		if v, err := m.typed(s); err == nil && v != nil {
			out[s.Name] = v
		}
	}
	return out
}

// Submit validates and returns the typed values.
func (m *Model) Submit() (map[string]any, bool) {
	if !m.Validate() {
		return nil, false
	}
	return m.TypedValues(), true
}

// HandleCommand applies a form or motion command. ok is false for commands
// the form does not handle.
func (m *Model) HandleCommand(cmd core.Command) (FormResult, bool) {
	switch cmd.ID {
	case "motion.down":
		m.Down()
	case "motion.up":
		m.Up()
	case "motion.top":
		m.Top()
	case "motion.bottom":
		m.Bottom()
	case CmdEdit:
		m.Editing = m.Editable()
	case CmdStop:
		if m.Editing {
			m.Editing = false
		} else {
			return FormResult{Cancelled: true}, true
		}
	case CmdCancel:
		return FormResult{Cancelled: true}, true
	case CmdToggle:
		m.ToggleBool()
	case CmdCycle:
		dir := 1
		if d, ok := cmd.Args["dir"].(int); ok {
			dir = d
		} else if d, ok := cmd.Args["dir"].(float64); ok {
			dir = int(d)
		}
		m.CycleEnum(dir)
	case CmdInsert:
		m.insert(cmd.Arg("text"))
	case CmdBackspace:
		m.Backspace()
	case CmdSet:
		m.SetValue(cmd.Arg("field"), cmd.Args["value"])
	case CmdPick:
		if s, ok := m.active(); ok {
			switch KindFor(s) {
			case KindPath:
				return FormResult{PickPath: &PathRequest{Field: s.Name}}, true
			case KindRef:
				return FormResult{PickRef: &RefRequest{Field: s.Name, RefKinds: s.RefKinds, SourceQuery: s.SourceQuery}}, true
			}
		}
	case CmdSubmit:
		vals, ok := m.Submit()
		if !ok {
			var bad []string
			for _, s := range m.Specs {
				if _, has := m.Errors[s.Name]; has {
					bad = append(bad, s.Name)
				}
			}
			return FormResult{ErrorFields: bad}, true
		}
		return FormResult{Submitted: true, Values: vals}, true
	default:
		return FormResult{}, false
	}
	return FormResult{}, true
}

// View renders the form as a ui.KindForm node matching ui's props contract.
func (m *Model) View(id string) ui.ViewNode {
	active := ""
	kind := ""
	if s, ok := m.active(); ok {
		active, kind = s.Name, string(KindFor(s))
	}
	values := make(map[string]any, len(m.Values))
	for k, v := range m.Values {
		values[k] = v
	}
	errs := make(map[string]any, len(m.Errors))
	for k, v := range m.Errors {
		errs[k] = v
	}
	props := map[string]any{
		"fields":      m.fields(),
		"values":      values,
		"active":      m.ActiveIdx,
		"submitLabel": m.SubmitLabel,
		"errors":      errs,
		"editing":     m.Editing,
	}
	return ui.ViewNode{
		ID:        id,
		Kind:      ui.KindForm,
		Scope:     "form",
		Title:     m.Title,
		Focusable: true,
		Facts:     map[string]any{"field": active, "fieldKind": kind, "editing": m.Editing, "dirty": m.Dirty, "invalid": len(m.Errors) > 0},
		Props:     props,
		CacheKey:  m.cacheKey(),
	}
}

// fields returns the specs with effective choices (Specs itself when no
// ChoiceResolver is set).
func (m *Model) fields() []core.ParamSpec {
	if m.ChoiceResolver == nil {
		return m.Specs
	}
	out := make([]core.ParamSpec, len(m.Specs))
	for i := range m.Specs {
		out[i] = m.spec(i)
	}
	return out
}

func (m *Model) cacheKey() uint64 {
	h := fnv.New64a()
	w := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	w(m.Title)
	w(m.SubmitLabel)
	w(strconv.Itoa(m.ActiveIdx))
	w(strconv.FormatBool(m.Editing))
	for _, s := range m.fields() {
		w(s.Name)
		w(string(s.Type))
		w(strconv.FormatBool(s.Required))
		w(strings.Join(s.Enum, ","))
		w(s.Description)
		w(fmt.Sprint(s.Default))
	}
	for _, mp := range []map[string]string{m.Errors} {
		keys := make([]string, 0, len(mp))
		for k := range mp {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			w(k)
			w(mp[k])
		}
	}
	keys := make([]string, 0, len(m.Values))
	for k := range m.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w(k)
		w(fmt.Sprint(m.Values[k]))
	}
	return h.Sum64()
}
