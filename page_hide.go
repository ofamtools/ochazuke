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
	_ = r.DeclareSetting(Setting{Name: PagesHiddenSetting, Type: SettingList, Default: r.defaultHiddenPages(), Description: "Page ids left out of the tabs and ctrl+j/k (page.hide and page.show write it); page.open still opens them"})
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

// pageArg is the page a page verb names: its id argument, else the current
// page.
func (r *Runtime) pageArg(a Arguments) string {
	if id := StringArg(a, "id", ""); id != "" {
		return id
	}
	return r.Page
}

// registerPageHiding registers page.hide and page.show with their
// availability: with an id, hide while that page is in the tabs and show
// while it is hidden; with none (the current page) both are offered, and
// the verb says when there is nothing to do, so the palette's verb list
// does not depend on the page it is opened over.
func registerPageHiding(r *Runtime) {
	id := Parameter{Type: "string", Noun: "ochazuke.pages", Description: "The page id; default the current page"}
	r.Register(Operation{Verb: "hide", Noun: "ochazuke.page", Description: "Hide a page from the tabs and ctrl+j/k (the pages.hidden setting); page.open still opens it", Parameters: map[string]Parameter{"id": id}, Positional: []string{"id"}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		target := r.pageArg(a)
		if r.PageOf(target) != nil && r.PageHidden(target) {
			return nil, fmt.Errorf("%s is already hidden from the tabs", r.pageLabel(target))
		}
		if err := r.HidePage(target); err != nil {
			return nil, err
		}
		if target == r.Page {
			r.UI().Info(r.pageLabel(target) + " is hidden: its tab goes when you leave it (page.show brings it back)")
		} else {
			r.UI().Info(r.pageLabel(target) + " is hidden from the tabs (page.show brings it back)")
		}
		return r.Context(), nil
	}})
	r.Register(Operation{Verb: "show", Noun: "ochazuke.page", Description: "Bring a hidden page back into the tabs and ctrl+j/k", Parameters: map[string]Parameter{"id": id}, Positional: []string{"id"}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		target := r.pageArg(a)
		if r.PageOf(target) != nil && !r.PageHidden(target) {
			return nil, fmt.Errorf("%s is already in the tabs", r.pageLabel(target))
		}
		if err := r.ShowPage(target); err != nil {
			return nil, err
		}
		r.UI().Info(r.pageLabel(target) + " is back in the tabs")
		return r.Context(), nil
	}})
	named := func(hidden bool) Guard {
		return func(r *Runtime, ev KeyEvent) bool {
			target := StringArg(ev.Args, "id", "")
			return target == "" || r.PageOf(target) == nil || r.PageHidden(target) == hidden
		}
	}
	r.DefineGuard("page-shown", named(false))
	r.DefineGuard("page-hidden", named(true))
	r.SetAvailability("ochazuke.page.hide", "page-shown")
	r.SetAvailability("ochazuke.page.show", "page-hidden")
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
