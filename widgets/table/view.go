package table

import (
	"fmt"
	"hash/fnv"
	"slices"

	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
)

// View renders the model as a ui.KindTable node.
func (m *Model) View(id string) ui.ViewNode {
	items := m.Items()
	m.clamp(len(items))
	sortKeys := m.effectiveSortKeys()
	cols := make([]any, len(m.Columns))
	for i, c := range m.Columns {
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
	for i, it := range items {
		cells := make([]any, len(m.Columns))
		if it.Header {
			mark := "▾"
			if it.Collapsed {
				mark = "▸"
			}
			if len(cells) > 0 {
				cells[0] = fmt.Sprintf("%s %s (%d)", mark, it.Group, it.Count)
			}
			for j := 1; j < len(cells); j++ {
				cells[j] = ""
			}
			rows[i] = ui.TableRowSpec{Cells: cells, Tone: "table.group"}
			continue
		}
		for j, c := range m.Columns {
			raw := it.Row.Cells[c.Key]
			if _, ok := ui.DecodeInlineRuns(raw); ok {
				cells[j] = raw
			} else {
				cells[j] = Text(raw)
			}
		}
		cellTones, hasTones := projectCellMap(m.Columns, it.Row.CellTones)
		cellIcons, hasIcons := projectCellMap(m.Columns, it.Row.CellIcons)
		iconTones, hasIconTones := projectCellMap(m.Columns, it.Row.IconTones)
		cellSpans, hasSpans := projectCellSpans(m.Columns, it.Row.CellSpans)
		if len(cells) > 0 && m.Marked[it.Row.ID] {
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
	if m.CellSelect && len(m.Columns) > 0 {
		selCol = max(0, min(m.SelectedCol, len(m.Columns)-1))
		props["selectedCol"] = selCol
	}
	n := ui.ViewNode{
		ID: id, Kind: ui.KindTable, Focusable: true,
		Traits: []string{traits.TraitNav, traits.TraitScroll, traits.TraitFilter, traits.TraitSelect},
		Props:  props,
	}
	cf := m.ContextFrame("", id)
	n.Scope, n.Ref, n.Facts = cf.Scope, cf.Subject, cf.Facts
	if r, ok := m.FocusedRow(); ok {
		n.Unread, n.Badges = r.Unread, r.Badges
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%v|%v|%d|%d|%v|%t|%d|%d|%d", cols, rows, m.SelectedIdx, m.Offset, n.Facts["isGroupHeader"], vOK, vFrom, vTo, selCol)
	n.CacheKey = h.Sum64() | 1
	return n
}

func projectCellMap(cols []Column, src map[string]string) ([]string, bool) {
	if len(src) == 0 {
		return nil, false
	}
	var out []string
	for j, c := range cols {
		if v := src[c.Key]; v != "" {
			if out == nil {
				out = make([]string, len(cols))
			}
			out[j] = v
		}
	}
	return out, out != nil
}

func projectCellSpans(cols []Column, src map[string][]ui.ToneSpan) ([][]ui.ToneSpan, bool) {
	if len(src) == 0 {
		return nil, false
	}
	var out [][]ui.ToneSpan
	for j, c := range cols {
		if sp := src[c.Key]; len(sp) > 0 {
			if out == nil {
				out = make([][]ui.ToneSpan, len(cols))
			}
			out[j] = slices.Clone(sp)
		}
	}
	return out, out != nil
}
