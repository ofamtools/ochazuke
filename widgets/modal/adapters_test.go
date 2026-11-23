package modal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/commandline"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/markdown"
	"ofam.dev/ochazuke/widgets/modal"
	"ofam.dev/ochazuke/widgets/palette"
	"ofam.dev/ochazuke/widgets/pathpicker"
	"ofam.dev/ochazuke/widgets/picker"
)

func TestOverlayFactMirroringAndKeymap(t *testing.T) {
	inner := ui.ViewNode{
		ID:        "child.inner",
		Kind:      ui.KindPicker,
		Scope:     "picker",
		Focusable: true,
		Facts:     map[string]any{"query": "abc", "shared": "from-child"},
	}
	ov := modal.Overlay(modal.OverlaySpec{
		ID:        "test",
		Scope:     "custom.scope",
		Title:     "Test",
		WidthPct:  65,
		HeightPct: 80,
		Facts:     map[string]any{"shared": "from-spec", "extra": true},
		Children:  []ui.ViewNode{inner},
	})

	if ov.ID != "modal.test" || ov.Kind != ui.KindOverlay || ov.Scope != "custom.scope" {
		t.Fatalf("unexpected overlay header: id=%q kind=%q scope=%q", ov.ID, ov.Kind, ov.Scope)
	}
	if ov.Props["widthPct"] != 65 || ov.Props["heightPct"] != 80 {
		t.Fatalf("unexpected overlay props: %+v", ov.Props)
	}
	if ov.Facts["query"] != "abc" || ov.Facts["shared"] != "from-spec" || ov.Facts["extra"] != true {
		t.Fatalf("unexpected mirrored facts: %+v", ov.Facts)
	}

	// Verify scope-targeted binding on the overlay scope sees mirrored inner facts.
	km, err := core.NewKeymap(
		[]core.Mode{{Name: modal.ModePicker}},
		[]core.Binding{{
			Scope:   "custom.scope",
			Mode:    modal.ModePicker,
			Keys:    []string{"enter"},
			When:    `facts.query == "abc" && facts.shared == "from-spec"`,
			Command: "test.match",
		}},
	)
	if err != nil {
		t.Fatalf("NewKeymap: %v", err)
	}
	chain := ui.ContextChain(ov, "child.inner", nil)
	res := km.Resolve(core.KeyQuery{Chain: chain, Mode: modal.ModePicker, Key: "enter", Modal: "test"})
	if res.Outcome != core.Matched || res.Command == nil || res.Command.ID != "test.match" {
		t.Fatalf("expected binding to match mirrored child fact, got outcome=%v cmd=%+v", res.Outcome, res.Command)
	}

	// Scope fallback: Scope -> ID -> "modal".
	if got := modal.Overlay(modal.OverlaySpec{ID: "foo"}).Scope; got != "foo" {
		t.Fatalf("Scope fallback to ID: got %q", got)
	}
	if got := modal.Overlay(modal.OverlaySpec{}).Scope; got != "modal" {
		t.Fatalf("Scope fallback to modal: got %q", got)
	}
}

func TestPickerModalAdapter(t *testing.T) {
	items := []picker.Item{
		{ID: "p1", Title: "Alpha"},
		{ID: "p2", Title: "Beta"},
	}
	var confirmed picker.Item
	var created string
	var closedReason modal.CloseReason
	var hooked bool

	pm := picker.New("Projects", items)
	m := modal.NewPickerModal(pm, modal.PickerOptions{
		ID:     "projects",
		Create: &modal.PickerCreateHook{Label: "Project"},
		OnCommand: func(_ *picker.Model, cmd core.Command) (modal.Result, bool) {
			if cmd.ID == "custom.hook" {
				hooked = true
				return modal.Result{Handled: true, Mode: "inspect"}, true
			}
			return modal.Result{}, false
		},
		OnConfirm: func(it picker.Item) modal.Result {
			confirmed = it
			return modal.Result{Close: true, CloseReason: modal.CloseSubmit}
		},
		OnCreate: func(q string) modal.Result {
			created = q
			return modal.Result{Close: true, CloseReason: modal.CloseSubmit}
		},
		OnClose: func(r modal.CloseReason) { closedReason = r },
	})

	// First-chance OnCommand hook & SetMode.
	if res := m.Handle(core.Command{ID: "custom.hook"}); !hooked || !res.Handled || res.Mode != "inspect" {
		t.Fatalf("OnCommand hook failed: %+v", res)
	}
	m.SetMode("inspect")
	if m.Mode() != "inspect" {
		t.Fatalf("SetMode failed: got %q", m.Mode())
	}
	m.SetMode(modal.ModePicker)

	// Scroll motions map to motion.down / motion.up.
	m.Handle(core.Command{ID: "motion.scroll.down"})
	if m.Model().SelectedIdx != 1 {
		t.Fatalf("expected motion.scroll.down -> SelectedIdx 1, got %d", m.Model().SelectedIdx)
	}
	m.Handle(core.Command{ID: "motion.scroll.up"})
	if m.Model().SelectedIdx != 0 {
		t.Fatalf("expected motion.scroll.up -> SelectedIdx 0, got %d", m.Model().SelectedIdx)
	}

	// Typing a new query appends the "+ Create new" row and confirming triggers OnCreate.
	m.Handle(core.Command{ID: modal.CmdPickerInsert, Args: map[string]any{"text": "Gamma"}})
	filtered := m.Model().Filtered()
	if len(filtered) != 1 || filtered[0].ID != modal.PickerCreateItemID {
		t.Fatalf("expected single create row, got %+v", filtered)
	}
	res := m.Handle(core.Command{ID: picker.CmdConfirm})
	if !res.Close || res.CloseReason != modal.CloseSubmit || created != "Gamma" {
		t.Fatalf("expected OnCreate(\"Gamma\"), got created=%q res=%+v", created, res)
	}

	// Backspacing to empty removes the create row; exact match hides create row.
	for range len("Gamma") {
		m.Handle(core.Command{ID: modal.CmdPickerBackspace})
	}
	if len(m.Model().Filtered()) != 2 {
		t.Fatalf("expected base items restored, got %d", len(m.Model().Filtered()))
	}
	m.Handle(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "alpha"}})
	if len(m.Model().Filtered()) != 1 || m.Model().Filtered()[0].ID != "p1" {
		t.Fatalf("exact match should suppress create row, got %+v", m.Model().Filtered())
	}

	// Confirm existing item.
	res = m.Handle(core.Command{ID: picker.CmdConfirm})
	if !res.Close || confirmed.ID != "p1" {
		t.Fatalf("expected OnConfirm p1, got %+v res=%+v", confirmed, res)
	}

	// SetItems updates base items cleanly.
	m.SetItems([]picker.Item{{ID: "p3", Title: "Delta"}})
	if len(m.Model().Items) != 2 { // query is still "alpha" so Delta + create row
		t.Fatalf("expected SetItems to re-sync create row for active query, got %+v", m.Model().Items)
	}

	// Cancel & OnClose.
	if res := m.Handle(core.Command{ID: modal.CmdCancel}); !res.Close || res.CloseReason != modal.CloseCancel {
		t.Fatalf("CmdCancel failed: %+v", res)
	}
	m.OnClose(modal.CloseCancel)
	if closedReason != modal.CloseCancel {
		t.Fatalf("OnClose not invoked")
	}
}

func TestFormModalAdapter(t *testing.T) {
	specs := []core.ParamSpec{
		{Name: "title", Type: core.ParamString, Required: true},
		{Name: "enabled", Type: core.ParamBool},
		{Name: "dir", Type: core.ParamPath},
		{Name: "owner", Type: core.ParamRef, RefKinds: []string{"user"}},
	}
	fm := form.New("Edit Item", specs, map[string]any{"title": "Old", "enabled": false})
	fm.SubmitRow = true
	fm.SetDrafts([]form.Draft{{Values: map[string]any{"title": "Draft 1", "enabled": true}, SavedAt: time.Unix(100, 0)}})

	var changed []string
	var pickPathField string
	var pickRefField string
	var discardedIdx = -1
	var submitted map[string]any

	m := modal.NewFormModal(fm, modal.FormOptions{
		ID:          "edit",
		DraftBadges: true,
		RenderBody: func(f *form.Model, focusID string) ui.ViewNode {
			return ui.ViewNode{
				Kind:     ui.KindPanel,
				Title:    "Panel Title",
				Scope:    "panel.scope",
				Badges:   []ui.Badge{{Text: "custom"}},
				Children: []ui.ViewNode{f.View(focusID)},
			}
		},
		OnChange: func(_ *form.Model, field string) modal.Result {
			changed = append(changed, field)
			return modal.Result{Status: "changed:" + field, SetStatus: true}
		},
		OnPickPath: func(req form.PathRequest, _ any) modal.Result {
			pickPathField = req.Field
			return modal.Result{Status: "pick-path", SetStatus: true}
		},
		OnPickRef: func(req form.RefRequest) modal.Result {
			pickRefField = req.Field
			return modal.Result{Status: "pick-ref", SetStatus: true}
		},
		OnDraftDiscard: func(idx int) modal.Result {
			discardedIdx = idx
			return modal.Result{Status: "dropped", SetStatus: true}
		},
		OnSubmit: func(vals map[string]any) modal.Result {
			submitted = vals
			return modal.Result{Close: true, CloseReason: modal.CloseSubmit}
		},
	})

	// Activate text field -> enters EditMode; stop -> returns to NormalMode and fires OnChange.
	res := m.Handle(core.Command{ID: modal.CmdFormActivate})
	if m.Mode() != modal.ModeFormEdit || res.Mode != modal.ModeFormEdit {
		t.Fatalf("expected ModeFormEdit after activating text field, got mode=%q res=%+v", m.Mode(), res)
	}
	m.Handle(core.Command{ID: form.CmdInsert, Args: map[string]any{"text": "!"}})
	res = m.Handle(core.Command{ID: form.CmdStop})
	if m.Mode() != modal.ModeForm || res.Mode != modal.ModeForm || len(changed) != 1 || changed[0] != "title" {
		t.Fatalf("expected ModeForm + OnChange(title) after CmdStop, got mode=%q changed=%v res=%+v", m.Mode(), changed, res)
	}

	// FocusField + activate bool -> toggles and fires OnChange.
	if !m.FocusField("enabled") {
		t.Fatalf("FocusField(enabled) returned false")
	}
	res = m.Handle(core.Command{ID: modal.CmdFormActivate})
	if fm.Values["enabled"] != true || len(changed) != 2 || changed[1] != "enabled" {
		t.Fatalf("expected bool toggle + OnChange(enabled), got val=%v changed=%v", fm.Values["enabled"], changed)
	}

	// Activate path and ref fields.
	m.FocusField("dir")
	m.Handle(core.Command{ID: modal.CmdFormActivate})
	if pickPathField != "dir" {
		t.Fatalf("expected OnPickPath(dir), got %q", pickPathField)
	}
	m.FocusField("owner")
	m.Handle(core.Command{ID: modal.CmdFormActivate})
	if pickRefField != "owner" {
		t.Fatalf("expected OnPickRef(owner), got %q", pickRefField)
	}

	// SetValue updates field and fires OnChange.
	if !m.SetValue("dir", "/tmp") || fm.Values["dir"] != "/tmp" || changed[len(changed)-1] != "dir" {
		t.Fatalf("SetValue failed: val=%v changed=%v", fm.Values["dir"], changed)
	}

	// Draft prev + View badges + discard.
	res = m.Handle(core.Command{ID: form.CmdDraftPrev})
	if fm.ActiveDraftIndex() != 0 || !strings.Contains(res.Status, "draft 1 of 1") {
		t.Fatalf("CmdDraftPrev failed: idx=%d res=%+v", fm.ActiveDraftIndex(), res)
	}
	ov := m.View(100, 30)
	if ov.Title != "Panel Title" || ov.Scope != "panel.scope" || len(ov.Badges) < 2 {
		t.Fatalf("FormModal View unexpected: title=%q scope=%q badges=%+v", ov.Title, ov.Scope, ov.Badges)
	}
	res = m.Handle(core.Command{ID: form.CmdDraftDiscard})
	if discardedIdx != 0 || res.Status != "dropped" {
		t.Fatalf("CmdDraftDiscard failed: idx=%d res=%+v", discardedIdx, res)
	}

	// Submit via bottom SubmitRow + CmdFormActivate.
	m.Handle(core.Command{ID: "motion.bottom"})
	res = m.Handle(core.Command{ID: modal.CmdFormActivate})
	if !res.Close || submitted["title"] != "Old!" {
		t.Fatalf("Submit via SubmitRow failed: submitted=%+v res=%+v", submitted, res)
	}

	// DisableDrafts and DisableSubmit guard.
	settings := modal.NewFormModal(form.New("Settings", specs[:2], nil), modal.FormOptions{
		ID:            "settings",
		DisableDrafts: true,
		DisableSubmit: true,
	})
	if r := settings.Handle(core.Command{ID: form.CmdSubmit}); !r.Handled || r.Close {
		t.Fatalf("DisableSubmit should ignore submit: %+v", r)
	}
	if r := settings.Handle(core.Command{ID: form.CmdDraftPrev}); !r.Handled || r.SetStatus {
		t.Fatalf("DisableDrafts should ignore draft commands: %+v", r)
	}
}

func TestConfirmModalAdapter(t *testing.T) {
	specs := modal.ConfirmSpecs()
	if len(specs) != 2 || specs[0].ID != modal.CmdConfirmAccept || specs[1].ID != modal.CmdConfirmCancel {
		t.Fatalf("unexpected ConfirmSpecs: %+v", specs)
	}

	accepted := false
	m := modal.NewConfirmModal(modal.ConfirmOptions{
		ID:           "delete",
		Title:        "Delete Task",
		Message:      "Delete permanently?",
		ConfirmLabel: "Delete",
		CancelLabel:  "Keep",
		Tone:         "error",
		OnConfirm: func() modal.Result {
			accepted = true
			return modal.Result{Status: "deleted", SetStatus: true}
		},
	})

	ov := m.View(80, 24)
	if ov.Tone != "error" || ov.Title != "Delete Task" || len(ov.Children) != 1 || ov.Children[0].Kind != ui.KindVStack {
		t.Fatalf("unexpected confirm overlay: %+v", ov)
	}
	hintText, _ := ov.Children[0].Children[1].Props["text"].(string)
	if hintText != "[y/enter] Delete   [n/esc] Keep" {
		t.Fatalf("unexpected confirm hint text: %q", hintText)
	}

	res := m.Handle(core.Command{ID: modal.CmdConfirmAccept})
	if !accepted || !res.Handled || !res.Close || res.CloseReason != modal.CloseSubmit || res.Status != "deleted" {
		t.Fatalf("unexpected accept result: accepted=%v res=%+v", accepted, res)
	}

	res = m.Handle(core.Command{ID: modal.CmdConfirmCancel})
	if !res.Handled || !res.Close || res.CloseReason != modal.CloseCancel || res.Status != "cancelled" {
		t.Fatalf("unexpected cancel result: %+v", res)
	}
}

func TestMarkdownModalAdapter(t *testing.T) {
	src := "# One\n\nSee [Guide](guide.md).\n\n```go\nfmt.Println(1)\n```\n"
	var opened, yanked string
	m := modal.NewMarkdownModal(markdown.New("README", src), modal.MarkdownOptions{
		Path:            "/docs/README.md",
		ShowStatsBadges: true,
		OnOpenLink: func(href string) modal.Result {
			opened = href
			return modal.Result{Status: "open:" + href, SetStatus: true}
		},
		OnYankCode: func(code string) modal.Result {
			yanked = code
			return modal.Result{Status: "yanked", SetStatus: true}
		},
	})

	ov := m.View(120, 40)
	if m.Model().PageSize != 40*90/100-6 {
		t.Fatalf("expected dynamic PageSize=%d, got %d", 40*90/100-6, m.Model().PageSize)
	}
	if len(ov.Badges) < 4 {
		t.Fatalf("expected stats + path badges, got %+v", ov.Badges)
	}

	m.Handle(core.Command{ID: markdown.CmdLinkOpen})
	m.Handle(core.Command{ID: markdown.CmdCodeYank})
	if opened != "guide.md" || yanked != "fmt.Println(1)" {
		t.Fatalf("expected link/code callbacks, got opened=%q yanked=%q", opened, yanked)
	}

	// Navigate & Back stack.
	m.Model().Offset = 2
	m.Navigate("Guide", "# Guide\nPlain text\n", "/docs/guide.md")
	if m.Path() != "/docs/guide.md" || m.BackLen() != 1 || m.Model().Offset != 0 {
		t.Fatalf("Navigate state mismatch: path=%q back=%d off=%d", m.Path(), m.BackLen(), m.Model().Offset)
	}
	if res := m.Handle(core.Command{ID: markdown.CmdLinkOpen}); res.Status != "no link here to open" {
		t.Fatalf("expected empty-link status, got %+v", res)
	}
	if !m.Back() || m.Path() != "/docs/README.md" || m.BackLen() != 0 || m.Model().Offset != 2 {
		t.Fatalf("Back restore mismatch: path=%q back=%d off=%d", m.Path(), m.BackLen(), m.Model().Offset)
	}
	if m.Back() {
		t.Fatalf("Back on empty history should return false")
	}
}

func TestPaletteModalAdapter(t *testing.T) {
	entries := []palette.Entry{
		{
			Spec: core.CommandSpec{ID: "plain.run", Description: "No args"},
			Item: picker.Item{ID: "plain.run", Title: "Plain Run"},
		},
		{
			Spec: core.CommandSpec{
				ID: "task.create",
				Params: []core.ParamSpec{
					{Name: "title", Type: core.ParamString, Required: true},
					{Name: "dir", Type: core.ParamPath},
				},
			},
			Item: picker.Item{ID: "task.create", Title: "Create Task"},
		},
	}

	var rawCmd string
	var pickPathField string
	m := modal.NewPaletteModal(palette.New("Command Palette", entries), modal.PaletteOptions{
		OnRawQuery: func(q string) (modal.Result, bool) {
			if rest, ok := strings.CutPrefix(q, "!"); ok {
				rawCmd = strings.TrimSpace(rest)
				return modal.Result{Close: true, CloseReason: modal.CloseSubmit}, true
			}
			return modal.Result{}, false
		},
		OnPickPath: func(req form.PathRequest, _ any) modal.Result {
			pickPathField = req.Field
			return modal.Result{Status: "pick:" + req.Field, SetStatus: true}
		},
	})

	// Entry 0 has no params -> default width 70.
	if w := m.View(120, 40).Props["widthPct"]; w != 70 {
		t.Fatalf("expected widthPct 70 without ActiveForm, got %v", w)
	}

	// Move down to task.create -> ActiveForm exists -> expanded width 90.
	m.Handle(core.Command{ID: "motion.down"})
	if w := m.View(120, 40).Props["widthPct"]; w != 90 {
		t.Fatalf("expected widthPct 90 with ActiveForm, got %v", w)
	}

	// Toggle focus to form -> ModeForm; activate text field -> ModeFormEdit; stop -> ModeForm.
	res := m.Handle(core.Command{ID: palette.CmdToggleFocus})
	if m.Mode() != modal.ModeForm || res.Mode != modal.ModeForm || res.FocusID != "palette.form" {
		t.Fatalf("expected ModeForm after toggle, got mode=%q res=%+v", m.Mode(), res)
	}
	res = m.Handle(core.Command{ID: modal.CmdFormActivate})
	if m.Mode() != modal.ModeFormEdit || res.Mode != modal.ModeFormEdit {
		t.Fatalf("expected ModeFormEdit after activate, got mode=%q res=%+v", m.Mode(), res)
	}
	m.Handle(core.Command{ID: form.CmdInsert, Args: map[string]any{"text": "Ship A1"}})
	m.Handle(core.Command{ID: form.CmdStop})

	// Activate path field inside palette form and apply picked value via SetPickedValue.
	m.Handle(core.Command{ID: "motion.down"})
	m.Handle(core.Command{ID: modal.CmdFormActivate})
	if pickPathField != "dir" {
		t.Fatalf("expected OnPickPath(dir), got %q", pickPathField)
	}
	if !m.SetPickedValue("dir", "/repo") {
		t.Fatalf("SetPickedValue failed")
	}

	// Submit emits core.Command with Source: "palette".
	res = m.Handle(core.Command{ID: form.CmdSubmit})
	if !res.Close || res.CloseReason != modal.CloseSubmit || len(res.Emit) != 1 {
		t.Fatalf("expected default Emit on submit, got %+v", res)
	}
	emitted := res.Emit[0]
	if emitted.ID != "task.create" || emitted.Source != "palette" || emitted.Args["title"] != "Ship A1" || emitted.Args["dir"] != "/repo" {
		t.Fatalf("unexpected emitted command: %+v", emitted)
	}

	// Raw "!cmd" query handling when picker is focused.
	m.Handle(core.Command{ID: palette.CmdFocusList})
	m.Handle(core.Command{ID: modal.CmdPickerInsert, Args: map[string]any{"text": "!echo hi"}})
	res = m.Handle(core.Command{ID: picker.CmdConfirm})
	if !res.Close || rawCmd != "echo hi" {
		t.Fatalf("expected OnRawQuery to run \"echo hi\", got rawCmd=%q res=%+v", rawCmd, res)
	}
}

type memFS struct {
	dirs map[string][]pathpicker.DirEntry
	home string
}

func (f memFS) ReadDir(dir string) ([]pathpicker.DirEntry, error) {
	es, ok := f.dirs[filepath.Clean(dir)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return es, nil
}
func (f memFS) ReadPreview(string, int) (string, error) { return "preview", nil }
func (f memFS) HomeDir() (string, error)                { return f.home, nil }

func TestPathPickerModalAdapter(t *testing.T) {
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
		t.Fatalf("pathpicker.New: %v", err)
	}
	var picked pathpicker.DirEntry
	m := modal.NewPathPickerModal(pp, modal.PathPickerOptions{
		OnPick: func(e pathpicker.DirEntry) modal.Result {
			picked = e
			return modal.Result{Close: true, CloseReason: modal.CloseSubmit}
		},
	})

	if m.Scope() != "pathpicker" {
		t.Fatalf("expected default scope pathpicker, got %q", m.Scope())
	}
	ov := m.View(100, 30)
	if ov.Facts["dir"] != "/work" || ov.Facts["pickMode"] != string(pathpicker.PickAny) {
		t.Fatalf("expected mirrored pathpicker facts on overlay, got %+v", ov.Facts)
	}

	// Drill into /work/sub (initial highlight is first real entry: sub/).
	m.Handle(core.Command{ID: pathpicker.CmdOpen})
	if m.Model().Dir != "/work/sub" {
		t.Fatalf("expected drill into /work/sub, got %q", m.Model().Dir)
	}

	// Filter and pick deep.go.
	m.Handle(core.Command{ID: modal.CmdPickerInsert, Args: map[string]any{"text": "deep"}})
	res := m.Handle(core.Command{ID: pathpicker.CmdOpen})
	if !res.Close || picked.Path != "/work/sub/deep.go" {
		t.Fatalf("expected pick /work/sub/deep.go, got picked=%+v res=%+v", picked, res)
	}

	if res := m.Handle(core.Command{ID: modal.CmdCancel}); !res.Close || res.CloseReason != modal.CloseCancel {
		t.Fatalf("CmdCancel failed: %+v", res)
	}
}

func TestCommandLineModalAdapter(t *testing.T) {
	cl := commandline.New(commandline.PrefixShell)
	cl.Vars = map[string]string{
		"REF":  "task:42",
		"FILE": "/very/long/path/that/exceeds/twenty/eight/characters.md",
	}

	var submitted commandline.Submission
	cancelled := false
	m := modal.NewCommandLineModal(cl, modal.CommandLineOptions{
		VarHintKeys: []string{"REF", "FILE", "MISSING"},
		OnSubmit: func(sub commandline.Submission) modal.Result {
			submitted = sub
			return modal.Result{Close: true, CloseReason: modal.CloseSubmit}
		},
		OnCancel: func() modal.Result {
			cancelled = true
			return modal.Result{Status: "aborted", SetStatus: true}
		},
	})

	ov := m.View(100, 20)
	if ov.Title != "Shell (!)" || len(ov.Children) != 2 || ov.Children[1].Tone != "muted" {
		t.Fatalf("unexpected cmdline overlay: title=%q children=%+v", ov.Title, ov.Children)
	}
	hint, _ := ov.Children[1].Props["text"].(string)
	if !strings.Contains(hint, "$REF=task:42") || !strings.Contains(hint, "$FILE=") || !strings.Contains(hint, "…") {
		t.Fatalf("unexpected var hints: %q", hint)
	}

	m.Handle(core.Command{ID: commandline.CmdInsert, Args: map[string]any{"text": "echo $REF"}})
	res := m.Handle(core.Command{ID: commandline.CmdSubmit})
	if !res.Close || res.CloseReason != modal.CloseSubmit || submitted.ShellCmd != "echo 'task:42'" {
		t.Fatalf("unexpected submit result: sub=%+v res=%+v", submitted, res)
	}

	res = m.Handle(core.Command{ID: modal.CmdCancel})
	if !cancelled || !res.Close || res.CloseReason != modal.CloseCancel || res.Status != "aborted" {
		t.Fatalf("unexpected cancel result: cancelled=%v res=%+v", cancelled, res)
	}
}
