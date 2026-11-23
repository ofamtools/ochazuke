// Package palette is a reusable command palette combining a filterable
// picker.Model of commands with a live, always-visible form.Model for the
// highlighted command's parameters. It performs no I/O and imports only the
// standard library, core, ui, widgets/picker, widgets/form and widgets/input.
package palette

import (
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/picker"
)

// Command IDs handled by Model.HandleCommand in addition to picker and form commands.
const (
	CmdFocusForm   = "palette.focus.form"
	CmdFocusList   = "palette.focus.list"
	CmdToggleFocus = "palette.toggle.focus"
)

// DefaultSplitRatio is the default left/right split ratio when arguments are visible.
const DefaultSplitRatio = 0.52

// Entry pairs a command specification with its picker row and optional default arguments.
type Entry struct {
	Spec     core.CommandSpec
	Item     picker.Item
	Defaults map[string]any
}

// PickRequest asks the host to open a path or record ref picker for Field.
type PickRequest struct {
	Field       string
	Kind        form.FieldKind // form.KindPath or form.KindRef
	RefKinds    []string
	SourceQuery string
}

// Result is returned by HandleCommand when a command is submitted, cancelled,
// or requests an external path/ref picker.
type Result struct {
	Submitted bool
	Cancelled bool
	Entry     Entry
	Args      map[string]any
	NeedPick  *PickRequest
	PickPath  *form.PathRequest
	PickRef   *form.RefRequest
}

// Model is the command palette state.
type Model struct {
	Title       string
	Placeholder string
	Picker      *picker.Model
	Entries     []Entry
	FocusForm   bool
	ActiveForm  *form.Model
	ActiveEntry *Entry
	SplitRatio  float64

	// ChoiceResolver, when set, is forwarded to each newly constructed
	// argument form so enum fields can offer dynamic choices.
	ChoiceResolver func(spec core.ParamSpec, values map[string]any) []string

	byID  map[string]int
	forms map[string]*form.Model
}

// CommandSpecs returns the CommandSpecs for the palette's focus commands.
func CommandSpecs() []core.CommandSpec {
	tags := []string{"palette", "focus"}
	return []core.CommandSpec{
		{ID: CmdFocusForm, Description: "Move focus to the command arguments form", Tags: tags},
		{ID: CmdFocusList, Description: "Return focus to the command list", Tags: tags},
		{ID: CmdToggleFocus, Description: "Toggle focus between the command list and arguments form", Tags: tags},
	}
}

// New builds a command palette over entries and immediately syncs the active
// entry and its argument form.
func New(title string, entries []Entry) *Model {
	norm := make([]Entry, len(entries))
	items := make([]picker.Item, len(entries))
	byID := make(map[string]int, len(entries))
	for i, e := range entries {
		if e.Item.ID == "" {
			e.Item.ID = e.Spec.ID
		}
		if e.Item.Title == "" {
			e.Item.Title = e.Spec.ID
		}
		norm[i] = e
		items[i] = e.Item
		if _, exists := byID[e.Item.ID]; !exists {
			byID[e.Item.ID] = i
		}
	}
	m := &Model{
		Title:      title,
		Picker:     picker.New(title, items),
		Entries:    norm,
		SplitRatio: DefaultSplitRatio,
		byID:       byID,
		forms:      map[string]*form.Model{},
	}
	m.Sync()
	return m
}

// Sync aligns ActiveEntry and ActiveForm with the picker's currently
// highlighted command. Per-command form state is cached for the lifetime of
// the palette so moving away and back preserves typed values.
func (m *Model) Sync() {
	if m.Picker == nil {
		m.ActiveEntry = nil
		m.ActiveForm = nil
		m.FocusForm = false
		return
	}
	it, ok := m.Picker.Focused()
	if !ok {
		m.ActiveEntry = nil
		m.ActiveForm = nil
		m.FocusForm = false
		return
	}
	idx, found := m.entryIndex(it.ID)
	if !found {
		m.ActiveEntry = nil
		m.ActiveForm = nil
		m.FocusForm = false
		return
	}
	entry := &m.Entries[idx]
	m.ActiveEntry = entry

	if len(entry.Spec.Params) == 0 {
		if m.ActiveForm != nil {
			m.ActiveForm.Editing = false
		}
		m.ActiveForm = nil
		m.FocusForm = false
		return
	}

	if m.forms == nil {
		m.forms = map[string]*form.Model{}
	}
	key := entry.Item.ID
	f, cached := m.forms[key]
	if !cached {
		f = form.New(entry.Spec.ID, entry.Spec.Params, entry.Defaults)
		f.SubmitLabel = "Run"
		if m.ChoiceResolver != nil {
			f.ChoiceResolver = m.ChoiceResolver
		}
		m.forms[key] = f
	}
	if m.ActiveForm != f {
		if m.ActiveForm != nil {
			m.ActiveForm.Editing = false
		}
		m.ActiveForm = f
	}
}

func (m *Model) entryIndex(id string) (int, bool) {
	if idx, ok := m.byID[id]; ok && idx >= 0 && idx < len(m.Entries) && m.Entries[idx].Item.ID == id {
		return idx, true
	}
	for i := range m.Entries {
		if m.Entries[i].Item.ID == id {
			if m.byID == nil {
				m.byID = map[string]int{}
			}
			m.byID[id] = i
			return i, true
		}
	}
	return -1, false
}

// FocusToForm moves focus into the active command's argument form when one exists.
func (m *Model) FocusToForm() bool {
	m.Sync()
	if m.ActiveForm == nil {
		return false
	}
	m.FocusForm = true
	return true
}

// FocusToList returns focus to the command picker and exits field editing.
func (m *Model) FocusToList() {
	m.FocusForm = false
	if m.ActiveForm != nil {
		m.ActiveForm.Editing = false
	}
}

// ToggleFocus switches focus between the command picker and the argument form.
func (m *Model) ToggleFocus() bool {
	if m.FocusForm {
		m.FocusToList()
		return false
	}
	return m.FocusToForm()
}

// EditingForm reports whether the user is actively typing inside a form field.
func (m *Model) EditingForm() bool {
	return m.FocusForm && m.ActiveForm != nil && m.ActiveForm.Editing
}

// SetPickedValue applies a host path or record picker result to the active form.
func (m *Model) SetPickedValue(field string, value any) bool {
	m.Sync()
	if m.ActiveForm == nil {
		return false
	}
	return m.ActiveForm.SetValue(field, value)
}

// FocusID returns the ViewNode ID that should hold UI focus for this palette.
func (m *Model) FocusID(id string) string {
	m.Sync()
	if m.FocusForm && m.ActiveForm != nil {
		return id + ".form"
	}
	return id + ".picker"
}

// HandleCommand dispatches palette focus commands, picker commands (when the
// list is focused), and form/readline/motion commands (when the argument form
// is focused).
func (m *Model) HandleCommand(cmd core.Command) (Result, bool) {
	m.Sync()
	switch cmd.ID {
	case CmdFocusForm:
		m.FocusToForm()
		return Result{}, true
	case CmdFocusList:
		m.FocusToList()
		return Result{}, true
	case CmdToggleFocus:
		m.ToggleFocus()
		return Result{}, true
	}

	if m.FocusForm && m.ActiveForm != nil {
		return m.handleFormCommand(cmd)
	}
	m.FocusForm = false
	return m.handlePickerCommand(cmd)
}

func (m *Model) handlePickerCommand(cmd core.Command) (Result, bool) {
	if m.Picker == nil {
		return Result{}, false
	}
	picked, handled := m.Picker.HandleCommand(cmd)
	if !handled {
		return Result{}, false
	}
	m.Sync()
	if picked == nil {
		return Result{}, true
	}
	if m.ActiveEntry == nil {
		return Result{}, true
	}
	entry := *m.ActiveEntry
	if m.ActiveForm != nil {
		if !m.ActiveForm.Validate() {
			m.FocusForm = true
			// Jump to the first invalid/missing field so the user can fill it immediately.
			for _, s := range m.ActiveForm.Specs {
				if m.ActiveForm.Errors[s.Name] != "" {
					m.ActiveForm.FocusField(s.Name)
					break
				}
			}
			return Result{}, true
		}
		return Result{
			Submitted: true,
			Entry:     entry,
			Args:      mergeArgs(entry.Spec.Params, entry.Defaults, m.ActiveForm.TypedValues()),
		}, true
	}
	return Result{
		Submitted: true,
		Entry:     entry,
		Args:      mergeArgs(nil, entry.Defaults, nil),
	}, true
}

func (m *Model) handleFormCommand(cmd core.Command) (Result, bool) {
	f := m.ActiveForm
	var entry Entry
	if m.ActiveEntry != nil {
		entry = *m.ActiveEntry
	}

	// Allow h/l (motion.left/motion.right) to cycle enum fields when not editing.
	if !f.Editing {
		switch cmd.ID {
		case "motion.left":
			f.CycleEnum(-1)
			return Result{}, true
		case "motion.right":
			f.CycleEnum(1)
			return Result{}, true
		}
	}

	// Treat picker.confirm while focused in the form as form.submit.
	if cmd.ID == picker.CmdConfirm {
		cmd = core.Command{ID: form.CmdSubmit}
	}

	fr, handled := f.HandleCommand(cmd)
	if !handled {
		// Guard against unhandled commands when input readline is checked by form.
		if input.IsReadlineCommand(cmd.ID) {
			return Result{}, true
		}
		return Result{}, false
	}

	if fr.PickPath != nil {
		return Result{
			Entry:    entry,
			PickPath: fr.PickPath,
			NeedPick: &PickRequest{
				Field: fr.PickPath.Field,
				Kind:  form.KindPath,
			},
		}, true
	}
	if fr.PickRef != nil {
		return Result{
			Entry:   entry,
			PickRef: fr.PickRef,
			NeedPick: &PickRequest{
				Field:       fr.PickRef.Field,
				Kind:        form.KindRef,
				RefKinds:    fr.PickRef.RefKinds,
				SourceQuery: fr.PickRef.SourceQuery,
			},
		}, true
	}
	if fr.Submitted {
		return Result{
			Submitted: true,
			Entry:     entry,
			Args:      mergeArgs(entry.Spec.Params, entry.Defaults, fr.Values),
		}, true
	}
	if fr.Cancelled {
		if cmd.ID == form.CmdStop {
			// Esc in form normal mode returns focus to the command picker.
			m.FocusToList()
			return Result{}, true
		}
		return Result{Cancelled: true, Entry: entry}, true
	}
	return Result{}, true
}

// mergeArgs combines entry.Defaults with typed form values. Declared params
// are governed by typed form values (which were initialized from Defaults),
// while any extra undeclared defaults are preserved.
func mergeArgs(params []core.ParamSpec, defaults, typed map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(typed))
	if len(params) == 0 {
		for k, v := range defaults {
			out[k] = v
		}
	} else {
		declared := make(map[string]bool, len(params))
		for _, p := range params {
			declared[p.Name] = true
		}
		for k, v := range defaults {
			if !declared[k] {
				out[k] = v
			}
		}
	}
	for k, v := range typed {
		out[k] = v
	}
	return out
}

// View renders the command palette. When the highlighted command declares
// parameters, it renders a split view with the picker on the left and a
// bordered live arguments panel on the right; otherwise it renders the picker
// alone.
func (m *Model) View(id string) ui.ViewNode {
	m.Sync()
	if m.Picker != nil {
		if m.Title != "" && m.Picker.Title == "" {
			m.Picker.Title = m.Title
		}
		if m.Placeholder != "" && m.Picker.Placeholder == "" {
			m.Picker.Placeholder = m.Placeholder
		}
	}

	activeCmd := ""
	if m.ActiveEntry != nil {
		activeCmd = m.ActiveEntry.Spec.ID
	}
	facts := map[string]any{
		"hasArgs":       m.ActiveForm != nil,
		"focusForm":     m.FocusForm && m.ActiveForm != nil,
		"editingForm":   m.EditingForm(),
		"activeCommand": activeCmd,
	}

	var pickerNode ui.ViewNode
	if m.Picker != nil {
		pickerNode = m.Picker.View(id + ".picker")
	} else {
		pickerNode = ui.ViewNode{ID: id + ".picker", Kind: ui.KindPicker, Focusable: true, Title: m.Title}
	}

	if m.ActiveForm == nil {
		if pickerNode.Facts == nil {
			pickerNode.Facts = map[string]any{}
		}
		for k, v := range facts {
			pickerNode.Facts[k] = v
		}
		return pickerNode
	}

	badge := ui.Badge{Text: "tab to edit", Tone: "muted"}
	if m.FocusForm {
		badge = ui.Badge{Text: "editing args · enter runs", Tone: "accent"}
	}
	argsPanel := ui.ViewNode{
		ID:       id + ".args",
		Kind:     ui.KindPanel,
		Title:    "Arguments · " + activeCmd,
		Border:   true,
		Badges:   []ui.Badge{badge},
		Children: []ui.ViewNode{m.ActiveForm.View(id + ".form")},
	}

	ratio := m.SplitRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = DefaultSplitRatio
	}
	return ui.ViewNode{
		ID:       id,
		Kind:     ui.KindSplit,
		Scope:    "palette",
		Ratio:    ratio,
		Gap:      1,
		Facts:    facts,
		Children: []ui.ViewNode{pickerNode, argsPanel},
	}
}
