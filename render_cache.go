package ochazuke

import (
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
)

// RenderMaxFPSSetting limits background frame composition; 0 disables it.
// Keys, paste, calls and resize always draw immediately. Work and delivery
// continue for every message; only composition is coalesced.
const RenderMaxFPSSetting = "render.max-fps"

// InvalidateRender forces composition on the next View. Hosts that change
// visible state after Model.Update, and headless drivers taking an explicit
// snapshot, can use it to bypass the background frame budget.
func (m *Model) InvalidateRender() {
	m.redraw.skip, m.redraw.dirty = false, false
	m.redraw.urgent = true
}

type renderFrameMsg struct {
	model *Model
	at    time.Time
}

type modelRedraw struct {
	view                       tea.View
	ready, skip, dirty, urgent bool
	last, pending              time.Time
	// Tests enable counting explicitly. There is no logging, I/O, map
	// allocation or per-message formatting when it is nil.
	trace *renderTrace
}

type renderTrace struct {
	messages                        map[string]int
	views, frames                   int
	compositions, modalCompositions int
}

func validRenderFPS(value any) error {
	n, ok := value.(int)
	if !ok || n < 0 || n > 240 {
		return fmt.Errorf("render.max-fps must be between 0 and 240")
	}
	return nil
}

func (c *modelRedraw) begin(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	c.skip = false
	if c.trace != nil {
		c.trace.messages[fmt.Sprintf("%T", msg)]++
	}
	if tick, ok := msg.(renderFrameMsg); ok && tick.model == m {
		if !tick.at.Equal(c.pending) {
			c.skip = c.ready && !c.urgent
			return nil, true
		}
		c.pending = time.Time{}
		changed := c.dirty
		c.dirty = false
		return c.schedule(m, msg, changed), true
	}
	return nil, false
}

// A terminal's identical read has no callback or visible job. Inspect its
// value before delivery, while its ticket is still current. Never suppress
// an application callback, an error, a non-terminal completion, or a read
// whose stream Status changed: an application may draw that state outside
// the pane (a "running"/"exited 0" title), so it is not the pane's cells.
func (c *modelRedraw) messageChanges(m *Model, msg tea.Msg) bool {
	if done, ok := msg.(Completion); ok {
		r := m.Runtime
		if !r.Accepts(done.Ticket) {
			return false
		}
		j := r.jobs[done.Job]
		if term := r.Terminal(done.Owner); term != nil && j != nil && j.then == nil && !tickerShows(r, j.JobInfo) && done.Err == nil {
			if f, ok := done.Value.(TerminalFrame); ok {
				// A Status change may show anywhere (a row, a title), drawn or not.
				if f.Status != term.frame.Status {
					return true
				}
				if c.ready && !r.rendered(done.Owner) {
					return false
				}
				return !sameTerminalFrame(f, term.frame)
			}
		}
	}
	return true // a host or a component may have changed arbitrary app state
}

// A placement belongs to the active page's last frame, not merely to an
// instance that still exists under an inactive page or a hidden tab.
func (r *Runtime) rendered(id string) bool {
	if r.insideModal(id) {
		return true
	}
	page := r.PageOf(r.Page)
	if page == nil {
		return false
	}
	return slices.ContainsFunc(page.drawn, func(p Placement) bool { return p.ID == id })
}

func (c *modelRedraw) schedule(m *Model, msg tea.Msg, changed bool) tea.Cmd {
	fps := m.Runtime.Settings().Int(RenderMaxFPSSetting)
	if fps <= 0 {
		return nil
	}
	if c.urgent {
		return nil
	}
	if !changed && c.ready {
		c.skip = true
		return nil
	}
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, tea.WindowSizeMsg, tea.BackgroundColorMsg, CallMsg, SettingsPollMsg:
		c.dirty = false
		c.urgent = true
		return nil
	}
	now := m.clock()
	due := c.last.Add(time.Second / time.Duration(fps))
	if !c.ready || now.Before(c.last) || !now.Before(due) {
		c.dirty = false
		c.urgent = true
		return nil
	}
	c.skip, c.dirty = true, true
	if !c.pending.IsZero() {
		return nil
	}
	c.pending = due
	return tea.Tick(due.Sub(now), func(time.Time) tea.Msg { return renderFrameMsg{m, due} })
}

func (c *modelRedraw) reuse() (tea.View, bool) {
	if c.trace != nil {
		c.trace.views++
	}
	if c.skip && c.ready {
		c.skip = false // direct View calls still observe public app fields
		v := c.view
		if v.Cursor != nil {
			cursor := *v.Cursor
			v.Cursor = &cursor
		}
		return v, true
	}
	return tea.View{}, false
}

func (c *modelRedraw) remember(m *Model, v tea.View) {
	c.view, c.ready, c.last = v, true, m.clock()
	c.urgent = false
	if v.Cursor != nil {
		cursor := *v.Cursor
		c.view.Cursor = &cursor
	}
	if c.trace != nil {
		c.trace.frames++
	}
}
