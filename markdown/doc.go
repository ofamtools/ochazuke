package markdown

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// Source is where a Doc's Markdown lives: Load reads it as it is now and
// Save stores a new text. A toggle loads the source again first, so a
// change made elsewhere since the Doc last read it is seen before anything
// is written; the framework makes the one-character edit and hands Save
// the whole text. A nil Save makes the Doc read-only (x is refused).
type Source struct {
	Load func() (string, error)
	Save func(text string) error
}

// File is a Source over a file: Save writes atomically (a temporary file
// beside it, then a rename over it, keeping its permissions), so a reader
// never sees half a file. A symlink is written through to its target.
func File(path string) Source {
	return Source{
		Load: func() (string, error) {
			b, err := os.ReadFile(path)
			return string(b), err
		},
		Save: func(text string) error { return writeAtomic(path, []byte(text)) },
	}
}

func writeAtomic(path string, data []byte) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// Nouns: the Doc instance (its verbs and keys) and its task rows.
const (
	DocNoun  = "markdown"
	TaskNoun = "markdown-task"
)

// Doc is a rendered Markdown document whose task items are a focus stop:
// j and k (the list noun's rows, counts too) move the one highlight
// between the items over the rendered text and spill to the next stop at
// either end, x toggles the selected item (markdown.toggle), u and ctrl+r
// undo and redo a toggle (markdown.undo, markdown.redo), each written to
// the Source at once. A toggle rewrites only the item's box character.
// Before writing, the Doc loads the source again: when it changed since
// the Doc read it, the item is found again by its text (and its place
// among items with the same text), and when it is gone or its text
// changed the toggle is refused with an error naming it, nothing written.
//
// The Doc is an instance of the markdown noun with a list child (id/tasks,
// added NoMarks) whose rows are instances of markdown-task, one per item;
// a document without items is not a focus stop. The application draws it
// with View in its page.
type Doc struct {
	r     *ochazuke.Runtime
	id    string
	src   Source
	list  *ochazuke.List
	text  string
	tasks []Task
	mark  string // text with the task markers, for rendering
	undo  []edit
	redo  []edit
	// offset is the first line View shows when it windows the document.
	offset int
}

// edit is one written toggle: the item (its text and its place among
// items with that text) and the box character before and after.
type edit struct {
	text     string
	nth      int
	from, to byte
}

// New loads a Doc from src and places it under parent (a page or an
// instance beneath it). It registers the markdown nouns once per runtime.
func New(r *ochazuke.Runtime, id, parent string, src Source) (*Doc, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("markdown id %q must be unique and nonempty", id)
	}
	if r.Instances[parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", parent)
	}
	if src.Load == nil {
		return nil, errors.New("markdown source needs a Load")
	}
	text, err := src.Load()
	if err != nil {
		return nil, fmt.Errorf("load markdown: %w", err)
	}
	Register(r)
	d := &Doc{r: r, id: id, src: src}
	r.Instance(id, ochazuke.Parent(parent), ochazuke.Kind(DocNoun)).Component = d
	list, err := r.AddList(d.ListID(), nil, ochazuke.Parent(id), ochazuke.NoMarks())
	if err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	d.list = list
	if err := d.set(text); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	return d, nil
}

// Of is the Doc an instance fronts, or nil.
func Of(r *ochazuke.Runtime, id string) *Doc {
	if inst := r.Instances[id]; inst != nil {
		if d, ok := inst.Component.(*Doc); ok {
			return d
		}
	}
	return nil
}

// ID is the Doc's instance id.
func (d *Doc) ID() string { return d.id }

// List is the list of task rows (the focus stop).
func (d *Doc) List() *ochazuke.List { return d.list }

// ListID is the task list's instance id: the Doc's id + "/tasks".
func (d *Doc) ListID() string { return d.id + "/tasks" }

// Text is the source as the Doc last read or wrote it.
func (d *Doc) Text() string { return d.text }

// Tasks are the document's task items.
func (d *Doc) Tasks() []Task { return append([]Task(nil), d.tasks...) }

// Selected is the selected task item.
func (d *Doc) Selected() (Task, bool) {
	i := d.selected()
	if i < 0 {
		return Task{}, false
	}
	return d.tasks[i], true
}

func (d *Doc) selected() int {
	item, ok := d.list.Selected()
	if !ok {
		return -1
	}
	i, _ := item.Data.(int)
	if i < 0 || i >= len(d.tasks) {
		return -1
	}
	return i
}

// Focus focuses the selected task item.
func (d *Doc) Focus() error { return d.list.Focus() }

// FocusTarget is where a jump to the Doc lands: its selected item.
func (d *Doc) FocusTarget() string { return d.list.FocusTarget() }

// Select selects the i-th task item (from 0) without taking the focus.
func (d *Doc) Select(i int) error {
	if i < 0 || i >= len(d.tasks) {
		return fmt.Errorf("no task %d", i)
	}
	return d.list.Select(d.rowID(i))
}

func (d *Doc) rowID(i int) string { return d.id + "/task/" + strconv.Itoa(i) }

// set takes a new text: its tasks, the rows (the selection is kept by
// place) and whether the Doc is a focus stop.
func (d *Doc) set(text string) error {
	d.text, d.tasks = text, Tasks(text)
	d.mark = marked(text, d.tasks)
	items := make([]ochazuke.ListItem, len(d.tasks))
	for i, t := range d.tasks {
		items[i] = ochazuke.ListItem{ID: d.rowID(i), Label: t.Text, Noun: TaskNoun, Data: i}
	}
	if err := d.list.SetItems(items); err != nil {
		return err
	}
	d.r.Instances[d.ListID()].Unfocusable = len(d.tasks) == 0
	return nil
}

// Reload reads the source again.
func (d *Doc) Reload() error {
	text, err := d.src.Load()
	if err != nil {
		return fmt.Errorf("load markdown: %w", err)
	}
	return d.set(text)
}

// Toggle flips the selected item: [ ] becomes [x], [x] or [X] becomes
// [ ], written at once; the item is found again first when the source
// changed (see Doc). The written toggle is one undo step.
func (d *Doc) Toggle() error {
	i := d.selected()
	if i < 0 {
		return errors.New("no task item selected")
	}
	t := d.tasks[i]
	to := byte('x')
	if t.Checked {
		to = ' '
	}
	e := edit{text: t.Text, nth: d.nth(d.tasks, i), to: to}
	from, wrote, err := d.apply(e.text, e.nth, to)
	if err != nil || !wrote {
		return err
	}
	e.from = from
	d.undo, d.redo = append(d.undo, e), nil
	return nil
}

// Undo puts the last toggle's box character back, written at once.
func (d *Doc) Undo() error { return d.history(&d.undo, &d.redo, false) }

// Redo writes the last undone toggle again.
func (d *Doc) Redo() error { return d.history(&d.redo, &d.undo, true) }

func (d *Doc) history(from, to *[]edit, redo bool) error {
	if len(*from) == 0 {
		if redo {
			return errors.New("nothing to redo")
		}
		return errors.New("nothing to undo")
	}
	e := (*from)[len(*from)-1]
	want := e.from
	if redo {
		want = e.to
	}
	if _, _, err := d.apply(e.text, e.nth, want); err != nil {
		return err
	}
	*from = (*from)[:len(*from)-1]
	*to = append(*to, e)
	return nil
}

// nth is the place of task i among the tasks with its text.
func (d *Doc) nth(tasks []Task, i int) int {
	n := 0
	for _, t := range tasks[:i] {
		if t.Text == tasks[i].Text {
			n++
		}
	}
	return n
}

// apply sets the box of the item (text, nth) to want in the source as it
// is now and writes it: it reports the character it replaced and whether
// it wrote (an item already in that state is not written again).
func (d *Doc) apply(text string, nth int, want byte) (from byte, wrote bool, err error) {
	if d.src.Save == nil {
		return 0, false, errors.New("this markdown is read-only")
	}
	current, err := d.src.Load()
	if err != nil {
		return 0, false, fmt.Errorf("not toggled: load markdown: %w", err)
	}
	tasks := d.tasks
	if current != d.text {
		tasks = Tasks(current)
	}
	t, i, ok := find(d.tasks, tasks, text, nth)
	if current != d.text {
		// Show the source as it is now, whatever happens next.
		if err := d.set(current); err != nil {
			return 0, false, err
		}
	}
	if !ok {
		return 0, false, fmt.Errorf("not toggled: %q changed or is gone in the source", text)
	}
	_ = d.Select(i)
	from = current[t.Box]
	if checked(from) == checked(want) {
		return from, false, nil
	}
	next := current[:t.Box] + string(want) + current[t.Box+1:]
	if err := d.src.Save(next); err != nil {
		return 0, false, fmt.Errorf("not toggled: save markdown: %w", err)
	}
	if err := d.set(next); err != nil {
		return from, true, err
	}
	_ = d.Select(i)
	return from, true, nil
}

func checked(box byte) bool { return box == 'x' || box == 'X' }

// find is the item (text, nth) of the tasks before in the tasks now: the
// nth with that text when as many have it as before, else the only one;
// several where the count changed is ambiguous and not found.
func find(before, now []Task, text string, nth int) (Task, int, bool) {
	count := func(tasks []Task) (out []int) {
		for i, t := range tasks {
			if t.Text == text {
				out = append(out, i)
			}
		}
		return out
	}
	was, is := count(before), count(now)
	switch {
	case len(is) == len(was) && nth < len(is):
		return now[is[nth]], is[nth], true
	case len(is) == 1:
		return now[is[0]], is[0], true
	}
	return Task{}, -1, false
}

// View draws the document width cells wide in the appearance's Glamour
// style (StyleOf), the selected item's lines in reverse video while its
// list holds the focus (the one highlight: an unfocused Doc draws no
// highlight). With height > 0 a longer document is windowed to height
// lines, the last a position marker ("12–40 / 300"), scrolled to keep the
// selected item visible with the page's scrolloff; with height 0 every
// line is drawn and the page scrolls. Either way it places the task list
// in its page's drawing order and reports the selected item's lines
// (Runtime.Placed, PlacedAt), so focus jumps and the page follow it.
func (d *Doc) View(width, height int, a ochazuke.Appearance) string {
	id := d.ListID()
	d.r.Placed(id)
	rend := render(d.mark, min(len(d.tasks), maxMarkers), width, StyleOf(a))
	lines := append([]string(nil), rend.lines...)
	focus, focusHeight := 0, len(lines)
	if i := d.selected(); i >= 0 && i < len(rend.spans) && rend.spans[i][0] >= 0 {
		top, bottom := rend.spans[i][0], rend.spans[i][1]
		focus, focusHeight = top, bottom-top+1
		if d.list.Focused() {
			for l := top; l <= bottom; l++ {
				lines[l] = highlight(lines[l])
			}
		}
	}
	if height > 0 && len(lines) > height {
		visible := max(1, height-1)
		if height == 1 {
			visible = 1
		}
		d.offset = scrollToShow(d.offset, focus, focus+max(1, focusHeight), visible, len(lines), d.scrolloff())
		end := min(len(lines), d.offset+visible)
		window := lines[d.offset:end]
		if height > 1 {
			window = append(window[:len(window):len(window)], fmt.Sprintf("%d–%d / %d", d.offset+1, end, len(lines)))
		}
		focus -= d.offset
		if focus < 0 || focus >= visible {
			focus, focusHeight = 0, len(window)
		}
		lines = window
	}
	return d.r.PlacedAt(id, strings.Join(lines, "\n"), focus, focusHeight)
}

// highlight draws a rendered line's text in reverse video, its indent and
// trailing padding plain, at the same width.
func highlight(line string) string {
	plain := ansi.Strip(line)
	width := ansi.StringWidth(plain)
	body := strings.TrimLeft(plain, " ")
	indent := len(plain) - len(body)
	body = strings.TrimRight(body, " ")
	if body == "" {
		return line
	}
	out := strings.Repeat(" ", indent) + lipgloss.NewStyle().Reverse(true).Render(body)
	return out + strings.Repeat(" ", max(0, width-indent-ansi.StringWidth(body)))
}

// scrolloff is the open page's margin around the focus (Page.Scrolloff:
// 0 is the default 2, negative none).
func (d *Doc) scrolloff() int {
	if page := d.r.PageOf(d.r.Page); page != nil {
		switch {
		case page.Scrolloff < 0:
			return 0
		case page.Scrolloff > 0:
			return page.Scrolloff
		}
	}
	return 2
}

// scrollToShow is the offset that shows lines [top, bottom) of total in a
// height-line window at offset with margin lines around them, moving as
// little as it can (the rule the page and lists follow).
func scrollToShow(offset, top, bottom, height, total, margin int) int {
	margin = min(margin, max(0, (height-1)/2))
	if bottom-top > height {
		offset = top
	} else {
		if bottom+margin > offset+height {
			offset = bottom + margin - height
		}
		if top-margin < offset {
			offset = top - margin
		}
	}
	return min(max(0, offset), max(0, total-height))
}

// Snapshot is the Doc's state as the verbs return it.
func (d *Doc) Snapshot() map[string]any {
	selected := -1
	if i := d.selected(); i >= 0 {
		selected = i
	}
	return map[string]any{"id": d.id, "tasks": d.Tasks(), "selected": selected, "undo": len(d.undo), "redo": len(d.redo)}
}

// Register defines the markdown nouns, their verbs and keys once per
// runtime: on the markdown noun in normal mode x toggles, u undoes and
// ctrl+r redoes; the task rows' j/k are the list noun's.
func Register(r *ochazuke.Runtime) {
	if _, ok := r.Operations[DocNoun+".toggle"]; ok {
		return
	}
	on := func(run func(*Doc) error) ochazuke.Handler {
		return func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			d, err := resolve(r, c, a)
			if err != nil {
				return nil, err
			}
			err = run(d)
			return d.Snapshot(), err
		}
	}
	r.Define(TaskNoun)
	r.Define(DocNoun).
		Verb("toggle", on((*Doc).Toggle), "Toggle the selected task item: rewrites only its box character in the source, written at once").
		Verb("undo", on((*Doc).Undo), "Undo the last toggle; the undo is written like a toggle").
		Verb("redo", on((*Doc).Redo), "Redo the last undone toggle").
		Verb("reload", on((*Doc).Reload), "Read the Markdown source again").
		Mode("normal").
		Bind("x", ochazuke.Do(DocNoun+".toggle")).
		Bind("u", ochazuke.Do(DocNoun+".undo")).
		Bind("ctrl+r", ochazuke.Do(DocNoun+".redo"))
	for _, verb := range []string{"toggle", "undo", "redo", "reload"} {
		op := r.Operations[DocNoun+"."+verb]
		op.Parameters = map[string]ochazuke.Parameter{"id": {Type: "string", Description: "The markdown instance (default the one on the focus path)"}}
		op.Result = "markdown state"
		r.Operations[op.Key()] = op
	}
}

// resolve is the Doc named by args.id, else the nearest on the context.
func resolve(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (*Doc, error) {
	if id := ochazuke.StringArg(a, "id", ""); id != "" {
		if d := Of(r, id); d != nil {
			return d, nil
		}
		return nil, fmt.Errorf("unknown markdown %q", id)
	}
	for i := len(c.Path) - 1; i >= 0; i-- {
		if d := Of(r, c.Path[i]); d != nil {
			return d, nil
		}
	}
	return nil, errors.New("no markdown on the focus path")
}
