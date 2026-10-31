package ochazuke

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/refdoc"

	tea "charm.land/bubbletea/v2"
)

// The About page: what this program is and where it runs. about.open opens
// (or switches to) a framework page of sections of rows, each row a label
// and a value: Build (the module, its version, commit, commit time, whether
// the tree was modified, the Go version and the binary), then the sections
// an application adds (AddAboutSection: its data directory, its daemon,
// its extensions), Dependencies (every module of the build whose path
// starts with a prefix in about.dep-prefixes, with where a replace or a
// go.work points it) and Runtime (the pid, when it started, the uptime and
// the status segments' last values). Rows are evaluated when the page
// opens and on about.refresh, never at startup, so a row that asks a daemon
// costs nothing until someone looks.
//
// about.show answers the same sections as data (the CLI and MCP read it),
// about.copy yanks them as text for a bug report. A row is an instance of
// the about-row noun: about-row.copy yanks its value, about-row.open opens
// a path row (Kind AboutPath) with the system opener, about-row.show
// answers the row. All of them are read-only, so agents may run them.
//
// With about.tab on (the default) the About page is a tab of the Settings
// page (LinkSettingsTab): ] and [ step from the Settings page's last tab to
// About and back, and the About page draws the Settings page's tab row.
const (
	// AboutPageID is the About page's instance id.
	AboutPageID = "__about"
	// AboutDepPrefixesSetting lists the module path prefixes whose
	// modules the Dependencies section shows.
	AboutDepPrefixesSetting = "about.dep-prefixes"
	// AboutTabSetting makes the About page a tab of the Settings page.
	AboutTabSetting = "about.tab"
	// AboutPath is the Kind of a row whose value is a file or directory:
	// about-row.open opens it.
	AboutPath = "path"

	aboutPageNoun  = "ochazuke.about-page"
	aboutRowNoun   = "ochazuke.about-row"
	aboutStateID   = "__about-state"
	aboutListID    = "__about/list"
	aboutDetailID  = "__about/detail"
	aboutRowPrefix = "__about/row/"

	aboutBuildTitle = "Build"
	aboutDepsTitle  = "Dependencies"
	aboutRunTitle   = "Runtime"
)

// AboutRow is one row of the About page: a label, its value and, for a
// path, Kind AboutPath. Note is a short remark drawn after the value (what
// a replaced module replaces).
type AboutRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Kind  string `json:"kind,omitempty"`
	Note  string `json:"note,omitempty"`
}

// AboutSection is a titled run of rows, as about.show answers it.
type AboutSection struct {
	Title string     `json:"title"`
	Rows  []AboutRow `json:"rows"`
}

// AboutEnv is where the About page reads the process from. A zero field
// is the real one; a test or a story sets them for a fixed page.
type AboutEnv struct {
	// BuildInfo is the build's module info (debug.ReadBuildInfo).
	BuildInfo func() (*debug.BuildInfo, bool)
	// Executable is the running binary's path (os.Executable).
	Executable func() (string, error)
	// PID is the process id (os.Getpid).
	PID int
	// Started is when the process started (when the package loaded).
	Started time.Time
	// Now is the clock the uptime reads (time.Now).
	Now func() time.Time
	// Open opens a path with the system's opener (open, xdg-open).
	Open func(path string) error
}

// processStarted stands for the process's start: the package loads first.
var processStarted = time.Now()

// About is the About page's state: the application's sections, the
// environment, and the rows the page last evaluated.
type About struct {
	sections []aboutSource
	env      AboutEnv
	shown    []AboutSection
	from     string
	owned    bool
	filled   bool // ensureAboutPage just evaluated the rows
}

type aboutSource struct {
	title string
	rows  func() []AboutRow
}

func (a *About) HandleMessage(tea.Msg) bool { return false }
func (a *About) PendingCommand() tea.Cmd    { return nil }
func (a *About) Close()                     {}

// about is the runtime's About state (registerAbout made it).
func (r *Runtime) about() *About {
	if inst := r.Instances[aboutStateID]; inst != nil {
		if a, ok := inst.Component.(*About); ok {
			return a
		}
	}
	a := &About{}
	inst := r.Instance(aboutStateID, Kind("ochazuke.about-state"))
	inst.Component = a
	return a
}

// AddAboutSection adds a section of the application's rows to the About
// page, after Build. rows runs each time the page opens or refreshes and
// each time about.show or about.copy runs, never at startup. Adding a
// title again replaces that section where it stands.
func (r *Runtime) AddAboutSection(title string, rows func() []AboutRow) {
	a := r.about()
	if i := slices.IndexFunc(a.sections, func(s aboutSource) bool { return s.title == title }); i >= 0 {
		a.sections[i].rows = rows
		return
	}
	a.sections = append(a.sections, aboutSource{title: title, rows: rows})
	refreshAboutPage(r)
}

// SetAboutEnv sets where the About page reads the process from (a test's
// or a story's fixed build); zero fields stay the real ones.
func (r *Runtime) SetAboutEnv(env AboutEnv) {
	r.about().env = env
	refreshAboutPage(r)
}

func (e AboutEnv) buildInfo() (*debug.BuildInfo, bool) {
	if e.BuildInfo != nil {
		return e.BuildInfo()
	}
	return debug.ReadBuildInfo()
}

func (e AboutEnv) executable() (string, error) {
	if e.Executable != nil {
		return e.Executable()
	}
	return os.Executable()
}

func (e AboutEnv) pid() int { return cmpInt(e.PID, os.Getpid()) }

func (e AboutEnv) started() time.Time {
	if !e.Started.IsZero() {
		return e.Started
	}
	return processStarted
}

func (e AboutEnv) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e AboutEnv) open(path string) error {
	if e.Open != nil {
		return e.Open(path)
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func cmpInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// About evaluates every section now: Build, the application's sections,
// Dependencies, Runtime. An application section that panics reads one row
// saying so rather than taking the page down.
func (r *Runtime) About() []AboutSection {
	a := r.about()
	bi, ok := a.env.buildInfo()
	if !ok {
		bi = nil
	}
	out := []AboutSection{{Title: aboutBuildTitle, Rows: aboutBuildRows(bi, a.env)}}
	for _, src := range a.sections {
		out = append(out, AboutSection{Title: src.title, Rows: evalAboutRows(src)})
	}
	if deps := aboutDepRows(bi, r.Settings().Strings(AboutDepPrefixesSetting)); len(deps) > 0 {
		out = append(out, AboutSection{Title: aboutDepsTitle, Rows: deps})
	}
	out = append(out, AboutSection{Title: aboutRunTitle, Rows: r.aboutRuntimeRows(a.env)})
	return out
}

func evalAboutRows(src aboutSource) (rows []AboutRow) {
	defer func() {
		if p := recover(); p != nil {
			rows = []AboutRow{{Label: "error", Value: fmt.Sprint(p)}}
		}
	}()
	if src.rows == nil {
		return nil
	}
	return slices.Clone(src.rows())
}

// aboutBuildRows is the Build section: the main module from the build info
// with the linker stamps (refdoc.Version, Commit, Date, Dirty) over it, the
// Go version and the binary.
func aboutBuildRows(bi *debug.BuildInfo, env AboutEnv) []AboutRow {
	var module, version, commit, when, goVersion string
	modified := ""
	if bi != nil {
		module, goVersion = bi.Main.Path, bi.GoVersion
		version = bi.Main.Version
		for _, kv := range bi.Settings {
			switch kv.Key {
			case "vcs.revision":
				commit = kv.Value
			case "vcs.time":
				when = kv.Value
			case "vcs.modified":
				modified = kv.Value
			}
		}
	}
	if refdoc.Commit != "" {
		commit, when, modified = refdoc.Commit, refdoc.Date, strconv.FormatBool(refdoc.Dirty == "true")
	}
	if refdoc.Version != "" {
		version = refdoc.Version
	}
	if version == "" || version == "(devel)" {
		version = "devel"
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	rows := []AboutRow{
		{Label: "module", Value: orUnknown(module)},
		{Label: "version", Value: version},
		{Label: "commit", Value: orUnknown(commit)},
	}
	if when != "" {
		rows = append(rows, AboutRow{Label: "commit time", Value: when})
	}
	switch modified {
	case "true":
		rows = append(rows, AboutRow{Label: "modified", Value: "yes", Note: "built from a tree with uncommitted changes"})
	case "false":
		rows = append(rows, AboutRow{Label: "modified", Value: "no"})
	}
	if goVersion == "" {
		goVersion = runtime.Version()
	}
	rows = append(rows, AboutRow{Label: "go", Value: goVersion})
	if path, err := env.executable(); err == nil && path != "" {
		rows = append(rows, AboutRow{Label: "binary", Value: path, Kind: AboutPath})
	} else {
		rows = append(rows, AboutRow{Label: "binary", Value: "unknown"})
	}
	return rows
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// aboutDepRows is the Dependencies section: each module of the build whose
// path starts with one of prefixes, labelled by the path after the prefix.
// A module replaced by a directory (a replace directive to a local path)
// reads the directory as a path row; one a go.work brings in (version
// (devel), no replace) reads "devel" with a note; one replaced by another
// module reads the replacement's path and version.
func aboutDepRows(bi *debug.BuildInfo, prefixes []string) []AboutRow {
	if bi == nil || len(prefixes) == 0 {
		return nil
	}
	var rows []AboutRow
	for _, d := range bi.Deps {
		prefix := ""
		for _, p := range prefixes {
			if p != "" && strings.HasPrefix(d.Path, p) && len(p) > len(prefix) {
				prefix = p
			}
		}
		if prefix == "" || d.Path == bi.Main.Path {
			continue
		}
		label := strings.TrimPrefix(d.Path, prefix)
		if label == "" {
			label = d.Path
		}
		row := AboutRow{Label: label, Value: d.Version}
		switch rep := d.Replace; {
		case rep != nil && rep.Version == "":
			row.Value, row.Kind = rep.Path, AboutPath
			row.Note = "local replace of " + cmpStr(d.Version, "devel")
		case rep != nil:
			row.Value = rep.Version
			if rep.Path != d.Path {
				row.Value = rep.Path + " " + rep.Version
			}
			row.Note = "replaces " + d.Version
		case d.Version == "(devel)" || d.Version == "":
			row.Value, row.Note = "devel", "local build (go.work)"
		}
		rows = append(rows, row)
	}
	return rows
}

func cmpStr(a, b string) string {
	if a != "" && a != "(devel)" {
		return a
	}
	return b
}

// aboutRuntimeRows is the Runtime section: the pid, the start time, the
// uptime and each status segment's last text.
func (r *Runtime) aboutRuntimeRows(env AboutEnv) []AboutRow {
	started := env.started()
	rows := []AboutRow{
		{Label: "pid", Value: strconv.Itoa(env.pid())},
		{Label: "started", Value: started.Local().Format("2006-01-02 15:04:05")},
		{Label: "uptime", Value: env.now().Sub(started).Round(time.Second).String()},
	}
	if g := r.segs(); g != nil && len(g.rows) > 0 {
		for _, seg := range r.Segments() {
			if seg.Text == "" {
				continue
			}
			label := seg.Label
			if label == "" {
				label = seg.ID
			}
			rows = append(rows, AboutRow{Label: label, Value: seg.Text, Note: "status segment " + seg.ID})
		}
	}
	return rows
}

// AboutText is sections as plain text, a block per section with its rows'
// labels aligned: what about.copy yanks for a bug report.
func AboutText(sections []AboutSection) string {
	var b strings.Builder
	for i, sec := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sec.Title + "\n")
		width := 0
		for _, row := range sec.Rows {
			width = max(width, ansi.StringWidth(row.Label))
		}
		for _, row := range sec.Rows {
			line := "  " + row.Label + strings.Repeat(" ", width-ansi.StringWidth(row.Label)) + "  " + row.Value
			if row.Note != "" {
				line += "  (" + row.Note + ")"
			}
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}
	return b.String()
}

// aboutRowID is a row's instance id: its section and label.
func aboutRowID(section, label string) string { return aboutRowPrefix + section + "/" + label }

// aboutRowRef is a row instance's Data.
type aboutRowRef struct {
	Section string `json:"section"`
	AboutRow
}

// refreshAboutPage evaluates the rows again and gives the open page's list
// them; the selection stays on its row.
func refreshAboutPage(r *Runtime) {
	if r.Instances[AboutPageID] == nil {
		return
	}
	a := r.about()
	a.shown = r.About()
	var items []ListItem
	labels := 0
	for _, sec := range a.shown {
		for _, row := range sec.Rows {
			labels = max(labels, ansi.StringWidth(row.Label))
			value := row.Value
			if row.Note != "" {
				value += "  (" + row.Note + ")"
			}
			items = append(items, ListItem{
				ID: aboutRowID(sec.Title, row.Label), Label: row.Label, Noun: aboutRowNoun, Section: sec.Title,
				Data: aboutRowRef{Section: sec.Title, AboutRow: row}, Cells: []string{row.Label, value},
			})
		}
	}
	list := r.List(aboutListID)
	if list == nil {
		var err error
		if list, err = r.AddList(aboutListID, nil, Parent(AboutPageID), NoMarks()); err != nil {
			return
		}
	}
	list.SetColumns(Column{Width: min(labels+2, 24)}, Column{Min: 6, Priority: 1})
	_ = list.SetItems(items)
}

// ensureAboutPage registers the page when it is not yet (owned when
// about.open does) and evaluates its rows.
func ensureAboutPage(r *Runtime, owned bool) (*Page, error) {
	a := r.about()
	if page := r.PageOf(AboutPageID); page != nil {
		refreshAboutPage(r)
		return page, nil
	}
	r.Instance(AboutPageID, Parent("app"), Kind(aboutPageNoun))
	refreshAboutPage(r)
	page := &Page{ID: AboutPageID, Title: "About", Focus: aboutListID, Render: aboutPageView}
	page.OnOpen = func(r *Runtime, from string) {
		a.from = from
		// A page registered just now holds fresh rows already.
		if a.filled {
			a.filled = false
			return
		}
		refreshAboutPage(r)
	}
	if err := r.RegisterPage(page, Kind(aboutPageNoun)); err != nil {
		_ = r.RemoveInstance(AboutPageID)
		return nil, err
	}
	if _, err := r.AddDetail(aboutDetailID, func() string { return selectedAboutRow(r) }, Parent(AboutPageID)); err != nil {
		_ = r.RemoveInstance(AboutPageID)
		return nil, err
	}
	if list := r.List(aboutListID); list != nil && list.Len() > 0 {
		if item, ok := list.Selected(); ok {
			page.Focus = item.ID
		}
	}
	a.owned, a.filled = owned, true
	return page, nil
}

// openAboutPage opens (or switches to) the About page.
func openAboutPage(r *Runtime) error {
	if r.Page == AboutPageID {
		refreshAboutPage(r)
		return nil
	}
	fresh := r.PageOf(AboutPageID) == nil
	if _, err := ensureAboutPage(r, fresh); err != nil {
		return err
	}
	if err := r.OpenPage(AboutPageID); err != nil {
		if fresh {
			_ = r.RemoveInstance(AboutPageID)
		}
		return err
	}
	return nil
}

// closeAboutPage goes back to the page the About page was opened from
// (else the first other page) and removes the page when about.open
// registered it.
func closeAboutPage(r *Runtime) error {
	if r.PageOf(AboutPageID) == nil {
		return errors.New("the about page is not open")
	}
	a := r.about()
	if r.Page == AboutPageID {
		back := a.from
		if back == AboutPageID || r.PageOf(back) == nil {
			back = ""
			for _, page := range r.PageList() {
				if page.ID != AboutPageID {
					back = page.ID
					break
				}
			}
		}
		if back == "" {
			return errors.New("there is no page to go back to")
		}
		if err := r.OpenPage(back); err != nil {
			return err
		}
	}
	if a.owned {
		a.from = ""
		return r.RemoveInstance(AboutPageID)
	}
	return nil
}

// selectedAboutRow is the list's selected row id (the detail's item).
func selectedAboutRow(r *Runtime) string {
	if list := r.List(aboutListID); list != nil {
		if item, ok := list.Selected(); ok {
			return item.ID
		}
	}
	return ""
}

// aboutRowOf is the row an about-row verb acts on: the focused row
// instance, else the page list's selected row.
func aboutRowOf(r *Runtime, c Context) (aboutRowRef, error) {
	for _, id := range []string{focusOf(c), selectedAboutRow(r)} {
		if inst := r.Instances[id]; inst != nil && inst.Noun == aboutRowNoun {
			if ref, ok := inst.Data.(aboutRowRef); ok {
				return ref, nil
			}
		}
	}
	return aboutRowRef{}, errors.New("no about row is focused")
}

// aboutRowDetail is the detail of the selected row: its section and label,
// the whole value (a list cell cuts a long one) and the note.
func aboutRowDetail(ref aboutRowRef) (title, text string) {
	lines := []string{ref.Value}
	if ref.Note != "" {
		lines = append(lines, "", ref.Note+".")
	}
	return ref.Section + " › " + ref.Label, strings.Join(lines, "\n")
}

// aboutPageView draws the Settings page's tabs (with about.tab), then the
// rows over the selected row's detail.
func aboutPageView(r *Runtime, width, height int) Block {
	if r.Instances[AboutPageID] == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	styles := NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if view := r.Settings().page; view.styles != nil {
		styles = view.styles()
	}
	layout := r.PaneLayout(AboutPageID, Layout{Styles: styles})
	var rows []string
	if titles, active := r.SettingsTabTitles(AboutPageID); active >= 0 && len(titles) > 1 && height > PaneMinHeight {
		rows = append(rows, layout.tabs(titles, active, width))
	}
	body := height - len(rows)
	list := r.List(aboutListID)
	if list == nil || body <= 0 {
		return Text(strings.Join(rows, "\n"))
	}
	// The rows take what they need (border included), leaving the detail
	// six lines at least.
	th := layout.Divide(max(min(list.Height()+headingLines(list)+2, body-6), PaneMinHeight), body, PaneMinHeight)
	tw, tth := PanelContentSize(width, th)
	tableBody := Text(list.View(tw, tth))
	dw, dch := PanelContentSize(width, body-th)
	title, text := "Detail", ""
	if inst := r.Instances[selectedAboutRow(r)]; inst != nil {
		if ref, ok := inst.Data.(aboutRowRef); ok {
			title, text = aboutRowDetail(ref)
		}
	}
	content, marker := "", ""
	if detail := r.Detail(aboutDetailID); detail != nil {
		content, marker = detail.Window(text, dw, dch)
	}
	if marker != "" {
		title += " · " + marker
	}
	focused := SplitNone
	if list.Focused() {
		focused = SplitLeft
	}
	pair := layout.panelPair("About", tableBody, title, Text(content), width, body, th, true, focused)
	if len(rows) == 0 {
		return pair
	}
	pair = pair.Offset(0, len(rows))
	return Block{Text: strings.Join(rows, "\n") + "\n" + pair.Text, Cursor: pair.Cursor}
}

// registerAbout registers the about and about-row verbs, the about
// settings' guard and the About tab of the Settings page.
func registerAbout(r *Runtime) {
	r.about()
	r.Register(Operation{Verb: "open", Noun: "ochazuke.about", Description: "Open the About page (or switch to it): the build, the application's own rows, the family dependencies and the process", Result: "about-section[]", HistoryKind: HistoryAction, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if err := openAboutPage(r); err != nil {
			return nil, err
		}
		return r.about().shown, nil
	}})
	r.Register(Operation{Verb: "close", Noun: "ochazuke.about", Description: "Leave the About page for the page it was opened from", Result: "context", HistoryKind: HistoryAction, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Context(), closeAboutPage(r)
	}})
	r.Register(Operation{Verb: "refresh", Noun: "ochazuke.about", Description: "Evaluate the About page's rows again", Result: "about-section[]", HistoryKind: HistoryFetch, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.Instances[AboutPageID] == nil {
			return r.About(), nil
		}
		refreshAboutPage(r)
		return r.about().shown, nil
	}})
	sectionParam := map[string]Parameter{"section": {Type: "string", Description: "Only this section, by title (Build, Dependencies, Runtime, or one the application adds); default every section"}}
	r.Register(Operation{Verb: "show", Noun: "ochazuke.about", Description: "Answer the About page's sections as data: the build, the application's rows, the family dependencies and the process", Parameters: sectionParam, Result: "about-section[]", HistoryKind: HistoryFetch, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return aboutSections(r, StringArg(a, "section", ""))
	}})
	r.Register(Operation{Verb: "copy", Noun: "ochazuke.about", Description: "Yank the About page's sections as text, for a bug report", Parameters: sectionParam, Result: "string", HistoryKind: HistoryChange, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		secs, err := aboutSections(r, StringArg(a, "section", ""))
		if err != nil {
			return nil, err
		}
		text := AboutText(secs)
		if err := r.storeYank(Yank{Text: text, Shape: "line"}, "", false); err != nil {
			return nil, err
		}
		return text, nil
	}})
	r.Register(Operation{Verb: "show", Noun: aboutRowNoun, Description: "Answer the about row: its section, label, value, kind and note", Result: "about-row", HistoryKind: HistoryFetch, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return aboutRowOf(r, c)
	}})
	r.Register(Operation{Verb: "copy", Noun: aboutRowNoun, Description: "Yank the about row's value", Result: "string", HistoryKind: HistoryChange, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		ref, err := aboutRowOf(r, c)
		if err != nil {
			return nil, err
		}
		if err := r.storeYank(Yank{Text: ref.Value, Shape: "char"}, "", false); err != nil {
			return nil, err
		}
		return ref.Value, nil
	}})
	r.Register(Operation{Verb: "open", Noun: aboutRowNoun, Description: "Open the about row's path (a file or a directory) with the system's opener", Result: "string", HistoryKind: HistoryAction, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		ref, err := aboutRowOf(r, c)
		if err != nil {
			return nil, err
		}
		if ref.Kind != AboutPath {
			return nil, fmt.Errorf("%s is not a path", ref.Label)
		}
		if err := r.about().env.open(ref.Value); err != nil {
			return nil, err
		}
		return ref.Value, nil
	}})
	r.DefineGuard("about-row-path", func(r *Runtime, ev KeyEvent) bool {
		inst := r.Instances[ev.Focus]
		if inst == nil || inst.Noun != aboutRowNoun {
			return false
		}
		ref, ok := inst.Data.(aboutRowRef)
		return ok && ref.Kind == AboutPath
	})
	r.DefineGuard("about-tab", func(r *Runtime, ev KeyEvent) bool {
		_, active := r.SettingsTabTitles(AboutPageID)
		return active >= 0
	})
	r.LinkSettingsTab(SettingsTabLink{
		Title:   "About",
		Page:    AboutPageID,
		Open:    Request{Noun: "ochazuke.about", Verb: "open"},
		Close:   Request{Noun: "ochazuke.about", Verb: "close"},
		Setting: AboutTabSetting,
	})
	r.Settings().Observe(AboutDepPrefixesSetting, func(string, any) { refreshAboutPage(r) })
}

// aboutSections is every section, or the one titled section (case
// ignored).
func aboutSections(r *Runtime, section string) ([]AboutSection, error) {
	all := r.About()
	if section == "" {
		return all, nil
	}
	var titles []string
	for _, sec := range all {
		if strings.EqualFold(sec.Title, section) {
			return []AboutSection{sec}, nil
		}
		titles = append(titles, sec.Title)
	}
	return nil, fmt.Errorf("no about section %q: one of %s", section, strings.Join(titles, ", "))
}
