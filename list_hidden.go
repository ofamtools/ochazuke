package ochazuke

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// Hidden lists (refresh.hidden): after a change, a From list nobody can see
// is not read again. A list is hidden when it sits beneath a page that is not
// the current one (Runtime.Page), is not inside an open modal, and was not
// drawn while the current page was current (an application that draws
// another page's list in a split draws it, so it counts as shown). Such a
// list keeps the rows it holds and is marked stale; it is read again when it
// is shown (its page opens, a modal or a split draws it: refillShown after
// every loop turn and every top-level request), when the application reads
// its rows (Items, Marked), or when a request target misses (TargetInstance
// reads the stale lists before it refuses). A fallback watch whose lists are
// all hidden takes no snapshot either. Nothing here looks at a page, noun or
// list name: the rule reads the instance tree, the modal chain and the last
// drawing.
//
// With a model drawing frames, a stale list shown again draws its last rows
// at once and reads its store after that frame (a job in the list's read
// slot, whose completion refills it on the loop); without one (the CLI, a
// test runtime) it reads at once.

// RefreshHiddenSetting chooses how a change reaches lists that are not
// shown: lazy (the default) marks them stale and reads them when shown,
// eager reads every list after every change, as before.
const RefreshHiddenSetting = "refresh.hidden"

// The values of refresh.hidden.
const (
	RefreshHiddenLazy  = "lazy"
	RefreshHiddenEager = "eager"
)

// lazyHidden reports whether refresh.hidden leaves hidden lists stale.
func (r *Runtime) lazyHidden() bool {
	return r.Settings().String(RefreshHiddenSetting) != RefreshHiddenEager
}

// Stale reports whether the list's rows may be older than its store: a
// change came while it was hidden (refresh.hidden lazy) and it has not been
// read since.
func (l *Collection) Stale() bool { return l.stale }

// hidden reports whether nothing shows the list now (see above).
func (l *Collection) hidden() bool {
	r := l.r
	page := ""
	for id := l.id; id != ""; {
		if slices.Contains(r.modals, id) {
			return false
		}
		inst := r.Instances[id]
		if inst == nil {
			return false
		}
		if page == "" && r.PageOf(id) != nil {
			page = id
		}
		id = inst.Parent
	}
	if page == "" || page == r.Page {
		return false
	}
	return l.drawnOn == "" || l.drawnOn != r.Page
}

// markHidden marks the list stale when refresh.hidden is lazy and nothing
// shows it, and reports whether it did; a stale list is read when shown.
func (l *Collection) markHidden() bool {
	if !l.r.lazyHidden() || !l.hidden() {
		return false
	}
	l.stale = true
	h := l.r.watchHub()
	if !slices.Contains(h.hidden, l.id) {
		h.hidden = append(h.hidden, l.id)
	}
	return true
}

// refreshStale reads a stale list again at once.
func (l *Collection) refreshStale() {
	if !l.stale {
		return
	}
	if err := l.Refill(); err != nil {
		l.r.UI().Error(fmt.Sprintf("%s list: %v", l.r.DisplayNoun(l.from), err))
	}
}

// refillShown reads the stale lists that something shows now: after the
// frame when a model draws (their last rows stay on screen meanwhile), at
// once otherwise.
func (r *Runtime) refillShown() {
	h := r.watches
	if h == nil || len(h.hidden) == 0 {
		return
	}
	keep := h.hidden[:0]
	var shown []*Collection
	for _, id := range h.hidden {
		l := r.List(id)
		switch {
		case l == nil || !l.stale:
		case l.hidden() && r.lazyHidden():
			keep = append(keep, id)
		default:
			shown = append(shown, l)
		}
	}
	h.hidden = slices.Clone(keep)
	for _, l := range shown {
		if h.frames == 0 {
			l.refreshStale()
			continue
		}
		l.refillAfterFrame()
	}
}

// refillStale reads every stale list at once, shown or not (a target that
// names no instance may name a row one of them has not read yet).
func (r *Runtime) refillStale() bool {
	h := r.watches
	if h == nil || len(h.hidden) == 0 {
		return false
	}
	ids := h.hidden
	h.hidden = nil
	read := false
	for _, id := range ids {
		if l := r.List(id); l != nil && l.stale {
			l.refreshStale()
			read = true
		}
	}
	return read
}

// refillAfterFrame reads the stale list once a frame has drawn it with its
// last rows: a job in the list's read slot waits one frame interval off the
// loop, and its completion reads the store on the loop when a frame was
// drawn since it started, else waits again (a few times at most). The list
// keeps its last rows meanwhile (its loading line shows only for a list
// with none).
func (l *Collection) refillAfterFrame() { l.waitFrame(l.r.watchHub().frames, 8) }

func (l *Collection) waitFrame(seen uint64, tries int) {
	r := l.r
	wait := time.Second / 60
	if fps := r.Settings().Int(RenderMaxFPSSetting); fps > 0 {
		wait = time.Second / time.Duration(fps)
	}
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context()}
	res := Pending(func(ctx context.Context) (any, error) {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
		return nil, nil
	}).Replacing(listReadSlot).Then(func(Ctx, any, error) {
		if r.watchHub().frames == seen && tries > 1 {
			l.waitFrame(seen, tries-1)
			return
		}
		l.refreshStale()
	})
	if _, err := r.Launch(l.id, l.from+".list", res, c); err != nil {
		l.refreshStale()
	}
}
