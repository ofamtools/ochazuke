package ochazuke

import (
	"fmt"
	"slices"
)

// A tree whose rows are other nouns' things (an outline of tasks under
// projects) lets the rows' own verbs act on them: x on a task row is
// task.toggle, not an outline verb of its own. Tree.RecordWrites keeps
// such a write on the outline's undo stack, so u in the outline takes it
// back in order with the outline's own edits, as RecordRequest does for
// a request the application runs itself: every write a person's request
// makes to a thing a row stands for is one step, undone through its
// history entries (history-entry.undo), redone by running their requests
// again. Writes the tree makes itself (OnChange, u, ctrl+r) and writes
// from an agent or a script are not recorded.

// treeThing is the text a thing is known by in Tree.things.
func treeThing(noun, key string) string { return noun + "\x00" + key }

// nodeThing is the noun and key of the thing a node stands for ("" key:
// none): its noun's key prop in its Data, then its Props, else a Data that
// is the key itself, as rowKey reads a row.
func (t *Tree) nodeThing(n TreeNode) (string, string) {
	noun := n.Noun
	if noun == "" {
		noun = t.RowNoun
	}
	noun = t.r.CanonicalNoun(noun)
	kp := t.r.Nouns[noun].keyProp()
	if kp == nil {
		return noun, ""
	}
	for _, v := range []any{dataField(n.Data, kp.Name), dataField(map[string]any(n.Props), kp.Name)} {
		if !emptyProp(v) {
			return noun, fmt.Sprint(v)
		}
	}
	if s, ok := n.Data.(string); ok {
		return noun, s
	}
	return noun, ""
}

// noteThings adds the things nodes stand for to the ones the tree has
// shown, so a write to a row's thing still counts after the write hid it
// (an archive takes the row out of the outline).
func (t *Tree) noteThings(nodes []TreeNode) {
	if !t.RecordWrites {
		return
	}
	if t.things == nil {
		t.things = map[string]bool{}
	}
	for _, n := range nodes {
		if noun, key := t.nodeThing(n); key != "" {
			t.things[treeThing(noun, key)] = true
		}
	}
}

// standsFor reports whether a change is to a thing one of the tree's rows
// stands for, or stood for while it was open.
func (t *Tree) standsFor(c ThingChange) bool {
	if c.Key == "" || c.Relation != nil {
		return false
	}
	t.noteThings(t.nodes)
	return t.things[treeThing(t.r.CanonicalNoun(c.Noun), c.Key)]
}

// pendingWrites is the step a request is making on the tree: the history
// entries of writes to its rows' things, oldest first.
type pendingWrites struct {
	tree    *Tree
	entries []HistoryEntry
}

// recordingTrees are the open trees that record writes to their rows'
// things (RecordWrites), in registration order.
func (r *Runtime) recordingTrees() []*Tree {
	var out []*Tree
	for _, id := range r.holders {
		if t := componentAt[*Tree](r, id); t != nil && t.RecordWrites && t.Editable() {
			out = append(out, t)
		}
	}
	return out
}

// noteWrite takes a history entry a person's request recorded: a write to
// a thing one of the tree's rows stands for joins the tree's step for the
// request (flushTreeWrites).
func (t *Tree) noteWrite(e HistoryEntry) bool {
	if t.replaying > 0 || len(e.Changes) == 0 || e.Restored {
		return false
	}
	if !slices.ContainsFunc(e.Changes, t.standsFor) {
		return false
	}
	t.pending = append(t.pending, e)
	return true
}

// noteTreeWrite hands a recorded entry to the recording trees while a
// person's request runs.
func (r *Runtime) noteTreeWrite(e HistoryEntry) {
	if !personSource(e.Context.Source) {
		return
	}
	for _, t := range r.recordingTrees() {
		t.noteWrite(e)
	}
}

// flushTreeWrites ends a top-level request: each recording tree that
// noted writes puts them on its undo stack as one step.
func (r *Runtime) flushTreeWrites() {
	for _, t := range r.recordingTrees() {
		if len(t.pending) == 0 {
			continue
		}
		entries := t.pending
		t.pending = nil
		t.recordEntries(entries)
	}
}

// recordEntries is the undo step of entries (oldest first): undo reverts
// each group's newest entry, newest first (undoing one entry of a group
// reverts the group); redo runs their requests again, oldest first, and
// keeps the entries those make for the next undo.
func (t *Tree) recordEntries(entries []HistoryEntry) {
	r := t.r
	seqsOf := func(entries []HistoryEntry) []uint64 {
		var out []uint64
		groups := map[string]bool{}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if e.Group != "" && groups[e.Group] {
				continue
			}
			groups[e.Group] = e.Group != ""
			out = append(out, e.Seq)
		}
		return out
	}
	requests := make([]Request, len(entries))
	for i, e := range entries {
		requests[i] = e.Request
	}
	seqs := seqsOf(entries)
	reload := func() error {
		if t.Reload == nil {
			return nil
		}
		return t.Reload()
	}
	undo := func() error {
		for _, seq := range seqs {
			if err := r.undoHistory(seq); err != nil {
				return err
			}
		}
		return reload()
	}
	redo := func() error {
		mark := lastSeq(r)
		for _, q := range requests {
			if _, err := r.Execute(q); err != nil {
				return err
			}
		}
		seqs = undoableSince(r, mark)
		return reload()
	}
	selected := ""
	if n, ok := t.Selected(); ok {
		selected = n.ID
	}
	_ = t.Record(TreeAction{ID: selected, Label: requests[len(requests)-1].Key(), Undo: undo, Redo: redo})
}

// onChange hands a change to OnChange; the writes the application makes
// for it are the tree's own edit, not a step RecordWrites adds.
func (t *Tree) onChange(c TreeChange) error {
	t.replaying++
	defer func() { t.replaying-- }()
	return t.OnChange(c)
}
