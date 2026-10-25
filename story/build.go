package story

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// Demo is the one application noun a story has: page instances and rows
// are of it, so its rows answer at every level of a story page.
const Demo = "demo"

// Session is one built story: the runtime, the model and the demo state
// the verbs act on (fake jobs and panes).
type Session struct {
	Story *Story
	R     *ochazuke.Runtime
	M     *ochazuke.Model
	// Reset is set by demo.reset; the host (TUI or Driver) rebuilds.
	Reset bool

	pages   map[string]*Page
	lists   map[string]*ochazuke.List
	inputs  map[string]*ochazuke.Input
	streams []*FakeStream
	jobs    map[string][]chan error // verb -> running jobs, oldest first
	done    chan struct{}
	cursor  *tea.Cursor
}

// Options change how a session renders, never what it does.
type Options struct {
	Mono bool // the mono theme: no colour, as goldens compare
}

// Build makes a session from a story's pages. A trimmed story's base is
// resolved by the caller (Resolve) before Build.
func Build(s *Story, opt Options) (*Session, error) {
	pages := s.AllPages()
	if len(pages) == 0 {
		return nil, errors.New("story has no page")
	}
	r := ochazuke.NewRuntime()
	m := ochazuke.NewModel(r)
	m.Title = "o-story"
	if opt.Mono {
		m.Appearance.Theme = "mono"
	}
	ss := &Session{Story: s, R: r, M: m, pages: map[string]*Page{}, lists: map[string]*ochazuke.List{}, inputs: map[string]*ochazuke.Input{}, jobs: map[string][]chan error{}, done: make(chan struct{})}
	if err := ss.defineDemo(pages); err != nil {
		return nil, err
	}
	open := ""
	for i, p := range pages {
		id := p.ID
		if id == "" {
			id = fmt.Sprintf("page%d", i+1)
			p.ID = id
		}
		if err := ss.addPage(p); err != nil {
			return nil, err
		}
		if open == "" || p.Open {
			open = id
		}
	}
	if err := r.OpenPage(open); err != nil {
		return nil, err
	}
	ss.size()
	m.View()
	page := ss.pages[open]
	if page.Offset > 0 {
		if _, err := r.Execute(ochazuke.Request{Noun: "surface", Verb: "scroll", Args: ochazuke.Arguments{"lines": page.Offset}}); err != nil {
			return nil, err
		}
	}
	for _, p := range pages {
		for _, j := range p.Jobs {
			for range j.Start {
				noun, verb, _ := strings.Cut(j.Verb, ".")
				if _, err := r.Execute(ochazuke.Request{Noun: noun, Verb: verb}); err != nil {
					return nil, fmt.Errorf("start %s: %w", j.Verb, err)
				}
			}
		}
	}
	return ss, nil
}

// Close releases every fake job and pane goroutine.
func (ss *Session) Close() {
	select {
	case <-ss.done:
	default:
		close(ss.done)
	}
}

// defineDemo registers the demo noun: its verbs and default rows (data),
// the job verbs the pages declare and their extra binding rows.
func (ss *Session) defineDemo(pages []*Page) error {
	r := ss.R
	d := r.Define(Demo).
		Verb("reset", func(ochazuke.Ctx) ochazuke.Result { ss.Reset = true; return ochazuke.Done(nil) }, "Rebuild the story from its file").
		Verb("open", ss.open, "Open the row: attach the fake pane when the story has one for it").
		Verb("notify", func(c ochazuke.Ctx) ochazuke.Result {
			ss.M.Notify("notice from " + label(c.Instance))
			return ochazuke.Done(nil)
		}, "Add a persistent notice naming the row").
		Verb("ping", func(c ochazuke.Ctx) ochazuke.Result {
			ss.M.Notify("ping " + label(c.Instance))
			return ochazuke.Done(nil)
		}, "Add a notice saying ping and the row").
		Verb("delete", ss.delete, "Remove the row from its list").Confirm("Delete {{label}}?").
		Verb("finish", ss.finish, "Finish (or fail) the oldest running job of a demo verb")
	r.Operations[Demo+".finish"] = withParams(r.Operations[Demo+".finish"], map[string]ochazuke.Parameter{
		"verb": {Type: "string", Required: true, Description: "the job verb, demo.<name>"},
		"fail": {Type: "boolean", Description: "finish it failed"},
	})
	d.Mode("normal").
		Bind("q", ochazuke.Do("app.quit")).
		Bind("r", ochazuke.Do(Demo+".reset")).
		Bind("enter", ochazuke.Do(Demo+".open")).
		Bind("n", ochazuke.Do(Demo+".notify")).
		Bind("x", ochazuke.Do(Demo+".delete"))
	for _, p := range pages {
		for _, j := range p.Jobs {
			noun, verb, ok := strings.Cut(j.Verb, ".")
			if !ok || noun != Demo || verb == "" {
				return fmt.Errorf("job verb %q: must be %s.<name>", j.Verb, Demo)
			}
			key := j.Verb
			if _, known := r.Operations[key]; !known {
				d.Verb(verb, func(ochazuke.Ctx) ochazuke.Result {
					finish := make(chan error, 1)
					ss.jobs[key] = append(ss.jobs[key], finish)
					done := ss.done
					return ochazuke.Pending(func(ctx context.Context) (any, error) {
						select {
						case err := <-finish:
							return nil, err
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-done:
							return nil, errors.New("story closed")
						}
					})
				}, "A fake job that runs until its finish key")
				if j.Quiet {
					d.Quiet()
				}
			}
			if j.Key != "" {
				d.Mode("normal").Bind(j.Key, ochazuke.Do(key))
			}
			if j.FinishOnKey != "" {
				d.Mode("normal").Bind(j.FinishOnKey, ochazuke.Do(Demo+".finish", ochazuke.Arguments{"verb": key}))
			}
			if j.FailOnKey != "" {
				d.Mode("normal").Bind(j.FailOnKey, ochazuke.Do(Demo+".finish", ochazuke.Arguments{"verb": key, "fail": true}))
			}
		}
		for _, b := range p.Bind {
			if _, ok := r.Operations[b.Do]; !ok {
				return fmt.Errorf("bind %s: unknown operation %q", b.Keys, b.Do)
			}
			mode := b.Mode
			if mode == "" {
				mode = "normal"
			}
			r.Define(b.Noun).Mode(mode).Bind(b.Keys, ochazuke.Do(b.Do, ochazuke.Arguments(b.Args)))
		}
	}
	return nil
}

func withParams(op ochazuke.Operation, params map[string]ochazuke.Parameter) ochazuke.Operation {
	op.Parameters = params
	return op
}

func label(inst *ochazuke.Instance) string {
	if inst == nil {
		return ""
	}
	if inst.Label != "" {
		return inst.Label
	}
	return inst.ID
}

func (ss *Session) finish(c ochazuke.Ctx) ochazuke.Result {
	key := ochazuke.StringArg(c.Args, "verb", "")
	running := ss.jobs[key]
	if len(running) == 0 {
		return ochazuke.Failed(fmt.Errorf("no running %s job", key))
	}
	var err error
	if fail, _ := c.Args["fail"].(bool); fail {
		err = errors.New("failed on key")
	}
	running[0] <- err
	ss.jobs[key] = running[1:]
	return ochazuke.Done(nil)
}

func (ss *Session) delete(c ochazuke.Ctx) ochazuke.Result {
	inst := c.Instance
	l := ss.R.List(inst.Parent)
	if l == nil {
		return ochazuke.Failed(errors.New("not a list row"))
	}
	var keep []ochazuke.ListItem
	for _, it := range l.Items() {
		if it.ID != inst.ID {
			keep = append(keep, it)
		}
	}
	if err := l.SetItems(keep); err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(inst.ID)
}

// open attaches the page's fake pane when the row is its attach row.
func (ss *Session) open(c ochazuke.Ctx) ochazuke.Result {
	inst := c.Instance
	r := ss.R
	page := ss.pages[r.Page]
	if page == nil || page.Term == nil || !page.Term.Fake || inst == nil {
		return ochazuke.Done("opened " + label(inst))
	}
	attach := page.Term.Attach
	if attach == "" && len(page.Lists) > 0 {
		attach = rowsOf(page.Lists[0])[0].ID
	}
	if inst.ID != attach {
		return ochazuke.Done("opened " + label(inst))
	}
	id := page.ID + "-pane"
	if r.Terminal(id) == nil {
		stream := newFakeStream(ss.done)
		ss.streams = append(ss.streams, stream)
		if _, err := r.AddTerminal(id, stream, ochazuke.Parent(page.ID), ochazuke.CloseOnDetach(), ochazuke.FocusOnClose(inst.ID)); err != nil {
			return ochazuke.Failed(err)
		}
	}
	if err := r.SetFocus(id); err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(id)
}

// Emit makes every live fake pane print one more output line (the
// "output" tick).
func (ss *Session) Emit() bool {
	any := false
	for _, s := range ss.streams {
		if !s.released() {
			s.Emit()
			any = true
		}
	}
	return any
}

// HasPane reports whether a fake pane is live.
func (ss *Session) HasPane() bool {
	for _, s := range ss.streams {
		if !s.released() {
			return true
		}
	}
	return false
}

func rowsOf(l ListSpec) []ochazuke.ListItem {
	var out []ochazuke.ListItem
	if len(l.Items) > 0 {
		for _, it := range l.Items {
			out = append(out, ochazuke.ListItem{ID: it.ID, Label: it.Label, Noun: Demo, Cells: it.Cells})
		}
		return out
	}
	n := max(l.Rows, len(l.Labels))
	for i := range n {
		id := fmt.Sprintf("%s%d", l.ID, i+1)
		lbl := id
		if i < len(l.Labels) {
			lbl = l.Labels[i]
		}
		out = append(out, ochazuke.ListItem{ID: id, Label: lbl, Noun: Demo})
	}
	return out
}

func (ss *Session) addPage(p *Page) error {
	r := ss.R
	r.Instance(p.ID, ochazuke.Kind(Demo))
	focus := p.Focus
	for _, l := range p.Lists {
		var opts []ochazuke.InstanceOption
		opts = append(opts, ochazuke.Parent(p.ID))
		if l.NoSpill {
			opts = append(opts, ochazuke.NoSpill())
		}
		if len(l.Columns) > 0 {
			opts = append(opts, ochazuke.Columns(l.Columns...))
		}
		items := rowsOf(l)
		list, err := r.AddList(l.ID, items, opts...)
		if err != nil {
			return fmt.Errorf("list %s: %w", l.ID, err)
		}
		ss.lists[l.ID] = list
		if l.Selected > 0 && l.Selected < len(items) {
			if err := list.Select(items[l.Selected].ID); err != nil {
				return err
			}
		}
		for _, i := range l.Marked {
			if i >= 0 && i < len(items) {
				if err := list.ToggleMark(items[i].ID); err != nil {
					return err
				}
			}
		}
		if focus == "" {
			focus = list.FocusTarget()
		}
	}
	for _, in := range p.Inputs {
		input := ochazuke.NewInput(in.ID, in.Text, false)
		if err := r.AddInput(input, ochazuke.Parent(p.ID)); err != nil {
			return fmt.Errorf("input %s: %w", in.ID, err)
		}
		ss.inputs[in.ID] = input
		if focus == "" {
			focus = in.ID
		}
	}
	ss.pages[p.ID] = p
	title := p.Title
	if title == "" {
		title = p.ID
	}
	var opts []ochazuke.InstanceOption
	if focus != "" {
		opts = append(opts, ochazuke.Focus(focus))
	}
	_, err := r.AddPage(p.ID, title, func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return ss.render(p, w, h) }, opts...)
	return err
}

// render draws a page: the lists (stacked, or side by side in a row), then
// the inputs, then the pane when attached.
func (ss *Session) render(p *Page, width, height int) ochazuke.Block {
	order := p.Order
	if len(order) == 0 {
		for _, l := range p.Lists {
			order = append(order, l.ID)
		}
	}
	spec := map[string]ListSpec{}
	for _, l := range p.Lists {
		spec[l.ID] = l
	}
	var parts []ochazuke.Block
	used := 0
	listBlock := func(id string, w, rest int) string {
		l, list := spec[id], ss.lists[id]
		h := l.Height
		switch {
		case h < 0:
			h = max(1, rest)
		case h == 0:
			h = max(1, list.Len())
			if len(list.Columns()) > 0 {
				h++ // the header line
			}
		}
		text := list.View(w, h)
		if !p.NoHead {
			text = id + "\n" + text
		}
		return text
	}
	head := 0
	if !p.NoHead {
		head = 1
	}
	if p.Layout == "row" && len(order) > 0 {
		w := max(1, (width-(len(order)-1))/len(order))
		var cols []string
		for i, id := range order {
			if i > 0 {
				cols = append(cols, " ")
			}
			cols = append(cols, lipgloss.NewStyle().Width(w).Render(listBlock(id, w, height-head)))
		}
		text := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
		parts = append(parts, ochazuke.Text(text))
		used += strings.Count(text, "\n") + 1
	} else {
		for _, id := range order {
			if _, ok := ss.lists[id]; !ok {
				continue
			}
			text := listBlock(id, width, height-used-head)
			parts = append(parts, ochazuke.Text(text))
			used += strings.Count(text, "\n") + 1
		}
	}
	for _, in := range p.Inputs {
		prefix := in.Label
		if prefix == "" {
			prefix = in.ID
		}
		prefix += ": "
		pw := ansi.StringWidth(prefix)
		block := ochazuke.InputBlock(ss.inputs[in.ID], max(1, width-pw), 1)
		parts = append(parts, block.Offset(pw, 0).WithText(prefix+block.Text))
		used++
	}
	if t := ss.R.Terminal(p.ID + "-pane"); t != nil {
		h := 6
		if p.Term != nil && p.Term.Height > 0 {
			h = p.Term.Height
		}
		state := "pane (detached)"
		if t.Focused() {
			state = "pane (attached: ctrl+a d detaches)"
		}
		block := t.ViewBlock(width, h)
		if !t.Focused() {
			block.Cursor = nil
		}
		parts = append(parts, ochazuke.Text(state), block)
	}
	return stack(parts)
}

// stack joins blocks top to bottom, keeping the one cursor in place.
func stack(parts []ochazuke.Block) ochazuke.Block {
	var texts []string
	var out ochazuke.Block
	line := 0
	for _, b := range parts {
		if b.Cursor != nil && out.Cursor == nil {
			out = b.Offset(0, line)
		}
		texts = append(texts, b.Text)
		line += strings.Count(b.Text, "\n") + 1
	}
	return out.WithText(strings.Join(texts, "\n"))
}

// size sets the model's viewport so the story's area is what the chrome
// mode shows: the whole shell (full), the page body alone (none: the
// shell is made exactly as much larger as its chrome and the body is cut
// out), or the ticker rows alone (ticker-only).
func (ss *Session) size() {
	w, h := ss.Story.Size()
	switch ss.Story.Chrome {
	case "none":
		ss.M.Width, ss.M.Height = w+4, h+6+min(2, len(ss.M.JobsTicker(w)))
	case "ticker-only":
		ss.M.Width, ss.M.Height = w+4, 24
	default:
		ss.M.Width, ss.M.Height = w, h
	}
}

// Frame renders the story's area: exactly Width x Height cells of trusted
// ANSI, by the story's chrome mode.
func (ss *Session) Frame() string {
	w, h := ss.Story.Size()
	ss.size()
	var rows []string
	view := ss.M.View()
	ss.cursor = view.Cursor
	switch ss.Story.Chrome {
	case "none":
		all := strings.Split(view.Content, "\n")
		for i := 3; i < 3+h && i < len(all); i++ {
			rows = append(rows, ansi.Cut(all[i], 2, 2+w))
		}
		if c := ss.cursor; c != nil {
			moved := *c
			moved.X, moved.Y = c.X-2, c.Y-3
			ss.cursor = &moved
		}
	case "ticker-only":
		rows = ss.M.JobsTicker(w)
		ss.cursor = nil
	default:
		rows = strings.Split(view.Content, "\n")
	}
	if c := ss.cursor; c != nil && (c.X < 0 || c.Y < 0 || c.X >= w || c.Y >= h) {
		ss.cursor = nil
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	for len(rows) < h {
		rows = append(rows, "")
	}
	for i, row := range rows {
		if pad := w - ansi.StringWidth(row); pad > 0 {
			row += strings.Repeat(" ", pad)
		}
		rows[i] = ansi.Truncate(row, w, "")
	}
	return strings.Join(rows, "\n")
}

// Cursor is the terminal cursor of the last Frame, relative to the
// story's area, or nil.
func (ss *Session) Cursor() *tea.Cursor { return ss.cursor }

// Snapshot bakes the session's current state into a page set: lists with
// their rows, selection and marks, the focus and the page offset. It
// reports what it cannot carry (an open modal, an attached pane, a mode
// other than normal).
func (ss *Session) Snapshot() ([]*Page, []string) {
	r := ss.R
	var warn []string
	if r.TopModal() != "" {
		warn = append(warn, "a modal is open ("+r.TopModal()+"); the snapshot starts without it")
	}
	if ss.HasPane() {
		warn = append(warn, "a fake pane is live; the snapshot starts detached")
	}
	if r.Mode.Name != "normal" {
		warn = append(warn, "mode is "+r.Mode.Name+"; the snapshot starts in normal")
	}
	offset, _ := r.Execute(ochazuke.Request{Noun: "surface", Verb: "scroll", Args: ochazuke.Arguments{"lines": 0}})
	var out []*Page
	for _, src := range ss.Story.AllPages() {
		p := *src
		p.Lists = nil
		for _, l := range src.Lists {
			list := ss.lists[l.ID]
			spec := ListSpec{ID: l.ID, NoSpill: l.NoSpill, Height: l.Height, Columns: list.Columns()}
			marked := map[string]bool{}
			for _, it := range list.Marked() {
				marked[it.ID] = true
			}
			sel, _ := list.Selected()
			for i, it := range list.Items() {
				spec.Items = append(spec.Items, ItemSpec{ID: it.ID, Label: it.Label, Cells: it.Cells})
				if it.ID == sel.ID {
					spec.Selected = i
				}
				if marked[it.ID] {
					spec.Marked = append(spec.Marked, i)
				}
			}
			p.Lists = append(p.Lists, spec)
		}
		p.Inputs = slices.Clone(src.Inputs)
		for i, in := range p.Inputs {
			if input := ss.inputs[in.ID]; input != nil {
				p.Inputs[i].Text = string(input.Text)
			}
		}
		p.Open = src.ID == r.Page
		p.Offset = 0
		if p.Open {
			if n, ok := offset.(int); ok {
				p.Offset = n
			}
			p.Focus = r.BaseFocus()
			if r.Terminal(p.Focus) != nil {
				p.Focus = ""
			}
		}
		p.Jobs = nil
		for _, j := range src.Jobs {
			j.Start = 0
			p.Jobs = append(p.Jobs, j)
		}
		out = append(out, &p)
	}
	return out, warn
}
