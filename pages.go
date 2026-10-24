package ochazuke

import (
	"errors"
	"fmt"
	"strings"
)

// Page is the component a page instance fronts: a title, the base frame that
// remembers where focus sits on the page, and a renderer. Render returns the
// page body for the given cell bounds as a Block; its cursor, when the page
// holds the focused input, is relative to the body's top-left cell. Pages
// without inputs return Text(body).
type Page struct {
	ID     string                         `json:"id"`
	Title  string                         `json:"title"`
	Focus  string                         `json:"focus"`
	Frame  *Frame                         `json:"frame"`
	Render func(*Runtime, int, int) Block `json:"-"`
}

// RegisterPage registers a page as an instance of kind page under app (the
// instance may already exist, registered through Instance with the inputs
// and lists it holds). Focus names where the page starts: the page itself or
// an instance beneath it. Registration does not activate the page.
func (r *Runtime) RegisterPage(page *Page, opts ...InstanceOption) error {
	if page == nil || strings.TrimSpace(page.ID) == "" || strings.TrimSpace(page.Title) == "" {
		return errors.New("page ID and title must be nonempty")
	}
	if r.PageOf(page.ID) != nil {
		return fmt.Errorf("page ID %q is already registered", page.ID)
	}
	for _, other := range r.PageList() {
		if other.Title == page.Title {
			return fmt.Errorf("page title %q is already registered", page.Title)
		}
	}
	if page.Render == nil {
		return errors.New("page renderer is required")
	}
	if page.Focus == "" {
		page.Focus = page.ID
	}
	if inst := r.Instances[page.ID]; inst != nil {
		if inst.Parent != "app" {
			return fmt.Errorf("page %q must be registered under app", page.ID)
		}
	} else {
		path := r.InstancePath(page.Focus)
		if page.Focus != page.ID && (len(path) < 2 || path[1] != page.ID) {
			return fmt.Errorf("page focus %q must sit beneath page %q", page.Focus, page.ID)
		}
	}
	if page.Focus != page.ID {
		if path := r.InstancePath(page.Focus); len(path) < 2 || path[1] != page.ID {
			return fmt.Errorf("page focus %q must sit beneath page %q", page.Focus, page.ID)
		}
	}
	inst := r.Instance(page.ID, append([]InstanceOption{Kind("page")}, opts...)...)
	inst.Component = page
	page.Frame = &Frame{ID: page.ID, Focus: page.Focus}
	if list := r.List(page.Focus); list != nil {
		// A page that starts on a list starts on its selected row.
		page.Frame.Focus = list.focusTarget()
	}
	return nil
}

// PageOf returns the page an instance fronts, or nil.
func (r *Runtime) PageOf(id string) *Page {
	if inst := r.Instances[id]; inst != nil {
		if page, ok := inst.Component.(*Page); ok {
			return page
		}
	}
	return nil
}

// PageList lists pages in registration order: app's children of kind page.
func (r *Runtime) PageList() []*Page {
	var out []*Page
	for _, inst := range r.Children("app") {
		if page, ok := inst.Component.(*Page); ok {
			out = append(out, page)
		}
	}
	return out
}

// OpenPage makes a page current and restores its exact base frame; an open
// modal blocks navigation. Runtime.Page always lies on the base frame's focus.
func (r *Runtime) OpenPage(id string) error {
	page := r.PageOf(id)
	if page == nil {
		return fmt.Errorf("unknown page %q", id)
	}
	if len(r.Frames) > 1 {
		return errors.New("close the active modal before opening a page")
	}
	r.Frames = []*Frame{page.Frame}
	r.Page = id
	r.CancelPendingBinding()
	return nil
}

func (r *Runtime) movePage(delta int) error {
	pages := r.PageList()
	if len(pages) == 0 {
		return errors.New("no pages registered")
	}
	index := -1
	for i, page := range pages {
		if page.ID == r.Page {
			index = i
			break
		}
	}
	if index == -1 {
		if delta > 0 {
			return r.OpenPage(pages[0].ID)
		}
		return r.OpenPage(pages[len(pages)-1].ID)
	}
	index = (index + delta + len(pages)) % len(pages)
	return r.OpenPage(pages[index].ID)
}

func registerPages(r *Runtime) {
	r.Define("page")
	r.Register(Operation{Verb: "list", Noun: "pages", Description: "List pages in registration order", Result: "page[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		pages := make([]map[string]any, 0)
		for _, page := range r.PageList() {
			pages = append(pages, map[string]any{"id": page.ID, "title": page.Title, "active": page.ID == r.Page})
		}
		return pages, nil
	}})
	r.Register(Operation{Verb: "open", Noun: "page", Description: "Open a registered page", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.OpenPage(StringArg(a, "id", ""))
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "next", Noun: "page", Description: "Open the next page, wrapping in registration order", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.movePage(1)
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "previous", Noun: "page", Description: "Open the previous page, wrapping in registration order", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.movePage(-1)
		return r.Context(), err
	}})
}
