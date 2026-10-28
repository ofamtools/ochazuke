package ochazuke

import "errors"

// List motions beyond j/k, the Vim set every list answers in normal mode
// (rows on the list noun, so a row noun, the list instance or a component
// that binds the same keys answers first):
//
//	g g / G         list.goto: the first / last row; with a count (5G, 5gg)
//	                the row of that number, clamped
//	ctrl+d / ctrl+u list.scroll {half}: half a page down / up; a count moves
//	                that many rows (Vim's [count]ctrl+d)
//	ctrl+f / ctrl+b list.scroll: a whole page down / up; a count is pages
//
// A page is the rows the list's own window showed in the last frame when
// the list windows itself (more rows than its height), else the height of
// the page body it is drawn on, else the rows it showed (a list in a
// modal), else defaultPageRows before any frame. A page motion moves the
// window with the selection by the same number of rows, so the selected
// row keeps its line on screen (Vim's ctrl+d), and windowOffset then keeps
// the scrolloff margin; at the ends the window stops and the selection
// goes on to the first or last row. None of them spills to another stop:
// they clamp at the list's edges, quietly (P8). All are motions
// (Operation.Motion): off the footer, under movement in ? help.

// defaultPageRows is a page before the list was drawn (the CLI, a test).
const defaultPageRows = 10

// pageRows is the number of rows one page motion counts as a page.
func (l *List) pageRows() int {
	if l.windowed && l.window > 0 {
		return l.window
	}
	if page := l.r.PageOf(l.r.focusRoot()); page != nil && page.height > 0 && l.r.inPage(l.id, page.ID) {
		return page.height
	}
	if l.window > 0 {
		return l.window
	}
	return defaultPageRows
}

// inPage reports whether instance id is drawn on page (the page is on its
// ancestry).
func (r *Runtime) inPage(id, page string) bool {
	for _, at := range r.Ancestry(id) {
		if at == page {
			return true
		}
	}
	return false
}

// goTo selects row index i (clamped), keeping the window where it is:
// the next View moves it only as far as the scrolloff needs.
func (l *List) goTo(i int) {
	if l.selected < 0 || len(l.items) == 0 {
		return
	}
	l.Move(min(max(i, 0), len(l.items)-1) - l.selected)
}

// page moves the selection by rows (negative up), clamped, and the window
// with it by the rows actually moved.
func (l *List) page(rows int) {
	if l.selected < 0 {
		return
	}
	from := l.selected
	l.Move(rows)
	l.offset = max(0, l.offset+l.selected-from)
}

func registerListMotions(r *Runtime) {
	registerColumnMotions(r)
	resolve := func(c Context, args Arguments) (*List, error) {
		if id := StringArg(args, "id", ""); id != "" {
			if list := r.List(id); list != nil {
				return list, nil
			}
			return nil, errors.New("unknown list " + id)
		}
		for _, id := range r.Ancestry(focusOf(c)) {
			if list := r.List(id); list != nil && c.InputID == "" {
				return list, nil
			}
		}
		return nil, errors.New("no focused list")
	}
	for _, spec := range []struct {
		verb, description string
		parameters        map[string]Parameter
		run               func(*List, Arguments)
	}{
		{"goto", "Select the first or last row, or the row numbered row (5G); clamps, never spills", map[string]Parameter{
			"id":       {Type: "string", Description: "The list (default: the focused one)"},
			"position": {Type: "string", Enum: []string{"first", "last"}, Description: "Where to go without a row number (default first)"},
			"row":      {Type: "integer", Description: "The row number, 1 the first (a count: 5G); 0 or absent goes to position"},
		}, func(l *List, a Arguments) {
			switch row := IntArg(a, "row", 0); {
			case row > 0:
				l.goTo(row - 1)
			case StringArg(a, "position", "first") == "last":
				l.goTo(len(l.items) - 1)
			default:
				l.goTo(0)
			}
		}},
		{"scroll", "Move the selection and the window a page (half a page with half) down or up; clamps, never spills", map[string]Parameter{
			"id":    {Type: "string", Description: "The list (default: the focused one)"},
			"delta": {Type: "integer", Required: true, Description: "1 down, -1 up"},
			"half":  {Type: "boolean", Description: "Half a page (ctrl+d, ctrl+u); steps is then rows"},
			"steps": {Type: "integer", Description: "A count: pages, or rows with half (Vim's [count]ctrl+d); 0 or absent is one page or half a page"},
		}, func(l *List, a Arguments) {
			rows, steps := l.pageRows(), IntArg(a, "steps", 0)
			half, _ := a["half"].(bool)
			switch {
			case half && steps > 0:
				rows = steps
			case half:
				rows = max(1, rows/2)
			default:
				rows *= max(1, steps)
			}
			if IntArg(a, "delta", 1) < 0 {
				rows = -rows
			}
			l.page(rows)
		}},
	} {
		r.Register(Operation{Verb: spec.verb, Noun: "ochazuke.list", Description: spec.description, Parameters: spec.parameters, Result: "list state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			list, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			spec.run(list, args)
			return list.state(), nil
		}})
	}
}
