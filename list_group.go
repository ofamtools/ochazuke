package ochazuke

import (
	"fmt"
	"net/url"
)

// groupHeading counts matched source rows on the current page, before folds.
// A continued group's count is deliberately not a whole-store aggregate.
func (l *Collection) groupHeading(label string) string {
	if !l.grouped {
		return label // application-owned Section labels are unchanged
	}
	if l.pages > 0 {
		return fmt.Sprintf("%s (%d on page)", label, l.groupCounts[label])
	}
	return fmt.Sprintf("%s (%d)", label, l.groupCounts[label])
}

// foldGroups projects a grouped page onto its visible rows. Expanded groups
// keep their existing nonselectable headings. A closed group has one collection
// row, not a record row: it stays navigable without posing as a hidden thing.
func (l *Collection) foldGroups(items []ListItem, keep string) ([]ListItem, string) {
	oldHeads := l.groupHeads
	keptGroup, keptHead := oldHeads[keep]
	l.groupHeads = nil
	l.groupCounts = nil
	if l.grouped {
		l.groupCounts = make(map[string]int)
		for _, item := range items {
			l.groupCounts[item.Section]++
			if keptHead && item.Section == keptGroup {
				keep, keptHead = item.ID, false
			}
		}
	}
	out := items
	if l.grouped && len(l.groupFolds) > 0 {
		out = make([]ListItem, 0, len(items))
		for lo := 0; lo < len(items); {
			label := items[lo].Section
			hi := lo + 1
			for hi < len(items) && items[hi].Section == label {
				hi++
			}
			if l.groupFolds[label] {
				head := l.foldedHeading(label)
				out = append(out, head)
				for _, item := range items[lo:hi] {
					if item.ID == keep {
						keep = head.ID
					}
				}
			} else {
				out = append(out, items[lo:hi]...)
			}
			lo = hi
		}
	}
	var gone []string
	for id := range oldHeads {
		if _, stays := l.groupHeads[id]; !stays {
			gone = append(gone, id)
		}
	}
	l.r.removeInstances(gone)
	return out, keep
}

func (l *Collection) foldedHeading(label string) ListItem {
	id := l.id + "/@group:" + url.PathEscape(label)
	row := ListItem{ID: id, Noun: "ochazuke.collection", Label: "▸ " + l.groupHeading(label)}
	if l.groupHeads == nil {
		l.groupHeads = make(map[string]string)
	}
	l.groupHeads[id] = label
	l.r.rows[id] = l.id
	inst := l.r.Instances[id]
	if inst == nil {
		inst = l.r.Instance(id, Parent(l.id), Kind(row.Noun))
	}
	inst.Label = row.Label
	return row
}

func (l *Collection) foldGroup(a Arguments) error {
	item, _ := l.Selected()
	label := item.Section
	if group, ok := l.groupHeads[item.ID]; ok {
		label = group
	}
	label = StringArg(a, "group", label)
	if _, ok := l.groupCounts[label]; !ok {
		return fmt.Errorf("no group %q on this page", label)
	}
	if l.groupFolds == nil {
		l.groupFolds = make(map[string]bool)
	}
	closed := !l.groupFolds[label]
	if value, ok := a["closed"].(bool); ok {
		closed = value
	}
	if closed {
		l.groupFolds[label] = true
	} else {
		delete(l.groupFolds, label)
	}
	l.rearranged(item.ID)
	return nil
}

// Explicit selection reveals a folded record already held on this page.
func (l *Collection) revealGroupRow(id string) bool {
	for i, item := range l.all {
		if item.ID != id {
			continue
		}
		label := item.Section
		if l.paged == nil {
			label = groupLabel(l.queryRows(l.all), i, l.effectiveView().GroupBy)
		}
		if !l.groupFolds[label] {
			return false
		}
		delete(l.groupFolds, label)
		l.rearranged(id)
		return true
	}
	return false
}
