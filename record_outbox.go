package ochazuke

import (
	"maps"
	"slices"
)

// recordOutbox is what one record editor still owes the store: the record
// as written (or queued to be), the latest values loaded from the store, the
// fields written in memory but not yet sent, the fields of the write in
// flight, and the last write's error. It knows nothing of the Runtime, the
// fields' widgets or the application; the editor and the writes queue ask it.
// Per-field failure marks stay on the fields and come in as arguments.
type recordOutbox struct {
	written  Record   // the record as written, or queued to be
	baseline Record   // latest loaded store values, independent of pending edits
	owner    string   // the instance whose job runs the writes, so they outlive the form
	dirty    []string // fields written in memory, not yet in the store
	sending  []string // the fields of the write in flight
	inflight bool
	err      string
}

func newRecordOutbox(baseline Record, owner string) recordOutbox {
	return recordOutbox{written: Record{}, baseline: baseline, owner: owner}
}

// queue marks names written in memory and not yet sent.
func (o *recordOutbox) queue(names ...string) {
	for _, name := range names {
		if !slices.Contains(o.dirty, name) {
			o.dirty = append(o.dirty, name)
		}
	}
}

// queueAllIfIdle queues names when nothing is queued (a run that writes the
// whole record).
func (o *recordOutbox) queueAllIfIdle(names []string) {
	if len(o.dirty) == 0 {
		o.queue(names...)
	}
}

// next is the fields to send now, taken from order (the form's field order):
// every dirty one, or with perField only the first dirty one that retry
// accepts. With perField, retry says whether a field that failed before has
// changed since; without it every dirty field goes. It does not change the
// outbox.
func (o *recordOutbox) next(order []string, perField bool, retry func(string) bool) []string {
	var out []string
	for _, name := range order {
		if !slices.Contains(o.dirty, name) || perField && !retry(name) {
			continue
		}
		out = append(out, name)
		if perField {
			break
		}
	}
	return out
}

// take removes names from the queue: they are being sent.
func (o *recordOutbox) take(names []string) {
	o.dirty = slices.DeleteFunc(o.dirty, func(name string) bool { return slices.Contains(names, name) })
}

// stage names the fields being handed to the application.
func (o *recordOutbox) stage(names []string) { o.sending = names }

// launch marks a write in flight for names.
func (o *recordOutbox) launch(names []string) { o.inflight, o.sending = true, names }

// settle ends the write in flight (or the staging), whatever its outcome.
func (o *recordOutbox) settle() { o.inflight, o.sending = false, nil }

// abandon ends the write in flight with reason and queues names again.
func (o *recordOutbox) abandon(reason string, names []string) {
	o.settle()
	o.err = reason
	o.queue(names...)
}

// idle reports that nothing is queued.
func (o *recordOutbox) idle() bool { return len(o.dirty) == 0 }

// pending reports that name is still being written: sending, or queued and
// not failed (failed is the field's own mark).
func (o *recordOutbox) pending(name string, failed bool) bool {
	return slices.Contains(o.sending, name) || slices.Contains(o.dirty, name) && !failed
}

// owes reports that name has a write queued or in flight.
func (o *recordOutbox) owes(name string) bool {
	return slices.Contains(o.dirty, name) || slices.Contains(o.sending, name)
}

// queued and sendingNames are copies of the queue and of the write in flight.
func (o *recordOutbox) queued() []string       { return slices.Clone(o.dirty) }
func (o *recordOutbox) sendingNames() []string { return slices.Clone(o.sending) }

// changedSince is the names whose value in rec differs from the loaded store
// values.
func (o *recordOutbox) changedSince(rec Record, names []string) []string {
	var out []string
	for _, name := range names {
		if rec[name] != o.baseline[name] {
			out = append(out, name)
		}
	}
	return out
}

// load takes a store value for name; it reports whether the written value
// should follow it (nothing of ours is waiting to be written over it).
func (o *recordOutbox) load(name, v string) (follow bool) {
	o.baseline[name] = v
	return v != o.written[name] && !o.owes(name)
}

// value is the written value of name; set writes it.
func (o *recordOutbox) value(name string) string { return o.written[name] }
func (o *recordOutbox) set(name, v string)       { o.written[name] = v }

// record is a copy of the record as written.
func (o *recordOutbox) record() Record { return maps.Clone(o.written) }

// adopt makes values the record as written.
func (o *recordOutbox) adopt(values Record) { o.written = values }
