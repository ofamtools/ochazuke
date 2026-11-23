package modal_test

import (
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/modal"
	"ofam.dev/ochazuke/widgets/palette"
	"ofam.dev/ochazuke/widgets/pathpicker"
	"ofam.dev/ochazuke/widgets/picker"
)

func TestPickerModalFilterModes(t *testing.T) {
	items := []picker.Item{
		{ID: "p1", Title: "Alpha"},
		{ID: "p2", Title: "Beta"},
	}

	// Default starts in normal ModePicker with Filtering=false.
	m := modal.NewPickerModal(picker.New("Items", items), modal.PickerOptions{
		ID:     "items",
		Create: &modal.PickerCreateHook{Label: "Item"},
	})
	if m.Mode() != modal.ModePicker || m.Model().Filtering {
		t.Fatalf("default mode=%q filtering=%v", m.Mode(), m.Model().Filtering)
	}

	// Enter filter mode -> ModePickerFilter.
	res := m.Handle(core.Command{ID: modal.CmdPickerFilterStart})
	if !res.Handled || res.Close || res.Mode != modal.ModePickerFilter || m.Mode() != modal.ModePickerFilter || !m.Model().Filtering {
		t.Fatalf("CmdPickerFilterStart: res=%+v mode=%q filtering=%v", res, m.Mode(), m.Model().Filtering)
	}

	// Type a query -> create row appears, mode stays ModePickerFilter.
	res = m.Handle(core.Command{ID: modal.CmdPickerInsert, Args: map[string]any{"text": "Gamma"}})
	if res.Mode != modal.ModePickerFilter || len(m.Model().Filtered()) != 1 || m.Model().Filtered()[0].ID != modal.PickerCreateItemID {
		t.Fatalf("insert in filter mode: res=%+v filtered=%+v", res, m.Model().Filtered())
	}

	// First Esc (CmdPickerFilterStop) returns to ModePicker and preserves query.
	res = m.Handle(core.Command{ID: modal.CmdPickerFilterStop})
	if !res.Handled || res.Close || res.Mode != modal.ModePicker || m.Mode() != modal.ModePicker || m.Model().Query != "Gamma" {
		t.Fatalf("CmdPickerFilterStop: res=%+v mode=%q query=%q", res, m.Mode(), m.Model().Query)
	}

	// Clear filter clears query, exits filter mode, and re-syncs create hook.
	m.Handle(core.Command{ID: modal.CmdPickerFilterStart})
	res = m.Handle(core.Command{ID: modal.CmdPickerFilterClear})
	if !res.Handled || res.Close || res.Mode != modal.ModePicker || m.Mode() != modal.ModePicker || m.Model().Query != "" {
		t.Fatalf("CmdPickerFilterClear: res=%+v mode=%q query=%q", res, m.Mode(), m.Model().Query)
	}
	if len(m.Model().Filtered()) != 2 {
		t.Fatalf("CmdPickerFilterClear should remove create row, got %+v", m.Model().Filtered())
	}

	// Second Esc (CmdCancel in normal mode) closes modal.
	res = m.Handle(core.Command{ID: modal.CmdCancel})
	if !res.Handled || !res.Close || res.CloseReason != modal.CloseCancel {
		t.Fatalf("second-stage CmdCancel: %+v", res)
	}

	// StartInFilter + SetMode transitions.
	st := modal.NewStack()
	sf := modal.NewPickerModal(picker.New("Items", items), modal.PickerOptions{
		ID:            "items-filter",
		StartInFilter: true,
	})
	if sf.Mode() != modal.ModePickerFilter || !sf.Model().Filtering {
		t.Fatalf("StartInFilter: mode=%q filtering=%v", sf.Mode(), sf.Model().Filtering)
	}
	state := st.Push(core.State{}, sf)
	if state.Modals[0].Mode != modal.ModePickerFilter {
		t.Fatalf("stack push mode=%q", state.Modals[0].Mode)
	}
	res = sf.Handle(core.Command{ID: modal.CmdPickerFilterStop})
	state = st.ApplyResult(state, res)
	if state.Modals[0].Mode != modal.ModePicker {
		t.Fatalf("stack after stop mode=%q", state.Modals[0].Mode)
	}
	sf.SetMode(modal.ModePickerFilter)
	if sf.Mode() != modal.ModePickerFilter || !sf.Model().Filtering {
		t.Fatalf("SetMode(filter): mode=%q filtering=%v", sf.Mode(), sf.Model().Filtering)
	}
	sf.SetMode("inspect")
	if sf.Mode() != "inspect" || sf.Model().Filtering {
		t.Fatalf("SetMode(inspect): mode=%q filtering=%v", sf.Mode(), sf.Model().Filtering)
	}
}

func TestPathPickerModalFilterModes(t *testing.T) {
	fs := memFS{
		home: "/work",
		dirs: map[string][]pathpicker.DirEntry{
			"/work": {
				{Name: "sub", Path: "/work/sub", IsDir: true},
				{Name: "notes.md", Path: "/work/notes.md", Size: 42},
			},
			"/work/sub": {
				{Name: "deep.go", Path: "/work/sub/deep.go", Size: 10},
			},
		},
	}

	pp, err := pathpicker.New(fs, "/work", pathpicker.PickAny)
	if err != nil {
		t.Fatal(err)
	}
	m := modal.NewPathPickerModal(pp, modal.PathPickerOptions{})
	if m.Mode() != modal.ModePicker || m.Model().Filtering() {
		t.Fatalf("default mode=%q filtering=%v", m.Mode(), m.Model().Filtering())
	}

	// Start filtering, insert query, stop filtering (preserving query).
	res := m.Handle(core.Command{ID: modal.CmdPickerFilterStart})
	if !res.Handled || res.Close || res.Mode != modal.ModePickerFilter || m.Mode() != modal.ModePickerFilter {
		t.Fatalf("CmdPickerFilterStart: res=%+v mode=%q", res, m.Mode())
	}
	res = m.Handle(core.Command{ID: modal.CmdPickerInsert, Args: map[string]any{"text": "notes"}})
	if res.Mode != modal.ModePickerFilter || m.Model().Query != "notes" {
		t.Fatalf("insert: res=%+v query=%q", res, m.Model().Query)
	}
	res = m.Handle(core.Command{ID: modal.CmdPickerFilterStop})
	if !res.Handled || res.Close || res.Mode != modal.ModePicker || m.Mode() != modal.ModePicker || m.Model().Query != "notes" {
		t.Fatalf("CmdPickerFilterStop: res=%+v mode=%q query=%q", res, m.Mode(), m.Model().Query)
	}

	// Clear filter restores listing and returns normal mode.
	m.Handle(core.Command{ID: modal.CmdPickerFilterStart})
	res = m.Handle(core.Command{ID: modal.CmdPickerFilterClear})
	if !res.Handled || res.Close || res.Mode != modal.ModePicker || m.Model().Query != "" || len(m.Model().Entries()) != 2 {
		t.Fatalf("CmdPickerFilterClear: res=%+v query=%q entries=%d", res, m.Model().Query, len(m.Model().Entries()))
	}

	// Second-stage Esc closes.
	res = m.Handle(core.Command{ID: modal.CmdCancel})
	if !res.Handled || !res.Close || res.CloseReason != modal.CloseCancel {
		t.Fatalf("CmdCancel: %+v", res)
	}

	// StartInFilter persists across directory drill.
	pp2, err := pathpicker.New(fs, "/work", pathpicker.PickAny)
	if err != nil {
		t.Fatal(err)
	}
	mf := modal.NewPathPickerModal(pp2, modal.PathPickerOptions{StartInFilter: true})
	if mf.Mode() != modal.ModePickerFilter || !mf.Model().Filtering() {
		t.Fatalf("StartInFilter: mode=%q filtering=%v", mf.Mode(), mf.Model().Filtering())
	}
	res = mf.Handle(core.Command{ID: pathpicker.CmdOpen})
	if res.Mode != modal.ModePickerFilter || mf.Model().Dir != "/work/sub" || !mf.Model().Filtering() {
		t.Fatalf("drill with StartInFilter: res=%+v dir=%q filtering=%v", res, mf.Model().Dir, mf.Model().Filtering())
	}
}

func TestPaletteModalFilterModes(t *testing.T) {
	entries := []palette.Entry{
		{
			Spec: core.CommandSpec{ID: "plain.run", Description: "No args"},
			Item: picker.Item{ID: "plain.run", Title: "Plain Run"},
		},
		{
			Spec: core.CommandSpec{
				ID:     "task.create",
				Params: []core.ParamSpec{{Name: "title", Type: core.ParamString, Required: true}},
			},
			Item: picker.Item{ID: "task.create", Title: "Create Task"},
		},
	}

	// Default starts in normal ModePicker.
	m := modal.NewPaletteModal(palette.New("Palette", entries), modal.PaletteOptions{})
	if m.Mode() != modal.ModePicker || m.Model().Picker.Filtering {
		t.Fatalf("default mode=%q filtering=%v", m.Mode(), m.Model().Picker.Filtering)
	}

	// Start filtering, type query narrowing to task.create, stop filtering preserving query.
	res := m.Handle(core.Command{ID: modal.CmdPickerFilterStart})
	if !res.Handled || res.Close || res.Mode != modal.ModePickerFilter || res.FocusID != "palette.picker" || m.Mode() != modal.ModePickerFilter {
		t.Fatalf("CmdPickerFilterStart: res=%+v mode=%q", res, m.Mode())
	}
	res = m.Handle(core.Command{ID: modal.CmdPickerInsert, Args: map[string]any{"text": "Create"}})
	if res.Mode != modal.ModePickerFilter || m.Model().ActiveEntry == nil || m.Model().ActiveEntry.Spec.ID != "task.create" {
		t.Fatalf("insert: res=%+v active=%+v", res, m.Model().ActiveEntry)
	}

	// Form mode takes precedence over picker filter mode while focused in form.
	res = m.Handle(core.Command{ID: palette.CmdToggleFocus})
	if res.Mode != modal.ModeForm || m.Mode() != modal.ModeForm {
		t.Fatalf("toggle to form: res=%+v mode=%q", res, m.Mode())
	}
	res = m.Handle(core.Command{ID: modal.CmdFormActivate})
	if res.Mode != modal.ModeFormEdit || m.Mode() != modal.ModeFormEdit {
		t.Fatalf("form activate: res=%+v mode=%q", res, m.Mode())
	}
	m.Handle(core.Command{ID: form.CmdStop})
	res = m.Handle(core.Command{ID: palette.CmdFocusList})
	if res.Mode != modal.ModePickerFilter || m.Mode() != modal.ModePickerFilter {
		t.Fatalf("return to filtered list: res=%+v mode=%q", res, m.Mode())
	}

	// First Esc (CmdPickerFilterStop) exits filter mode and preserves query.
	res = m.Handle(core.Command{ID: modal.CmdPickerFilterStop})
	if !res.Handled || res.Close || res.Mode != modal.ModePicker || m.Mode() != modal.ModePicker || m.Model().Picker.Query != "Create" {
		t.Fatalf("CmdPickerFilterStop: res=%+v mode=%q query=%q", res, m.Mode(), m.Model().Picker.Query)
	}

	// Clear filter clears query, exits filter mode, and re-syncs active entry back to plain.run.
	m.Handle(core.Command{ID: modal.CmdPickerFilterStart})
	res = m.Handle(core.Command{ID: modal.CmdPickerFilterClear})
	if !res.Handled || res.Close || res.Mode != modal.ModePicker || m.Model().Picker.Query != "" || m.Model().ActiveEntry.Spec.ID != "plain.run" {
		t.Fatalf("CmdPickerFilterClear: res=%+v query=%q active=%+v", res, m.Model().Picker.Query, m.Model().ActiveEntry)
	}

	// Second Esc (CmdCancel) closes modal.
	res = m.Handle(core.Command{ID: modal.CmdCancel})
	if !res.Handled || !res.Close || res.CloseReason != modal.CloseCancel {
		t.Fatalf("CmdCancel: %+v", res)
	}

	// StartInFilter starts directly in ModePickerFilter.
	mf := modal.NewPaletteModal(palette.New("Palette", entries), modal.PaletteOptions{StartInFilter: true})
	if mf.Mode() != modal.ModePickerFilter || !mf.Model().Picker.Filtering {
		t.Fatalf("StartInFilter: mode=%q filtering=%v", mf.Mode(), mf.Model().Picker.Filtering)
	}
}
