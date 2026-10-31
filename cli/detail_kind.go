package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/ochazuke"
)

// DetailRecord is the default detail kind (CatalogDetail.Kind ""): the
// selected row's record as key: value lines.
const DetailRecord = "record"

// A detail kind draws a data page's detail as something other than the
// record: a catalog names the kind (CatalogDetail.Kind, "markdown") and
// the host registers what draws it (RegisterDetailKind, or
// PageConfig.DetailKinds for one mount). Built-in CatalogDetail.Sections
// need no factory. Other views, such as an editable file document, still
// use registered factories: markdown/cliview exports its factory and the
// host registers it. A page naming a kind the host has not registered
// shows the record, with a line saying so.
//
// The view is built once per mounted page (DetailFactory) and told the
// selected row whenever it changes (DetailView.Show): a different row, a
// row whose record changed after a reload, or no row. Its instances go
// under DetailHost.Parent, an instance that stands for the selected row:
// its noun is the row's host noun, its Data the row's own id (as a row
// instance's), its Label the row's label, so the row noun's verbs and keys
// answer from inside the view (rows nearer the focus, the view's own,
// answer first), acting on the row. A view whose instances hold a focus
// stop (a list) is a focus stop like any list of the page, in drawing
// order after the lists.

// DetailView is a mounted page's detail of a registered kind.
type DetailView interface {
	// Show points the view at the selected row (ok false: none is). It
	// runs on the loop when the row the detail follows changes, or its
	// record does.
	Show(row Row, ok bool) error
	// Block draws the view width by height cells (the detail panel's
	// content), placing its focus stops in the page's drawing order.
	Block(width, height int) ochazuke.Block
	// Focused reports whether the focus is inside the view (the panel's
	// border takes the accent).
	Focused() bool
}

// DetailHost is what a DetailFactory builds a view with.
type DetailHost struct {
	Model *ochazuke.Model
	// Page is the mounted page's id.
	Page string
	// Parent is the instance the view's own instances go under: it stands
	// for the selected row (noun, Data and Label follow it).
	Parent string
	// Spec is the page's detail as the catalog declares it.
	Spec CatalogDetail
}

// Field is the selected row's record field name as a string ("" when the
// record is not an object or lacks it): the view reads PathField with it.
func (h DetailHost) Field(row Row, name string) string {
	obj, ok := row.Data.(map[string]any)
	if !ok || name == "" {
		return ""
	}
	if s, ok := obj[name].(string); ok {
		return s
	}
	if v, ok := obj[name]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// DetailFactory builds the view of one mounted page's detail.
type DetailFactory func(DetailHost) (DetailView, error)

var detailKinds = struct {
	sync.RWMutex
	m map[string]DetailFactory
}{m: map[string]DetailFactory{}}

// RegisterDetailKind registers the factory of a detail kind for every
// page this process mounts (PageConfig.DetailKinds wins for one mount). A
// nil factory removes the kind. The record kind cannot be replaced.
func RegisterDetailKind(kind string, f DetailFactory) error {
	if kind == "" || kind == DetailRecord {
		return fmt.Errorf("detail kind %q is the record view", kind)
	}
	detailKinds.Lock()
	defer detailKinds.Unlock()
	if f == nil {
		delete(detailKinds.m, kind)
		return nil
	}
	detailKinds.m[kind] = f
	return nil
}

// DetailKinds lists the registered detail kinds, sorted (the record kind
// is always there and not listed).
func DetailKinds() []string {
	detailKinds.RLock()
	defer detailKinds.RUnlock()
	out := make([]string, 0, len(detailKinds.m))
	for k := range detailKinds.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// detailFactory is the factory of kind for cfg: the mount's own, else
// the registered one, else nil.
func detailFactory(cfg PageConfig, kind string) DetailFactory {
	if f, ok := cfg.DetailKinds[kind]; ok && f != nil {
		return f
	}
	detailKinds.RLock()
	defer detailKinds.RUnlock()
	return detailKinds.m[kind]
}

// isRecord reports whether a detail kind is the record view.
func isRecord(kind string) bool { return kind == "" || kind == DetailRecord }

// detailRowNoun is the noun of the instance a kind's view lives under
// while no row is selected.
const detailRowNoun = "ochazuke.detail-row"

// kindView is a mounted page's detail of a registered kind.
type kindView struct {
	view   DetailView
	parent string // the row stand-in the view lives under
	shown  string // the row instance shown, "" none
	record string // the shown row's record hash (RecordHash)
	err    string // the view's last Show error
}

// mountKind builds the page's detail kind view, or records why the record
// view stands in.
func (v *PageView) mountKind() error {
	kind := v.spec.Detail.Kind
	if isRecord(kind) {
		return nil
	}
	f := detailFactory(v.cfg, kind)
	if f == nil {
		v.kindNote = fmt.Sprintf("detail kind %q is not registered here; showing the record", kind)
		return nil
	}
	r := v.m.Runtime
	parent := v.detail.ID() + "/row"
	r.Define(detailRowNoun)
	// The stand-in's component follows the selection after every event
	// (the runtime drains every component's PendingCommand), so a key
	// that moves the selection and one that moves the focus into the view
	// before the next frame find the view on the new row.
	r.Instance(parent, ochazuke.Parent(v.detail.ID()), ochazuke.Kind(detailRowNoun)).Component = kindSync{v}
	view, err := f(DetailHost{Model: v.m, Page: v.spec.ID, Parent: parent, Spec: *v.spec.Detail})
	if err != nil {
		_ = r.RemoveInstance(parent)
		return fmt.Errorf("page %s: detail kind %s: %w", v.spec.ID, kind, err)
	}
	v.kind = &kindView{view: view, parent: parent}
	return nil
}

// kindSync is the row stand-in's component: it syncs the view after
// every event.
type kindSync struct{ v *PageView }

func (k kindSync) HandleMessage(tea.Msg) bool { return false }
func (k kindSync) Close()                     {}
func (k kindSync) PendingCommand() tea.Cmd {
	if k.v.kind != nil {
		k.v.syncKind(k.v.selectedID())
	}
	return nil
}

// syncKind tells the view the selected row when it changed: the stand-in
// instance takes the row's noun, id and label first.
func (v *PageView) syncKind(id string) {
	k := v.kind
	row, ok := v.rows[id]
	record := ""
	if ok {
		js, _ := json.Marshal(row)
		record = RecordHash(js)
	}
	if id == k.shown && record == k.record {
		return
	}
	k.shown, k.record = id, record
	if inst := v.m.Runtime.Instances[k.parent]; inst != nil {
		inst.Noun, inst.Data, inst.Label = detailRowNoun, nil, ""
		if ok {
			inst.Noun, inst.Data, inst.Label = v.rowNounOf(id), row.ID, row.Label
		}
	}
	k.err = ""
	if err := k.view.Show(row, ok); err != nil {
		k.err = err.Error()
	}
}

// rowNounOf is the host noun of a row instance: its list item's noun.
func (v *PageView) rowNounOf(id string) string {
	for _, pl := range v.lists {
		if slices.Contains(pl.rowIDs, id) {
			return pl.rowNoun
		}
	}
	return detailRowNoun
}

// kindPanel draws the detail of a registered kind.
func (v *PageView) kindPanel(layout ochazuke.Layout, width, height int) ochazuke.Block {
	k := v.kind
	id := v.selectedID()
	v.syncKind(id)
	cw, ch := ochazuke.PanelContentSize(width, height)
	title := "detail"
	if row, ok := v.rows[id]; ok {
		title = row.Label
		if v.spec.Detail.Title != "" {
			title = v.spec.Detail.Title + " · " + row.Label
		}
	} else if v.spec.Detail.Title != "" {
		title = v.spec.Detail.Title
	}
	var body ochazuke.Block
	if k.err != "" {
		body = ochazuke.Text(layout.Styles.Muted.Render(plainText(k.err)))
		k.view.Block(0, 0) // keeps its place in the drawing order
	} else {
		body = k.view.Block(cw, ch)
	}
	return layout.PanelBlock(title, body, width, height, k.view.Focused())
}

// DetailView is the page's detail view of a registered kind, or nil (the
// record view, or a kind the host did not register).
func (v *PageView) DetailView() DetailView {
	if v.kind == nil {
		return nil
	}
	return v.kind.view
}

// DetailParent is the instance a detail kind's view lives under (it
// stands for the selected row), "" for the record view.
func (v *PageView) DetailParent() string {
	if v.kind == nil {
		return ""
	}
	return v.kind.parent
}
