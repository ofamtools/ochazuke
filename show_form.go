package ochazuke

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The show form (noun scaffold b, an ochazuke.detail component placed as
// a page's detail): a ShowForm follows a list's selection and holds the
// selected thing's edit form (Runtime.EditSpec, read-only fields muted,
// commit on change, u undoes) with the relation sections and the history
// under it (a Related stack with History: ochazuke.relation-section). A
// generated index page has one; a hand-built page opts in with
// AddShowForm and draws it with Block under its own text (owner decision
// form-under-text). The form reads the thing once per selection: the
// caller's key is known (no probe), and the form keeps no watch of its
// own: a row its list reads again with other data reloads it once.
// Setting detail.show-form off draws only the page's text.

// ShowFormSpec is what a show form follows.
type ShowFormSpec struct {
	// List is the list whose selected row the form shows, read on every
	// sync.
	List func() *Collection
	// Target, when set, replaces List's selection with the thing to show
	// (a mounted page's selected row: catalog detail kind record-form).
	// Data names the row data it was read for ("" none), on every sync.
	Target func() (noun, key string)
	Data   func() string
	// Form is the form instance's id (default the show form's id + /form).
	Form string
	// Related replaces the native related/history stack with this spec.
	// The value is copied; its Target is replaced with the form's target,
	// so its Noun and Key are not used. All settings, row hooks, placed
	// views and History are otherwise unchanged. Nil uses the native
	// detail.related, detail.related-hidden and History: true defaults.
	Related *RelatedSpec
	// NoSections leaves all sections out, including Related when supplied.
	NoSections bool
}

// ShowForm is a page's show form: the record editor of the selected
// thing and its sections.
type ShowForm struct {
	r    *Runtime
	id   string
	spec ShowFormSpec
	// key and noun are the thing the form holds ("" none); data is the
	// selected row's data it was read for.
	noun, key, data string
	syncing         bool
	// again is set by a sync asked while one runs (the form's get is a
	// request, and a list read after it may move the selection): the
	// running sync runs once more when it ends.
	again   bool
	related *Related
}

// AddShowForm registers a show form under Parent (its page): the form
// follows spec.List's selection after every request. The wrapper's noun
// defaults to ochazuke.detail; Kind may name another declared noun.
func (r *Runtime) AddShowForm(id string, spec ShowFormSpec, opts ...InstanceOption) (*ShowForm, error) {
	if spec.List == nil && spec.Target == nil {
		return nil, fmt.Errorf("show form %s: no list or target to follow", id)
	}
	target := spec.Target
	if target == nil {
		target = r.SelectionTarget(spec.List)
	}
	spec.Target = target
	if spec.Form == "" {
		spec.Form = id + "/form"
	}
	is := instanceSpec{parent: "app", noun: detailNoun}
	for _, opt := range opts {
		opt(&is)
	}
	inst := r.Instance(id, Parent(is.parent), Kind(is.noun), FocusOnClose(is.closeFocus))
	sf := &ShowForm{r: r, id: id, spec: spec}
	inst.Component = sf
	if !spec.NoSections {
		parent := inst.Parent
		related := RelatedSpec{Enabled: RelatedDetailSetting, Hidden: RelatedDetailHiddenSetting, History: true}
		if spec.Related != nil {
			related = *spec.Related
		}
		related.Target = target
		rel, err := r.AddRelated(id+"/related", related, Parent(parent))
		if err != nil {
			_ = r.RemoveInstance(id)
			return nil, err
		}
		sf.related = rel
	}
	return sf, nil
}

// ShowFormOf is the show form registered under id, nil when none.
func (r *Runtime) ShowFormOf(id string) *ShowForm { return componentAt[*ShowForm](r, id) }

// Form is the record editor the show form holds now, nil when none.
func (sf *ShowForm) Form() *RecordEditor { return sf.r.RecordEditor(sf.spec.Form) }

// Related is the show form's sections, nil with NoSections.
func (sf *ShowForm) Related() *Related { return sf.related }

func (sf *ShowForm) HandleMessage(tea.Msg) bool { return false }
func (sf *ShowForm) PendingCommand() tea.Cmd    { return nil }
func (sf *ShowForm) Close()                     {}

// Sync gives the form the selected thing: made again when the selection
// names another thing, read again when its row's data changed. It runs
// after each request (the current page's show forms), never on a draw.
func (sf *ShowForm) Sync() {
	if sf.syncing {
		sf.again = true
		return
	}
	for range 3 { // a sync asked meanwhile runs again, a bounded number of times
		sf.syncing, sf.again = true, false
		sf.syncOnce()
		sf.syncing = false
		if !sf.again {
			return
		}
	}
}

func (sf *ShowForm) syncOnce() {
	r, formID := sf.r, sf.spec.Form
	var list *Collection
	noun, key, data := "", "", ""
	if sf.spec.List != nil {
		list = sf.spec.List()
	}
	if r.Settings().Bool(DetailShowFormSetting) {
		noun, key = sf.spec.Target()
		if sf.spec.Data != nil {
			data = sf.spec.Data()
		} else if list != nil {
			if item, ok := list.Selected(); ok {
				data = fmt.Sprint(item.Data)
			}
		}
	}
	if form := sf.Form(); noun == sf.noun && key == sf.key && (key == "" || form != nil) {
		if form != nil && data != sf.data {
			// The list read the thing again with other props (a write,
			// here or outside): the form reads it once more.
			sf.data = data
			_ = form.Reload()
		}
		return
	}
	if r.Instances[formID] != nil {
		if inside := slices.Contains(r.InstancePath(r.Focus), formID); inside {
			back := r.Instances[sf.id].Parent
			if list != nil {
				back = list.focusTarget()
			}
			r.changeFocus(back, focusMove{cause: focusSettle})
		}
		_ = r.RemoveInstance(formID)
	}
	sf.noun, sf.key = "", ""
	if key == "" {
		return
	}
	spec, err := r.EditSpec(noun, key)
	if err != nil {
		return // a noun the form cannot hold keeps the page's text
	}
	spec.Watch, spec.WatchArgs = "", nil
	parent := r.Instances[sf.id].Parent
	if _, err := r.AddRecordEditor(formID, spec, Parent(parent)); err == nil {
		sf.noun, sf.key, sf.data = noun, key, data
	}
}

// Block draws the show form under text (the page's own detail, "" none):
// the text up to two fifths of the height, then the form at its natural
// height up to three fifths of what is left with the sections under it.
// With detail.show-form off, or no form, it is the text alone.
func (sf *ShowForm) Block(text string, width, height int) Block {
	form := sf.Form()
	if form == nil || form.Closed() || !sf.r.Settings().Bool(DetailShowFormSetting) {
		return Text(text)
	}
	if text == "" {
		return sf.r.formWithSections(form, sf.related, width, height)
	}
	lines := strings.Split(text, "\n")
	top := min(len(lines), height*2/5)
	body := sf.r.formWithSections(form, sf.related, width, height-top-1)
	if body.Cursor != nil {
		c := *body.Cursor
		c.Y += top + 1
		body.Cursor = &c
	}
	return Block{Text: strings.Join(lines[:top], "\n") + "\n\n" + body.Text, Cursor: body.Cursor}
}

// Holds reports whether the focus is inside the show form's form.
func (sf *ShowForm) Holds(focus string) bool {
	return slices.Contains(sf.r.InstancePath(focus), sf.spec.Form)
}

// syncShowForms syncs every show form of the current page.
func (r *Runtime) syncShowForms() {
	for _, inst := range r.Children(r.Page) {
		if sf, ok := inst.Component.(*ShowForm); ok {
			sf.Sync()
		}
	}
}
