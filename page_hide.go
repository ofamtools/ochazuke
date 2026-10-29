package ochazuke

import (
	"fmt"
	"slices"
)

// PagesHiddenSetting is the framework setting that hides pages from the
// tab row and the page-cycling keys (page.next, page.previous, ctrl+j/k):
// a list of page ids. A hidden page stays registered and reachable:
// page.open, the page picker (marked hidden), the palette, the CLI and an
// application's own OpenPage still open it, and while it is current its
// tab shows so the chrome says where the focus is. Its default is the ids
// of the pages declared hidden by default (Page.DefaultHidden, the
// DefaultHidden option, a catalog page's defaultHidden), so an
// application's opt-in pages start hidden. Once settings.json holds a
// value it is the whole list; settings reset pages.hidden goes back to the
// application's defaults. It is read at every frame and every page motion,
// so settings set, page.hide, page.show, config.reload and another
// process's write apply on the next one.
const PagesHiddenSetting = "pages.hidden"

// DefaultHidden declares a page hidden by default (Page.DefaultHidden):
// it starts out of the tabs until page.show (or the pages.hidden setting)
// brings it in. AddPage and RegisterPage read it; other constructors
// ignore it.
func DefaultHidden() InstanceOption { return func(s *instanceSpec) { s.defaultHidden = true } }

// ensurePagesSetting declares pages.hidden unless the application did,
// with the declared-hidden pages as its default.
func (r *Runtime) ensurePagesSetting() {
	s := r.Settings()
	if s.find(PagesHiddenSetting) != nil {
		return
	}
	_ = r.declareBuiltinSetting(Setting{Name: PagesHiddenSetting, Type: SettingList, Default: r.defaultHiddenPages(), Description: "Page ids left out of the tabs and ctrl+j/k (page.hide and page.show write it); page.open still opens them"})
}

// defaultHiddenPages are the ids of the pages declared hidden by default,
// in registration order; never nil.
func (r *Runtime) defaultHiddenPages() []string {
	out := []string{}
	for _, page := range r.PageList() {
		if page.DefaultHidden {
			out = append(out, page.ID)
		}
	}
	return out
}

// syncPagesHiddenDefault sets pages.hidden's default to the declared-hidden
// pages, when it is declared, telling its observers when the value changed.
func (r *Runtime) syncPagesHiddenDefault() {
	s := r.Settings()
	one := s.find(PagesHiddenSetting)
	if one == nil {
		return
	}
	old := s.current(*one)
	one.Default = r.defaultHiddenPages()
	s.changed(PagesHiddenSetting, old)
}

// HiddenPages is the pages.hidden value: the ids left out of the tabs.
// Before the setting is declared (EnsureConfigSettings, Model.Init, or the
// first page.hide or page.show) it is the declared-hidden pages.
func (r *Runtime) HiddenPages() []string {
	if r.Settings().find(PagesHiddenSetting) == nil {
		return r.defaultHiddenPages()
	}
	return r.Settings().Strings(PagesHiddenSetting)
}

// PageHidden reports whether pages.hidden holds the page id.
func (r *Runtime) PageHidden(id string) bool { return slices.Contains(r.HiddenPages(), id) }

// TabPages are the pages the tab row draws and page.next/previous cycle,
// in registration order: every page not hidden, and the current page even
// when it is (it leaves the tabs once another page opens).
func (r *Runtime) TabPages() []*Page {
	hidden := r.HiddenPages()
	pages := r.PageList()
	if len(hidden) == 0 {
		return pages
	}
	return slices.DeleteFunc(pages, func(p *Page) bool { return p.ID != r.Page && slices.Contains(hidden, p.ID) })
}

// visiblePages are the pages not hidden, in registration order.
func (r *Runtime) visiblePages() []*Page {
	hidden := r.HiddenPages()
	return slices.DeleteFunc(r.PageList(), func(p *Page) bool { return slices.Contains(hidden, p.ID) })
}

// HidePage adds a page to pages.hidden. It refuses an unknown page and the
// last page not hidden, so the tabs always hold one; hiding the current
// page keeps it open, its tab shown until another page opens.
func (r *Runtime) HidePage(id string) error {
	r.ensurePagesSetting()
	if r.PageOf(id) == nil {
		return fmt.Errorf("unknown page %q", id)
	}
	hidden := r.HiddenPages()
	if slices.Contains(hidden, id) {
		return nil
	}
	if visible := r.visiblePages(); len(visible) == 1 && visible[0].ID == id {
		return fmt.Errorf("%s is the only page in the tabs: show another page before hiding it", r.pageLabel(id))
	}
	return r.Settings().Set(PagesHiddenSetting, append(hidden, id))
}

// ShowPage takes a page out of pages.hidden, back into the tabs in its
// registration place.
func (r *Runtime) ShowPage(id string) error {
	r.ensurePagesSetting()
	if r.PageOf(id) == nil {
		return fmt.Errorf("unknown page %q", id)
	}
	hidden := r.HiddenPages()
	if !slices.Contains(hidden, id) {
		return nil
	}
	return r.Settings().Set(PagesHiddenSetting, slices.DeleteFunc(hidden, func(h string) bool { return h == id }))
}

// OpenVisibleStart leaves a hidden start page: when the current page is
// hidden and another page is not, it opens the first page not hidden.
// Model.Init calls it once the settings are read, so an application whose
// start page the user hid opens on the first page in the tabs instead;
// later, opening a hidden page (page.open, OpenPage) stays on it.
func (r *Runtime) OpenVisibleStart() error {
	if r.Page == "" || !r.PageHidden(r.Page) {
		return nil
	}
	visible := r.visiblePages()
	if len(visible) == 0 {
		return nil
	}
	return r.OpenPage(visible[0].ID)
}

// pageLabel is a page's title, else its id.
func (r *Runtime) pageLabel(id string) string {
	if page := r.PageOf(id); page != nil && page.Title != "" {
		return page.Title
	}
	return id
}

// The page noun's things (standard verbs, lane S7): ochazuke.page is an
// instances noun, its things the registered pages (their instances), keyed
// by id, so get, list, set and watch are standard. Its one writable prop is
// hidden, which writes pages.hidden (HidePage, ShowPage); page.hide and
// page.show are aliases of set with the prop and value preset.

// pageProps are a page thing's props.
var pageProps = []PropSpec{
	{Name: "id", Key: true, Parameter: Parameter{Description: "The page id"}},
	{Name: "title", Role: RoleLabel, ReadOnly: true, Parameter: Parameter{Description: "The page's title (its tab)"}},
	{Name: "hidden", Parameter: Parameter{Type: SettingBoolean, Description: "Out of the tabs and ctrl+j/k (the pages.hidden setting); page.open still opens it"}},
	{Name: "active", ReadOnly: true, Parameter: Parameter{Type: SettingBoolean, Description: "The current page"}},
}

// registerPageHiding declares the page noun's props and store, and
// page.hide and page.show as aliases of page.set {prop: hidden}. set
// writing hidden is available unless it names a page whose hidden prop
// already holds the value (so hide is offered while the named page is in
// the tabs and show while it is hidden); with no id (the current page) it
// is offered, and the write says when there is nothing to do, so the
// palette's verb list does not depend on the page it is opened over.
func registerPageHiding(r *Runtime) {
	r.Define("ochazuke.page").Props(pageProps...).Store(Store{
		Kind: Instances,
		Get: func(c Ctx, id string) (Thing, error) {
			r := c.Runtime
			page := r.PageOf(id)
			if page == nil {
				return nil, fmt.Errorf("unknown page %q", id)
			}
			return Thing{"title": r.pageLabel(id), "hidden": r.PageHidden(id), "active": id == r.Page}, nil
		},
		Live: func(r *Runtime) []string {
			var out []string
			for _, page := range r.PageList() {
				out = append(out, page.ID)
			}
			return out
		},
		Patch: patchPage,
		When:  map[string]string{"set": "page-set-changes"},
	})
	for _, alias := range []ActionAlias{
		{Alias: "ochazuke.page.hide", Action: "ochazuke.page.set", Args: Arguments{"prop": "hidden", "value": true}, Note: "Hide a page from the tabs and ctrl+j/k (the pages.hidden setting); page.open still opens it"},
		{Alias: "ochazuke.page.show", Action: "ochazuke.page.set", Args: Arguments{"prop": "hidden", "value": false}, Note: "Bring a hidden page back into the tabs and ctrl+j/k"},
	} {
		if err := r.AddActionAlias(alias); err != nil {
			panic(err.Error())
		}
	}
	r.DefineGuard("page-set-changes", func(r *Runtime, ev KeyEvent) bool {
		target := StringArg(ev.Args, "id", "")
		value, given := ev.Args["value"]
		if target == "" || !given || StringArg(ev.Args, "prop", "") != "hidden" || r.PageOf(target) == nil {
			return true
		}
		hide, err := propValue(pageProps[2], value)
		return err != nil || r.PageHidden(target) != hide.(bool)
	})
	// The guards page.hide and page.show had before they were aliases,
	// kept for binding rows that name them.
	named := func(hidden bool) Guard {
		return func(r *Runtime, ev KeyEvent) bool {
			target := StringArg(ev.Args, "id", "")
			return target == "" || r.PageOf(target) == nil || r.PageHidden(target) == hidden
		}
	}
	r.DefineGuard("page-shown", named(false))
	r.DefineGuard("page-hidden", named(true))
	// A watch of pages lists again when pages.hidden changes.
	r.Settings().Observe(PagesHiddenSetting, func(string, any) { r.Changed("ochazuke.page") })
}

// patchPage is the page store's write: hidden true hides the page from
// the tabs (HidePage: never the last one in them), false shows it, each
// saying so; a write that changes nothing is refused with what holds.
func patchPage(c Ctx, id string, changes Thing) (Thing, error) {
	r := c.Runtime
	v, ok := changes["hidden"]
	if !ok {
		return nil, nil
	}
	hide, _ := v.(bool)
	switch {
	case hide && r.PageHidden(id):
		return nil, fmt.Errorf("%s is already hidden from the tabs", r.pageLabel(id))
	case !hide && !r.PageHidden(id):
		return nil, fmt.Errorf("%s is already in the tabs", r.pageLabel(id))
	case hide:
		if err := r.HidePage(id); err != nil {
			return nil, err
		}
		if id == r.Page {
			c.Info(r.pageLabel(id) + " is hidden: its tab goes when you leave it (page.show brings it back)")
		} else {
			c.Info(r.pageLabel(id) + " is hidden from the tabs (page.show brings it back)")
		}
	default:
		if err := r.ShowPage(id); err != nil {
			return nil, err
		}
		c.Info(r.pageLabel(id) + " is back in the tabs")
	}
	return nil, nil
}

// pageRows are pages.list's rows: every page in registration order with
// whether it is current and whether it is hidden from the tabs.
func (r *Runtime) pageRows() []map[string]any {
	hidden := r.HiddenPages()
	out := make([]map[string]any, 0)
	for _, page := range r.PageList() {
		out = append(out, map[string]any{"id": page.ID, "title": page.Title, "active": page.ID == r.Page, "hidden": slices.Contains(hidden, page.ID)})
	}
	return out
}
