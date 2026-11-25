package table

import (
	"fmt"
	"slices"

	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
)

const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

var emptyCellAny any = ""

type fnv64a uint64

func newFNV64a() fnv64a { return fnv64a(fnvOffset64) }

func (h *fnv64a) writeByte(b byte) {
	v := uint64(*h)
	v ^= uint64(b)
	v *= fnvPrime64
	*h = fnv64a(v)
}

func (h *fnv64a) writeBool(b bool) {
	if b {
		h.writeByte(1)
		return
	}
	h.writeByte(0)
}

func (h *fnv64a) writeInt(n int) {
	v := uint64(*h)
	u := uint64(n)
	for i := 0; i < 8; i++ {
		v ^= u & 0xff
		v *= fnvPrime64
		u >>= 8
	}
	*h = fnv64a(v)
}

func (h *fnv64a) writeString(s string) {
	h.writeInt(len(s))
	v := uint64(*h)
	for i := 0; i < len(s); i++ {
		v ^= uint64(s[i])
		v *= fnvPrime64
	}
	*h = fnv64a(v)
}

func (h *fnv64a) writeStrings(ss []string) {
	h.writeInt(len(ss))
	for _, s := range ss {
		h.writeString(s)
	}
}

func (h *fnv64a) writeToneSpans(spans [][]ui.ToneSpan) {
	h.writeInt(len(spans))
	for _, colSpans := range spans {
		h.writeInt(len(colSpans))
		for _, sp := range colSpans {
			h.writeInt(sp.From)
			h.writeInt(sp.To)
			h.writeString(sp.Tone)
			h.writeString(sp.Bg)
		}
	}
}

func (h *fnv64a) writeInlineRun(r ui.InlineRun) {
	h.writeString(r.Kind)
	h.writeString(r.Text)
	h.writeString(r.Label)
	h.writeString(r.Tone)
	h.writeString(r.Bg)
	h.writeString(r.Href)
	if r.Ref != nil {
		h.writeByte(1)
		h.writeString(r.Ref.Kind)
		h.writeString(r.Ref.ID)
	} else {
		h.writeByte(0)
	}
	h.writeString(r.Command)
	h.writeInt(len(r.Args))
	if len(r.Args) > 0 {
		keys := make([]string, 0, len(r.Args))
		for k := range r.Args {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			h.writeString(k)
			h.writeString(Text(r.Args[k]))
		}
	}
}

func (h *fnv64a) writeCell(cell any) {
	switch x := cell.(type) {
	case string:
		h.writeByte(1)
		h.writeString(x)
	case []ui.InlineRun:
		h.writeByte(2)
		h.writeInt(len(x))
		for i := range x {
			h.writeInlineRun(x[i])
		}
	case ui.InlineRun:
		h.writeByte(3)
		h.writeInlineRun(x)
	case *ui.InlineRun:
		if x == nil {
			h.writeByte(0)
		} else {
			h.writeByte(3)
			h.writeInlineRun(*x)
		}
	default:
		if runs, ok := ui.DecodeInlineRuns(cell); ok {
			h.writeByte(2)
			h.writeInt(len(runs))
			for i := range runs {
				h.writeInlineRun(runs[i])
			}
		} else {
			h.writeByte(4)
			h.writeString(Text(cell))
		}
	}
}

func (h *fnv64a) writeCells(cells []any) {
	h.writeInt(len(cells))
	for _, c := range cells {
		h.writeCell(c)
	}
}

func (h *fnv64a) writeRow(row any) {
	switch x := row.(type) {
	case []any:
		h.writeByte(1)
		h.writeCells(x)
	case ui.TableRowSpec:
		h.writeByte(2)
		h.writeCells(x.Cells)
		h.writeString(x.Tone)
		h.writeStrings(x.CellTones)
		h.writeStrings(x.CellIcons)
		h.writeStrings(x.IconTones)
		h.writeToneSpans(x.CellSpans)
	case *ui.TableRowSpec:
		if x == nil {
			h.writeByte(0)
			return
		}
		h.writeByte(2)
		h.writeCells(x.Cells)
		h.writeString(x.Tone)
		h.writeStrings(x.CellTones)
		h.writeStrings(x.CellIcons)
		h.writeStrings(x.IconTones)
		h.writeToneSpans(x.CellSpans)
	}
}

// View renders the model as a ui.KindTable node.
func (m *Model) View(id string) ui.ViewNode {
	items := m.Items()
	m.clamp(len(items))
	sortKeys := m.effectiveSortKeys()
	h := newFNV64a()
	ncols := len(m.Columns)
	h.writeInt(ncols)
	cols := make([]any, ncols)
	colKeys := make([]string, ncols)
	for i, c := range m.Columns {
		colKeys[i] = c.Key
		title := c.Title
		if title == "" {
			title = c.Key
		}
		for idx, sk := range sortKeys {
			if c.Key == sk.Field && sk.Dir != SortNone {
				arrow := "▲"
				if sk.Dir == SortDesc {
					arrow = "▼"
				}
				if len(sortKeys) > 1 {
					title += fmt.Sprintf(" %s%d", arrow, idx+1)
				} else {
					title += " " + arrow
				}
				break
			}
		}
		h.writeString(c.Key)
		h.writeString(title)
		h.writeInt(c.Width)
		h.writeString(c.Align)
		col := map[string]any{"key": c.Key, "title": title}
		if c.Width > 0 {
			col["width"] = c.Width
		}
		if c.Align != "" {
			col["align"] = c.Align
		}
		cols[i] = col
	}

	rows := make([]any, len(items))
	var cellSlab []any
	if ncols > 0 && len(items) > 0 {
		cellSlab = make([]any, len(items)*ncols)
	}
	var slab strSlab
	hasMarks := len(m.Marked) > 0
	for i := range items {
		it := &items[i]
		var cells []any
		if ncols > 0 {
			base := i * ncols
			cells = cellSlab[base : base+ncols : base+ncols]
		} else {
			cells = make([]any, 0)
		}
		if it.Header {
			mark := "▾"
			if it.Collapsed {
				mark = "▸"
			}
			if ncols > 0 {
				cells[0] = fmt.Sprintf("%s %s (%d)", mark, it.Group, it.Count)
			}
			for j := 1; j < ncols; j++ {
				cells[j] = emptyCellAny
			}
			rows[i] = ui.TableRowSpec{Cells: cells, Tone: "table.group"}
			continue
		}
		rowCells := it.Row.Cells
		for j, key := range colKeys {
			raw := rowCells[key]
			if _, ok := raw.(string); ok {
				cells[j] = raw
			} else if raw == nil {
				cells[j] = emptyCellAny
			} else if _, ok := ui.DecodeInlineRuns(raw); ok {
				cells[j] = raw
			} else {
				cells[j] = Text(raw)
			}
		}
		var cellTones, cellIcons, iconTones []string
		var cellSpans [][]ui.ToneSpan
		var hasTones, hasIcons, hasIconTones, hasSpans bool
		if len(it.Row.CellTones) > 0 {
			cellTones, hasTones = projectCellMap(colKeys, it.Row.CellTones, &slab)
		}
		if len(it.Row.CellIcons) > 0 {
			cellIcons, hasIcons = projectCellMap(colKeys, it.Row.CellIcons, &slab)
		}
		if len(it.Row.IconTones) > 0 {
			iconTones, hasIconTones = projectCellMap(colKeys, it.Row.IconTones, &slab)
		}
		if len(it.Row.CellSpans) > 0 {
			cellSpans, hasSpans = projectCellSpans(colKeys, it.Row.CellSpans)
		}
		if ncols > 0 && hasMarks && m.Marked[it.Row.ID] {
			const markPrefix = "● "
			if runs, ok := ui.DecodeInlineRuns(cells[0]); ok {
				prefixed := make([]ui.InlineRun, 0, len(runs)+1)
				prefixed = append(prefixed, ui.InlineRun{Text: markPrefix})
				prefixed = append(prefixed, runs...)
				cells[0] = prefixed
			} else {
				cells[0] = markPrefix + Text(cells[0])
			}
			if hasSpans && len(cellSpans[0]) > 0 {
				shift := len([]rune(markPrefix))
				shifted := make([]ui.ToneSpan, len(cellSpans[0]))
				for si, sp := range cellSpans[0] {
					sp.From += shift
					if sp.To > 0 {
						sp.To += shift
					}
					shifted[si] = sp
				}
				cellSpans[0] = shifted
			}
		}
		if it.Row.Tone != "" || hasTones || hasIcons || hasIconTones || hasSpans {
			rows[i] = ui.TableRowSpec{
				Cells:     cells,
				Tone:      it.Row.Tone,
				CellTones: cellTones,
				CellIcons: cellIcons,
				IconTones: iconTones,
				CellSpans: cellSpans,
			}
		} else {
			rows[i] = cells
		}
	}
	props := map[string]any{"columns": cols, "rows": rows, "selected": m.SelectedIdx, "offset": m.Offset}
	vFrom, vTo, vOK := m.visualBounds(len(items))
	if vOK {
		props["visualFrom"] = vFrom
		props["visualTo"] = vTo
	}
	selCol := -1
	if m.CellSelect && ncols > 0 {
		selCol = max(0, min(m.SelectedCol, ncols-1))
		props["selectedCol"] = selCol
	}
	n := ui.ViewNode{
		ID: id, Kind: ui.KindTable, Focusable: true,
		Traits: []string{traits.TraitNav, traits.TraitScroll, traits.TraitFilter, traits.TraitSelect},
		Props:  props,
	}
	cf := m.contextFrameFor("", id, items)
	n.Scope, n.Ref, n.Facts = cf.Scope, cf.Subject, cf.Facts
	if r, ok := m.focusedRowFor(items); ok {
		n.Unread, n.Badges = r.Unread, r.Badges
	}

	rowCount, _ := n.Facts["rowCount"].(int)
	markedCount, _ := n.Facts["markedCount"].(int)
	focusedMarked, _ := n.Facts["marked"].(bool)
	isGroupHeader, _ := n.Facts["isGroupHeader"].(bool)

	h.writeInt(len(items))
	h.writeInt(rowCount)
	h.writeInt(m.SelectedIdx)
	h.writeInt(m.Offset)
	h.writeInt(m.PageSize)
	h.writeString(m.Query)
	h.writeString(m.FilterField)
	h.writeString(string(m.MatchMode))
	h.writeString(m.FilterErr)
	h.writeString(m.GroupField)
	h.writeString(m.SortField)
	h.writeInt(int(m.SortDir))
	h.writeInt(len(sortKeys))
	for _, sk := range sortKeys {
		h.writeString(sk.Field)
		h.writeInt(int(sk.Dir))
	}
	h.writeBool(isGroupHeader)
	h.writeBool(vOK)
	h.writeInt(vFrom)
	h.writeInt(vTo)
	h.writeInt(m.VisualAnchor)
	h.writeBool(m.CellSelect)
	h.writeInt(selCol)
	h.writeInt(markedCount)
	h.writeBool(focusedMarked)
	h.writeBool(n.Unread)
	h.writeInt(len(n.Badges))
	for _, b := range n.Badges {
		h.writeString(b.Text)
		h.writeString(b.Tone)
	}

	start, end := 0, len(rows)
	if len(rows) > 64 {
		start = max(0, m.Offset-2)
		end = min(len(rows), m.Offset+max(m.PageSize, 32)+4)
		if start > end {
			start = end
		}
	}
	h.writeInt(start)
	h.writeInt(end)
	for i := start; i < end; i++ {
		h.writeRow(rows[i])
		if hasMarks && !items[i].Header {
			h.writeBool(m.Marked[items[i].Row.ID])
		}
	}
	n.CacheKey = uint64(h) | 1
	return n
}

type strSlab struct {
	buf []string
}

func (s *strSlab) alloc(n int) []string {
	if len(s.buf) < n {
		s.buf = make([]string, max(n*64, 256))
	}
	out := s.buf[:n:n]
	s.buf = s.buf[n:]
	return out
}

func projectCellMap(colKeys []string, src map[string]string, slab *strSlab) ([]string, bool) {
	if len(src) == 0 {
		return nil, false
	}
	var out []string
	if len(src) < len(colKeys) {
		for k, v := range src {
			if v == "" {
				continue
			}
			for j, key := range colKeys {
				if key == k {
					if out == nil {
						out = slab.alloc(len(colKeys))
					}
					out[j] = v
				}
			}
		}
		return out, out != nil
	}
	for j, key := range colKeys {
		if v := src[key]; v != "" {
			if out == nil {
				out = slab.alloc(len(colKeys))
			}
			out[j] = v
		}
	}
	return out, out != nil
}

func projectCellSpans(colKeys []string, src map[string][]ui.ToneSpan) ([][]ui.ToneSpan, bool) {
	if len(src) == 0 {
		return nil, false
	}
	var out [][]ui.ToneSpan
	for j, key := range colKeys {
		if sp := src[key]; len(sp) > 0 {
			if out == nil {
				out = make([][]ui.ToneSpan, len(colKeys))
			}
			out[j] = slices.Clone(sp)
		}
	}
	return out, out != nil
}
