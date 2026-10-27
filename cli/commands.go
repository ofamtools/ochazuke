package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// CommandEntry is one row of the Commands page: a command a tool can run,
// as oden's catalog lists it.
type CommandEntry struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // "inline" (in the tool) or "external" (<bin>-<name> on PATH)
	Summary  string   `json:"summary,omitempty"`
	Source   string   `json:"source"` // where an inline command was registered; an external's path
	Aliases  []string `json:"aliases,omitempty"`
	Shadowed []string `json:"shadowed,omitempty"` // each losing claim, "kind source"
	// Help is the command's help text when it is known without running
	// anything (an inline command); "" until command help fetches it.
	Help string `json:"help,omitempty"`
}

// CommandSource is what the Commands page lists and runs. OdenCommands
// answers it from an oden App; a host with its own command table (or a
// test) fills it directly.
type CommandSource interface {
	// Commands lists the entries, sorted as they are shown.
	Commands() []CommandEntry
	// Help is the full help text of one command. It may run the command
	// (an external's --help), so it runs off the loop and honours ctx.
	Help(ctx context.Context, name string) (string, error)
	// Command is the unstarted process that runs name with args, as the
	// tool's own command line would run it.
	Command(name string, args []string) (*exec.Cmd, error)
}

// CommandNoun is the noun of every Commands row.
const CommandNoun = "app.command"

// CommandsConfig configures AddCommands.
type CommandsConfig struct {
	ID    string // the page id; "commands" when empty
	Title string // the page title; "Commands" when empty
	// Start runs a command for command run; nil is StartPty. A stream
	// that implements ochazuke.TerminalReleaser is released when the run's
	// terminal closes, and one with Status() string reports it in the
	// panel title.
	Start func(cmd *exec.Cmd, cols, rows int) (ochazuke.TerminalStream, error)
}

// Commands is the Commands page: a table of a tool's commands (rows of the
// command noun) beside a panel that shows the selected command's help (a
// detail) or the terminal its last run opened. command run opens the
// command in a terminal instance under its row, attached; ctrl+a d
// detaches back to the row and the pane keeps its last screen. Rows are
// set once, when the page is added.
type Commands struct {
	m      *ochazuke.Model
	src    CommandSource
	start  func(cmd *exec.Cmd, cols, rows int) (ochazuke.TerminalStream, error)
	page   string
	list   *ochazuke.List
	detail *ochazuke.Detail

	entries map[string]CommandEntry
	help    map[string]string // help fetched by command help
	helpErr map[string]string
	runs    map[string]*commandRun
	showing map[string]string // name -> "run" or "help": what the panel shows
	seq     int
	cols    int // the terminal panel's last content size, for the next run
	rows    int
}

// commandRun is one run of a command: its terminal instance and stream.
type commandRun struct {
	id     string
	argv   []string
	stream ochazuke.TerminalStream
}

// AddCommands defines the command noun on m's runtime (once) and adds the
// Commands page listing src's commands. Its verbs: command list (the
// rows), command run {name, args} (enter on a row), command help {name}
// (i), command which {name} and command stop {name} (x, confirmed). Every
// verb's name defaults to the row it runs on.
func AddCommands(m *ochazuke.Model, src CommandSource, cfg CommandsConfig) (*Commands, error) {
	if m == nil || src == nil {
		return nil, errors.New("a model and a command source are required")
	}
	r := m.Runtime
	if r.Nouns[CommandNoun] != nil {
		return nil, fmt.Errorf("noun %q is already defined", CommandNoun)
	}
	c := &Commands{
		m: m, src: src, start: cfg.Start, page: cfg.ID,
		entries: map[string]CommandEntry{}, help: map[string]string{}, helpErr: map[string]string{},
		runs: map[string]*commandRun{}, showing: map[string]string{}, cols: 80, rows: 24,
	}
	if c.page == "" {
		c.page = "commands"
	}
	title := cfg.Title
	if title == "" {
		title = "Commands"
	}
	if c.start == nil {
		c.start = func(cmd *exec.Cmd, cols, rows int) (ochazuke.TerminalStream, error) { return StartPty(cmd, cols, rows) }
	}
	c.define()
	// Runs open in terminals: the leader is the user's terminal-leader.
	r.EnsureTerminalLeader()
	r.Instance(c.page)
	var items []ochazuke.ListItem
	for _, e := range src.Commands() {
		c.entries[e.Name] = e
		items = append(items, ochazuke.ListItem{
			ID: rowID(e.Name), Label: e.Name, Noun: CommandNoun, Data: e.Name,
			Cells: []string{e.Name, e.Kind, e.Summary, e.Source},
		})
	}
	var err error
	c.list, err = r.AddList(c.page+"/list", items, ochazuke.Parent(c.page), ochazuke.NoMarks(), ochazuke.Columns(r.Noun(CommandNoun).Columns...))
	if err != nil {
		return nil, err
	}
	c.detail, err = r.AddDetail(c.page+"/detail", c.detailItem, ochazuke.Parent(c.page))
	if err != nil {
		return nil, err
	}
	if _, err := r.AddPage(c.page, title, c.render, ochazuke.Focus(c.list.FocusTarget())); err != nil {
		return nil, err
	}
	return c, nil
}

// rowID is the instance id of a command's row.
func rowID(name string) string { return "command:" + name }

// Page is the page's instance id.
func (c *Commands) Page() string { return c.page }

// List is the page's table of commands.
func (c *Commands) List() *ochazuke.List { return c.list }

// Terminal is the terminal of name's last run, or nil.
func (c *Commands) Terminal(name string) *ochazuke.Terminal {
	if run := c.runs[name]; run != nil {
		return c.m.Runtime.Terminal(run.id)
	}
	return nil
}

// commandHasRun is the guard on command stop: the row has a run to end,
// so x answers (and shows in the footer) only there.
const commandHasRun = "command-has-run"

// define registers the command noun: its columns, verbs and row keys.
func (c *Commands) define() {
	c.m.Runtime.Builtin(c.register)
}

func (c *Commands) register() {
	r := c.m.Runtime
	r.DefineGuard(commandHasRun, func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		name, err := c.nameAt(r, ev.Focus, nil)
		return err == nil && c.runs[name] != nil
	})
	name := ochazuke.Parameter{Type: "string", Description: "Command name (defaults to the focused row)", Source: "app.command.list"}
	r.Define(CommandNoun).
		Columns(
			// A narrow table drops the highest Priority first.
			ochazuke.Column{Title: "Name", Min: 8},
			ochazuke.Column{Title: "Kind", Width: 8, Priority: 2},
			ochazuke.Column{Title: "Summary", Priority: 1},
			ochazuke.Column{Title: "Source", Priority: 3},
		).
		Verb("list", c.listVerb, "List the tool's commands: inline ones and <bin>-<name> on PATH").Rows(CommandNoun).
		Verb("run", c.runVerb, "Run a command in a terminal on the page and attach to it (attach again while it runs)").Positional("name", "args").
		Verb("help", c.helpVerb, "Show a command's help text in the detail").Positional("name").
		Verb("which", c.whichVerb, "Report where a command comes from and what it shadows").Positional("name").
		Verb("stop", c.stopVerb, "End a command's run: hang up its process group and close its terminal").Positional("name").Confirm("Stop {{label}}?").When(commandHasRun).
		Mode("normal").
		Bind("enter", ochazuke.Do("app.command.run")).
		Bind("i", ochazuke.Do("app.command.help")).
		Bind("x", ochazuke.Do("app.command.stop"))
	for key, params := range map[string]map[string]ochazuke.Parameter{
		"app.command.run":   {"name": name, "args": {Type: "string", Description: "Arguments, split on spaces"}},
		"app.command.help":  {"name": name},
		"app.command.which": {"name": name},
		"app.command.stop":  {"name": name},
	} {
		op := r.Operations[key]
		op.Parameters = params
		r.Operations[key] = op
	}
}

// nameOf is the command a verb addresses: its name argument, else the
// command row on the focus path.
func (c *Commands) nameOf(r *ochazuke.Runtime, ctx ochazuke.Context, args ochazuke.Arguments) (string, error) {
	focus := ctx.InputID
	if focus == "" && len(ctx.Path) > 0 {
		focus = ctx.Path[len(ctx.Path)-1]
	}
	return c.nameAt(r, focus, args)
}

// nameAt is nameOf for a focus id.
func (c *Commands) nameAt(r *ochazuke.Runtime, focus string, args ochazuke.Arguments) (string, error) {
	if name := ochazuke.StringArg(args, "name", ""); name != "" {
		if _, ok := c.entries[name]; !ok {
			return "", fmt.Errorf("unknown command %q", name)
		}
		return name, nil
	}
	for _, id := range r.Ancestry(focus) {
		if inst := r.Instances[id]; inst != nil && inst.Noun == CommandNoun {
			if name, ok := inst.Data.(string); ok {
				return name, nil
			}
		}
	}
	return "", errors.New("name is required off a command row")
}

func (c *Commands) listVerb(r *ochazuke.Runtime, _ ochazuke.Context, _ ochazuke.Arguments) (any, error) {
	return c.list.Items(), nil
}

// runVerb attaches to name's live run when it has one and no arguments
// were given; otherwise it ends any earlier run and starts a new one in a
// terminal under the row, focused.
func (c *Commands) runVerb(r *ochazuke.Runtime, ctx ochazuke.Context, args ochazuke.Arguments) (any, error) {
	name, err := c.nameOf(r, ctx, args)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(ochazuke.StringArg(args, "args", ""))
	if err := c.list.Select(rowID(name)); err != nil {
		return nil, err
	}
	c.showing[name] = "run"
	if run := c.runs[name]; run != nil && len(words) == 0 && !finished(run.stream) {
		return c.runResult(name, run), r.SetFocus(run.id)
	}
	cmd, err := c.src.Command(name, words)
	if err != nil {
		return nil, err
	}
	stream, err := c.start(cmd, c.cols, c.rows)
	if err != nil {
		return nil, err
	}
	if err := c.endRun(name); err != nil {
		if rel, ok := stream.(ochazuke.TerminalReleaser); ok {
			_ = rel.Release()
		}
		return nil, err
	}
	c.seq++
	run := &commandRun{id: fmt.Sprintf("%s/run/%d", c.page, c.seq), argv: cmd.Args, stream: stream}
	if _, err := r.AddTerminal(run.id, stream, ochazuke.Parent(rowID(name))); err != nil {
		if rel, ok := stream.(ochazuke.TerminalReleaser); ok {
			_ = rel.Release()
		}
		return nil, err
	}
	c.runs[name] = run
	return c.runResult(name, run), r.SetFocus(run.id)
}

func (c *Commands) runResult(name string, run *commandRun) map[string]any {
	return map[string]any{"name": name, "terminal": run.id, "argv": run.argv, "status": status(run.stream)}
}

// endRun removes name's last run, releasing its stream.
func (c *Commands) endRun(name string) error {
	run := c.runs[name]
	if run == nil {
		return nil
	}
	delete(c.runs, name)
	if c.m.Runtime.Instances[run.id] == nil {
		return nil
	}
	return c.m.Runtime.RemoveInstance(run.id)
}

func (c *Commands) stopVerb(r *ochazuke.Runtime, ctx ochazuke.Context, args ochazuke.Arguments) (any, error) {
	name, err := c.nameOf(r, ctx, args)
	if err != nil {
		return nil, err
	}
	if c.runs[name] == nil {
		return nil, fmt.Errorf("%s has no run", name)
	}
	c.showing[name] = "help"
	return name, c.endRun(name)
}

// helpVerb shows name's help in the detail: at once when it is known,
// else fetched off the loop (the source may run the command's --help).
func (c *Commands) helpVerb(x ochazuke.Ctx) ochazuke.Result {
	name, err := c.nameOf(x.Runtime, x.Context, x.Args)
	if err != nil {
		return ochazuke.Failed(err)
	}
	if err := c.list.Select(rowID(name)); err != nil {
		return ochazuke.Failed(err)
	}
	c.showing[name] = "help"
	if text := c.helpText(name); text != "" {
		return ochazuke.Done(text)
	}
	src := c.src
	return ochazuke.Pending(func(ctx context.Context) (any, error) { return src.Help(ctx, name) }).
		Replacing("help").Label(name).
		Then(func(_ ochazuke.Ctx, v any, err error) {
			if err != nil {
				c.helpErr[name] = err.Error()
				return
			}
			text, _ := v.(string)
			c.help[name] = text
			delete(c.helpErr, name)
		})
}

// helpText is name's known help: the entry's own, else what help fetched.
func (c *Commands) helpText(name string) string {
	if text := c.entries[name].Help; text != "" {
		return text
	}
	return c.help[name]
}

func (c *Commands) whichVerb(r *ochazuke.Runtime, ctx ochazuke.Context, args ochazuke.Arguments) (any, error) {
	name, err := c.nameOf(r, ctx, args)
	if err != nil {
		return nil, err
	}
	e := c.entries[name]
	return map[string]any{"name": e.Name, "kind": e.Kind, "source": e.Source, "aliases": e.Aliases, "shadowed": e.Shadowed}, nil
}

// finished reports whether a stream says its process has exited.
func finished(s ochazuke.TerminalStream) bool {
	if e, ok := s.(interface{ Exited() (bool, error) }); ok {
		done, _ := e.Exited()
		return done
	}
	return false
}

// status is a stream's Status() when it has one.
func status(s ochazuke.TerminalStream) string {
	if st, ok := s.(interface{ Status() string }); ok {
		return st.Status()
	}
	return ""
}

// selected is the selected command's name.
func (c *Commands) selected() string {
	item, ok := c.list.Selected()
	if !ok {
		return ""
	}
	name, _ := item.Data.(string)
	return name
}

// detailItem names what the detail shows: the selected command's help.
func (c *Commands) detailItem() string { return c.selected() }

// showsRun reports whether the panel shows name's terminal.
func (c *Commands) showsRun(name string) bool {
	return c.showing[name] == "run" && c.Terminal(name) != nil
}

// detailText is the detail of one command: what it is, where it comes
// from, then its help.
func (c *Commands) detailText(name string) string {
	e, ok := c.entries[name]
	if !ok {
		return "No command selected."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", e.Name, e.Kind)
	if e.Summary != "" {
		fmt.Fprintf(&b, "%s\n", e.Summary)
	}
	fmt.Fprintf(&b, "source: %s\n", e.Source)
	if len(e.Aliases) > 0 {
		fmt.Fprintf(&b, "aliases: %s\n", strings.Join(e.Aliases, ", "))
	}
	for _, s := range e.Shadowed {
		fmt.Fprintf(&b, "shadows: %s\n", s)
	}
	b.WriteString("\n")
	switch {
	case c.helpText(name) != "":
		b.WriteString(c.helpText(name))
	case c.helpErr[name] != "":
		fmt.Fprintf(&b, "help failed: %s", c.helpErr[name])
	default:
		b.WriteString("enter runs it · i shows its help")
	}
	return plainText(b.String())
}

// plainText drops escapes and control characters but newlines and tabs, so
// a command's help cannot style or move the page.
func plainText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

// render draws the page: the table, and beside it (below it on a narrow
// screen) the selected command's terminal or help.
func (c *Commands) render(r *ochazuke.Runtime, width, height int) ochazuke.Block {
	if width <= 0 || height <= 0 {
		return ochazuke.Block{}
	}
	layout := ochazuke.Layout{Styles: ochazuke.NewStyles(*c.m.Appearance)}
	name := c.selected()
	listPanel := func(w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		return layout.PanelBlock("Commands", ochazuke.Text(c.list.View(cw, ch)), w, h, c.list.Focused())
	}
	sidePanel := func(w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		if c.showsRun(name) {
			term := c.Terminal(name)
			c.cols, c.rows = max(1, cw), max(1, ch)
			title := name + " · " + c.runState(name, term)
			body := term.ViewBlock(cw, ch)
			if !term.Focused() {
				body.Cursor = nil
			}
			return layout.PanelBlock(title, body, w, h, term.Focused())
		}
		body, marker := c.detail.Window(c.detailText(name), cw, ch)
		title := "Help"
		if name != "" {
			title = name + " · help"
		}
		if marker != "" {
			title += " · " + marker
		}
		return layout.PanelBlock(title, ochazuke.Text(body), w, h, false)
	}
	if width >= 72 {
		left := (width - 1) * 2 / 5
		return beside(listPanel(left, height), sidePanel(width-left-1, height), left, height)
	}
	top := min(c.list.Height()+3, height*2/5)
	return below(listPanel(width, top), sidePanel(width, height-top), top)
}

// runState is the panel title's state of a run: running or how it ended,
// and whether keys go to it.
func (c *Commands) runState(name string, term *ochazuke.Terminal) string {
	state := status(c.runs[name].stream)
	if state == "" {
		state = "running"
	}
	if term.Err() != "" {
		state = "error: " + plainText(term.Err())
	}
	if term.Focused() {
		return state + " · attached"
	}
	return state + " · enter attaches"
}

// beside joins two blocks of height rows left and right, a one-cell gap
// between them; the right block's cursor is moved with it.
func beside(left, right ochazuke.Block, leftWidth, height int) ochazuke.Block {
	ls, rs := strings.Split(left.Text, "\n"), strings.Split(right.Text, "\n")
	rows := make([]string, height)
	for i := range rows {
		l := ""
		if i < len(ls) {
			l = ls[i]
		}
		if pad := leftWidth - ansi.StringWidth(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		rest := ""
		if i < len(rs) {
			rest = rs[i]
		}
		rows[i] = l + " " + rest
	}
	out := ochazuke.Block{Text: strings.Join(rows, "\n"), Cursor: left.Cursor}
	if right.Cursor != nil {
		out.Cursor = right.Offset(leftWidth+1, 0).Cursor
	}
	return out
}

// below stacks top (top rows) over bottom.
func below(top, bottom ochazuke.Block, topHeight int) ochazuke.Block {
	out := ochazuke.Block{Text: top.Text + "\n" + bottom.Text, Cursor: top.Cursor}
	if bottom.Cursor != nil {
		out.Cursor = bottom.Offset(0, topHeight).Cursor
	}
	return out
}

// Names lists the commands in row order.
func (c *Commands) Names() []string {
	var out []string
	for _, item := range c.list.Items() {
		if name, ok := item.Data.(string); ok {
			out = append(out, name)
		}
	}
	return slices.Clip(out)
}
