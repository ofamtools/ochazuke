package ochazuke

import (
	"fmt"
	"strings"
)

// The reads of a related stack's sections, kept off the UI loop.
//
// A section (or a split, for all its values) reads its things through
// Related.readSection. With RelatedBackgroundSetting on (the default) the
// read is a stepped run (runSteppedReads, as a remote section's read
// was): it runs on the loop until a store makes a slow call through
// Ctx.OffLoop (a local store's SQLite query) or Ctx.Await (a remote
// store's), and that call runs as a background job (jobs.background-max
// bounds them; Runtime.Lifetime is their context). A store that makes no
// such call answers at once, on the loop, as before.
//
// While reads run, the sections keep drawing what they drew; the answers
// of one round land together, in one update, when the last of them lands
// (relatedLanding), so the stack swaps from the old thing's rows to the new
// thing's in one frame. A read whose stack moved to another thing before
// it landed is dropped, and a newer read of the same section replaces an
// older one (its job's slot): a fast j j j ends showing the last thing's
// rows. A section that never had rows says it is loading, with the one
// loading text (LoadingText).
//
// The stacks drawn in the last frame are brought in step at the end of
// every update (Runtime.syncDrawnRelated), so a new selection or a change a
// watch saw starts its reads in the update, whose commands the adapter
// runs, never as a read inside View.

// RelatedBackgroundSetting turns the off-loop reads of related sections
// on (default) or off: off, every local section is read on the loop when
// the stack's thing changes, as before.
const RelatedBackgroundSetting = "detail.related-background"

// relatedLanding is one round of a stack's reads off the loop: the reads
// still running (by read id, the sequence of their newest read and the
// instance that owns the job), and the answers that wait for them.
type relatedLanding struct {
	noun, key string
	pending   map[string]uint64
	owners    map[string]string
	held      []func()
}

// background reports whether the stack reads its local sections off the
// loop (RelatedBackgroundSetting; a setting that does not exist counts as
// on).
func (rel *Related) background() bool {
	if _, ok := rel.r.settingValue(RelatedBackgroundSetting); !ok {
		return true
	}
	return rel.r.Settings().Bool(RelatedBackgroundSetting)
}

// stepped reports whether ref's things are read by a stepped run: a remote
// store's while the remote setting is on (a list or history section never
// is), a local one's while the background setting is on. A remote store's
// section read otherwise answers empty, as before.
func (rel *Related) stepped(ref Referrer) bool {
	if rel.remoteRef(ref) {
		return !ref.List && !ref.History && rel.r.Settings().Bool(RelatedRemoteSetting)
	}
	return rel.background()
}

// read reads ref's things as a page of q and hands the page to land: at
// once when the read is made on the loop or answers there, else when the
// round of reads it belongs to has landed. id names the read (a section's
// or a split's id); s is the section it fills, nil for a split. A failed
// read lands an empty page and says why.
func (rel *Related) readSection(id string, s *relatedSection, ref Referrer, q ListQuery, land func(ListPage)) {
	r := rel.r
	if !rel.stepped(ref) {
		page, err := rel.readRefPage(r.backgroundCtx(), ref, q)
		if err != nil {
			rel.readFailed(ref, err)
			page = ListPage{}
		}
		rel.answer(id, 0, func() { land(page) })
		return
	}
	owner := rel.id // a split's read; its slot is its id, as a section's is on the section
	if s != nil {
		owner = s.id
	}
	b := rel.landingNow()
	rel.readSeq++
	seq := rel.readSeq
	b.pending[id], b.owners[id] = seq, owner
	var page ListPage
	body := func(c Ctx) (any, error) {
		got, err := rel.readRefPage(c, ref, q)
		page = got
		return nil, err
	}
	finish := func(_ any, err error) (any, error) {
		if rel.closed || rel.landing != b || b.pending[id] != seq || b.noun != rel.noun || b.key != rel.key {
			return nil, nil // the stack moved on, or a newer read of it runs
		}
		if err != nil {
			page = ListPage{}
		}
		rel.answer(id, seq, func() { land(page) })
		return nil, err
	}
	c := r.backgroundCtx()
	res := r.runSteppedReads(c, ref.Noun+".list", body, finish)
	if res.err != nil || res.work == nil {
		if res.err != nil {
			rel.readFailed(ref, res.err)
		}
		return // answered at once
	}
	if _, err := r.Launch(owner, ref.Noun+".list", res.Replacing(id).Then(func(_ Ctx, _ any, err error) {
		if err != nil {
			rel.readFailed(ref, err)
		}
	}), c); err != nil {
		delete(b.pending, id)
		delete(b.owners, id)
		rel.readFailed(ref, err)
		rel.flushLanding()
		return
	}
	if s != nil && !s.filled && !s.loading {
		s.loading = true
		s.render()
	}
}

// readFailed says a section's read failed.
func (rel *Related) readFailed(ref Referrer, err error) {
	r := rel.r
	r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(ref.Noun), err))
}

// landingNow is the round the stack's reads join: the one running for the
// thing the stack shows now, else a new one (an older thing's round is
// dropped with what it holds).
func (rel *Related) landingNow() *relatedLanding {
	if b := rel.landing; b != nil && b.noun == rel.noun && b.key == rel.key {
		return b
	}
	rel.landing = &relatedLanding{noun: rel.noun, key: rel.key, pending: map[string]uint64{}, owners: map[string]string{}}
	return rel.landing
}

// answer lands a read's answer (apply), or holds it while other reads of
// its round still run; seq is the read's sequence, 0 for a read made on
// the loop. An answer for a thing the stack no longer shows, or one a
// newer read replaced, is dropped.
func (rel *Related) answer(id string, seq uint64, apply func()) {
	b := rel.landing
	if b != nil && (b.noun != rel.noun || b.key != rel.key) {
		rel.landing, b = nil, nil // an older thing's round: what it holds is dropped
	}
	switch {
	case seq != 0 && (b == nil || b.pending[id] != seq):
		return // an older thing's read, or one a newer read of it replaced
	case b == nil:
		apply()
		return
	case seq != 0:
		delete(b.pending, id)
		delete(b.owners, id)
	}
	b.held = append(b.held, apply)
	rel.flushLanding()
}

// flushLanding lands the round's answers, in the order they came, once no
// read of it runs (a read whose owner was removed never lands: it is
// forgotten), then brings the group headings in step.
func (rel *Related) flushLanding() {
	b := rel.landing
	if b == nil {
		return
	}
	for id, owner := range b.owners {
		if rel.r.Instances[owner] == nil {
			delete(b.pending, id)
			delete(b.owners, id)
		}
	}
	if len(b.pending) > 0 {
		return
	}
	rel.landing = nil
	for _, apply := range b.held {
		apply()
	}
	rel.syncGroups()
}

// markDrawn records that the stack is drawn in this frame, then brings it
// in step: the draw's own sync.
func (rel *Related) markDrawn() {
	rel.drawn = rel.r.watchHub().frames
	rel.sync()
}

// syncDrawnRelated brings in step, at the end of an update, every stack
// drawn in the last frame: a read the update calls for (a new selection, a
// change a watch saw) starts here, where its job's command reaches the
// adapter, and the frame that follows draws without reading. A stack not
// drawn is left until it is (as a hidden list is, refresh.hidden).
func (r *Runtime) syncDrawnRelated() {
	frame := r.watchHub().frames
	if frame == 0 {
		return
	}
	for _, inst := range r.components() {
		if rel, ok := inst.Component.(*Related); ok && rel.drawn == frame {
			rel.sync()
		}
	}
}

// syncArchived sets each section list's archived toggle from the Archived
// setting when the setting changed since the lists were last set (a new
// section is set when it is made, before its first read), and reads again
// only the sections whose toggle it changed.
func (rel *Related) syncArchived() {
	if rel.spec.Archived == "" {
		return
	}
	named := rel.listSetting(rel.spec.Archived)
	value := strings.Join(named, "\x00")
	if value == rel.archived {
		return
	}
	rel.archived = value
	var changed []*relatedSection
	for _, s := range rel.sections {
		if rel.setArchived(s, named) && s.filled {
			changed = append(changed, s)
		}
	}
	for _, s := range changed {
		s.fill()
	}
}

// setArchived sets one section list's archived toggle from the setting's
// entries and reports whether it changed.
func (rel *Related) setArchived(s *relatedSection, named []string) bool {
	if s.list == nil || s.grouping {
		return false
	}
	want := namedBy(named, s.ref.ID()) != ""
	if s.list.ShowsArchived() == want {
		return false
	}
	s.list.SetShowArchived(want)
	return true
}
