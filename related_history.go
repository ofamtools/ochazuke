package ochazuke

import (
	"fmt"
	"strings"
)

// The history section of a related stack (RelatedSpec.History, the
// relation-section component's last section): the history entries that
// changed or named the shown thing, newest first, a row each, read again
// whenever the history has a newer entry than the one it was filled at.

// historyRef is the history section's referrer.
func historyRef() Referrer {
	return Referrer{Noun: historyEntryNoun, Prop: "history", History: true, Label: "History"}
}

// ensureHistorySection adds the history section last when the spec asks
// for it.
func (rel *Related) ensureHistorySection() {
	ref := historyRef()
	if !rel.spec.History || rel.noun == "" || rel.byID[ref.ID()] != nil {
		return
	}
	rel.addSection(ref)
}

// syncHistory fills the history section again when the history grew.
func (rel *Related) syncHistory(keyChanged bool) {
	s := rel.byID[historyRef().ID()]
	if s == nil {
		return
	}
	newest := rel.r.history.newestSeq()
	if !keyChanged && newest == rel.histSeq {
		return
	}
	rel.histSeq = newest
	if !keyChanged {
		s.fill()
	}
}

// rowItems are the section's rows: the referring things, or for the
// history section an entry per row (when, the request, who).
func (s *relatedSection) rowItems(c Ctx, things []Thing) []ListItem {
	r := s.rel.r
	if !s.ref.History {
		return r.thingItems(c, s.n, s.list.id, things, s.list.columns)
	}
	items := make([]ListItem, 0, len(things))
	for _, t := range things {
		var e HistoryEntry
		if err := t.Into(&e); err != nil {
			continue
		}
		label := fmt.Sprintf("%s  %s", e.At.Local().Format("Jan 2 15:04"), r.DisplayKey(e.Request.Key()))
		if who := strings.TrimSpace(e.Context.Actor.String()); who != "" {
			label += "  " + who
		}
		item := ListItem{ID: fmt.Sprintf("%s/%d", s.list.id, e.Seq), Label: label, Noun: historyEntryNoun, Data: t}
		if n := len(s.list.columns); n > 0 {
			item.Cells = make([]string, n)
			item.Cells[0] = label
		}
		items = append(items, item)
	}
	return items
}

// newestSeq is the seq of the newest entry kept (0 when none).
func (h *actionHistory) newestSeq() uint64 {
	if n := len(h.entries); n > 0 {
		return h.entries[n-1].Seq
	}
	return 0
}
