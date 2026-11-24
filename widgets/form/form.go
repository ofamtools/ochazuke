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
	"ofam.dev/ochazuke/expr"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
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
	CmdSubmit       = "form.submit"
	CmdCancel       = "form.cancel"
	CmdEdit         = "form.edit"   // start editing the active text field
	CmdStop         = "form.stop"   // leave editing; cancels when not editing
	CmdToggle       = "form.toggle" // toggle bool / cycle enum
	CmdCycle        = "form.cycle"  // Args dir: -1 or 1
	CmdInsert       = "form.insert" // Args text
	CmdBackspace    = "form.backspace"
	CmdPick         = "form.pick"          // request a picker for the active path/ref field
	CmdSet          = "form.set"           // Args field, value
	CmdDraftPrev    = "form.draft.prev"    // step backward into newest -> older drafts
	CmdDraftNext    = "form.draft.next"    // step forward toward newer drafts -> live working state
	CmdDraftDiscard = "form.draft.discard" // discard currently loaded draft
	CmdItemAdd      = "form.item.add"      // append a new item to the focused repeatable field
	CmdItemRemove   = "form.item.remove"   // remove the focused item from a repeatable field
)

// FormResult is a signal returned by HandleCommand.
type FormResult struct {
	Submitted         bool
	Cancelled         bool
	PickPath          *PathRequest
	PickRef           *RefRequest
	Values            map[string]any // typed values when Submitted
	ErrorFields       []string       // fields failing validation on a rejected submit
	DiscardedDraftIdx int
	HasDiscardedDraft bool
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
	SubmitRow   bool
	Wrap        bool

	// Drafts holds saved snapshots (newest first). DraftIdx is -1 while editing
	// live working values, or 0..len(Drafts)-1 when viewing a draft.
	Drafts   []Draft
	DraftIdx int

	// ChoiceResolver, when set, supplies an enum field's choices from the
	// current values, so one field's options can depend on another (the
	// models offered for the chosen harness). A nil or empty result falls
	// back to spec.Enum. A field whose spec has no Enum becomes an enum
	// field while the resolver returns choices for it. The resolver must
	// not modify values.
	ChoiceResolver func(spec core.ParamSpec, values map[string]any) []string

	// ShowAllFields bypasses VisibleWhen predicates so every field is shown.
	ShowAllFields bool

	compiledWhen map[string]expr.Expr
	line         input.Line
	lineField    string
	lineItem     int
	liveValues   map[string]any
	liveDirty    bool
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
	m := &Model{
		Title:       title,
		Specs:       specs,
		Values:      map[string]any{},
		Errors:      map[string]string{},
		SubmitLabel: "Submit",
		DraftIdx:    -1,
		lineItem:    -1,
	}
	for _, s := range specs {
		if v, ok := initial[s.Name]; ok {
			m.Values[s.Name] = display(s, v)
		} else if s.Default != nil {
			m.Values[s.Name] = display(s, s.Default)
		} else if s.Repeatable {
			m.Values[s.Name] = []string{}
		} else if KindFor(s) == KindBool {
			m.Values[s.Name] = false
		}
	}
	return m
}

func display(s core.ParamSpec, v any) any {
	if s.Repeatable {
		return normalizeRepeatable(s, v)
	}
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
	return displayScalar(s, v)
}

func displayScalar(s core.ParamSpec, v any) string {
	k := KindFor(s)
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

func (m *Model) text(name string) string {
	s, _ := m.Values[name].(string)
	return s
}

func (m *Model) touch(name string) {
	m.touchWithAnchor(name, navRow{}, false)
}

func (m *Model) touchWithAnchor(name string, anchor navRow, hadAnchor bool) {
	m.Dirty = true
	delete(m.Errors, name)

	var env map[string]any
	if m.hasVisibleWhen() {
		env = m.exprEnv()
		for i, s := range m.Specs {
			if !m.isFieldVisibleWithEnv(i, env) {
				delete(m.Errors, s.Name)
			}
		}
	}

	if m.ChoiceResolver != nil {
		// Dependent choices may have changed: re-check the other visible enum
		// fields so a value no longer offered is flagged at once.
		if m.Errors == nil {
			m.Errors = map[string]string{}
		}
		for i := range m.Specs {
			if !m.isFieldVisibleWithEnv(i, env) {
				continue
			}
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

	m.reanchor(anchor, hadAnchor)
}

func (m *Model) maxIdx() int {
	return len(m.navRows()) - 1
}

// Down, Up, Top and Bottom move the active field and stop editing.
func (m *Model) Down() { m.move(m.ActiveIdx + 1) }

// Up moves to the previous field.
func (m *Model) Up() { m.move(m.ActiveIdx - 1) }

// Top moves to the first field.
func (m *Model) Top() { m.move(0) }

// Bottom moves to the last field (or the Submit row when SubmitRow is enabled).
func (m *Model) Bottom() { m.move(m.maxIdx()) }

func (m *Model) move(i int) {
	m.ActiveIdx = max(0, min(i, m.maxIdx()))
	m.Editing = false
	m.lineField = ""
	m.lineItem = -1
}

// finishEditing syncs any in-progress line edit into m.Values and exits editing mode.
func (m *Model) finishEditing() {
	if !m.Editing {
		return
	}
	if s, row, ok := m.activeEditableRow(); ok && m.Editable() && m.lineField == s.Name && m.lineItem == row.ItemIdx {
		val := m.line.Value()
		if m.rowText(s, row) != val {
			m.setActiveLineValue(s, row, val)
		}
	}
	m.Editing = false
	m.lineField = ""
	m.lineItem = -1
}

// SetDrafts replaces the form's draft ring with a clone of drafts and resets
// cycling back to the live working state.
func (m *Model) SetDrafts(drafts []Draft) {
	m.Drafts = cloneDrafts(drafts)
	m.DraftIdx = -1
	m.liveValues = nil
	m.liveDirty = false
}

// ActiveDraftIndex returns the active draft index (-1 when on live working values).
func (m *Model) ActiveDraftIndex() int { return m.DraftIdx }

// DraftCount returns the number of drafts loaded in the form.
func (m *Model) DraftCount() int { return len(m.Drafts) }

func (m *Model) loadDraftValues(src map[string]any) {
	m.Values = map[string]any{}
	for _, s := range m.Specs {
		if v, ok := src[s.Name]; ok {
			m.Values[s.Name] = display(s, v)
		} else if s.Default != nil {
			m.Values[s.Name] = display(s, s.Default)
		} else if s.Repeatable {
			m.Values[s.Name] = []string{}
		} else if KindFor(s) == KindBool {
			m.Values[s.Name] = false
		}
	}
}

// DraftPrev steps backward into the draft ring (live -> newest -> older).
func (m *Model) DraftPrev() bool {
	if len(m.Drafts) == 0 || m.DraftIdx >= len(m.Drafts)-1 {
		return false
	}
	anchor, hasAnchor := m.activeRow()
	if m.DraftIdx < 0 {
		m.finishEditing()
		anchor, hasAnchor = m.activeRow()
		m.liveValues = cloneValueMap(m.Values)
		m.liveDirty = m.Dirty
		m.DraftIdx = 0
	} else {
		m.DraftIdx++
	}
	m.loadDraftValues(m.Drafts[m.DraftIdx].Values)
	m.Errors = map[string]string{}
	m.Editing = false
	m.lineField = ""
	m.lineItem = -1
	m.Dirty = true
	m.reanchor(anchor, hasAnchor)
	return true
}

// DraftNext steps forward toward newer drafts and back to live working values.
func (m *Model) DraftNext() bool {
	if m.DraftIdx < 0 {
		return false
	}
	anchor, hasAnchor := m.activeRow()
	m.DraftIdx--
	if m.DraftIdx < 0 {
		m.DraftIdx = -1
		if m.liveValues != nil {
			m.Values = cloneValueMap(m.liveValues)
		} else {
			m.Values = map[string]any{}
		}
		m.Dirty = m.liveDirty
		m.liveValues = nil
	} else {
		m.loadDraftValues(m.Drafts[m.DraftIdx].Values)
		m.Dirty = true
	}
	m.Errors = map[string]string{}
	m.Editing = false
	m.lineField = ""
	m.lineItem = -1
	m.reanchor(anchor, hasAnchor)
	return true
}

// DraftDiscard removes the currently viewed draft, returning its original index.
func (m *Model) DraftDiscard() (removedIdx int, ok bool) {
	if m.DraftIdx < 0 || m.DraftIdx >= len(m.Drafts) {
		return -1, false
	}
	anchor, hasAnchor := m.activeRow()
	removedIdx = m.DraftIdx
	m.Drafts = append(m.Drafts[:removedIdx], m.Drafts[removedIdx+1:]...)
	if len(m.Drafts) == 0 {
		m.DraftIdx = -1
		if m.liveValues != nil {
			m.Values = cloneValueMap(m.liveValues)
		} else {
			m.Values = map[string]any{}
		}
		m.Dirty = m.liveDirty
		m.liveValues = nil
	} else {
		if m.DraftIdx >= len(m.Drafts) {
			m.DraftIdx = len(m.Drafts) - 1
		}
		m.loadDraftValues(m.Drafts[m.DraftIdx].Values)
		m.Dirty = true
	}
	m.Errors = map[string]string{}
	m.Editing = false
	m.lineField = ""
	m.lineItem = -1
	m.reanchor(anchor, hasAnchor)
	return removedIdx, true
}

// syncLine aligns the active line editor with the focused text field or repeatable item.
func (m *Model) syncLine() (core.ParamSpec, bool) {
	s, row, ok := m.activeEditableRow()
	if !ok || !m.Editable() {
		return core.ParamSpec{}, false
	}
	val := m.rowText(s, row)
	if m.lineField != s.Name || m.lineItem != row.ItemIdx {
		m.lineField = s.Name
		m.lineItem = row.ItemIdx
		m.line.Set(val)
	} else {
		m.line.Sync(val)
	}
	return s, true
}

// Cursor returns the rune offset of the active text field's editing cursor.
func (m *Model) Cursor() int {
	if _, ok := m.syncLine(); !ok {
		return 0
	}
	return m.line.Cursor
}

// Editable reports whether the active row takes typed text.
func (m *Model) Editable() bool {
	s, _, ok := m.activeEditableRow()
	if !ok {
		return false
	}
	k := KindFor(s)
	return k != KindBool && k != KindEnum
}

// InsertRune inserts r at the cursor in the active text field.
func (m *Model) InsertRune(r rune) { m.insert(string(r)) }

func (m *Model) insert(text string) {
	s, ok := m.syncLine()
	if !ok {
		return
	}
	_, row, ok := m.activeEditableRow()
	if !ok {
		return
	}
	if m.line.Insert(text) {
		m.setActiveLineValue(s, row, m.line.Value())
	}
}

// Backspace removes the rune before the cursor in the active text field.
func (m *Model) Backspace() {
	s, ok := m.syncLine()
	if !ok {
		return
	}
	_, row, ok := m.activeEditableRow()
	if !ok {
		return
	}
	if changed, _ := m.line.HandleCommand(core.Command{ID: input.CmdBackspace}); changed {
		m.setActiveLineValue(s, row, m.line.Value())
	}
}

// ToggleBool flips the active bool field (and cycles an enum forward).
func (m *Model) ToggleBool() {
	s, row, ok := m.activeEditableRow()
	if !ok {
		return
	}
	switch KindFor(s) {
	case KindBool:
		if row.Kind == rowItem {
			next := "true"
			if m.rowText(s, row) == "true" {
				next = "false"
			}
			m.setActiveLineValue(s, row, next)
			return
		}
		b, _ := m.Values[s.Name].(bool)
		m.Values[s.Name] = !b
		m.touchWithAnchor(s.Name, row, true)
	case KindEnum:
		m.CycleEnum(1)
	}
}

// CycleEnum selects the next (dir>0) or previous enum option of the active field.
func (m *Model) CycleEnum(dir int) {
	s, row, ok := m.activeEditableRow()
	if !ok || KindFor(s) != KindEnum || len(s.Enum) == 0 {
		return
	}
	n := len(s.Enum)
	cur := -1
	curVal := m.rowText(s, row)
	for i, e := range s.Enum {
		if e == curVal {
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
	m.setActiveLineValue(s, row, s.Enum[cur])
}

// SetValue sets a field from any Go value (e.g. a picker result).
func (m *Model) SetValue(name string, v any) bool {
	for i := range m.Specs {
		if s := m.spec(i); s.Name == name {
			anchor, hasAnchor := m.activeRow()
			if s.Repeatable {
				m.setRepeatableValue(i, s, v, anchor, hasAnchor)
			} else {
				m.Values[name] = display(s, v)
			}
			m.touchWithAnchor(name, anchor, hasAnchor)
			return true
		}
	}
	return false
}

// Validate checks every visible field and records per-field Errors.
func (m *Model) Validate() bool {
	m.Errors = map[string]string{}
	for _, i := range m.VisibleIndices() {
		s := m.spec(i)
		if _, err := m.typed(s); err != nil {
			m.Errors[s.Name] = err.Error()
		}
	}
	return len(m.Errors) == 0
}

// typed converts one field. A nil value with nil error means "omit".
func (m *Model) typed(s core.ParamSpec) (any, error) {
	if s.Repeatable {
		return m.typedRepeatable(s)
	}
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

// TypedValues converts the visible values that are valid; hidden, invalid, and
// empty optional fields are omitted.
func (m *Model) TypedValues() map[string]any {
	out := map[string]any{}
	for _, i := range m.VisibleIndices() {
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

func (m *Model) handleSubmit() (FormResult, bool) {
	m.finishEditing()
	vals, ok := m.Submit()
	if !ok {
		var bad []string
		for _, i := range m.VisibleIndices() {
			s := m.Specs[i]
			if _, has := m.Errors[s.Name]; has {
				bad = append(bad, s.Name)
			}
		}
		return FormResult{ErrorFields: bad}, true
	}
	return FormResult{Submitted: true, Values: vals}, true
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
		if m.OnSubmitRow() {
			return m.handleSubmit()
		}
		if m.OnAddRow() {
			m.AddItem()
			return FormResult{}, true
		}
		m.Editing = m.Editable()
		if m.Editing {
			m.lineField = ""
			m.lineItem = -1
			m.syncLine()
		}
	case CmdStop:
		if m.Editing {
			m.finishEditing()
		} else {
			return FormResult{Cancelled: true}, true
		}
	case CmdCancel:
		return FormResult{Cancelled: true}, true
	case CmdToggle:
		if m.OnSubmitRow() {
			return m.handleSubmit()
		}
		if m.OnAddRow() {
			m.AddItem()
			return FormResult{}, true
		}
		m.ToggleBool()
	case CmdCycle:
		dir := 1
		if d, ok := cmd.Args["dir"].(int); ok {
			dir = d
		} else if d, ok := cmd.Args["dir"].(float64); ok {
			dir = int(d)
		}
		m.CycleEnum(dir)
	case CmdInsert, input.CmdInsert:
		m.insert(cmd.Arg("text"))
	case CmdBackspace, input.CmdBackspace:
		m.Backspace()
	case CmdSet:
		m.SetValue(cmd.Arg("field"), cmd.Args["value"])
	case CmdPick:
		if s, _, ok := m.activeEditableRow(); ok {
			switch KindFor(s) {
			case KindPath:
				return FormResult{PickPath: &PathRequest{Field: s.Name}}, true
			case KindRef:
				return FormResult{PickRef: &RefRequest{Field: s.Name, RefKinds: s.RefKinds, SourceQuery: s.SourceQuery}}, true
			}
		}
	case CmdItemAdd:
		m.AddItem()
	case CmdItemRemove:
		m.RemoveItem()
	case CmdDraftPrev:
		m.DraftPrev()
	case CmdDraftNext:
		m.DraftNext()
	case CmdDraftDiscard:
		if idx, ok := m.DraftDiscard(); ok {
			return FormResult{DiscardedDraftIdx: idx, HasDiscardedDraft: true}, true
		}
	case CmdSubmit:
		return m.handleSubmit()
	default:
		if input.IsReadlineCommand(cmd.ID) {
			if s, ok := m.syncLine(); ok {
				_, row, ok := m.activeEditableRow()
				if ok {
					if changed, _ := m.line.HandleCommand(cmd); changed {
						m.setActiveLineValue(s, row, m.line.Value())
					}
				}
			}
			return FormResult{}, true
		}
		return FormResult{}, false
	}
	return FormResult{}, true
}

// View renders the form as a ui.KindForm node matching ui's props contract.
func (m *Model) View(id string) ui.ViewNode {
	active := ""
	kind := ""
	repeatable := false
	if s, ok := m.ActiveField(); ok {
		active, kind, repeatable = s.Name, string(KindFor(s)), s.Repeatable
	}
	visFields := m.VisibleFields()
	values := make(map[string]any, len(m.Values))
	for k, v := range m.Values {
		values[k] = cloneValue(v)
	}
	errs := make(map[string]any, len(m.Errors))
	for k, v := range m.Errors {
		errs[k] = v
	}
	props := map[string]any{
		"fields":      visFields,
		"values":      values,
		"active":      m.ActiveIdx,
		"submitLabel": m.SubmitLabel,
		"errors":      errs,
		"editing":     m.Editing,
		"wrap":        m.Wrap,
	}
	if m.Editing {
		props["cursor"] = m.Cursor()
	}
	facts := map[string]any{
		"field":        active,
		"fieldKind":    kind,
		"editing":      m.Editing,
		"dirty":        m.Dirty,
		"invalid":      len(m.Errors) > 0,
		"draftCount":   len(m.Drafts),
		"draftIndex":   m.DraftIdx,
		"repeatable":   repeatable,
		"onAddRow":     m.OnAddRow(),
		"itemIndex":    m.ActiveItemIndex(),
		"visibleCount": len(visFields),
	}
	if m.SubmitRow {
		props["submitRow"] = true
		facts["onSubmit"] = m.OnSubmitRow()
	}
	return ui.ViewNode{
		ID:        id,
		Kind:      ui.KindForm,
		Scope:     "form",
		Title:     m.Title,
		Focusable: true,
		Facts:     facts,
		Props:     props,
		CacheKey:  m.cacheKey(),
	}
}

func (m *Model) cacheKey() uint64 {
	h := fnv.New64a()
	w := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	w(m.Title)
	w(m.SubmitLabel)
	w(strconv.FormatBool(m.SubmitRow))
	w(strconv.FormatBool(m.Wrap))
	w(strconv.FormatBool(m.ShowAllFields))
	w(strconv.Itoa(m.ActiveIdx))
	w(strconv.FormatBool(m.Editing))
	w(strconv.FormatBool(m.Dirty))
	w(strconv.Itoa(m.DraftIdx))
	w(strconv.Itoa(len(m.Drafts)))
	if m.Editing {
		w(strconv.Itoa(m.Cursor()))
	}
	for _, s := range m.VisibleFields() {
		w(s.Name)
		w(string(s.Type))
		w(strconv.FormatBool(s.Required))
		w(strconv.FormatBool(s.Repeatable))
		w(s.AddLabel)
		w(s.VisibleWhen)
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
		if xs, ok := m.Values[k].([]string); ok {
			w("[]string")
			w(strconv.Itoa(len(xs)))
			for _, item := range xs {
				w(item)
			}
		} else {
			w(fmt.Sprint(m.Values[k]))
		}
	}
	return h.Sum64()
}
