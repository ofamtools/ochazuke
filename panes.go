package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PanesFile is the remembered pane dividers in the state directory: an
// object of page id to Layout.Shift.
const PanesFile = "panes.json"

// PanesPath is the panes file, "" when dividers are not persisted (no
// state directory: Config.Disable, a test binary).
func (c *Config) PanesPath() string {
	dir := c.StateDirectory()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, PanesFile)
}

// panesState is the dividers by page id: read from the panes file on first
// use, kept in memory when there is no state directory.
func (c *Config) panesState() map[string]int {
	if c.panes != nil {
		return c.panes
	}
	c.panes = map[string]int{}
	path := c.PanesPath()
	if path == "" {
		return c.panes
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c.panes
	}
	if err == nil {
		err = json.Unmarshal(data, &c.panes)
	}
	if err != nil {
		c.panes = map[string]int{}
		c.report(PanesFile + ": " + err.Error() + " (dividers reset)")
	}
	for page, shift := range c.panes {
		c.panes[page] = clampShift(shift)
	}
	return c.panes
}

// writePanes replaces the panes file atomically; nothing is written
// without a state directory.
func (c *Config) writePanes() error {
	path := c.PanesPath()
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(c.panes, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, ".panes-*.json", append(data, '\n'))
}

// writeAtomic replaces path with data: a temp file in the same directory
// (named by pattern), renamed over it.
func writeAtomic(path, pattern string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func clampShift(shift int) int { return min(max(shift, -PaneShiftLimit), PaneShiftLimit) }

// pageAt is the page instance id is, or is beneath; nil outside a page.
func (r *Runtime) pageAt(id string) *Page {
	for _, at := range r.Ancestry(id) {
		if page := r.PageOf(at); page != nil {
			return page
		}
	}
	return nil
}

// PaneShift is the remembered divider of page (Layout.Shift), 0 for the
// split's default.
func (r *Runtime) PaneShift(page string) int {
	c := r.Config()
	if c == nil {
		return 0
	}
	return c.panesState()[page]
}

// SetPaneShift remembers page's divider, clamped to PaneShiftLimit either
// way, and writes the panes file (the state directory's PanesFile) when
// there is one; a failed write is a notice and the divider still holds for
// the session. It returns the shift kept.
func (r *Runtime) SetPaneShift(page string, shift int) (int, error) {
	if r.PageOf(page) == nil {
		return 0, fmt.Errorf("unknown page %q", page)
	}
	c := r.Config()
	if c == nil {
		return 0, errors.New("no config")
	}
	shift = clampShift(shift)
	panes := c.panesState()
	if panes[page] == shift {
		return shift, nil
	}
	if shift == 0 {
		delete(panes, page)
	} else {
		panes[page] = shift
	}
	if err := c.writePanes(); err != nil {
		c.report(PanesFile + ": " + err.Error() + " (not saved)")
	}
	return shift, nil
}

// PaneLayout is l with the divider of the page id is on (or is): a page
// renderer that draws a list/detail split passes its Layout through it, so
// Split, SplitBlock, SplitFocusBlock and Divide follow the page's pane
// keys. Called while the page renders, it also records that the page draws
// a split, which is when pane.grow, pane.shrink and pane.reset answer their
// keys and show in the hints.
func (r *Runtime) PaneLayout(id string, l Layout) Layout {
	page := r.pageAt(id)
	if page == nil {
		return l
	}
	if page.rendering {
		page.splitting = true
	}
	l.Shift = r.PaneShift(page.ID)
	return l
}

// paneSplit is the guard of the pane rows: no modal is open and the page
// holding the focus drew a split in its last frame.
func paneSplit(r *Runtime, ev KeyEvent) bool {
	if r.TopModal() != "" {
		return false
	}
	page := r.pageAt(ev.Focus)
	return page != nil && page.split
}

func registerPanes(r *Runtime) {
	r.Define("app.pane")
	r.DefineGuard("pane-split", paneSplit)
	params := map[string]Parameter{
		"steps": {Type: "integer", Description: fmt.Sprintf("How many steps of %d%% to move the divider (the count; default 1)", PaneStep)},
		"page":  {Type: "string", Description: "The page (default the current page)"},
	}
	page := func(a Arguments) string { return StringArg(a, "page", r.Page) }
	result := func(id string, shift int) any { return map[string]any{"page": id, "shift": shift} }
	move := func(sign int) Handler {
		return func(r *Runtime, c Context, a Arguments) (any, error) {
			id := page(a)
			shift, err := r.SetPaneShift(id, r.PaneShift(id)+sign*PaneStep*max(1, IntArg(a, "steps", 1)))
			if err != nil {
				return nil, err
			}
			return result(id, shift), nil
		}
	}
	r.Register(Operation{Verb: "grow", Noun: "app.pane", Description: "Grow the page's first pane (the list, left or top) by moving the split's divider; remembered per page", Parameters: params, Result: "page and shift", Run: move(1)})
	r.Register(Operation{Verb: "shrink", Noun: "app.pane", Description: "Shrink the page's first pane (the list, left or top) by moving the split's divider; remembered per page", Parameters: params, Result: "page and shift", Run: move(-1)})
	r.Register(Operation{Verb: "reset", Noun: "app.pane", Description: "Put the page's split divider back at its default", Parameters: map[string]Parameter{"page": params["page"]}, Result: "page and shift", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := page(a)
		shift, err := r.SetPaneShift(id, 0)
		if err != nil {
			return nil, err
		}
		return result(id, shift), nil
	}})
	// > and < move the divider (Vim's ctrl+w > and ctrl+w < without the
	// prefix), = puts it back (ctrl+w =); counts repeat (3>). Only while
	// the page draws a split; one footer hint for the three.
	pane := func(op string) Binding {
		return Do(op).FromPayload("steps", SourceCount).Guard("pane-split")
	}
	r.Define("app").Mode("normal").
		Bind(">", pane("app.pane.grow").Hinted(">/< resize")).
		Bind("<", pane("app.pane.shrink").Hide()).
		Bind("=", Do("app.pane.reset").Guard("pane-split").Hide())
}
