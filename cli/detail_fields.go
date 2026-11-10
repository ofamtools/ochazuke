package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
)

//go:embed detail_sections.json
var sectionBindings []byte

const detailFieldsID = "__catalog-detail-fields"
const detailFieldNoun = "ochazuke.detail-field"

type detailFieldValue struct{ label, full string }

// mountSections keeps page-specific state on the detail instance, leaving
// the runtime's Detail component and J/K contract intact.
func (v *PageView) mountSections() {
	r := v.m.Runtime
	r.Instances[v.detail.ID()].Data = v
	if _, ok := r.Operation("ochazuke.detail.fields"); ok {
		return
	}
	r.Builtin(func() {
		r.DefineGuard("catalog-detail-fields", func(r *ochazuke.Runtime, e ochazuke.KeyEvent) bool {
			v := sectionPage(r, e.Focus)
			return r.EventTopModal(e) == "" && v != nil && v.hasMetadata() && r.Settings().Bool("detail.sections")
		})
		r.Define("ochazuke.detail").Verb("fields", func(c ochazuke.Ctx) ochazuke.Result {
			v := sectionPage(c.Runtime, c.Runtime.Focus)
			if v == nil {
				return ochazuke.Failed(errors.New("no section detail on this page"))
			}
			return v.openFields()
		}, "Show the detail's metadata fields for copying and expansion").When("catalog-detail-fields").Fetch()
		r.Define(detailFieldNoun).
			Verb("show", func(c ochazuke.Ctx) ochazuke.Result {
				if c.Instance == nil {
					return ochazuke.Failed(errors.New("no metadata field selected"))
				}
				value, ok := c.Instance.Data.(detailFieldValue)
				if !ok {
					return ochazuke.Failed(errors.New("no metadata field selected"))
				}
				_, err := c.Runtime.OpenTextView(value.label, value.full)
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Done(nil)
			}, "Expand the complete metadata value in a read-only text view").Fetch().
			Verb("yank", func(c ochazuke.Ctx) ochazuke.Result {
				if c.Instance == nil {
					return ochazuke.Failed(errors.New("no metadata field selected"))
				}
				value, ok := c.Instance.Data.(detailFieldValue)
				if !ok {
					return ochazuke.Failed(errors.New("no metadata field selected"))
				}
				if err := c.Runtime.StoreYank(ochazuke.Yank{Text: value.full, Shape: "char"}, ochazuke.StringArg(c.Args, "register", ""), false); err != nil {
					return ochazuke.Failed(err)
				}
				c.Info("Copied " + value.label)
				return ochazuke.Done(value.full)
			}, "Copy the complete metadata value").Fetch()
		op, _ := r.Operation(detailFieldNoun + ".yank")
		op.Parameters = map[string]ochazuke.Parameter{"register": {Type: "string", Description: "Register to copy into (default active register)"}}
		r.Operations.Set(op)
		r.MustLoadDefaultBindings(ochazuke.FrameworkBindingsSource+"cli/detail_sections.json", sectionBindings)
	})
}

func sectionPage(r *ochazuke.Runtime, focus string) *PageView {
	for _, id := range r.Ancestry(focus) {
		if r.PageOf(id) != nil {
			if d := r.Instances[id+"/detail"]; d != nil {
				v, _ := d.Data.(*PageView)
				return v
			}
		}
	}
	return nil
}

func (v *PageView) metadata() []detailFieldValue {
	row, ok := v.rows[v.selectedID()]
	if !ok {
		return nil
	}
	var fields []detailFieldValue
	for _, s := range v.spec.Detail.Sections {
		if s.Kind != "fields" || s.Region == "side" && !v.sideVisible() {
			continue
		}
		for _, f := range s.Fields {
			if value := detailValue(row.Data, f.Field); value != nil {
				_, full := formatDetail(value, f.Format, v.m.Now())
				fields = append(fields, detailFieldValue{f.Label, full})
			}
		}
	}
	return fields
}

// Hints and key availability only need existence, not formatted values.
func (v *PageView) hasMetadata() bool {
	row, ok := v.rows[v.selectedID()]
	if !ok {
		return false
	}
	for _, s := range v.spec.Detail.Sections {
		if s.Kind != "fields" || s.Region == "side" && !v.sideVisible() {
			continue
		}
		for _, f := range s.Fields {
			if detailValue(row.Data, f.Field) != nil {
				return true
			}
		}
	}
	return false
}

func (v *PageView) openFields() ochazuke.Result {
	r := v.m.Runtime
	inst, err := r.OpenModal(detailFieldsID, ochazuke.Payload{})
	if err != nil {
		return ochazuke.Failed(err)
	}
	var rows []ochazuke.ListItem
	for i, f := range v.metadata() {
		rows = append(rows, ochazuke.ListItem{ID: fmt.Sprintf("%s/%d", detailFieldsID, i), Noun: detailFieldNoun, Label: f.label, Cells: []string{f.label, plainText(strings.ReplaceAll(f.full, "\n", " "))}, Data: f})
	}
	list, err := r.AddList(detailFieldsID+"/list", rows, ochazuke.Parent(inst.ID), ochazuke.NoMarks(), ochazuke.Columns(ochazuke.Column{Title: "Field", Pinned: true}, ochazuke.Column{Title: "Value"}))
	if err != nil {
		_ = r.Pop()
		return ochazuke.Failed(err)
	}
	inst.Component = &detailFieldsModal{list}
	if err := r.SetFocus(list.FocusTarget()); err != nil {
		_ = r.Pop()
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(nil)
}

type detailFieldsModal struct{ list *ochazuke.Collection }

func (m *detailFieldsModal) ModalTitle() string { return "Detail fields" }
func (m *detailFieldsModal) ModalBlock(_ ochazuke.Styles, w, h int) ochazuke.Block {
	return ochazuke.Text(m.list.View(w, h))
}
func (m *detailFieldsModal) HandleMessage(tea.Msg) bool { return false }
func (m *detailFieldsModal) PendingCommand() tea.Cmd    { return nil }
func (m *detailFieldsModal) Close()                     {}

// mountRelated makes the stack of a related detail section: it follows
// the selected row of the followed list, whatever its noun.
func (v *PageView) mountRelated() error {
	if v.related != nil {
		return nil
	}
	list := func() *ochazuke.Collection {
		v.follow()
		return v.lists[v.current].list
	}
	spec := ochazuke.RelatedSpec{Target: v.m.Runtime.SelectionTarget(list), Enabled: ochazuke.RelatedDetailSetting, Hidden: ochazuke.RelatedDetailHiddenSetting}
	if c := v.cfg.Related; c != nil {
		if c.Hidden != "" {
			spec.Hidden = c.Hidden
		}
		spec.Empty = c.Empty
	}
	rel, err := v.m.Runtime.AddRelated(v.detail.ID()+"/related", spec, ochazuke.Parent(v.detail.ID()))
	v.related = rel
	return err
}

// withHostRelated is text with the host's related stack (PageConfig.Related)
// under it, a blank line between; text as it is when the host asked for none,
// the stack is empty or off, or the detail declares a related section of
// its own.
func (v *PageView) withHostRelated(text string, width int) string {
	stack := v.hostRelated(width)
	switch {
	case stack == "":
		return text
	case text == "":
		return stack
	}
	return text + "\n\n" + stack
}

// hostRelated is the host's related stack drawn width cells wide, "" when
// there is none to draw.
func (v *PageView) hostRelated(width int) string {
	if v.cfg.Related == nil || v.related == nil || v.declaresRelated() {
		return ""
	}
	return v.related.Text(width)
}

// declaresRelated reports whether the detail's own sections include a
// related one, which draws the stack itself.
func (v *PageView) declaresRelated() bool {
	if v.spec.Detail == nil {
		return false
	}
	for _, s := range v.spec.Detail.Sections {
		if s.Kind == "related" {
			return true
		}
	}
	return false
}
