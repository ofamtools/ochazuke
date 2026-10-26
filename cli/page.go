package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// CatalogPage is a page in registration order. A page drawn by Go code
// (ochazuke.AddPage) is only its id and title; a data page (DeclarePage)
// also carries what draws it, so a host builds it from the catalog with
// MountPage and no Go code of the extension's:
//
//	{id: "ai", title: "AI", layout: "groups",
//	 lists: [{id: "ai/skills", noun: "skill"}, {id: "ai/tools", noun: "mcp-tool", source: "mcp-tool.list", title: "tools"}],
//	 detail: {title: "record", fields: ["name", "path"]}}
//
// Layout is how the lists share the page (PageLayouts): stack (default),
// split or groups. Each list shows the rows of its noun's row source
// (RowSource), or of Source (NOUN.VERB) when given; a noun with neither
// lists its verbs, enter running each. Detail, when present, shows the
// selected row's record (Row.Data) as key: value lines, in Fields order
// when given (else sorted keys), scrolled with J/K. Widgets
// (CatalogWidget: a key/value panel, a status line, a table) are drawn
// above the lists from their sources' answers.
type CatalogPage struct {
	ID     string         `json:"id"`
	Title  string         `json:"title"`
	Layout string         `json:"layout,omitempty"`
	Lists  []CatalogList  `json:"lists,omitempty"`
	Detail *CatalogDetail `json:"detail,omitempty"`
	// Widgets are drawn above the lists, in order (CatalogWidget).
	Widgets []CatalogWidget `json:"widgets,omitempty"`
}

// CatalogList is one list of a data page.
type CatalogList struct {
	ID     string `json:"id"`               // the list's instance id
	Noun   string `json:"noun"`             // the noun whose rows it shows (a state answer's Row.Noun)
	Source string `json:"source,omitempty"` // NOUN.VERB answering the rows; default the noun's RowSource
	Title  string `json:"title,omitempty"`  // the panel title; default the noun
}

// CatalogDetail is a data page's detail panel.
type CatalogDetail struct {
	Title  string   `json:"title,omitempty"`  // the panel title; default the selected row's label
	Fields []string `json:"fields,omitempty"` // the record fields shown, in order; default every field, sorted
}

// The layouts of a data page (CatalogPage.Layout).
const (
	// LayoutStack draws the lists one under the other, full width, the
	// detail (if any) under them.
	LayoutStack = "stack"
	// LayoutSplit draws the lists in a column on the left and the detail on
	// the right (under the lists below 72 cells); without a detail it is
	// stack.
	LayoutSplit = "split"
	// LayoutGroups draws a Groups rail naming the lists on the left and the
	// selected one beside it (the detail under it): only that list is a
	// focus stop; enter, l or right on a group moves into its list and h or
	// left moves back.
	LayoutGroups = "groups"
)

// PageLayouts lists the layouts a data page may name.
var PageLayouts = []string{LayoutStack, LayoutSplit, LayoutGroups}

// IsData reports whether the page is drawn from its data (it has lists),
// not by the extension's Go code.
func (p CatalogPage) IsData() bool { return len(p.Lists) > 0 }

// declaredPage is what a data page's instance carries (Instance.Data), so
// Catalog publishes it.
type declaredPage struct {
	Page CatalogPage `json:"page"`
}

// DeclarePage registers a data page on an extension's runtime so its
// catalog publishes it (CatalogDoc.Pages): the page instance, titled, with
// a placeholder renderer. It adds no lists: the extension's verbs answer
// the rows, a host draws them (MountPage), and an extension that shows its
// own TUI mounts the page over itself the same way.
func DeclarePage(r *ochazuke.Runtime, p CatalogPage) error {
	if err := checkPage(p); err != nil {
		return err
	}
	if _, err := r.AddPage(p.ID, p.Title, func(r *ochazuke.Runtime, w, h int) ochazuke.Block {
		return ochazuke.Text(ansi.Truncate("data page "+p.ID+": cli.MountPage draws it", max(0, w), "…"))
	}); err != nil {
		return err
	}
	r.Instances[p.ID].Data = &declaredPage{Page: p}
	return nil
}

// checkPage refuses a page that cannot be drawn.
func checkPage(p CatalogPage) error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Title) == "" {
		return errors.New("page id and title must be nonempty")
	}
	if p.Layout != "" && !slices.Contains(PageLayouts, p.Layout) {
		return fmt.Errorf("page %s: layout %q is not one of %s", p.ID, p.Layout, strings.Join(PageLayouts, ", "))
	}
	if len(p.Lists) == 0 {
		return fmt.Errorf("page %s: a data page needs a list", p.ID)
	}
	seen := map[string]bool{}
	for _, w := range p.Widgets {
		if err := checkWidget(p.ID, w); err != nil {
			return err
		}
		if seen["widget:"+w.ID] {
			return fmt.Errorf("page %s: widget %s is declared twice", p.ID, w.ID)
		}
		seen["widget:"+w.ID] = true
	}
	for _, l := range p.Lists {
		if l.ID == "" || l.Noun == "" {
			return fmt.Errorf("page %s: a list needs an id and a noun", p.ID)
		}
		if seen[l.ID] {
			return fmt.Errorf("page %s: list %s is declared twice", p.ID, l.ID)
		}
		seen[l.ID] = true
		if l.Source != "" {
			if noun, verb, ok := strings.Cut(l.Source, "."); !ok || noun == "" || verb == "" {
				return fmt.Errorf("page %s: list %s: source %q is not NOUN.VERB", p.ID, l.ID, l.Source)
			}
		}
	}
	return nil
}

// declaredPages is every page of r in registration order as the catalog
// writes it: a declared data page whole, any other page its id and title.
func declaredPages(r *ochazuke.Runtime) []CatalogPage {
	out := []CatalogPage{}
	for _, page := range r.PageList() {
		if d, ok := r.Instances[page.ID].Data.(*declaredPage); ok {
			out = append(out, d.Page)
			continue
		}
		out = append(out, CatalogPage{ID: page.ID, Title: page.Title})
	}
	return out
}

// PageConfig is how a host mounts a data page.
type PageConfig struct {
	// Noun is the host's noun of a catalog noun (nabe mounts skill as
	// ai-skill); nil keeps the catalog's names. Row instances are of the
	// host noun, so the host's proxy verbs answer on them.
	Noun func(catalogNoun string) string
	// Fetch runs one of the extension's verbs with no arguments and
	// returns its stdout (a JSON array or JSON Lines of rows, or any JSON
	// value for a widget); it runs off the loop and honours ctx. Nil means
	// the host pushes rows itself (PageView.SetRows, SetState).
	Fetch func(ctx context.Context, noun, verb string) ([]byte, error)
	// FetchFlags, when set beside Fetch, runs the state verb like Fetch with flags
	// appended to its command line (the state delta protocol: --since-cache
	// ID --since LIST=HASH,...; StateFlags). When the catalog declares
	// stateDelta, Reload asks through it for what changed since the rows it
	// holds and applies the answer (ApplyState); without it, or against a
	// catalog without stateDelta (an extension built with an older
	// ochazuke), every reload is a full answer through Fetch.
	FetchFlags func(ctx context.Context, noun, verb string, flags []string) ([]byte, error)
	// OnRows, when set, runs on the loop after a list's rows land: a
	// reload's answer (one call per list, or a state answer filed under
	// each list, an empty one included), or the host's own SetRows or
	// SetState. list is the CatalogList's id and rows are the rows as
	// filed (empty and repeated ids dropped, a missing label the id), so a
	// host can follow a list's rows (re-sync a pick list) without wrapping
	// Reload. A failed load keeps the rows and does not call it.
	OnRows func(list string, rows []Row)
}

// PageView is a mounted data page: its lists of rows (instances of the
// host's nouns, ids "<list>/<row id>", Data the row's own id, the
// --target an extension takes), the optional detail and the renderer.
// Rows are the host's cached state: Reload fetches them off the loop, or
// the host pushes them; keys, focus and page switches never fetch.
type PageView struct {
	m       *ochazuke.Model
	doc     CatalogDoc
	spec    CatalogPage
	cfg     PageConfig
	lists   []*pageList
	groups  *ochazuke.List
	detail  *ochazuke.Detail
	widgets []*widgetState
	current int               // the list the detail and the groups rail follow
	rows    map[string]Row    // row instance id -> the row it shows
	hashes  map[string]string // row instance id -> its record hash (RecordHash), from a delta answer
	cache   string            // the cache id of the last delta answer
	status  string
}

// pageList is one mounted list.
type pageList struct {
	spec        CatalogList
	noun, verb  string   // the row source (the extension's names); verb "" lists the noun's verbs
	rowNoun     string   // the host noun the rows answer to
	rowIDs      []string // the row instances SetRows made, keys of PageView.rows
	hash        string   // the rows' content hash (ListHash) from a delta answer, "" unknown
	list        *ochazuke.List
	err         string
	loaded      bool
	loading     bool
	verbsListed bool
}

// LoadVerb is the job verb of a data page's loads: no operation has it,
// so the jobs ticker leaves loads off.
const LoadVerb = "catalog-load"

// MountPages mounts every data page of doc (CatalogPage.IsData) in
// catalog order; pages drawn by the extension's Go code are skipped (a
// host cannot draw them).
func MountPages(m *ochazuke.Model, doc CatalogDoc, cfg PageConfig) ([]*PageView, error) {
	var out []*PageView
	for _, p := range doc.Pages {
		if !p.IsData() {
			continue
		}
		v, err := MountPage(m, doc, p, cfg)
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
	return out, nil
}

// MountPage adds a data page to m's runtime: the page (or, when the
// runtime declared it with DeclarePage, that page, redrawn), a list per
// CatalogList with the columns its rows' noun declares (ListColumns), the
// detail and the renderer. A host may edit p first (a prefixed id, its
// own title). The rows' nouns are defined when the host has not yet.
func MountPage(m *ochazuke.Model, doc CatalogDoc, p CatalogPage, cfg PageConfig) (*PageView, error) {
	if m == nil {
		return nil, errors.New("a model is required")
	}
	if err := checkPage(p); err != nil {
		return nil, err
	}
	r := m.Runtime
	v := &PageView{m: m, doc: doc, spec: p, cfg: cfg, rows: map[string]Row{}, hashes: map[string]string{}}
	for _, w := range p.Widgets {
		v.widgets = append(v.widgets, &widgetState{spec: w})
	}
	if v.cfg.Noun == nil {
		v.cfg.Noun = func(n string) string { return n }
	}
	declared := false
	if inst := r.Instances[p.ID]; inst != nil {
		if _, ok := inst.Data.(*declaredPage); !ok || r.PageOf(p.ID) == nil {
			return nil, fmt.Errorf("page %s: instance already exists", p.ID)
		}
		declared = true
	} else {
		r.Instance(p.ID, ochazuke.Kind("page"))
	}
	for _, l := range p.Lists {
		pl := &pageList{spec: l}
		if err := v.resolve(pl); err != nil {
			return nil, err
		}
		r.Define(pl.rowNoun)
		opts := []ochazuke.InstanceOption{ochazuke.Parent(p.ID)}
		if p.Layout == LayoutGroups {
			opts = append(opts, ochazuke.NoSpill())
		}
		if pl.verb != "" {
			opts = append(opts, ochazuke.Columns(v.columns(pl)...))
		}
		list, err := r.AddList(l.ID, nil, opts...)
		if err != nil {
			return nil, err
		}
		pl.list = list
		if pl.verb == "" {
			if err := v.listVerbs(pl); err != nil {
				return nil, err
			}
		}
		v.lists = append(v.lists, pl)
	}
	if p.Layout == LayoutGroups {
		if err := v.addGroups(); err != nil {
			return nil, err
		}
	}
	if p.Detail != nil {
		d, err := r.AddDetail(p.ID+"/detail", v.selectedID, ochazuke.Parent(p.ID))
		if err != nil {
			return nil, err
		}
		v.detail = d
	}
	focus := v.lists[0].list.FocusTarget()
	if v.groups != nil {
		focus = v.groups.FocusTarget()
	}
	if declared {
		page := r.PageOf(p.ID)
		page.Render, page.Focus = v.ListsRender, focus
		return v, nil
	}
	if _, err := r.AddPage(p.ID, p.Title, v.ListsRender, ochazuke.Focus(focus)); err != nil {
		return nil, err
	}
	return v, nil
}

// resolve finds a list's row source and the host noun of its rows.
func (v *PageView) resolve(pl *pageList) error {
	l := pl.spec
	if l.Source == "" {
		verb, rows := v.doc.RowSource(l.Noun)
		pl.noun, pl.verb, pl.rowNoun = l.Noun, verb, v.cfg.Noun(rows)
		if verb == "" {
			if _, ok := v.doc.noun(l.Noun); !ok {
				return fmt.Errorf("page %s: list %s: noun %s is not in the catalog", v.spec.ID, l.ID, l.Noun)
			}
			pl.rowNoun = v.cfg.Noun(l.Noun)
		}
		return nil
	}
	noun, verb, _ := strings.Cut(l.Source, ".")
	n, ok := v.doc.noun(noun)
	if !ok {
		return fmt.Errorf("page %s: list %s: source %s is not in the catalog", v.spec.ID, l.ID, l.Source)
	}
	i := slices.IndexFunc(n.Verbs, func(cv CatalogVerb) bool { return cv.Name == verb })
	if i < 0 {
		return fmt.Errorf("page %s: list %s: source %s is not in the catalog", v.spec.ID, l.ID, l.Source)
	}
	rows := n.Verbs[i].Rows
	if rows == "" {
		rows = noun
	}
	pl.noun, pl.verb, pl.rowNoun = noun, verb, v.cfg.Noun(rows)
	return nil
}

// columns are the columns of the catalog noun a list's rows answer to.
func (v *PageView) columns(pl *pageList) []ochazuke.Column {
	if pl.spec.Source == "" {
		return v.doc.ListColumns(pl.spec.Noun)
	}
	noun, verb, _ := strings.Cut(pl.spec.Source, ".")
	n, _ := v.doc.noun(noun)
	rows := noun
	for _, cv := range n.Verbs {
		if cv.Name == verb && cv.Rows != "" {
			rows = cv.Rows
		}
	}
	if rn, ok := v.doc.noun(rows); ok {
		return slices.Clone(rn.Columns)
	}
	return nil
}

// listVerbs fills a list of a noun without a row source with one row per
// verb the tool defined (CatalogNoun.AppVerbs: never the framework's
// builtin verbs), enter running the host's verb of that name; a verb with
// required parameters, or one the host has not defined, gets no key (the
// palette runs it).
func (v *PageView) listVerbs(pl *pageList) error {
	n, _ := v.doc.noun(pl.spec.Noun)
	n.Verbs = n.AppVerbs()
	local := v.cfg.Noun(n.Name)
	items := make([]ochazuke.ListItem, 0, len(n.Verbs))
	for _, cv := range n.Verbs {
		label := cv.Name
		if req := requiredParams(cv.Params); len(req) > 0 {
			label += " (palette: " + strings.Join(req, ", ") + ")"
		}
		if cv.Description != "" {
			label += "  " + cv.Description
		}
		items = append(items, ochazuke.ListItem{ID: pl.spec.ID + "/" + cv.Name, Label: label, Noun: local})
	}
	if err := pl.list.SetItems(items); err != nil {
		return err
	}
	for _, cv := range n.Verbs {
		if _, ok := v.m.Runtime.Operations[local+"."+cv.Name]; !ok || len(requiredParams(cv.Params)) > 0 {
			continue // the host has no such verb, or a row cannot supply its parameters
		}
		row := ochazuke.Binding{Instance: pl.spec.ID + "/" + cv.Name, Mode: "normal", Keys: "enter", Request: ochazuke.Request{Noun: local, Verb: cv.Name}}
		if err := v.m.Runtime.Bind(row); err != nil {
			return err
		}
	}
	pl.loaded, pl.verbsListed = true, true
	return nil
}

func requiredParams(params map[string]ochazuke.Parameter) []string {
	var out []string
	for name, p := range params {
		if p.Required {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// addGroups adds the groups rail: a row per list, enter, l or right
// moving into the list (the only focusable one) and h or left on the list
// moving back to its group.
func (v *PageView) addGroups() error {
	r := v.m.Runtime
	r.Define(GroupNoun)
	items := make([]ochazuke.ListItem, 0, len(v.lists))
	for i, pl := range v.lists {
		items = append(items, ochazuke.ListItem{ID: v.groupID(pl), Label: pl.title(), Noun: GroupNoun, Data: i})
	}
	var err error
	v.groups, err = r.AddList(v.spec.ID+"/groups", items, ochazuke.Parent(v.spec.ID), ochazuke.NoSpill(), ochazuke.NoMarks())
	if err != nil {
		return err
	}
	for _, pl := range v.lists {
		for _, key := range []string{"enter", "l", "right"} {
			if err := r.Bind(ochazuke.Binding{Instance: v.groupID(pl), Mode: "normal", Keys: key, Request: ochazuke.Request{Noun: "focus", Verb: "next"}}); err != nil {
				return err
			}
		}
		for _, key := range []string{"h", "left"} {
			if err := r.Bind(ochazuke.Binding{Instance: pl.spec.ID, Mode: "normal", Keys: key, Request: ochazuke.Request{Noun: "focus", Verb: "set", Args: ochazuke.Arguments{"id": v.groupID(pl)}}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// GroupNoun is the noun of a groups rail's rows.
const GroupNoun = "page-group"

func (v *PageView) groupID(pl *pageList) string { return v.spec.ID + "/groups/" + pl.spec.ID }

func (pl *pageList) title() string {
	if pl.spec.Title != "" {
		return pl.spec.Title
	}
	return pl.spec.Noun
}

// Page is the page's instance id.
func (v *PageView) Page() string { return v.spec.ID }

// Spec is the page as mounted.
func (v *PageView) Spec() CatalogPage { return v.spec }

// List is the mounted list of a CatalogList id, or nil.
func (v *PageView) List(id string) *ochazuke.List {
	if pl := v.list(id); pl != nil {
		return pl.list
	}
	return nil
}

func (v *PageView) list(id string) *pageList {
	for _, pl := range v.lists {
		if pl.spec.ID == id {
			return pl
		}
	}
	return nil
}

// Row is the row a row instance shows.
func (v *PageView) Row(instance string) (Row, bool) {
	row, ok := v.rows[instance]
	return row, ok
}

// SetStatus sets the muted line under the page's lists (the last verb's
// result); "" clears it.
func (v *PageView) SetStatus(line string) { v.status = line }

// SetRows replaces a list's rows: instance ids "<list>/<row id>", an empty
// or repeated id dropped, a missing label the id. It marks the list
// loaded, clears its error and calls PageConfig.OnRows. Rows the host
// pushes have no hashes: the next delta reload asks for the list in full.
func (v *PageView) SetRows(list string, rows []Row) error {
	pl := v.list(list)
	if pl == nil {
		return fmt.Errorf("page %s has no list %s", v.spec.ID, list)
	}
	if pl.verbsListed {
		return fmt.Errorf("list %s lists its noun's verbs", list)
	}
	return v.setRows(pl, rows, nil)
}

// setRows is SetRows with each row's record hash (by index; nil for none),
// which sets the list's content hash.
func (v *PageView) setRows(pl *pageList, rows []Row, hashes []string) error {
	items := make([]ochazuke.ListItem, 0, len(rows))
	seen := map[string]bool{}
	for _, k := range pl.rowIDs {
		delete(v.rows, k)
		delete(v.hashes, k)
	}
	pl.rowIDs = pl.rowIDs[:0]
	filed := make([]Row, 0, len(rows))
	var records []string
	for i, row := range rows {
		if row.ID == "" || seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		if row.Label == "" {
			row.Label = row.ID
		}
		id := pl.spec.ID + "/" + row.ID
		v.rows[id] = row
		if hashes != nil {
			v.hashes[id] = hashes[i]
			records = append(records, hashes[i])
		}
		pl.rowIDs = append(pl.rowIDs, id)
		filed = append(filed, row)
		items = append(items, v.item(pl, row))
	}
	pl.loaded, pl.err, pl.hash = true, "", ""
	if hashes != nil {
		pl.hash = ListHash(records)
	}
	if err := pl.list.SetItems(items); err != nil {
		pl.hash = ""
		return err
	}
	if v.cfg.OnRows != nil {
		v.cfg.OnRows(pl.spec.ID, filed)
	}
	return nil
}

// item is the list row of a row (its label filled in).
func (v *PageView) item(pl *pageList, row Row) ochazuke.ListItem {
	return ochazuke.ListItem{ID: pl.spec.ID + "/" + row.ID, Label: row.Label, Noun: pl.rowNoun, Data: row.ID, Cells: row.Cells}
}

// SetState files a state answer's rows (CatalogDoc.State) under the lists
// of their noun (Row.Noun is a CatalogList's Noun); a list whose noun has
// no rows in it is emptied.
func (v *PageView) SetState(rows []Row) error {
	by := map[string][]Row{}
	for _, row := range rows {
		by[row.Noun] = append(by[row.Noun], row)
	}
	var errs []error
	for _, pl := range v.lists {
		if pl.verbsListed {
			continue
		}
		if err := v.SetRows(pl.spec.ID, by[pl.spec.Noun]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SetError shows why a list's rows did not load; its rows stay.
func (v *PageView) SetError(list string, err error) {
	if pl := v.list(list); pl != nil && err != nil {
		pl.err = err.Error()
	}
}

// Reload fetches the page's rows off the loop (PageConfig.Fetch): one
// state call when the catalog has a state source, else one call per list,
// and one per widget with a source, each a job under LoadVerb in its own
// slot (a newer load replaces an older one). A state call asks for what
// changed since the rows the page holds when the host gave FetchFlags and
// the catalog declares stateDelta (ApplyState; a mismatched list is
// fetched again in full); Refresh asks for every list in full. Without
// Fetch it does nothing.
func (v *PageView) Reload() error {
	if v.cfg.Fetch == nil {
		return nil
	}
	werr := v.reloadWidgets()
	if noun, verb, ok := strings.Cut(v.doc.State, "."); ok && v.speaksDelta() {
		return errors.Join(werr, v.reloadState(noun, verb, true))
	}
	if noun, verb, ok := strings.Cut(v.doc.State, "."); ok {
		for _, pl := range v.lists {
			pl.loading = !pl.verbsListed
		}
		return errors.Join(werr, v.launch(v.spec.ID, "state", noun, verb, func(out []byte, err error) {
			rows, derr := DecodeRows("", out)
			for _, pl := range v.lists {
				pl.loading = false
			}
			if err == nil {
				err = derr
			}
			if err == nil {
				err = v.SetState(rows)
			}
			if err != nil {
				for _, pl := range v.lists {
					if !pl.verbsListed {
						pl.err = err.Error()
					}
				}
			}
		}))
	}
	errs := []error{werr}
	for _, pl := range v.lists {
		if pl.verbsListed {
			continue
		}
		pl.loading = true
		err := v.launch(pl.spec.ID, "rows", pl.noun, pl.verb, func(out []byte, err error) {
			pl.loading = false
			var rows []Row
			if err == nil {
				rows, err = DecodeRows(pl.spec.Noun, out)
			}
			if err == nil {
				err = v.SetRows(pl.spec.ID, rows)
			}
			if err != nil {
				pl.err = err.Error()
			}
		})
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// launch runs Fetch(noun, verb) as a job of owner in slot.
func (v *PageView) launch(owner, slot, noun, verb string, done func([]byte, error)) error {
	fetch := v.cfg.Fetch
	return v.launchWith(owner, slot, noun, verb, func(ctx context.Context) ([]byte, error) {
		return fetch(ctx, noun, verb)
	}, done)
}

// launchWith runs fetch as a job of owner in slot, labelled noun.verb.
func (v *PageView) launchWith(owner, slot, noun, verb string, fetch func(context.Context) ([]byte, error), done func([]byte, error)) error {
	res := ochazuke.Pending(func(ctx context.Context) (any, error) {
		return fetch(ctx)
	}).Replacing(slot).Label(noun + "." + verb).Then(func(_ ochazuke.Ctx, value any, err error) {
		out, _ := value.([]byte)
		if err != nil {
			err = fmt.Errorf("%s %s: %w", noun, verb, err)
		}
		done(out, err)
	})
	_, err := v.m.Runtime.Launch(owner, LoadVerb, res, ochazuke.Ctx{Runtime: v.m.Runtime})
	return err
}

// DecodeRows reads a list answer: a JSON array of rows or JSON Lines of
// them. A row is an object ({id, label, cells, data}, or any record whose
// id is its first of id or ID and label its first of label, Label, name,
// Name, title or Title; either stands in for the other) or a scalar (id and
// label both). An object's data is its data field when it has one, else
// the object less the row's own fields (noun, id, label, cells). noun fills a row without its own. An empty answer is
// no rows.
func DecodeRows(noun string, out []byte) ([]Row, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 || string(out) == "null" {
		return nil, nil
	}
	var values []any
	if out[0] == '[' {
		if err := json.Unmarshal(out, &values); err != nil {
			return nil, fmt.Errorf("rows: %w", err)
		}
	} else {
		for _, line := range bytes.Split(out, []byte("\n")) {
			if line = bytes.TrimSpace(line); len(line) == 0 {
				continue
			}
			var value any
			if json.Unmarshal(line, &value) != nil {
				continue // a malformed line is skipped, as RowReader does
			}
			values = append(values, value)
		}
		if len(values) == 0 {
			return nil, ErrNoRows
		}
	}
	rows := make([]Row, 0, len(values))
	for _, value := range values {
		rows = append(rows, rowOf(noun, value))
	}
	return rows, nil
}

// The record fields a row's id and label are read from, first present wins.
var (
	rowIDFields    = []string{"id", "ID"}
	rowLabelFields = []string{"label", "Label", "name", "Name", "title", "Title"}
	// rowFields are a Row's own fields, left out of a record without data.
	rowFields = []string{"noun", "id", "label", "cells"}
)

func rowOf(noun string, value any) Row {
	obj, ok := value.(map[string]any)
	if !ok {
		s := fmt.Sprint(value)
		return Row{Noun: noun, ID: s, Label: s}
	}
	pick := func(fields []string) string {
		for _, f := range fields {
			if v, ok := obj[f]; ok && v != nil && fmt.Sprint(v) != "" {
				return fmt.Sprint(v)
			}
		}
		return ""
	}
	row := Row{Noun: noun, ID: pick(rowIDFields), Label: pick(rowLabelFields)}
	if n, ok := obj["noun"].(string); ok && n != "" {
		row.Noun = n
	}
	if data, ok := obj["data"]; ok {
		row.Data = data
	} else {
		// A record is the object less a row's own fields.
		rest := map[string]any{}
		for k, v := range obj {
			if !slices.Contains(rowFields, k) {
				rest[k] = v
			}
		}
		if len(rest) > 0 {
			row.Data = rest
		}
	}
	if cells, ok := obj["cells"].([]any); ok {
		row.Cells = make([]string, len(cells))
		for i, c := range cells {
			if c != nil {
				row.Cells[i] = fmt.Sprint(c)
			}
		}
	}
	if row.ID == "" {
		row.ID = row.Label
	}
	if row.Label == "" {
		row.Label = row.ID
	}
	return row
}

// focusedList is the index of the list holding the focus, else -1.
func (v *PageView) focusedList() int {
	for i, pl := range v.lists {
		if pl.list.Focused() {
			return i
		}
	}
	return -1
}

// follow updates the list the detail and the groups rail follow: the
// focused list, else the selected group, else the last one.
func (v *PageView) follow() {
	if i := v.focusedList(); i >= 0 {
		v.current = i
		if v.groups != nil {
			if sel, ok := v.groups.Selected(); ok {
				v.groups.Move(i - sel.Data.(int))
			}
		}
		return
	}
	if v.groups != nil {
		if sel, ok := v.groups.Selected(); ok {
			v.current = sel.Data.(int)
		}
	}
}

// selectedID is the detail's item: the followed list's selected row.
func (v *PageView) selectedID() string {
	v.follow()
	if item, ok := v.lists[v.current].list.Selected(); ok {
		return item.ID
	}
	return ""
}

// detailText is the selected row's record as key: value lines.
func (v *PageView) detailText(id string) (title, text string) {
	row, ok := v.rows[id]
	if !ok {
		if inst := v.m.Runtime.Instances[id]; inst != nil && inst.Label != "" {
			return inst.Label, ""
		}
		return "", "no row selected"
	}
	title = row.Label
	if v.spec.Detail.Title != "" {
		title = v.spec.Detail.Title + " · " + row.Label
	}
	return title, recordText(row.Data, v.spec.Detail.Fields, row)
}

// recordText draws a record: an object's fields (in fields order when
// given, else sorted; a field the record lacks is left out) as aligned
// key  value lines, any other value as its JSON. A row without a record,
// or without any of the fields, shows its id and label.
func recordText(data any, fields []string, row Row) string {
	obj, ok := data.(map[string]any)
	if !ok {
		if data == nil {
			return "id: " + row.ID + "\nlabel: " + row.Label
		}
		js, _ := json.MarshalIndent(data, "", "  ")
		return string(js)
	}
	var keys []string
	for _, k := range fields {
		if _, ok := obj[k]; ok {
			keys = append(keys, k)
		}
	}
	if len(fields) == 0 {
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	if len(keys) == 0 {
		return "id: " + row.ID + "\nlabel: " + row.Label
	}
	return keyValues(keys, func(k string) string { return valueText(obj[k]) })
}

// keyValues draws keys and their values as "key  value" lines, keys padded
// to the widest.
func keyValues(keys []string, value func(string) string) string {
	width := 0
	for _, k := range keys {
		width = max(width, ansi.StringWidth(k))
	}
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+strings.Repeat(" ", width-ansi.StringWidth(k))+"  "+value(k))
	}
	return strings.Join(lines, "\n")
}

// valueText is a value on one line: a string as is, anything else as JSON.
func valueText(value any) string {
	if s, ok := value.(string); ok {
		return plainText(strings.ReplaceAll(s, "\n", " "))
	}
	js, _ := json.Marshal(value)
	return string(js)
}

// ListsRender is the page's renderer (the one MountPage gives AddPage):
// the lists in their layout, the detail, inside the page body the
// model's Layout.Shell frames; widgets above them take at most half the
// height.
func (v *PageView) ListsRender(_ *ochazuke.Runtime, width, height int) ochazuke.Block {
	if width <= 0 || height <= 0 {
		return ochazuke.Block{}
	}
	v.follow()
	if v.groups != nil {
		for i, pl := range v.lists {
			v.m.Runtime.Instances[pl.spec.ID].Unfocusable = i != v.current
		}
	}
	layout := ochazuke.Layout{Styles: ochazuke.NewStyles(*v.m.Appearance)}
	if v.detail != nil {
		// The lists/detail split follows the page's divider (pane.grow,
		// pane.shrink), and the pane keys answer on this page.
		layout = v.m.Runtime.PaneLayout(v.spec.ID, layout)
	}
	if len(v.widgets) == 0 {
		return v.body(layout, width, height)
	}
	top, used := v.widgetsBlock(layout, width, height)
	if used == 0 {
		return v.body(layout, width, height)
	}
	return below(top, v.body(layout, width, height-used), used)
}

// body draws the lists and the detail in width by height.
func (v *PageView) body(layout ochazuke.Layout, width, height int) ochazuke.Block {
	switch {
	case v.groups != nil:
		rail := min(28, max(1, (width-1)/3))
		rw, rh := ochazuke.PanelContentSize(rail, height)
		left := layout.PanelBlock("Groups", ochazuke.Text(v.groups.View(rw, rh)), rail, height, v.groups.Focused())
		right := v.listsAndDetail(layout, []*pageList{v.lists[v.current]}, width-rail-1, height, false)
		return beside(left, right, rail, height)
	case v.spec.Layout == LayoutSplit && v.detail != nil && width >= 72:
		left := layout.Divide((width-1)*2/5, width-1, ochazuke.PaneMinWidth)
		lists := v.stackLists(layout, v.lists, left, height)
		return beside(lists, v.detailPanel(layout, width-left-1, height), left, height)
	}
	return v.listsAndDetail(layout, v.lists, width, height, true)
}

// listsAndDetail stacks lists over the detail (when the page has one).
func (v *PageView) listsAndDetail(layout ochazuke.Layout, lists []*pageList, width, height int, stack bool) ochazuke.Block {
	if v.detail == nil {
		return v.stackLists(layout, lists, width, height)
	}
	dh := max(3, height/3)
	if !stack {
		dh = max(3, height*2/5)
	}
	th := layout.Divide(height-min(dh, height), height, ochazuke.PaneMinHeight)
	top := v.stackLists(layout, lists, width, th)
	return below(top, v.detailPanel(layout, width, height-th), th)
}

// stackLists draws lists one under the other, sharing height evenly.
func (v *PageView) stackLists(layout ochazuke.Layout, lists []*pageList, width, height int) ochazuke.Block {
	var out ochazuke.Block
	used := 0
	for i, pl := range lists {
		h := (height - used) / (len(lists) - i)
		if h <= 0 {
			pl.list.View(0, 0) // keeps its place in the drawing order
			continue
		}
		panel := v.listPanel(layout, pl, width, h)
		if i == 0 || used == 0 {
			out = panel
		} else {
			out = below(out, panel, used)
		}
		used += h
	}
	return out
}

// listPanel draws one list in a titled panel: its rows, or loading…, no
// rows, and its error and the page status under them.
func (v *PageView) listPanel(layout ochazuke.Layout, pl *pageList, width, height int) ochazuke.Block {
	s := layout.Styles
	cw, ch := ochazuke.PanelContentSize(width, height)
	var notes []string
	for _, line := range []string{pl.err, v.statusFor(pl)} {
		if line != "" {
			notes = append(notes, s.Muted.Render(ansi.Truncate(plainText(line), cw, "…")))
		}
	}
	rows := max(0, ch-len(notes))
	text := pl.list.View(cw, rows)
	if pl.list.Len() == 0 && rows > 0 {
		note := "no rows"
		if pl.loading && !pl.loaded {
			note = "loading…"
		}
		text = s.Muted.Render(note)
		if len(pl.list.Columns()) > 0 && rows > 1 {
			head, _, _ := strings.Cut(pl.list.View(cw, 2), "\n")
			text = head + "\n" + text
		}
	}
	if len(notes) > 0 {
		text = strings.TrimRight(text, "\n") + "\n" + strings.Join(notes, "\n")
	}
	title := fmt.Sprintf("%s (%d)", pl.title(), pl.list.Len())
	if pl.loading && pl.loaded {
		title += " · refreshing…"
	}
	return layout.PanelBlock(title, ochazuke.Text(text), width, height, pl.list.Focused())
}

// statusFor is the page status, shown under the last list drawn.
func (v *PageView) statusFor(pl *pageList) string {
	last := v.lists[len(v.lists)-1]
	if v.groups != nil {
		last = v.lists[v.current]
	}
	if pl != last {
		return ""
	}
	return v.status
}

// detailPanel draws the detail of the followed list's selected row.
func (v *PageView) detailPanel(layout ochazuke.Layout, width, height int) ochazuke.Block {
	cw, ch := ochazuke.PanelContentSize(width, height)
	title, text := v.detailText(v.selectedID())
	body, marker := v.detail.Window(text, cw, ch)
	if title == "" {
		title = "detail"
	}
	if marker != "" {
		title += " · " + marker
	}
	return layout.PanelBlock(title, ochazuke.Text(body), width, height, false)
}
