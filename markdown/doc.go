package markdown

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

// Source is where a Doc's Markdown lives: Load reads it as it is now and
// Save stores a new text. Every edit loads the source again first, so a
// change made elsewhere since the Doc last read it is seen before anything
// is written; the framework makes the edit and hands Save the whole text.
// A nil Save makes the Doc read-only (x, i, o and every other edit are
// refused).
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

// WriteFile writes text to path atomically, as File's Save does.
func WriteFile(path, text string) error { return writeAtomic(path, []byte(text)) }

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

// ErrRefused wraps an edit, toggle, undo or redo the Doc would not make
// because what it acts on is gone, changed or ambiguous in the source as
// it is now; nothing was written.
var ErrRefused = errors.New("not toggled")

// refusal is an error that is ErrRefused with a message of its own.
type refusal struct{ msg string }

func (e refusal) Error() string        { return e.msg }
func (e refusal) Is(target error) bool { return target == ErrRefused }

func refused(format string, args ...any) error { return refusal{fmt.Sprintf(format, args...)} }

// Nouns: the Doc instance (its verbs and keys), its line rows (a task's
// lines are task rows, a note's note rows) and the block editor's input.
const (
	DocNoun  = "ochazuke.markdown"
	TaskNoun = "ochazuke.markdown-task"
	LineNoun = "ochazuke.markdown-line"
	NoteNoun = "ochazuke.markdown-note"
	EditNoun = "ochazuke.markdown-edit"
)

// AuthorSetting names the author a new note is signed with.
const AuthorSetting = "markdown.note-author"

// CursorSetting chooses element-wise (item, the default) or drawn-line movement.
const CursorSetting = "markdown.cursor"

// CursorWidthSetting chooses a pane-wide cursor or the former text-only bar.
const CursorWidthSetting = "markdown.cursor-width"

// ExtraSpaceSetting adds a blank line between rendered Markdown components.
// The default is false; source text and spacing within a component are unchanged.
const ExtraSpaceSetting = "markdown.extra-space"

// DefaultDebounce is how long typing in the block editor pauses before
// the block is written.
const DefaultDebounce = 400 * time.Millisecond

// DebounceMsg is the block editor's pause timer.
type DebounceMsg struct {
	Doc string
	Seq int
}

// Doc is a rendered Markdown document with an element cursor, edited in place.
//
// The document is a sequence of elements (Elements): headings,
// paragraphs, list items, table rows, code blocks, quotes, rules and notes.
// The Doc's list child (id/lines, added NoMarks) has a row per drawn line.
// By default j and k visit elements and their inline links, highlighting
// every line of the containing element. markdown.link-stops=false restores
// element-only movement. markdown.cursor=line restores drawn-line movement.
// gg/G and paging
// remain line-wise list motions. Counts and spills work in either mode.
// ctrl+e/y scroll within long elements without leaving item mode.
// }/{ move by element, ]]/[[ to the next element of the same kind, ] or
// [ with a kind letter (KindLetters) to the next of that kind.
//
// Editing: i, a, I and A edit the cursor's element as raw Markdown in the
// ochazuke editor, in place (only that element switches to its source);
// o and O open a new line below or above that continues its structure;
// esc draws it again. dd, yy and p work on lines, x toggles a checklist
// item, n adds a note under an item and za folds an item's notes; u and
// ctrl+r undo and redo every edit. There is no save key: typing is written
// after a pause (Debounce) and when the element is left, every other edit
// at once, through Source.Save. An edit rewrites only the bytes of the
// element it changes; before writing, the Doc loads the source again and
// finds what it acts on again when it moved, refusing with an error
// wrapping ErrRefused when it is gone or changed.
type Doc struct {
	r     *ochazuke.Runtime
	id    string
	src   Source
	list  *ochazuke.List
	text  string
	tasks []Task
	elems []Element

	links                            []Link
	linkIndex, linkElement, linkLine int
	linkExplicit                     bool
	// OpenURL opens a focused link. Nil uses the system default handler.
	OpenURL func(string) error

	opts options // how View renders (New's options)
	undo []step
	redo []step
	// offset is the first line View shows when it windows the document,
	// visible how many lines the last View showed then (0 unwindowed).
	offset  int
	visible int
	// width and style are what the rows were laid out for: the last
	// View's, 80 and dark before the first.
	width      int
	style      string
	lay        layout
	dirty      bool
	folds      map[string]bool
	done       string // document-local visibility override; empty follows DoneSetting
	doneHidden int    // checked tasks omitted from the current layout
	edit       *session
	// leftAt is where the last element left starts in the source.
	leftAt int
	// Debounce is the pause in typing before the open element is written.
	Debounce time.Duration
	// Now is the clock a new note is dated by (default time.Now).
	Now func() time.Time
	// Author, when set, signs new notes instead of the AuthorSetting.
	Author string
}

// step is one undoable edit: a toggle (found again by its item's text)
// or a splice (at, old replaced by new; ctx is the text before at).
type step struct {
	toggle   *edit
	at       int
	old, new string
	ctx      string
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
// The options are View's (PreserveNewlines); they change only how the
// document is drawn, never what an edit writes.
func New(r *ochazuke.Runtime, id, parent string, src Source, opts ...Option) (*Doc, error) {
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
	d := &Doc{linkIndex: -1, linkElement: -1, linkLine: -1, r: r, id: id, src: src, opts: optionsOf(opts), width: 80, style: StyleDark, folds: map[string]bool{}, Debounce: DefaultDebounce}
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

// List is the list of line rows (the focus stop).
func (d *Doc) List() *ochazuke.List { return d.list }

// ListID is the line list's instance id: the Doc's id + "/lines".
func (d *Doc) ListID() string { return d.id + "/lines" }

// EditID is the block editor's input id while an element is open.
func (d *Doc) EditID() string { return d.id + "/edit" }

// Text is the source as the Doc last read or wrote it.
func (d *Doc) Text() string { return d.text }

// Tasks are the document's task items.
func (d *Doc) Tasks() []Task { return append([]Task(nil), d.tasks...) }

// Elements are the document's elements.
func (d *Doc) Elements() []Element { return append([]Element(nil), d.elems...) }

// Editable reports whether the Doc writes (its Source has a Save).
func (d *Doc) Editable() bool { return d.src.Save != nil }

// Editing reports whether an element is open in the block editor.
func (d *Doc) Editing() bool { return d.edit != nil }

// SetSource points the Doc at another source (a buffer named with save
// as): it is loaded and the next edit is written there; the undo history
// stays.
func (d *Doc) SetSource(src Source) error {
	if src.Load == nil {
		return errors.New("markdown source needs a Load")
	}
	if err := d.Leave(); err != nil {
		return err
	}
	d.src = src
	return d.Reload()
}

// Selected is the task item the cursor is on.
func (d *Doc) Selected() (Task, bool) {
	i := d.selected()
	if i < 0 {
		return Task{}, false
	}
	return d.tasks[i], true
}

// selected is the index of the task the cursor's element is, or -1.
func (d *Doc) selected() int {
	e, _ := d.cursor()
	if e < 0 || d.elems[e].Kind != KindTask {
		return -1
	}
	for i, t := range d.tasks {
		if t.Box == d.elems[e].Box {
			return i
		}
	}
	return -1
}

// Cursor is the element the cursor is on (an index into Elements, -1 for
// none) and the cursor's line within it, from 0.
func (d *Doc) Cursor() (int, int) { return d.cursor() }

func (d *Doc) cursor() (int, int) {
	if d.edit != nil {
		return d.edit.elem, 0
	}
	item, ok := d.list.Selected()
	if !ok {
		return -1, 0
	}
	line, _ := item.Data.(int)
	if line < 0 || line >= len(d.lay.owner) {
		return -1, 0
	}
	e := d.lay.owner[line]
	if e < 0 || e >= len(d.elems) {
		return -1, 0
	}
	return e, line - d.lay.spans[e][0]
}

// Focus focuses the cursor's line.
func (d *Doc) Focus() error { return d.list.Focus() }

// FocusTarget is where a jump to the Doc lands: its cursor's line.
func (d *Doc) FocusTarget() string { return d.list.FocusTarget() }

// Select puts the cursor on the i-th task item (from 0) without taking the
// focus.
func (d *Doc) Select(i int) error {
	if i < 0 || i >= len(d.tasks) {
		return fmt.Errorf("no task %d", i)
	}
	for e, el := range d.elems {
		if el.Kind == KindTask && el.Box == d.tasks[i].Box {
			return d.SelectElement(e)
		}
	}
	return fmt.Errorf("task %d is not drawn", i)
}

// SelectElement puts the cursor on element e, focusing its first link
// when link stops are enabled, otherwise its first line.
func (d *Doc) SelectElement(e int) error {
	if err := d.selectAt(e, 0); err != nil {
		return err
	}
	d.linkIndex, d.linkElement, d.linkLine, d.linkExplicit = -1, e, d.cursorLine(), false
	if d.r.Settings().Bool(LinkStopsSetting) {
		if i := d.firstLink(e); i >= 0 {
			return d.selectLink(i, false)
		}
	}
	return nil
}

func (d *Doc) selectAt(e, off int) error {
	if e < 0 || e >= len(d.elems) || e >= len(d.lay.spans) || d.lay.spans[e][0] < 0 {
		return fmt.Errorf("no element %d drawn", e)
	}
	s := d.lay.spans[e]
	return d.selectLine(min(s[0]+max(0, off), s[1]))
}

func (d *Doc) selectLine(line int) error {
	for i, l := range d.lay.rows {
		if l == line {
			return d.list.Select(d.rowID(i))
		}
	}
	return fmt.Errorf("line %d is not a row", line)
}

func (d *Doc) rowID(i int) string { return d.id + "/line/" + strconv.Itoa(i) }

// set takes a new text: its tasks, elements and rows (the cursor stays on
// its element by place, at the same line within it).
func (d *Doc) set(text string) error {
	e, off := d.cursor()
	d.text, d.tasks, d.elems = text, Tasks(text), Elements(text)
	if d.edit != nil {
		d.dirty = true
		return nil
	}
	return d.relayout(e, off)
}

// keepAt sets text and puts the cursor on the element holding byte at
// (or the nearest after it, else the last).
func (d *Doc) keepAt(text string, at int) error {
	if err := d.set(text); err != nil {
		return err
	}
	if e := d.elementAt(at); e >= 0 {
		_ = d.SelectElement(e)
	}
	return nil
}

// elementAt is the element holding byte at, else the first after it,
// else the last; -1 in a document without any.
func (d *Doc) elementAt(at int) int {
	for i, e := range d.elems {
		if at <= e.End && d.drawn(i) {
			return i
		}
	}
	for i := len(d.elems) - 1; i >= 0; i-- {
		if d.drawn(i) {
			return i
		}
	}
	return -1
}

func (d *Doc) drawn(e int) bool { return e < len(d.lay.spans) && d.lay.spans[e][0] >= 0 }

// folded reports whether item's notes are folded.
func (d *Doc) folded(item int) bool {
	return item >= 0 && item < len(d.elems) && d.folds[d.foldKey(item)]
}

func (d *Doc) foldKey(item int) string {
	e := d.elems[item]
	return strings.TrimSpace(d.text[e.Start:e.End])
}

// relayout draws the rows at the Doc's width and style and puts the
// cursor on element e at line off within it.
func (d *Doc) relayout(e, off int) error {
	old, keep := d.FocusedLink()
	occurrence := 0
	if keep {
		for i := 0; i < d.linkIndex; i++ {
			l := d.links[i]
			if l.Element == old.Element && l.URL == old.URL && l.Text == old.Text {
				occurrence++
			}
		}
	}
	d.dirty = false
	hidden, count := d.hiddenElements()
	d.lay = layoutOf(d.text, d.elems, d.width, d.style, d.opts, d.folded, hidden, d.r.Settings().Bool(ExtraSpaceSetting))
	d.links = layoutLinks(d.text, d.elems, d.lay)
	d.doneHidden = count
	if len(d.lay.rows) == 0 && (d.Editable() || count > 0) {
		// An empty document still has a line to stand on.
		d.lay.lines = append(d.lay.lines, "")
		d.lay.owner = append(d.lay.owner, -1)
		d.lay.rows = append(d.lay.rows, len(d.lay.lines)-1)
	}
	items := make([]ochazuke.ListItem, len(d.lay.rows))
	for i, line := range d.lay.rows {
		noun := LineNoun
		if el := d.lay.owner[line]; el >= 0 {
			switch d.elems[el].Kind {
			case KindTask:
				noun = TaskNoun
			case KindNote:
				noun = NoteNoun
			}
		}
		items[i] = ochazuke.ListItem{ID: d.rowID(i), Label: strings.TrimSpace(ansi.Strip(d.lay.lines[line])), Noun: noun, Data: line}
	}
	if err := d.list.SetItems(items); err != nil {
		return err
	}
	d.r.Instances[d.ListID()].Unfocusable = len(items) == 0
	if e >= len(d.elems) {
		e = len(d.elems) - 1
	}
	if e >= 0 && len(hidden) > e && hidden[e] {
		for next := e + 1; next < len(d.elems); next++ {
			if d.drawn(next) {
				return d.selectAt(next, 0)
			}
		}
	}
	for ; e >= 0; e-- {
		if d.drawn(e) {
			_ = d.selectAt(e, off)
			break
		}
		off = 0
	}
	if keep {
		for i, l := range d.links {
			if l.Element == old.Element && l.URL == old.URL && l.Text == old.Text {
				if occurrence == 0 {
					_ = d.selectLink(i, d.linkExplicit)
					break
				}
				occurrence--
			}
		}
	}
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

// Toggle flips the task item under the cursor: [ ] becomes [x], [x] or
// [X] becomes [ ], written at once; the item is found again first when
// the source changed (see Doc). The written toggle is one undo step.
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
	d.undo, d.redo = append(d.undo, step{toggle: &e}), nil
	return nil
}

// Undo takes back the last edit, written at once.
func (d *Doc) Undo() error { return d.history(&d.undo, &d.redo, false) }

// Redo writes the last undone edit again.
func (d *Doc) Redo() error { return d.history(&d.redo, &d.undo, true) }

func (d *Doc) history(from, to *[]step, redo bool) error {
	if err := d.Leave(); err != nil {
		return err
	}
	if len(*from) == 0 {
		if redo {
			return errors.New("nothing to redo")
		}
		return errors.New("nothing to undo")
	}
	s := (*from)[len(*from)-1]
	if s.toggle != nil {
		want := s.toggle.from
		if redo {
			want = s.toggle.to
		}
		if _, _, err := d.apply(s.toggle.text, s.toggle.nth, want); err != nil {
			return err
		}
	} else {
		old, new := s.old, s.new
		if redo {
			old, new = new, old
		}
		if err := d.splice(s.at, s.ctx, new, old); err != nil {
			return err
		}
	}
	*from = (*from)[:len(*from)-1]
	*to = append(*to, s)
	return nil
}

// splice replaces have (found at at, or again by ctx+have when the
// source moved) with want, writes it and puts the cursor there.
func (d *Doc) splice(at int, ctx, have, want string) error {
	if d.src.Save == nil {
		return errors.New("this markdown is read-only")
	}
	current, err := d.src.Load()
	if err != nil {
		return fmt.Errorf("not written: load markdown: %w", err)
	}
	if at > len(current) || current[at:min(len(current), at+len(have))] != have || current[max(0, at-len(ctx)):at] != ctx {
		at = locate(current, ctx, have)
		if at < 0 {
			_ = d.set(current)
			return refused("not written: the text changed in the source")
		}
	}
	next := current[:at] + want + current[at+len(have):]
	if err := d.src.Save(next); err != nil {
		return fmt.Errorf("not written: save markdown: %w", err)
	}
	return d.keepAt(next, at)
}

// locate is where have starts in text after ctx, when it does so once;
// -1 otherwise.
func locate(text, ctx, have string) int {
	probe := ctx + have
	if probe == "" {
		return -1
	}
	i := strings.Index(text, probe)
	if i < 0 || strings.Contains(text[i+1:], probe) {
		return -1
	}
	return i + len(ctx)
}

// diff is the one splice that turns a into b: at, the old and new text,
// and up to 32 bytes of the text before at.
func diff(a, b string) step {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return step{at: p, old: a[p : len(a)-s], new: b[p : len(b)-s], ctx: a[max(0, p-32):p]}
}

// write saves next as one undo step (the splice from the text the Doc
// holds) and puts the cursor on the element holding byte at.
func (d *Doc) write(next string, at int) error {
	if d.src.Save == nil {
		return errors.New("this markdown is read-only")
	}
	if next == d.text {
		return nil
	}
	s := diff(d.text, next)
	if err := d.src.Save(next); err != nil {
		return fmt.Errorf("not written: save markdown: %w", err)
	}
	d.undo, d.redo = append(d.undo, s), nil
	return d.keepAt(next, at)
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
		return 0, false, refused("not toggled: %q changed or is gone in the source", text)
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

// View draws the document width cells wide (see Block).
func (d *Doc) View(width, height int, a ochazuke.Appearance) string {
	return d.Block(width, height, a).Text
}

// Block draws the document width cells wide in the appearance's Glamour
// style (StyleOf), the cursor's line in reverse video while the Doc holds
// the focus (the one highlight: an unfocused Doc draws none), notes in
// boxes of their own. While an element is open its lines are the block
// editor, with the terminal cursor in it. With height > 0 a longer
// document is windowed to height lines, the last a position marker
// ("12–40 / 300"), scrolled to keep the cursor visible with the page's
// scrolloff; with height 0 every line is drawn and the page scrolls.
// Either way it places the line list in its page's drawing order and
// reports the cursor's lines (Runtime.Placed, PlacedAt), so focus jumps
// and the page follow it.
func (d *Doc) Block(width, height int, a ochazuke.Appearance) ochazuke.Block {
	id := d.ListID()
	d.r.Placed(id)
	width = max(1, width)
	if style, opts := StyleOf(a), d.opts.withTones(a); d.edit == nil && (width != d.width || style != d.style || opts != d.opts || d.dirty) {
		e, off := d.cursor()
		d.width, d.style, d.opts = width, style, opts
		_ = d.relayout(e, off)
	}
	indicator := d.doneIndicator(d.doneHidden)
	onlyIndicator := indicator != "" && height == 1
	if indicator != "" && height > 1 {
		height--
	}
	var lines []string
	focus, focusHeight := 0, 1
	var cursor *ochazuke.Pos
	if s := d.edit; s != nil {
		lay := s.lay
		lines = append(lines, lay.lines[:s.top]...)
		block := ochazuke.InputBlock(s.input, max(1, width-s.x), max(1, strings.Count(string(s.input.Text), "\n")+1))
		pad := strings.Repeat(" ", s.x)
		focus = len(lines)
		for _, l := range strings.Split(block.Text, "\n") {
			lines = append(lines, pad+l)
		}
		focusHeight = len(lines) - focus
		if block.Cursor != nil {
			cursor = &ochazuke.Pos{X: block.Cursor.X + s.x, Y: focus + block.Cursor.Y}
		}
		lines = append(lines, lay.lines[min(len(lay.lines), s.top+s.count):]...)
	} else {
		if link, ok := d.FocusedLink(); ok {
			_ = d.selectLink(d.linkIndex, d.linkExplicit)
			focus = link.Line
		}
		lines = append([]string(nil), d.lay.lines...)
		if item, ok := d.list.Selected(); ok {
			if line, _ := item.Data.(int); line >= 0 && line < len(lines) {
				focus = line
				top, bottom := d.cursorSpan(line)
				for l := top; l <= bottom; l++ {
					lines[l] = d.highlightLink(lines[l], l, width, a)
				}
			}
		}
	}
	d.visible = 0
	if height > 0 && len(lines) > height {
		visible := max(1, height-1)
		if height == 1 {
			visible = 1
		}
		d.visible = visible
		d.offset = scrollToShow(d.offset, focus, focus+max(1, focusHeight), visible, len(lines), d.scrolloff())
		end := min(len(lines), d.offset+visible)
		window := lines[d.offset:end]
		if height > 1 {
			window = append(window[:len(window):len(window)], fmt.Sprintf("%d–%d / %d", d.offset+1, end, len(lines)))
		}
		if cursor != nil {
			cursor.Y -= d.offset
			if cursor.Y < 0 || cursor.Y >= visible {
				cursor = nil
			}
		}
		focus -= d.offset
		if focus < 0 || focus >= visible {
			focus, focusHeight = 0, len(window)
		}
		lines = window
	}
	if indicator != "" {
		lines = append([]string{indicator}, lines...)
		focus++
		if cursor != nil {
			cursor.Y++
		}
		if onlyIndicator {
			lines, cursor, focus, focusHeight = lines[:1], nil, 0, 1
		}
	}
	out := ochazuke.Block{Text: d.r.PlacedAt(id, strings.Join(lines, "\n"), focus, focusHeight), Cursor: cursor}
	return out
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

// CanScrollLines reports whether the last frame windowed the document
// (more lines than its window) and no element is open (LineScroller, the
// framework's view.scroll on ctrl+e and ctrl+y).
func (d *Doc) CanScrollLines() bool {
	return d.edit == nil && d.visible > 0 && len(d.lay.lines) > d.visible
}

// ScrollLines moves the window by lines, clamped, as Vim's ctrl+e and
// ctrl+y do: the cursor stays unless it would leave the window's
// scrolloff margin, then moves to the nearest line it can rest on inside.
func (d *Doc) ScrollLines(lines int) {
	if !d.CanScrollLines() {
		return
	}
	total, visible := len(d.lay.lines), d.visible
	d.offset = min(max(0, d.offset+lines), total-visible)
	margin := min(d.scrolloff(), max(0, (visible-1)/2))
	lo, hi := d.offset+margin, d.offset+visible-1-margin
	if d.offset == 0 {
		lo = 0
	}
	if d.offset+visible >= total {
		hi = total - 1
	}
	at := d.cursorLine()
	if at >= lo && at <= hi {
		return
	}
	best := -1
	for _, line := range d.lay.rows {
		if line >= lo && line <= hi && (best < 0 || abs(line-at) < abs(best-at)) {
			best = line
		}
	}
	if best >= 0 {
		_ = d.selectLine(best)
	}
}

// cursorLine is the drawn line the cursor is on, or -1.
func (d *Doc) cursorLine() int {
	item, ok := d.list.Selected()
	if !ok {
		return -1
	}
	if line, ok := item.Data.(int); ok {
		return line
	}
	return -1
}

// halfPage applies the half-page rule (ochazuke.MotionScroll, UX-RULES
// R3.14) to a block motion that moved the cursor from line from: when
// showing the new line would scroll the window more than half its height,
// the window scrolls half toward it and the cursor stays in the window,
// on the row nearest the target, until the target shows (a long code
// block is read half a window per }).
func (d *Doc) halfPage(from int) {
	if _, ok := d.FocusedLink(); ok {
		return
	}
	to := d.cursorLine()
	if d.visible <= 0 || d.edit != nil || from < 0 || to < 0 || to == from {
		return
	}
	total, margin := len(d.lay.lines), d.scrolloff()
	want := scrollToShow(d.offset, to, to+1, d.visible, total, margin) - d.offset
	scroll := ochazuke.MotionScroll(want, d.visible)
	if scroll == want {
		return
	}
	off := min(max(0, d.offset+scroll), max(0, total-d.visible))
	lo, hi := off+margin, off+d.visible-1-margin
	if lo > hi {
		lo, hi = off, off+d.visible-1
	}
	d.offset = off
	if to >= lo && to <= hi {
		return
	}
	// The row between from and to nearest the target inside the window.
	best := -1
	for _, line := range d.lay.rows {
		if line < lo || line > hi || line < min(from, to) || line > max(from, to) {
			continue
		}
		if best < 0 || abs(line-to) < abs(best-to) {
			best = line
		}
	}
	if best < 0 {
		best = from
	}
	_ = d.selectLine(best)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
	selected := d.selected()
	e, off := d.cursor()
	out := map[string]any{"id": d.id, "tasks": d.Tasks(), "selected": selected, "undo": len(d.undo), "redo": len(d.redo), "element": e, "line": off, "editing": d.edit != nil, "done": d.Done(), "doneHidden": d.doneHidden}
	if link, ok := d.FocusedLink(); ok {
		out["link"] = link
	}
	if e >= 0 {
		out["kind"] = d.elems[e].Kind
		out["sourceLine"] = d.elems[e].Line
	}
	return out
}
