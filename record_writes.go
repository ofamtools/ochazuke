package ochazuke

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const recordWritesID = "__record-writes"

// recordWrites keeps a thing's writers alive and ordered after their views close.
// Only editors with the same explicit WriteKey share a queue.
type recordWrites struct {
	queues map[string][]*RecordEditor
}

func (*recordWrites) HandleMessage(tea.Msg) bool { return false }
func (*recordWrites) PendingCommand() tea.Cmd    { return nil }
func (*recordWrites) Close()                     {}

func (r *Runtime) recordWrites() *recordWrites {
	if inst := r.Instances[recordWritesID]; inst != nil {
		return inst.Component.(*recordWrites)
	}
	s := &recordWrites{queues: map[string][]*RecordEditor{}}
	r.Instance(recordWritesID, Parent("app")).Component = s
	return s
}

func (e *RecordEditor) claimWrite() bool {
	key := e.spec.WriteKey
	if key == "" {
		return true
	}
	s := e.r.recordWrites()
	q := s.queues[key]
	if !slices.Contains(q, e) {
		q = append(q, e)
		s.queues[key] = q
	}
	return q[0] == e
}

func (e *RecordEditor) releaseWrite() {
	key := e.spec.WriteKey
	if key == "" {
		return
	}
	s := e.r.recordWrites()
	q := s.queues[key]
	if len(q) == 0 || q[0] != e {
		return
	}
	q = q[1:]
	if len(q) == 0 {
		delete(s.queues, key)
		return
	}
	s.queues[key] = q
	q[0].fail(q[0].send())
}

// pendingRecord overlays only fields still being written, in write order.
func (r *Runtime) pendingRecord(key string, values Record) {
	if inst := r.Instances[recordWritesID]; key != "" && inst != nil {
		for _, e := range inst.Component.(*recordWrites).queues[key] {
			for _, f := range e.fields {
				if slices.Contains(e.sending, f.Name) || slices.Contains(e.dirty, f.Name) && f.writeErr == "" {
					values[f.Name] = e.written[f.Name]
				}
			}
		}
	}
}

func (e *RecordEditor) markFlush(j *job) {
	j.flush = true
	j.Label = e.spec.Title + " · " + strings.Join(e.sending, ", ")
	j.abandon = func(reason string) []string {
		editors := []*RecordEditor{e}
		if e.spec.WriteKey != "" {
			editors = e.r.recordWrites().queues[e.spec.WriteKey]
		}
		var messages []string
		for _, editor := range editors {
			var names []string
			for _, f := range editor.fields {
				if slices.Contains(editor.sending, f.Name) || slices.Contains(editor.dirty, f.Name) {
					names = append(names, f.Name)
				}
			}
			messages = append(messages, editor.reportUnsaved(names, editor.written, reason)...)
		}
		return messages
	}
}
