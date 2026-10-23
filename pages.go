package ochazuke

import (
	"errors"
	"fmt"
	"strings"
)

// Page retains its base frame and renderer across navigation.
type Page struct {
	ID     string                          `json:"id"`
	Title  string                          `json:"title"`
	Frame  *Frame                          `json:"frame"`
	Render func(*Runtime, int, int) string `json:"-"`
}

// RegisterPage uses an existing layer and does not activate the page.
func (r *Runtime) RegisterPage(page *Page) error {
	if page == nil || strings.TrimSpace(page.ID) == "" || strings.TrimSpace(page.Title) == "" {
		return errors.New("page ID and title must be nonempty")
	}
	if r.Pages[page.ID] != nil {
		return fmt.Errorf("page ID %q is already registered", page.ID)
	}
	for _, id := range r.PageOrder {
		if r.Pages[id].Title == page.Title {
			return fmt.Errorf("page title %q is already registered", page.Title)
		}
	}
	if page.Frame == nil || page.Render == nil {
		return errors.New("page frame and renderer are required")
	}
	path := page.Frame.Context.Path
	if len(path) < 2 || path[0] != "app" || path[1] != page.ID {
		return errors.New("page interaction path must start with app and the page ID")
	}
	if err := r.validateContext(page.Frame.Context); err != nil {
		return err
	}
	page.Frame.Context = page.Frame.Context.Clone()
	if page.Frame.State == nil {
		page.Frame.State = map[string]any{}
	}
	r.Pages[page.ID] = page
	r.PageOrder = append(r.PageOrder, page.ID)
	return nil
}

// OpenPage restores the exact page-owned frame; an open modal blocks navigation.
func (r *Runtime) OpenPage(id string) error {
	page := r.Pages[id]
	if page == nil {
		return fmt.Errorf("unknown page %q", id)
	}
	if len(r.Frames) > 1 {
		return errors.New("close the active modal before opening a page")
	}
	r.Frames = []*Frame{page.Frame}
	r.ActivePage = id
	r.CancelPendingBinding()
	return nil
}

// validateSelection guards navigation, not explicitly targeted execution.
func (r *Runtime) validateSelection(c Context) error {
	if err := r.validateContext(c); err != nil {
		return err
	}
	if r.Pages[r.ActivePage] != nil {
		if len(r.Frames) == 1 && (len(c.Path) < 2 || c.Path[1] != r.ActivePage) {
			return errors.New("select a context inside the active page; open a modal for a modal input")
		}
		for _, id := range c.Path[1:] {
			if r.Pages[id] != nil && id != r.ActivePage {
				return fmt.Errorf("open page %q before selecting its context", id)
			}
		}
	}
	return nil
}

func (r *Runtime) movePage(delta int) error {
	if len(r.PageOrder) == 0 {
		return errors.New("no pages registered")
	}
	index := -1
	for i, id := range r.PageOrder {
		if id == r.ActivePage {
			index = i
			break
		}
	}
	if index == -1 {
		if delta > 0 {
			return r.OpenPage(r.PageOrder[0])
		}
		return r.OpenPage(r.PageOrder[len(r.PageOrder)-1])
	}
	index = (index + delta + len(r.PageOrder)) % len(r.PageOrder)
	return r.OpenPage(r.PageOrder[index])
}

func registerPages(r *Runtime) {
	r.Register(Operation{Verb: "list", Noun: "pages", Description: "List pages in registration order", Result: "page[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		pages := make([]map[string]any, 0, len(r.PageOrder))
		for _, id := range r.PageOrder {
			page := r.Pages[id]
			pages = append(pages, map[string]any{"id": page.ID, "title": page.Title, "active": id == r.ActivePage})
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
