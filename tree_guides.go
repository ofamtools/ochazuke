package ochazuke

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// TreeGuidesSetting keeps the application's per-tree Guides choice with
// "app" (the default), or hides every tree's branch lines with "off".
const TreeGuidesSetting = "tree.guides"

func (t *Tree) guidesEnabled() bool {
	return t.Guides && t.r.treeSetting(TreeGuidesSetting, "app", "app", "off") != "off"
}

func (t *Tree) indent(id string) string {
	depth := t.shownDepth(id)
	if !t.guidesEnabled() {
		return strings.Repeat("  ", depth)
	}
	chain := make([]string, depth)
	for i := depth - 1; i >= 0; i-- {
		chain[i], id = id, t.parentOf(id)
	}
	var out strings.Builder
	for i, node := range chain {
		last := t.guideLast[t.parentOf(node)] == node
		switch {
		case i == len(chain)-1 && last:
			out.WriteString("└─ ")
		case i == len(chain)-1:
			out.WriteString("├─ ")
		case last:
			out.WriteString("   ")
		default:
			out.WriteString("│  ")
		}
	}
	return out.String()
}

// guideRows decorates the rows after filtering, before windowing. Include
// ancestors of text matches when finding the last kept child, even when
// the text filter hides those ancestors. A hidden sibling never leaves a
// dangling vertical line. No guides are stored in the search cache.
func (t *Tree) guideRows() {
	if t.list == nil {
		return // AddList's initial empty SetItems precedes assigning t.list.
	}
	enabled := t.guidesEnabled()
	if !enabled && !t.guidesShown {
		return
	}
	t.guidesShown = enabled
	kept := make(map[string]bool, len(t.list.items))
	for _, item := range t.list.items {
		kept[item.ID] = true
	}
	t.guideLast = make(map[string]string)
	from, to := t.zoomRange()
	for i := to - 1; i >= from; i-- {
		n := t.nodes[i]
		if kept[n.ID] {
			kept[n.Parent] = true
			if _, exists := t.guideLast[n.Parent]; !exists {
				t.guideLast[n.Parent] = n.ID
			}
		}
	}
	decorate := func(items []ListItem) []ListItem {
		out := slices.Clone(items)
		for i, item := range out {
			at := t.index(item.ID)
			if at < 0 {
				continue
			}
			n := t.nodes[at]
			prefix := t.prefix(n)
			item.Label = prefix + n.Label
			item.Cells = slices.Clone(n.Cells)
			if len(item.Cells) > 0 {
				item.Cells[0] = prefix + item.Cells[0]
			}
			item.Tones = shiftTones(n.Tones, utf8.RuneCountInString(prefix))
			if depth := t.shownDepth(n.ID); enabled && depth > 0 {
				item.Tones = append(item.Tones, SpanTone(0, 0, depth*3, ToneTreeGuide))
			}
			out[i] = item
		}
		return out
	}
	t.list.all = decorate(t.list.all)
	t.list.items = decorate(t.list.items)
	t.list.widest = nil
}
