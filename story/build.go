package story

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/ochazuke/markdown"
	"github.com/unixsuperhero/ochazuke/markdown/cliview"
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
	// StateDir is the state directory of a story with State, "" without;
	// Close removes it unless the session is restarting into it.
	StateDir string
	// configDir is the temp config directory of a story with Config.
	configDir string

	pages map[string]*Page
	lists map[string]*ochazuke.List
	trees map[string]*ochazuke.Tree
	// records are the page-drawn record editors by id.
	records map[string]*ochazuke.RecordEditor
	// Writes is the log of every change an editable tree, record editor or
	// Markdown document wrote.
	Writes  []string
	inputs  map[string]*ochazuke.Input
	details map[string]*ochazuke.Detail
	docs    map[string]*markdown.Doc // the page's Markdown document by page id
	// timelines are the page's timeline by page id; streamed counts the
	// lines timeline-stream added.
	timelines map[string]*ochazuke.Timeline
	streamed  int
	streams   []*FakeStream
	// hidden are the stops demo.hide took off the page (drawn again by
	// the next demo.hide of the same id).
	hidden map[string]bool
	jobs   map[string][]chan error // verb -> running jobs, oldest first
	done   chan struct{}
	cursor *tea.Cursor
}

// Options change how a session renders, never what it does.
type Options struct {
	Mono bool // the mono theme: no colour, as goldens compare
}

// Build makes a session from a story's pages. A trimmed story's base is
// resolved by the caller (Resolve) before Build.
func Build(s *Story, opt Options) (*Session, error) { return build(s, opt, "") }

// build is Build with the state directory a restart keeps ("" makes one
// for a story with State).
func build(s *Story, opt Options, stateDir string) (*Session, error) {
	pages := s.AllPages()
	if len(pages) == 0 {
		return nil, errors.New("story has no page")
	}
	if s.State && stateDir == "" {
		dir, err := os.MkdirTemp("", "o-story-state-")
		if err != nil {
			return nil, err
		}
		stateDir = dir
	}
	r := ochazuke.NewRuntime()
	// A story is a sandbox: it reads and writes no user config files,
	// and state only in its own directory.
	r.Config().Disable()
	r.Config().StateDir = stateDir
	if err := r.DeclareSetting(s.Settings...); err != nil {
		return nil, err
	}
	m := ochazuke.NewModel(r)
	m.Title = "o-story"
	// A foreground program runs headless (no terminal to hand over): no
	// input, its output dropped, its exit the job's completion.
	m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd {
		return func() tea.Msg {
			msg.Cmd.Stdin, msg.Cmd.Stdout, msg.Cmd.Stderr = strings.NewReader(""), io.Discard, io.Discard
			return msg.Done(msg.Cmd.Run())
		}
	}
	if opt.Mono {
		m.Appearance.Theme = "mono"
	}
	if len(s.Themes) > 0 {
		schemes, problems := ochazuke.ParseSchemes(s.Themes)
		m.Appearance.Schemes = ochazuke.NewSchemeSet(schemes)
		for _, p := range problems {
			r.UI().Notify(p)
		}
	}
	if s.Theme != "" {
		m.Appearance.Theme = s.Theme
	}
	if s.Accent != "" {
		m.Appearance.Accent = lipgloss.Color(s.Accent)
	}
	ss := &Session{Story: s, R: r, M: m, StateDir: stateDir, pages: map[string]*Page{}, lists: map[string]*ochazuke.List{}, trees: map[string]*ochazuke.Tree{}, records: map[string]*ochazuke.RecordEditor{}, inputs: map[string]*ochazuke.Input{}, details: map[string]*ochazuke.Detail{}, docs: map[string]*markdown.Doc{}, timelines: map[string]*ochazuke.Timeline{}, hidden: map[string]bool{}, jobs: map[string][]chan error{}, done: make(chan struct{})}
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
	// A hidden start page gives way to the first page in the tabs, as
	// Model.Init does.
	if err := r.OpenVisibleStart(); err != nil {
		return nil, err
	}
	open = r.Page
	for _, rs := range ss.pages[open].Records {
		if rs.Modal {
			if _, err := r.OpenRecordEditor(rs.ID, ss.recordSpec(rs)); err != nil {
				return nil, fmt.Errorf("record %s: %w", rs.ID, err)
			}
		}
	}
	if cs := ss.pages[open].Chooser; cs != nil {
		if _, err := r.OpenChooser(chooserSpec(cs)); err != nil {
			return nil, fmt.Errorf("chooser: %w", err)
		}
	}
	if len(s.Config) > 0 {
		if err := ss.loadConfig(); err != nil {
			return nil, err
		}
	}
	for _, c := range s.Checks {
		if err := ss.addCheck(c); err != nil {
			return nil, err
		}
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
		for _, sp := range p.Segments {
			texts, n := sp.Texts, 0
			r.SegmentProvider("story-"+sp.ID, func(context.Context, ochazuke.Segment) (ochazuke.SegmentValue, error) {
				if len(texts) == 0 {
					return ochazuke.SegmentValue{}, nil
				}
				text := texts[min(n, len(texts)-1)]
				n++
				return ochazuke.SegmentValue{Text: text}, nil
			})
			if err := r.AddSegment(ochazuke.Segment{ID: sp.ID, Label: sp.Label, Provider: "story-" + sp.ID, Tone: sp.Tone, Order: sp.Order, Enabled: sp.Enabled}); err != nil {
				return nil, fmt.Errorf("segment %s: %w", sp.ID, err)
			}
		}
		for _, j := range p.Jobs {
			for range j.Start {
				noun, verb := ochazuke.SplitKey(j.Verb)
				if _, err := r.Execute(ochazuke.Request{Noun: noun, Verb: verb}); err != nil {
					return nil, fmt.Errorf("start %s: %w", j.Verb, err)
				}
			}
		}
	}
	return ss, nil
}

// addCheck hangs a story's verb check.
func (ss *Session) addCheck(c CheckSpec) error {
	verb, name := c.Verb, c.Name
	if verb == "" {
		verb = "app.quit"
	}
	if name == "" {
		name = "story"
	}
	return ss.R.AddVerbCheck(verb, ochazuke.VerbCheck{Name: name, Check: func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) ochazuke.Verdict {
		if c.Dirty && len(ss.Writes) == 0 {
			return ochazuke.Allow()
		}
		return ochazuke.Verdict{Ask: c.Ask, Refuse: c.Refuse}
	}})
}

// chooserSpec is the framework spec of a story's chooser.
func chooserSpec(cs *ChooserSpec) ochazuke.ChooserSpec {
	spec := ochazuke.ChooserSpec{Kind: cs.Kind, Title: cs.Title, Keep: cs.Keep}
	if spec.Kind == "" {
		spec.Kind = "story-chooser"
	}
	previews := map[string]string{}
	for _, row := range cs.Rows {
		spec.Rows = append(spec.Rows, ochazuke.ChooserRow{ID: row.ID, Label: row.Label, Cells: row.Cells})
		if row.Preview != "" {
			previews[row.ID] = row.Preview
		}
	}
	if len(previews) > 0 {
		spec.Preview = func(row ochazuke.ChooserRow) string { return previews[row.ID] }
	}
	return spec
}

// Close releases every fake job and pane goroutine.
func (ss *Session) Close() { ss.close(false) }

// close ends the session, removing its state directory unless keep.
func (ss *Session) close(keep bool) {
	select {
	case <-ss.done:
	default:
		close(ss.done)
	}
	if ss.StateDir != "" && !keep {
		os.RemoveAll(ss.StateDir)
	}
	if ss.configDir != "" {
		os.RemoveAll(ss.configDir)
	}
}

// loadConfig writes the story's config files into a temp config directory
// and loads them as Model.Init does: actions.json, then bindings.json.
func (ss *Session) loadConfig() error {
	dir, err := os.MkdirTemp("", "o-story-config-")
	if err != nil {
		return err
	}
	ss.configDir = dir
	for name, data := range ss.Story.Config {
		if name != filepath.Base(name) || name == "" {
			return fmt.Errorf("config file %q: a file name, not a path", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
	}
	ss.R.Config().Dir = dir
	ss.R.EnsureUserActions()
	ss.R.EnsureUserBindings()
	return nil
}

// defineDemo registers the demo noun: its verbs and default rows (data),
// the job verbs the pages declare and their extra binding rows.
func (ss *Session) defineDemo(pages []*Page) error {
	r := ss.R
	d := r.Define(Demo).
		Verb("reset", func(ochazuke.Ctx) ochazuke.Result { ss.Reset = true; return ochazuke.Done(nil) }, "Rebuild the story from its file").
		Verb("open", ss.open, "Open the row: attach the fake pane when the story has one for it").
		Verb("notify", func(c ochazuke.Ctx) ochazuke.Result {
			c.Notify("notice from " + label(c.Instance))
			return ochazuke.Done(nil)
		}, "Add a persistent notice naming the row").
		Verb("ping", func(c ochazuke.Ctx) ochazuke.Result {
			c.Notify("ping " + label(c.Instance))
			return ochazuke.Done(nil)
		}, "Add a notice saying ping and the row").
		Verb("delete", ss.delete, "Remove the row from its list").Confirm("Delete {{label}}?").
		Verb("finish", ss.finish, "Finish (or fail) the oldest running job of a demo verb").
		Verb("check", ss.check, "Toggle a [x] mark on the outline row, an application change on the outline's undo stack").
		Verb("hide", func(c ochazuke.Ctx) ochazuke.Result {
			id := ochazuke.StringArg(c.Args, "id", "")
			ss.hidden[id] = !ss.hidden[id]
			return ochazuke.Done(ss.hidden[id])
		}, "Stop drawing a list, tree, input or the timeline (by id), or draw it again: a tab switched away from it")
	r.Operations[Demo+".hide"] = withParams(r.Operations[Demo+".hide"], map[string]ochazuke.Parameter{
		"id": {Type: "string", Required: true, Description: "the id of the list, tree, input or timeline"},
	})
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
			noun, verb, ok := ochazuke.CutKey(j.Verb)
			if !ok || noun != Demo || verb == "" {
				return fmt.Errorf("job verb %q: must be %s.<name>", j.Verb, Demo)
			}
			key := j.Verb
			if _, known := r.Operations[key]; !known {
				d.Verb(verb, func(ochazuke.Ctx) ochazuke.Result {
					finish := make(chan error, 1)
					ss.jobs[key] = append(ss.jobs[key], finish)
					done := ss.done
					label := j.Label
					return ochazuke.Pending(func(ctx context.Context) (any, error) {
						select {
						case err := <-finish:
							return nil, err
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-done:
							return nil, errors.New("story closed")
						}
					}).Label(label)
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
		if p.Term != nil && p.Term.Hints != nil {
			ss.defineHints(p.Term.Hints)
		}
		for _, v := range p.Verbs {
			if err := ss.defineVerb(d, v); err != nil {
				return err
			}
		}
		for _, name := range p.Pending {
			if err := r.DefineMode(name, true); err != nil {
				return fmt.Errorf("pending %s: %w", name, err)
			}
		}
		for _, b := range p.Bind {
			if _, ok := r.Operation(b.Do); !ok {
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

// HintsMode is the application mode the hints recipe enters over a pane.
const HintsMode = "hints"

// defineHints is the hints recipe over the fake pane (HintsSpec): an
// application base mode, passthrough so every ctrl+letter reaches its
// rows, entered by a leader row through a verb that also sets the overlay.
// It is the recipe AGENTS.md's terminal row describes, as rows.
func (ss *Session) defineHints(h *HintsSpec) {
	r := ss.R
	if r.Operations[Demo+".hints"].Verb != "" {
		return
	}
	r.DefineMode(HintsMode, false)
	_ = r.DefinePassthrough(HintsMode)
	leave := func(c ochazuke.Ctx) {
		if term := c.Runtime.Terminal(c.Runtime.Focus); term != nil {
			term.SetOverlay(nil)
		}
		_ = c.Runtime.SetMode("terminal")
	}
	r.Define(Demo).
		Verb("hints", func(c ochazuke.Ctx) ochazuke.Result {
			term := c.Runtime.Terminal(c.Runtime.Focus)
			if term == nil {
				return ochazuke.Failed(errors.New("no attached pane"))
			}
			o := &ochazuke.TerminalOverlay{Dim: h.Dim}
			for _, m := range h.Marks {
				o.Marks = append(o.Marks, ochazuke.OverlayMark{Row: m.Row, Col: m.Col, Text: m.Text})
			}
			term.SetOverlay(o)
			if err := c.Runtime.SetMode(HintsMode); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(nil)
		}, "Label the pane's links and enter hints mode").
		Verb("hint", func(c ochazuke.Ctx) ochazuke.Result {
			ss.Writes = append(ss.Writes, "hint "+ochazuke.StringArg(c.Args, "action", "")+" "+ochazuke.StringArg(c.Args, "label", ""))
			leave(c)
			return ochazuke.Done(nil)
		}, "Act on the labelled link and leave hints mode").
		Verb("hints-cancel", func(c ochazuke.Ctx) ochazuke.Result { leave(c); return ochazuke.Done(nil) }, "Leave hints mode").
		Verb("hints-ignore", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Swallow a key in hints mode")
	r.Operations[Demo+".hint"] = withParams(r.Operations[Demo+".hint"], map[string]ochazuke.Parameter{
		"label":  {Type: "string", Description: "the label letter"},
		"action": {Type: "string", Description: "copy, type or open"},
	})
	key := h.Key
	if key == "" {
		key = "f"
	}
	hint := func(action string) ochazuke.Binding {
		return ochazuke.Do(Demo+".hint", ochazuke.Arguments{"action": action}).FromPayload("label", ochazuke.SourceLetter).Hinted(action)
	}
	r.Define("terminal").
		Mode("leader_pending").Bind(key, ochazuke.Do(Demo+".hints")).
		Mode(HintsMode).
		Pattern(ochazuke.Alpha, hint("copy")).
		Pattern(ochazuke.Upper, hint("type")).
		Pattern(ochazuke.CtrlAlpha, hint("open")).
		Bind("esc", ochazuke.Do(Demo+".hints-cancel").Hinted("cancel")).
		Pattern(ochazuke.Paste, ochazuke.Do(Demo+".hints-ignore").Hide()).
		Pattern(ochazuke.Any, ochazuke.Do(Demo+".hints-ignore").Hide())
}

// check toggles a "[x] " mark on a tree row's label the way an application
// changes a field it keeps outside the tree (a task's status): it rewrites
// the nodes, logs the write and records the change on the tree's undo
// stack (Tree.Record), so u and ctrl+r reverse it in order with the
// tree's own edits.
func (ss *Session) check(c ochazuke.Ctx) ochazuke.Result {
	id := ""
	if c.Instance != nil {
		id = c.Instance.ID
	}
	for _, tree := range ss.trees {
		if !slices.ContainsFunc(tree.Nodes(), func(n ochazuke.TreeNode) bool { return n.ID == id }) {
			continue
		}
		toggle := func() error {
			nodes := tree.Nodes()
			verb := "check"
			for i := range nodes {
				if nodes[i].ID != id {
					continue
				}
				if rest, ok := strings.CutPrefix(nodes[i].Label, "[x] "); ok {
					nodes[i].Label, verb = rest, "uncheck"
				} else {
					nodes[i].Label = "[x] " + nodes[i].Label
				}
			}
			ss.Writes = append(ss.Writes, verb+" "+id)
			return tree.SetNodes(nodes)
		}
		if err := toggle(); err != nil {
			return ochazuke.Failed(err)
		}
		if err := tree.Record(ochazuke.TreeAction{ID: id, Label: "check " + id, Undo: toggle, Redo: toggle}); err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(id)
	}
	return ochazuke.Failed(errors.New("not an outline row"))
}

// defineVerb registers a VerbSpec on the demo noun.
func (ss *Session) defineVerb(d *ochazuke.NounBuilder, v VerbSpec) error {
	noun, verb, ok := ochazuke.CutKey(v.Verb)
	if !ok || noun != Demo || verb == "" {
		return fmt.Errorf("verb %q: must be %s.<name>", v.Verb, Demo)
	}
	key, rows := v.Verb, slices.Clone(v.Rows)
	d.Verb(verb, func(c ochazuke.Ctx) ochazuke.Result {
		entry := key
		for _, name := range slices.Sorted(maps.Keys(c.Args)) {
			entry += fmt.Sprintf(" %s=%v", name, c.Args[name])
		}
		if c.Invoked.Alias {
			entry += " via=" + c.Invoked.Name
		}
		if v.Row && c.Instance != nil {
			if slices.Contains(v.Fail, c.Instance.ID) {
				return ochazuke.Failed(fmt.Errorf("%s refuses", c.Instance.ID))
			}
			entry += " row=" + c.Instance.ID
		}
		if v.Many {
			entry += " subjects=" + strings.Join(c.SubjectIDs(), ",")
		}
		ss.Writes = append(ss.Writes, entry)
		if rows != nil {
			return ochazuke.Done(rows)
		}
		return ochazuke.Done(nil)
	}, v.Description)
	if len(v.Positional) > 0 {
		d.Positional(v.Positional...)
	}
	if v.Many {
		d.Many()
	}
	ss.R.Operations[key] = withParams(ss.R.Operations[key], v.Parameters)
	for _, a := range v.Aliases {
		a.Action = key
		if err := ss.R.AddActionAlias(a); err != nil {
			return fmt.Errorf("verb %s: %w", key, err)
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
			out = append(out, ochazuke.ListItem{ID: it.ID, Label: it.Label, Noun: Demo, Cells: it.Cells, Tones: it.Tones})
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
	if p.Commands != nil {
		return ss.addCommandsPage(p)
	}
	if p.Catalog != nil {
		return ss.addCatalogPage(p)
	}
	r := ss.R
	r.Instance(p.ID, ochazuke.Kind(Demo))
	focus := p.Focus
	for _, l := range p.Lists {
		var opts []ochazuke.InstanceOption
		opts = append(opts, ochazuke.Parent(p.ID))
		if l.NoSpill {
			opts = append(opts, ochazuke.NoSpill())
		}
		if l.NoMarks {
			opts = append(opts, ochazuke.NoMarks())
		}
		if l.ClearFilterOnEnter {
			opts = append(opts, ochazuke.ClearFilterOnEnter())
		}
		if l.CellSelect {
			opts = append(opts, ochazuke.CellSelect())
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
	for _, ts := range p.Trees {
		topts := []ochazuke.InstanceOption{ochazuke.Parent(p.ID)}
		if len(ts.Columns) > 0 {
			topts = append(topts, ochazuke.Columns(ts.Columns...))
		}
		tree, err := r.AddTree(ts.ID, ts.Nodes, topts...)
		if err != nil {
			return fmt.Errorf("tree %s: %w", ts.ID, err)
		}
		for _, id := range ts.Collapsed {
			if err := tree.SetCollapsed(id, true); err != nil {
				return err
			}
		}
		if ts.Editable {
			fail := ts.FailWrites
			tree.OnChange = func(c ochazuke.TreeChange) error {
				if fail > 0 {
					fail--
					return errors.New("store unavailable")
				}
				ss.Writes = append(ss.Writes, c.String())
				return nil
			}
		}
		ss.trees[ts.ID] = tree
		if focus == "" && len(ts.Nodes) > 0 {
			focus = tree.FocusTarget()
		}
	}
	for _, rs := range p.Records {
		if rs.Modal {
			continue
		}
		// Leaving the editor lands on the page's first list.
		back := ""
		if len(p.Lists) > 0 {
			back = ss.lists[p.Lists[0].ID].FocusTarget()
		}
		ed, err := r.AddRecordEditor(rs.ID, ss.recordSpec(rs), ochazuke.Parent(p.ID), ochazuke.FocusOnClose(back))
		if err != nil {
			return fmt.Errorf("record %s: %w", rs.ID, err)
		}
		ss.records[rs.ID] = ed
		if focus == "" {
			focus = ed.FocusTarget()
		}
	}
	for _, in := range p.Inputs {
		input := ochazuke.NewInput(in.ID, in.Text, false)
		opts := []ochazuke.InstanceOption{ochazuke.Parent(p.ID)}
		if len(in.Completions) > 0 {
			opts = append(opts, ochazuke.Completions(completionFakes(in.Completions)...))
		}
		if err := r.AddInput(input, opts...); err != nil {
			return fmt.Errorf("input %s: %w", in.ID, err)
		}
		ss.inputs[in.ID] = input
		if focus == "" {
			focus = in.ID
		}
	}
	if p.Markdown != nil {
		var opts []markdown.Option
		if p.Markdown.PreserveNewlines {
			opts = append(opts, markdown.PreserveNewlines())
		}
		doc, err := markdown.New(r, markdownID(p), p.ID, ss.markdownSource(p.Markdown), opts...)
		if err != nil {
			return fmt.Errorf("markdown: %w", err)
		}
		ss.docs[p.ID] = doc
		doc.Author = p.Markdown.Author
		if p.Markdown.Today != "" {
			if day, err := time.Parse("2006-01-02", p.Markdown.Today); err == nil {
				doc.Now = func() time.Time { return day }
			}
		}
		if focus == "" && doc.List().Len() > 0 {
			focus = doc.FocusTarget()
		}
	}
	if p.Timeline != nil {
		tl, err := ss.addTimeline(p)
		if err != nil {
			return fmt.Errorf("timeline: %w", err)
		}
		if focus == "" && tl.List().Len() > 0 {
			focus = tl.FocusTarget()
		}
	}
	if p.Detail != nil {
		d, err := r.AddDetail(p.ID+"-detail", func() string { return ss.detailItem(p) }, ochazuke.Parent(p.ID))
		if err != nil {
			return fmt.Errorf("detail: %w", err)
		}
		ss.details[p.ID] = d
	}
	if p.Focus == "" && len(p.Order) > 0 {
		// The first stop in drawing order is focused, a tree or a list.
		for _, id := range p.Order {
			if t := ss.trees[id]; t != nil && t.List().Len() > 0 {
				focus = t.FocusTarget()
				break
			}
			if l := ss.lists[id]; l != nil {
				focus = l.FocusTarget()
				break
			}
			if doc := ss.docs[p.ID]; doc != nil && id == doc.ID() && len(doc.Tasks()) > 0 {
				focus = doc.FocusTarget()
				break
			}
			if tl := ss.timelines[p.ID]; tl != nil && id == tl.ID() && tl.List().Len() > 0 {
				focus = tl.FocusTarget()
				break
			}
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
	if p.DefaultHidden {
		opts = append(opts, ochazuke.DefaultHidden())
	}
	_, err := r.AddPage(p.ID, title, func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return ss.render(p, w, h) }, opts...)
	return err
}

// timelineID is the page's timeline's id.
func timelineID(p *Page) string {
	if p.Timeline.ID != "" {
		return p.Timeline.ID
	}
	return "timeline"
}

// storyCard is the noun of a story timeline's cards: a appends the next
// queued card, s streams a line into the last card.
const storyCard = "story-card"

// card is a CardSpec as an ochazuke.Card of the story-card noun.
func card(c CardSpec) ochazuke.Card {
	out := ochazuke.Card{ID: c.ID, Noun: storyCard, Header: c.Header, Meta: c.Meta, Open: c.Open, Active: c.Active, Muted: c.Muted, Group: c.Group, Event: c.Event}
	if c.Body != "" {
		out.Body = ochazuke.TextBody(c.Body)
	}
	for _, s := range c.Sections {
		sec := ochazuke.CardSection{Name: s.Name, Title: s.Title, Summary: s.Summary, Role: ochazuke.CardRole(s.Role), Folded: s.Folded}
		if s.Text != "" {
			sec.Body = ochazuke.TextBody(s.Text)
		}
		out.Sections = append(out.Sections, sec)
	}
	for _, st := range c.Steps {
		step := ochazuke.CardStep{ID: st.ID, Title: st.Title, Summary: st.Summary, Role: ochazuke.CardRole(st.Role), Folded: st.Folded, Header: st.Header, Paragraphs: st.Paragraphs}
		if st.Text != "" {
			step.Body = ochazuke.TextBody(st.Text)
		}
		out.Steps = append(out.Steps, step)
	}
	return out
}

func (ss *Session) addTimeline(p *Page) (*ochazuke.Timeline, error) {
	r := ss.R
	cards := make([]ochazuke.Card, len(p.Timeline.Cards))
	for i, c := range p.Timeline.Cards {
		cards[i] = card(c)
	}
	if r.Noun(storyCard) == nil {
		r.Define(storyCard)
	}
	opts := []ochazuke.InstanceOption{ochazuke.Parent(p.ID), ochazuke.HeaderLines(p.Timeline.HeaderLines)}
	if p.Timeline.FollowTail {
		opts = append(opts, ochazuke.FollowTail())
	}
	if p.Timeline.NoSpill {
		opts = append(opts, ochazuke.NoSpill())
	}
	if p.Timeline.ParentStop != "" {
		opts = append(opts, ochazuke.ParentStop(p.Timeline.ParentStop))
	}
	tl, err := r.AddTimeline(timelineID(p), cards, opts...)
	if err != nil {
		return nil, err
	}
	ss.timelines[p.ID] = tl
	queue := slices.Clone(p.Timeline.Append)
	r.Define(storyCard).
		Verb("timeline-append", func(c ochazuke.Ctx) ochazuke.Result {
			t := c.Runtime.Timeline(timelineID(p))
			if len(queue) == 0 || t == nil {
				return ochazuke.Done(nil)
			}
			next := queue[0]
			queue = queue[1:]
			if err := t.Patch(card(next)); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(nil)
		}, "Append the next queued card to the timeline").
		Verb("timeline-stream", func(c ochazuke.Ctx) ochazuke.Result {
			t := c.Runtime.Timeline(timelineID(p))
			cards := t.Cards()
			if len(cards) == 0 {
				return ochazuke.Done(nil)
			}
			last := cards[len(cards)-1]
			ss.streamed++
			text := ""
			if last.Body != nil {
				text = last.Body(0) + "\n"
			}
			text += fmt.Sprintf("streamed line %d", ss.streamed)
			last.Body, last.Revision = ochazuke.TextBody(text), last.Revision+1
			if err := t.Patch(last); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(nil)
		}, "Add a line to the last card's body, as a streaming answer").
		Mode("normal").
		Bind("a", ochazuke.Do(storyCard+".timeline-append")).
		Bind("s", ochazuke.Do(storyCard+".timeline-stream"))
	return tl, nil
}

// markdownID is the page's Markdown document's id.
func markdownID(p *Page) string {
	if p.Markdown.ID != "" {
		return p.Markdown.ID
	}
	return "markdown"
}

// markdownSource holds the spec's text (External from the first toggle
// on) and logs every save: a toggle as "toggle LINE [x] TEXT", any other
// write as "write LINE -OLD +NEW" (the changed lines, | between lines).
func (ss *Session) markdownSource(spec *MarkdownSpec) markdown.Source {
	stored, loads := spec.Text, 0
	return markdown.Source{
		Load: func() (string, error) {
			loads++
			if loads == 2 && spec.External != nil {
				stored = *spec.External
			}
			return stored, nil
		},
		Save: func(text string) error {
			entry := writeEntry(stored, text)
			for _, t := range markdown.Tasks(text) {
				if len(stored) == len(text) && stored[:t.Box] == text[:t.Box] && stored[t.Box+1:] == text[t.Box+1:] && stored[t.Box] != text[t.Box] {
					entry = fmt.Sprintf("toggle %d [%c] %s", t.Line, text[t.Box], t.Text)
				}
			}
			ss.Writes = append(ss.Writes, entry)
			stored = text
			return nil
		},
	}
}

// writeEntry names a write by the lines it changed: "write LINE -OLD
// +NEW", the removed and added lines joined by |.
func writeEntry(before, after string) string {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	q := 0
	for q < len(a)-p && q < len(b)-p && a[len(a)-1-q] == b[len(b)-1-q] {
		q++
	}
	entry := fmt.Sprintf("write %d", p+1)
	if old := a[p : len(a)-q]; len(old) > 0 {
		entry += " -" + strings.Join(old, "|")
	}
	if add := b[p : len(b)-q]; len(add) > 0 {
		entry += " +" + strings.Join(add, "|")
	}
	return entry
}

// recordSpec is a record editor's spec from the story: its values loaded,
// its writes logged (the first FailWrites fail).
// addCommandsPage adds cli's Commands page over the spec's entries; a run
// starts a fake pane instead of a process.
func (ss *Session) addCommandsPage(p *Page) error {
	start := func(*exec.Cmd, int, int) (ochazuke.TerminalStream, error) {
		stream := newFakeStream(ss.done)
		ss.streams = append(ss.streams, stream)
		return stream, nil
	}
	if _, err := cli.AddCommands(ss.M, storyCommands{*p.Commands}, cli.CommandsConfig{ID: p.ID, Title: p.Title, Start: start}); err != nil {
		return fmt.Errorf("commands page %s: %w", p.ID, err)
	}
	ss.pages[p.ID] = p
	return nil
}

// addCatalogPage mounts the spec's data page over its canned answers.
func (ss *Session) addCatalogPage(p *Page) error {
	spec := p.Catalog
	page := spec.Page
	page.ID = p.ID // the story page's id (the one Build opens)
	if page.Title == "" {
		page.Title = cmpString(p.Title, p.ID)
	}
	doc := cli.CatalogDoc{Schema: cli.Schema, Name: "story", State: spec.State, Nouns: spec.Nouns, Pages: []cli.CatalogPage{page}}
	fetch := func(_ context.Context, noun, verb string) ([]byte, error) {
		raw, ok := spec.Answers[noun+"."+verb]
		if !ok {
			return nil, fmt.Errorf("exit status 1: %s %s has no answer", noun, verb)
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return []byte(text), nil
		}
		return raw, nil
	}
	cfg := cli.PageConfig{Fetch: fetch, DetailKinds: map[string]cli.DetailFactory{cliview.Kind: ss.catalogMarkdown(spec)}}
	v, err := cli.MountPage(ss.M, doc, page, cfg)
	if err != nil {
		return fmt.Errorf("catalog page %s: %w", page.ID, err)
	}
	if err := v.Reload(); err != nil {
		return fmt.Errorf("catalog page %s: %w", page.ID, err)
	}
	// q leaves the story, as on every demo page (its rows are not demo's).
	if err := ss.R.Bind(ochazuke.Binding{Instance: page.ID, Mode: "normal", Keys: "q", Request: ochazuke.Request{Noun: "app", Verb: "quit"}}); err != nil {
		return err
	}
	if !spec.Pending {
		_ = cli.Settle(ss.R, nil) // a failed load is the page's to show
	}
	ss.pages[p.ID] = p
	return nil
}

// catalogMarkdown is the markdown detail kind over the spec's files:
// every save logged as a markdown page's writes, notes signed and dated
// by the spec.
func (ss *Session) catalogMarkdown(spec *CatalogSpec) cli.DetailFactory {
	files := map[string]string{}
	for k, v := range spec.Files {
		files[k] = v
	}
	source := func(path string) markdown.Source {
		return markdown.Source{
			Load: func() (string, error) {
				text, ok := files[path]
				if !ok {
					return "", fmt.Errorf("open %s: no such file", path)
				}
				return text, nil
			},
			Save: func(text string) error {
				ss.Writes = append(ss.Writes, path+" "+writeEntry(files[path], text))
				files[path] = text
				return nil
			},
		}
	}
	factory := cliview.Factory(cliview.Source(source), cliview.Author(spec.Author))
	return func(h cli.DetailHost) (cli.DetailView, error) {
		view, err := factory(h)
		if err != nil {
			return nil, err
		}
		return datedView{view, spec.Today}, nil
	}
}

// datedView dates the notes of the Doc it shows by the story's day.
type datedView struct {
	cli.DetailView
	today string
}

func (d datedView) Block(width, height int) ochazuke.Block {
	if doc := cliview.DocOf(d.DetailView); doc != nil && d.today != "" {
		if day, err := time.Parse("2006-01-02", d.today); err == nil {
			doc.Now = func() time.Time { return day }
		}
	}
	return d.DetailView.Block(width, height)
}

func cmpString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// storyCommands is a CommandSource from a story file: its commands never
// start (the page's Start hands out fake panes).
type storyCommands struct{ spec CommandsSpec }

func (s storyCommands) Commands() []cli.CommandEntry { return s.spec.Entries }

func (s storyCommands) Help(_ context.Context, name string) (string, error) {
	if text, ok := s.spec.Help[name]; ok {
		return text, nil
	}
	return "", fmt.Errorf("no help for %s", name)
}

func (s storyCommands) Command(name string, args []string) (*exec.Cmd, error) {
	return exec.Command(name, args...), nil
}

func (ss *Session) recordSpec(rs RecordSpec) ochazuke.RecordSpec {
	fail := rs.FailWrites
	return ochazuke.RecordSpec{
		Title:  rs.Title,
		Fields: rs.Fields,
		Insert: rs.Insert,
		Load:   func() (ochazuke.Record, error) { return ochazuke.Record(rs.Values), nil },
		Save: func(w ochazuke.RecordWrite) ochazuke.Result {
			if fail > 0 {
				fail--
				return ochazuke.Failed(errors.New("store unavailable"))
			}
			ss.Writes = append(ss.Writes, w.String())
			return ochazuke.Done(nil)
		},
	}
}

// render draws a page: the main column, with the detail to its right when
// the page has one.
func (ss *Session) render(p *Page, width, height int) ochazuke.Block {
	d := ss.details[p.ID]
	if d == nil {
		return ss.renderMain(p, width, height)
	}
	dw := p.Detail.Width
	if dw <= 0 {
		dw = width / 2
	}
	dw = min(max(1, dw), max(1, width-2))
	main := ss.renderMain(p, width-dw-1, height)
	item := ss.detailItem(p)
	lines := make([]string, p.Detail.Lines)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s line %d", item, i+1)
	}
	body, marker := d.Window(strings.Join(lines, "\n"), dw, max(1, height-1))
	head := "detail"
	if marker != "" {
		head += " " + marker
	}
	left := lipgloss.NewStyle().Width(width - dw - 1).Render(main.Text)
	right := lipgloss.NewStyle().Width(dw).Render(head + "\n" + body)
	return main.WithText(lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right))
}

// detailItem is the row the detail is about: the focused row when it is a
// list's (a tree's rows are its list's), else the first list's selection.
func (ss *Session) detailItem(p *Page) string {
	r := ss.R
	focus := r.BaseFocus()
	if inst := r.Instances[focus]; inst != nil && r.List(inst.Parent) != nil {
		return focus
	}
	for _, l := range p.Lists {
		if sel, ok := ss.lists[l.ID].Selected(); ok {
			return sel.ID
		}
	}
	return ""
}

// renderMain draws the lists (stacked, or side by side in a row), then the
// inputs, then the pane when attached.
func (ss *Session) renderMain(p *Page, width, height int) ochazuke.Block {
	order := p.Order
	if len(order) == 0 {
		for _, l := range p.Lists {
			order = append(order, l.ID)
		}
		for _, t := range p.Trees {
			order = append(order, t.ID)
		}
		for _, rs := range p.Records {
			order = append(order, rs.ID)
		}
		if p.Markdown != nil {
			order = append(order, markdownID(p))
		}
		if p.Timeline != nil {
			order = append(order, timelineID(p))
		}
	}
	order = slices.DeleteFunc(slices.Clone(order), func(id string) bool { return ss.hidden[id] })
	recordSpec := map[string]RecordSpec{}
	for _, rs := range p.Records {
		recordSpec[rs.ID] = rs
	}
	treeSpec := map[string]TreeSpec{}
	for _, t := range p.Trees {
		treeSpec[t.ID] = t
	}
	spec := map[string]ListSpec{}
	for _, l := range p.Lists {
		spec[l.ID] = l
	}
	var parts []ochazuke.Block
	used := 0
	drawn := map[string]bool{}
	listBlock := func(id string, w, rest int) string {
		l, list := spec[id], ss.lists[id]
		h := l.Height
		switch {
		case h < 0:
			h = max(1, rest)
		case h == 0:
			h = max(1, list.Height())
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
	if p.Layout == "split" && len(order) >= 2 {
		// The first two lists in Layout.SplitFocusBlock, the panel whose
		// list holds the focus drawn focused.
		l := ss.R.PaneLayout(p.ID, ochazuke.Layout{Width: ss.M.Width, Height: ss.M.Height, Styles: ochazuke.NewStyles(*ss.M.Appearance)})
		side := ochazuke.SplitNone
		for _, at := range ss.R.Ancestry(ss.R.Focus) {
			switch at {
			case order[0]:
				side = ochazuke.SplitLeft
			case order[1]:
				side = ochazuke.SplitRight
			}
		}
		body := func(id string) ochazuke.Block {
			return ochazuke.Text(ss.lists[id].View(max(1, width/2), max(1, height-2)))
		}
		return l.SplitFocusBlock(side, order[0], body(order[0]), order[1], body(order[1]))
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
			if b, ok := ss.placed(p, id, width, drawn); ok {
				parts = append(parts, b)
				used += strings.Count(b.Text, "\n") + 1
				continue
			}
			if tl := ss.timelines[p.ID]; tl != nil && id == tl.ID() {
				h := p.Timeline.Height
				if h <= 0 {
					h = max(1, height-used-head)
				}
				text := tl.View(width, h)
				if !p.NoHead {
					text = id + "\n" + text
				}
				parts = append(parts, ochazuke.Text(text))
				used += strings.Count(text, "\n") + 1
				continue
			}
			if doc := ss.docs[p.ID]; doc != nil && id == doc.ID() {
				h := p.Markdown.Height
				if h < 0 {
					h = max(1, height-used-head)
				}
				block := doc.Block(width, h, *ss.M.Appearance)
				if !p.NoHead {
					block = block.Offset(0, 1).WithText(id + "\n" + block.Text)
				}
				parts = append(parts, block)
				used += strings.Count(block.Text, "\n") + 1
				continue
			}
			if tree, ok := ss.trees[id]; ok {
				h := treeSpec[id].Height
				switch {
				case h < 0:
					h = max(1, height-used-head)
				case h == 0:
					h = max(1, tree.Height())
					if len(tree.List().Columns()) > 0 {
						h++ // the header line
					}
				}
				block := tree.Block(width, h)
				if !p.NoHead {
					block = block.Offset(0, 1).WithText(id + "\n" + block.Text)
				}
				parts = append(parts, block)
				used += strings.Count(block.Text, "\n") + 1
				continue
			}
			if ed, ok := ss.records[id]; ok {
				if ed.Closed() {
					continue
				}
				h := recordSpec[id].Height
				if h <= 0 {
					h = max(1, height-used-head)
				}
				ed.Styles = ochazuke.NewStyles(*ss.M.Appearance)
				block := ed.Block(width, h)
				if !p.NoHead {
					block = block.Offset(0, 1).WithText(id + "\n" + block.Text)
				}
				parts = append(parts, block)
				used += strings.Count(block.Text, "\n") + 1
				continue
			}
			if _, ok := ss.lists[id]; !ok {
				continue
			}
			text := listBlock(id, width, height-used-head)
			parts = append(parts, ochazuke.Text(text))
			used += strings.Count(text, "\n") + 1
		}
	}
	// Inputs, then texts, then the readout, where the order left them out.
	var rest []string
	for _, in := range p.Inputs {
		rest = append(rest, in.ID)
	}
	for _, t := range p.Texts {
		rest = append(rest, t.ID)
	}
	if p.Readout {
		rest = append(rest, readoutID)
	}
	for _, id := range rest {
		if drawn[id] {
			continue
		}
		if b, ok := ss.placed(p, id, width, drawn); ok {
			parts = append(parts, b)
			used += strings.Count(b.Text, "\n") + 1
		}
	}
	if t := ss.R.Terminal(p.ID + "-pane"); t != nil {
		h := 6
		if p.Term != nil && p.Term.Height > 0 {
			h = p.Term.Height
		}
		state := "pane (detached)"
		if t.Focused() {
			state = "pane (attached: " + ss.R.TerminalLeader() + " d detaches)"
		}
		block := t.ViewBlock(width, h)
		if !t.Focused() {
			block.Cursor = nil
		}
		parts = append(parts, ochazuke.Text(state), block)
	}
	return stack(parts)
}

// readoutID names the key readout in a page's order.
const readoutID = "readout"

// placed draws one of the page's inputs, texts or its readout by id (ok
// false for any other id, or a hidden one) and marks it drawn.
func (ss *Session) placed(p *Page, id string, width int, drawn map[string]bool) (ochazuke.Block, bool) {
	if ss.hidden[id] || drawn[id] {
		return ochazuke.Block{}, false
	}
	for _, in := range p.Inputs {
		if in.ID != id {
			continue
		}
		drawn[id] = true
		prefix := in.Label
		if prefix == "" {
			prefix = in.ID
		}
		prefix += ": "
		pw := ansi.StringWidth(prefix)
		block := ochazuke.InputBlock(ss.inputs[in.ID], max(1, width-pw), 1)
		return block.Offset(pw, 0).WithText(prefix + block.Text), true
	}
	for _, t := range p.Texts {
		if t.ID == id {
			drawn[id] = true
			return ochazuke.Text(t.Text), true
		}
	}
	if id == readoutID && p.Readout {
		drawn[id] = true
		lines := ss.readout()
		for i, l := range lines {
			l = ansi.Truncate(l, width, "…")
			lines[i] = strings.Repeat(" ", max(0, width-ansi.StringWidth(l))) + l
		}
		return ochazuke.Text(strings.Join(lines, "\n")), true
	}
	return ochazuke.Block{}, false
}

// readout is the key readout's lines: the mode, the focus path innermost
// first (instance ids, app left out), and the last key a row answered
// with the request it ran, read from the action history. The runtime
// does not say at which level of the stack the row answered, so the
// readout names the request's target when the row addressed one.
func (ss *Session) readout() []string {
	r := ss.R
	path := slices.Clone(r.InstancePath(r.Focus))
	path = slices.DeleteFunc(path, func(id string) bool { return id == ochazuke.AppNoun })
	slices.Reverse(path)
	lines := []string{"mode " + r.Mode.Name, "focus " + strings.Join(path, " < ")}
	key := "key -"
	hist := r.History(0)
	for i := len(hist) - 1; i >= 0; i-- {
		h := hist[i]
		keys, ok := strings.CutPrefix(h.Context.Source, "key:")
		if !ok || h.Depth != 0 {
			continue
		}
		key = "key " + keys + " → " + r.DisplayKey(h.Request.Key()) + " in " + h.Context.Mode
		if h.Request.Target != "" {
			key += " at " + h.Request.Target
		}
		if !h.Result.OK {
			key += " (failed)"
		}
		break
	}
	return append(lines, key)
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
// out), or the ticker alone (ticker-only: the rule line above the footer,
// where the ticker rides, cut out of a full shell).
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
		all := strings.Split(view.Content, "\n")
		rows = []string{ansi.Cut(all[len(all)-3], 2, 2+w)}
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
			spec := ListSpec{ID: l.ID, NoSpill: l.NoSpill, ClearFilterOnEnter: l.ClearFilterOnEnter, CellSelect: l.CellSelect, Height: l.Height, Columns: list.Columns()}
			marked := map[string]bool{}
			for _, it := range list.Marked() {
				marked[it.ID] = true
			}
			sel, _ := list.Selected()
			for i, it := range list.Items() {
				spec.Items = append(spec.Items, ItemSpec{ID: it.ID, Label: it.Label, Cells: it.Cells, Tones: it.Tones})
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

// completionFakes are a story input's completion triggers: each search
// answers its items holding the query, ignoring case, or fails.
func completionFakes(specs []CompletionSpec) []ochazuke.CompletionTrigger {
	var out []ochazuke.CompletionTrigger
	for _, spec := range specs {
		out = append(out, ochazuke.CompletionTrigger{Trigger: spec.Trigger, Search: ochazuke.SearchStrings(func(_ context.Context, query string) ([]string, error) {
			if spec.Fail != "" {
				return nil, errors.New(spec.Fail)
			}
			var items []string
			for _, item := range spec.Items {
				if strings.Contains(strings.ToLower(item), strings.ToLower(query)) {
					items = append(items, item)
				}
			}
			return items, nil
		})})
	}
	return out
}
