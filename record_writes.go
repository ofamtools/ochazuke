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

func (e *RecordEditor) markFlush(j *job, w RecordWrite) {
	j.flush = true
	j.Label = e.spec.Title + " · " + strings.Join(e.sending, ", ")
	j.abandon = func(reason string) []string {
		editors := []*RecordEditor{e}
		if e.r.Quit && e.spec.WriteKey != "" {
			s := e.r.recordWrites()
			editors = s.queues[e.spec.WriteKey]
			delete(s.queues, e.spec.WriteKey)
		}
		var messages []string
		for _, editor := range editors {
			names := slices.Clone(editor.dirty)
			pending := slices.Clone(names)
			if editor == e {
				messages = append(messages, editor.reportUnsaved(w.Changed, w.Values, reason)...)
				pending = slices.DeleteFunc(pending, func(name string) bool {
					return slices.Contains(w.Changed, name) && editor.written[name] == w.Values[name]
				})
				for _, name := range w.Changed {
					if !slices.Contains(names, name) {
						names = append(names, name)
					}
				}
			}
			editor.inflight, editor.sending = false, nil
			editor.writeErr = reason
			editor.requeue(names)
			for _, name := range names {
				f := editor.field(name)
				f.writeErr, f.failedValue = reason, editor.written[name]
			}
			messages = append(messages, editor.reportUnsaved(pending, editor.written, reason)...)
		}
		e.releaseWrite()
		return messages
	}
}
