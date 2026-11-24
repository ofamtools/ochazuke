// Package diff is a pure, format-agnostic diff viewer widget: one data model
// (Document, Section, Hunk, Line) shared by git diffs, change-set diffs and
// JSON path diffs, three builders (ParseUnified, FromStrings, FromJSON), and a
// stateful Model that handles motions, hunk jumps, section collapse and yank
// and renders to a ui.KindPanel holding one ui.KindText node, in unified or
// aligned two-column side-by-side layout.
//
// It performs no I/O and imports only the standard library, core and ui.
package diff

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
)

// Command IDs handled by Model.HandleCommand besides the standard motions.
const (
	CmdModeToggle    = "diff.mode.toggle"
	CmdSectionToggle = "diff.section.toggle"
	CmdYank          = "diff.yank"
)

// Specs returns the specs of the diff-specific commands. The standard motions
// come from core.MotionSpecs.
func Specs() []core.CommandSpec {
	return []core.CommandSpec{
		{ID: CmdModeToggle, Description: "Toggle unified and side-by-side diff layout", Tags: []string{"diff"},
			Params: []core.ParamSpec{{Name: "mode", Type: core.ParamString, Enum: []string{string(ModeUnified), string(ModeSideBySide)}, Description: "set a layout instead of toggling"}}},
		{ID: CmdSectionToggle, Description: "Collapse or expand the section under the cursor", Tags: []string{"diff"}},
		{ID: CmdYank, Description: "Copy the focused line, hunk or section text", Tags: []string{"diff"},
			Params: []core.ParamSpec{{Name: "scope", Type: core.ParamString, Enum: []string{"line", "hunk", "section"}, Description: "what to copy (default: the focused row)"}}},
	}
}

// LineKind says how a line changed.
type LineKind int

// Line kinds.
const (
	LineContext LineKind = iota
	LineAdded
	LineRemoved
)

func (k LineKind) String() string {
	switch k {
	case LineAdded:
		return "added"
	case LineRemoved:
		return "removed"
	}
	return "context"
}

func (k LineKind) sign() string {
	switch k {
	case LineAdded:
		return "+"
	case LineRemoved:
		return "-"
	}
	return " "
}

// Line is one diff line. OldNum or NewNum is 0 on the side the line is not on.
type Line struct {
	Kind           LineKind
	OldNum, NewNum int
	Text           string
}

// Hunk is a run of lines with its old and new ranges.
type Hunk struct {
	Header                                 string
	OldStart, OldCount, NewStart, NewCount int
	Lines                                  []Line
}

// Section groups the hunks of one file, change-set entry or JSON path.
type Section struct {
	ID, Title, OldLabel, NewLabel string
	Collapsed                     bool
	Hunks                         []Hunk
}

// Document is a titled list of sections.
type Document struct {
	Title    string
	Sections []Section
}

// Mode is the diff layout.
type Mode string

// Layouts.
const (
	ModeUnified    Mode = "unified"
	ModeSideBySide Mode = "side-by-side"
)

// RowKind says what a visible row is.
type RowKind int

// Row kinds.
const (
	RowSection RowKind = iota
	RowHunk
	RowLine
)

// Row is one visible row. For RowLine, Old and New index Hunk.Lines on the
// left and right side (-1 for none): a context row has both set to the same
// index, an unpaired removal has only Old, an unpaired addition only New, and
// in side-by-side a paired change has both. Hunk is -1 on a section row.
type Row struct {
	Kind     RowKind
	Section  int
	Hunk     int
	Old, New int
}

// Model is the diff viewer state. Use pointer methods to mutate. Collapsing a
// section writes Doc.Sections[i].Collapsed, so copy the sections slice first
// if the caller keeps sharing it.
type Model struct {
	Doc       Document
	Mode      Mode
	Cursor    int // flat visible row index
	Offset    int // first visible row
	PageSize  int // rows per page (default 20 when <= 0)
	ScrollOff int // vertical context rows kept around cursor
	Width     int // text columns inside the panel for side-by-side layout; set it to the panel's inner text width (default 80 when <= 0)

	// Yanked holds the text of the last diff.yank command.
	Yanked string
}

// New returns a unified-mode model with defaults.
func New(doc Document) *Model {
	return &Model{Doc: doc, Mode: ModeUnified, PageSize: 20, Width: 80}
}

func (m *Model) pageSize() int {
	if m.PageSize <= 0 {
		return 20
	}
	return m.PageSize
}

func (m *Model) width() int {
	if m.Width <= 0 {
		return 80
	}
	return m.Width
}

// Rows returns the visible rows in the current mode: section headers, hunk
// headers of expanded sections, and their lines (paired in side-by-side).
func (m *Model) Rows() []Row {
	var rows []Row
	for si, s := range m.Doc.Sections {
		rows = append(rows, Row{Kind: RowSection, Section: si, Hunk: -1, Old: -1, New: -1})
		if s.Collapsed {
			continue
		}
		for hi, h := range s.Hunks {
			rows = append(rows, Row{Kind: RowHunk, Section: si, Hunk: hi, Old: -1, New: -1})
			if m.Mode != ModeSideBySide {
				for li := range h.Lines {
					r := Row{Kind: RowLine, Section: si, Hunk: hi, Old: li, New: li}
					switch h.Lines[li].Kind {
					case LineAdded:
						r.Old = -1
					case LineRemoved:
						r.New = -1
					}
					rows = append(rows, r)
				}
				continue
			}
			for li := 0; li < len(h.Lines); {
				if h.Lines[li].Kind == LineContext {
					rows = append(rows, Row{Kind: RowLine, Section: si, Hunk: hi, Old: li, New: li})
					li++
					continue
				}
				var rem, add []int
				for li < len(h.Lines) && h.Lines[li].Kind == LineRemoved {
					rem = append(rem, li)
					li++
				}
				for li < len(h.Lines) && h.Lines[li].Kind == LineAdded {
					add = append(add, li)
					li++
				}
				for k := 0; k < max(len(rem), len(add)); k++ {
					r := Row{Kind: RowLine, Section: si, Hunk: hi, Old: -1, New: -1}
					if k < len(rem) {
						r.Old = rem[k]
					}
					if k < len(add) {
						r.New = add[k]
					}
					rows = append(rows, r)
				}
			}
		}
	}
	return rows
}

func (m *Model) viewportState(n int) traits.ViewportState {
	return traits.ViewportState{Cursor: m.Cursor, Offset: m.Offset, Total: n, Height: m.pageSize()}
}

func (m *Model) viewportConfig() traits.ViewportConfig {
	return traits.ViewportConfig{ScrollOff: m.ScrollOff, Mode: traits.ViewportModeCursor}
}

func (m *Model) clamp(n int) {
	st := traits.ViewportClamp(m.viewportState(n), m.viewportConfig())
	m.Cursor, m.Offset = st.Cursor, st.Offset
}

// Focused returns the row under the cursor.
func (m *Model) Focused() (Row, bool) {
	rows := m.Rows()
	m.clamp(len(rows))
	if len(rows) == 0 {
		return Row{}, false
	}
	return rows[m.Cursor], true
}

// HandleCommand applies a motion or diff command; it reports whether the
// command was handled. diff.yank stores its text in Model.Yanked.
func (m *Model) HandleCommand(cmd core.Command) bool {
	rows := m.Rows()
	m.clamp(len(rows))
	if next, _, handled := traits.ViewportStep(m.viewportState(len(rows)), m.viewportConfig(), cmd); handled {
		m.Cursor, m.Offset = next.Cursor, next.Offset
		return true
	}
	n := cmd.Times()
	last := len(rows) - 1
	move := func(to int) { m.Cursor = max(0, min(to, last)) }
	switch cmd.ID {
	case "motion.block.next", "motion.block.prev":
		next := cmd.ID == "motion.block.next"
		for ; n > 0; n-- {
			to := m.Cursor
			if next {
				for i := m.Cursor + 1; i <= last; i++ {
					if rows[i].Kind == RowHunk {
						to = i
						break
					}
				}
			} else {
				cur := m.Cursor
				if cur >= 0 && cur <= last && rows[cur].Kind == RowHunk {
					cur--
				}
				for i := cur; i >= 0; i-- {
					if rows[i].Kind == RowHunk {
						to = i
						break
					}
				}
			}
			move(to)
		}
	case CmdModeToggle:
		m.setMode(cmd.Arg("mode"))
	case CmdSectionToggle:
		m.applyFold(traits.FoldCmdToggle)
	case traits.FoldCmdToggle, traits.FoldCmdOpen, traits.FoldCmdClose, traits.FoldCmdOpenAll, traits.FoldCmdCloseAll:
		m.applyFold(cmd.ID)
	case CmdYank:
		m.Yanked = m.Yank(cmd.Arg("scope"))
	default:
		return false
	}
	m.clamp(len(m.Rows()))
	return true
}

// setMode switches to want (unified or side-by-side), or toggles when want is
// empty, keeping the cursor on the same source line.
func (m *Model) setMode(want string) {
	row, ok := m.Focused()
	next := ModeSideBySide
	if m.Mode == ModeSideBySide {
		next = ModeUnified
	}
	switch Mode(want) {
	case ModeUnified, ModeSideBySide:
		next = Mode(want)
	}
	m.Mode = next
	rows := m.Rows()
	if !ok {
		m.clamp(len(rows))
		return
	}
	idx := row.Old
	if idx < 0 {
		idx = row.New
	}
	best := -1
	for i, r := range rows {
		if r.Section != row.Section {
			continue
		}
		switch {
		case row.Kind == RowSection && r.Kind == RowSection:
			best = i
		case row.Kind == RowHunk && r.Kind == RowHunk && r.Hunk == row.Hunk:
			best = i
		case row.Kind == RowLine && r.Kind == RowLine && r.Hunk == row.Hunk && (r.Old == idx || r.New == idx):
			best = i
		}
		if best == i {
			break
		}
	}
	if best >= 0 {
		m.Cursor = best
	}
	m.clamp(len(rows))
}

func (m *Model) foldState() (traits.FoldState, []string) {
	collapsed := make(map[string]bool)
	all := make([]string, len(m.Doc.Sections))
	for i, s := range m.Doc.Sections {
		k := strconv.Itoa(i)
		all[i] = k
		if s.Collapsed {
			collapsed[k] = true
		}
	}
	return traits.FoldState{Collapsed: collapsed}, all
}

// applyFold delegates section collapse/expand commands to traits.FoldHandle and
// snaps the cursor to the target section header when needed.
func (m *Model) applyFold(cmdID string) bool {
	row, ok := m.Focused()
	st, all := m.foldState()
	var target traits.FoldTarget
	if ok {
		secKey := strconv.Itoa(row.Section)
		if row.Kind == RowSection {
			target = traits.FoldTarget{ID: secKey, HasChildren: true}
		} else {
			target = traits.FoldTarget{ParentID: secKey}
		}
	}
	next, focusID, _, handled := traits.FoldHandle(
		st,
		traits.FoldConfig{LeafFoldsParent: true},
		target, all, core.Command{ID: cmdID},
	)
	if !handled {
		return false
	}
	for i := range m.Doc.Sections {
		m.Doc.Sections[i].Collapsed = next.Collapsed[strconv.Itoa(i)]
	}
	snapSec := -1
	if focusID != "" {
		snapSec, _ = strconv.Atoi(focusID)
	} else if ok && (cmdID == traits.FoldCmdToggle || cmdID == traits.FoldCmdCloseAll) {
		snapSec = row.Section
	}
	if snapSec >= 0 {
		for i, r := range m.Rows() {
			if r.Kind == RowSection && r.Section == snapSec {
				m.Cursor = i
				break
			}
		}
	}
	return true
}

// Yank returns the text of the focused row: scope "line", "hunk" or
// "section", or "" for the row's own level (a line row yields its line, a
// hunk header its hunk, a section header its section).
func (m *Model) Yank(scope string) string {
	row, ok := m.Focused()
	if !ok {
		return ""
	}
	s := m.Doc.Sections[row.Section]
	if scope == "" {
		switch row.Kind {
		case RowSection:
			scope = "section"
		case RowHunk:
			scope = "hunk"
		default:
			scope = "line"
		}
	}
	switch scope {
	case "section":
		return sectionText(s)
	case "hunk":
		if row.Hunk >= 0 {
			return hunkText(s.Hunks[row.Hunk])
		}
		return sectionText(s)
	}
	if row.Kind != RowLine {
		if row.Hunk >= 0 {
			return hunkText(s.Hunks[row.Hunk])
		}
		return sectionText(s)
	}
	h := s.Hunks[row.Hunk]
	switch {
	case row.Old >= 0 && row.New >= 0 && row.Old != row.New:
		return h.Lines[row.Old].signed() + "\n" + h.Lines[row.New].signed()
	case row.Old >= 0:
		return h.Lines[row.Old].Text
	}
	return h.Lines[row.New].Text
}

func (l Line) signed() string { return l.Kind.sign() + l.Text }

func hunkText(h Hunk) string {
	out := []string{hunkHeader(h)}
	for _, l := range h.Lines {
		out = append(out, l.signed())
	}
	return strings.Join(out, "\n")
}

func sectionText(s Section) string {
	var out []string
	if s.OldLabel != "" || s.NewLabel != "" {
		out = append(out, "--- "+s.OldLabel, "+++ "+s.NewLabel)
	}
	for _, h := range s.Hunks {
		out = append(out, hunkText(h))
	}
	return strings.Join(out, "\n")
}

func hunkHeader(h Hunk) string {
	if h.Header != "" {
		return h.Header
	}
	return fmt.Sprintf("@@ -%s +%s @@", formatRange(h.OldStart, h.OldCount), formatRange(h.NewStart, h.NewCount))
}

func formatRange(start, count int) string {
	if count == 1 {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

// Stats counts added and removed lines in a section.
func (s Section) Stats() (added, removed int) {
	for _, h := range s.Hunks {
		for _, l := range h.Lines {
			switch l.Kind {
			case LineAdded:
				added++
			case LineRemoved:
				removed++
			}
		}
	}
	return
}

// --- rendering ---------------------------------------------------------------

type builder struct {
	sb    strings.Builder
	n     int // runes written
	spans []ui.ToneSpan
}

func (b *builder) write(s, tone string) {
	if s == "" {
		return
	}
	from := b.n
	b.sb.WriteString(s)
	b.n += utf8.RuneCountInString(s)
	if tone != "" {
		b.spans = append(b.spans, ui.ToneSpan{From: from, To: b.n, Tone: tone})
	}
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\n", " ")
}

func num(n int) string {
	if n <= 0 {
		return "    "
	}
	return fmt.Sprintf("%4d", n)
}

// fit truncates or pads s to exactly w runes.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	rs := []rune(clean(s))
	if len(rs) > w {
		return string(rs[:w-1]) + "…"
	}
	return string(rs) + strings.Repeat(" ", w-len(rs))
}

func toneOf(k LineKind) string {
	switch k {
	case LineAdded:
		return "success"
	case LineRemoved:
		return "error"
	}
	return ""
}

func (m *Model) writeRow(b *builder, r Row, cursor bool, colW int) {
	if cursor {
		b.write("▌ ", "accent")
	} else {
		b.write("  ", "")
	}
	s := m.Doc.Sections[r.Section]
	switch r.Kind {
	case RowSection:
		mark := "▾"
		if s.Collapsed {
			mark = "▸"
		}
		title := s.Title
		if title == "" {
			title = s.ID
		}
		b.write(mark+" "+clean(title), "accent")
		add, rem := s.Stats()
		if add > 0 {
			b.write(fmt.Sprintf(" +%d", add), "success")
		}
		if rem > 0 {
			b.write(fmt.Sprintf(" -%d", rem), "error")
		}
		if s.OldLabel != "" && s.NewLabel != "" && s.OldLabel != s.NewLabel {
			b.write("  "+clean(s.OldLabel)+" → "+clean(s.NewLabel), "muted")
		}
	case RowHunk:
		b.write("  "+clean(hunkHeader(s.Hunks[r.Hunk])), "muted")
	default:
		h := s.Hunks[r.Hunk]
		if m.Mode == ModeSideBySide {
			m.writeSide(b, h, r, colW)
			return
		}
		l := h.Lines[max(r.Old, r.New)]
		if r.Old >= 0 {
			l = h.Lines[r.Old]
		}
		b.write("  "+num(l.OldNum)+" "+num(l.NewNum)+" ", "muted")
		b.write(l.Kind.sign()+clean(l.Text), toneOf(l.Kind))
	}
}

func (m *Model) writeSide(b *builder, h Hunk, r Row, colW int) {
	b.write("  ", "")
	cell := func(idx int, left bool) {
		if idx < 0 {
			b.write(strings.Repeat(" ", colW), "")
			return
		}
		l := h.Lines[idx]
		n, sign := l.OldNum, " "
		if !left {
			n = l.NewNum
		}
		if l.Kind != LineContext {
			sign = l.Kind.sign()
		}
		b.write(num(n)+" "+sign, "muted")
		b.write(fit(l.Text, colW-6), toneOf(l.Kind))
	}
	cell(r.Old, true)
	b.write(" │ ", "muted")
	cell(r.New, false)
}

// View renders the model as a ui.KindPanel holding one ui.KindText node.
func (m *Model) View(id string) ui.ViewNode {
	rows := m.Rows()
	m.clamp(len(rows))
	ps := m.pageSize()
	colW := max(12, (m.width()-2-3)/2)

	var b builder
	if len(rows) == 0 {
		b.write("(no changes)", "muted")
	}
	end := min(len(rows), m.Offset+ps)
	for i := m.Offset; i < end; i++ {
		if i > m.Offset {
			b.write("\n", "")
		}
		m.writeRow(&b, rows[i], i == m.Cursor, colW)
	}
	text := b.sb.String()

	title := m.Doc.Title
	if title == "" {
		title = "diff"
	}
	n := ui.ViewNode{
		ID: id, Kind: ui.KindPanel, Scope: "diff", Traits: []string{traits.TraitScroll}, Focusable: true, Border: true,
		Title:  title,
		Badges: []ui.Badge{{Text: string(m.mode()), Tone: "muted"}},
		Children: []ui.ViewNode{{
			ID: id + "/text", Kind: ui.KindText,
			Props: map[string]any{"text": text, "spans": b.spans},
		}},
	}

	facts := traits.MergeFacts(traits.ViewportFacts(m.viewportState(len(rows)), m.viewportConfig()), map[string]any{
		"mode":         string(m.mode()),
		"cursor":       m.Cursor,
		"offset":       m.Offset,
		"rowCount":     len(rows),
		"sectionCount": len(m.Doc.Sections),
		"hunkCount":    m.hunkCount(),
		"hasChanges":   len(m.Doc.Sections) > 0,
		"rowKind":      "",
	})
	if len(rows) > 0 {
		r := rows[m.Cursor]
		s := m.Doc.Sections[r.Section]
		secKey := strconv.Itoa(r.Section)
		target := traits.FoldTarget{ParentID: secKey}
		if r.Kind == RowSection {
			target = traits.FoldTarget{ID: secKey, HasChildren: true}
		}
		fst, _ := m.foldState()
		facts = traits.MergeFacts(facts, traits.FoldFacts(fst, target))
		facts["rowKind"] = [...]string{"section", "hunk", "line"}[r.Kind]
		facts["sectionIndex"] = r.Section
		facts["sectionId"] = s.ID
		facts["collapsed"] = s.Collapsed
		facts["hunkIndex"] = r.Hunk
		if r.Kind == RowLine {
			idx := r.Old
			if idx < 0 {
				idx = r.New
			}
			facts["lineKind"] = s.Hunks[r.Hunk].Lines[idx].Kind.String()
		}
		if s.ID != "" {
			n.Ref = &core.Ref{Kind: "diff.section", ID: s.ID}
		}
	}
	n.Facts = facts

	h := fnv.New64a()
	fmt.Fprintf(h, "%s|%v|%s|%d|%d", text, b.spans, title, m.Cursor, m.Offset)
	n.CacheKey = h.Sum64() | 1
	return n
}

func (m *Model) mode() Mode {
	if m.Mode == ModeSideBySide {
		return ModeSideBySide
	}
	return ModeUnified
}

func (m *Model) hunkCount() int {
	c := 0
	for _, s := range m.Doc.Sections {
		c += len(s.Hunks)
	}
	return c
}
