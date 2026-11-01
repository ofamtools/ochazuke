package ochazuke

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ListFilterLingerSetting controls how long rows leaving a filter remain
// visible. The default is 30s; zero disables lingering.
const ListFilterLingerSetting = "list.filter-linger"

// ToneLingering marks a changed row that will leave its filter shortly.
const ToneLingering = "list.lingering"

// filterLinger remembers actual matches separately from retained rows, so
// repeated refreshes never restart a deadline or resurrect an expired row.
// Lists and choosers share this identity-based transition and timer.
type filterLinger struct {
	matched map[string]bool
	leaving map[string]time.Time
	shown   []string
	epoch   uint64
	tickAt  time.Time
	cancel  context.CancelFunc
}

func (f *filterLinger) stop() {
	f.epoch++
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.tickAt = time.Time{}
}

func (f *filterLinger) reset() { f.stop(); f.matched, f.leaving, f.shown = nil, nil, nil }

func (f *filterLinger) apply(all, matches []string, now time.Time, duration time.Duration) []string {
	if duration <= 0 {
		f.reset()
		return matches
	}
	present, matched := make(map[string]bool, len(all)), make(map[string]bool, len(matches))
	for _, id := range all {
		present[id] = true
	}
	for _, id := range matches {
		matched[id] = true
	}
	if f.leaving == nil {
		f.leaving = map[string]time.Time{}
	}
	for _, id := range f.shown {
		if id != "" && f.matched[id] && !matched[id] && present[id] {
			f.leaving[id] = now.Add(duration)
		}
	}
	for id, row := range f.leaving {
		if !present[id] || matched[id] || !now.Before(row) {
			delete(f.leaving, id)
		}
	}
	// Merge in one pass over the previous drawing order. Departures hold
	// their old slots; actual matches fill the gaps in their new order.
	shown := make([]string, 0, len(matches)+len(f.leaving))
	next := 0
	for at, id := range f.shown {
		if _, ok := f.leaving[id]; ok {
			n := min(max(0, at-len(shown)), len(matches)-next)
			shown = append(shown, matches[next:next+n]...)
			next += n
			shown = append(shown, id)
		}
	}
	shown = append(shown, matches[next:]...)
	f.matched, f.shown = matched, shown
	if len(f.leaving) == 0 {
		f.stop()
	}
	return shown
}

func (f *filterLinger) command(now time.Time) tea.Cmd {
	var next time.Time
	for _, row := range f.leaving {
		if next.IsZero() || row.Before(next) {
			next = row
		}
	}
	if next.Equal(f.tickAt) {
		return nil
	}
	f.stop()
	if next.IsZero() {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel, f.tickAt = cancel, next
	epoch := f.epoch
	return func() tea.Msg {
		timer := time.NewTimer(max(0, next.Sub(now)))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return JobLingerMsg{At: next, filter: f, epoch: epoch}
		}
	}
}

func (f *filterLinger) fired(msg tea.Msg) bool {
	tick, ok := msg.(JobLingerMsg)
	if !ok || tick.filter != f || tick.epoch != f.epoch || !tick.At.Equal(f.tickAt) {
		return false
	}
	f.stop()
	return true
}

func (l *List) filterController() *List    { return l }
func (c *Chooser) filterController() *List { return c.filters }

func (l *List) filterNow() time.Time {
	if now := l.r.UI().Now; now != nil {
		return now()
	}
	return time.Now()
}

func (l *List) lingerRows(matches []ListItem) []ListItem {
	if !l.Filtering() || l.r.Settings().Duration(ListFilterLingerSetting) <= 0 {
		l.linger.reset()
		return matches
	}
	all, ids := make([]string, 0, len(l.all)), make([]string, 0, len(matches))
	byID := make(map[string]ListItem, len(l.all))
	for _, row := range l.all {
		all = append(all, row.ID)
		byID[row.ID] = row
	}
	for _, row := range matches {
		ids = append(ids, row.ID)
	}
	duration := l.r.Settings().Duration(ListFilterLingerSetting)
	shown := l.linger.apply(all, ids, l.filterNow(), duration)
	out := make([]ListItem, 0, len(shown))
	for _, id := range shown {
		out = append(out, byID[id])
	}
	return out
}

// Lingering reports whether id is retained only until its filter deadline.
func (l *List) Lingering(id string) bool { _, ok := l.linger.leaving[id]; return ok }

// FlushLingering removes all leaving rows immediately, keeping the filter.
func (l *List) FlushLingering() {
	l.linger.reset()
	if l.filterChanged != nil {
		l.filterChanged()
		return
	}
	previous, _ := l.Selected()
	l.refilter(previous.ID)
	l.syncVisual()
	l.syncFocus(previous, nil)
}

func (l *List) HandleMessage(msg tea.Msg) bool {
	if !l.linger.fired(msg) {
		return false
	}
	if l.filterChanged != nil {
		l.filterChanged()
		return true
	}
	previous, _ := l.Selected()
	l.refilter(previous.ID)
	l.syncVisual()
	l.syncFocus(previous, nil)
	return true
}

func (l *List) PendingCommand() tea.Cmd {
	if len(l.linger.leaving) > 0 && l.r.Settings().Duration(ListFilterLingerSetting) <= 0 {
		l.FlushLingering()
	}
	return l.linger.command(l.filterNow())
}
func (l *List) Close() { l.linger.reset() }

func (l *List) rowGutter(id string) string {
	mark := " "
	if l.marks[id] {
		mark = "*"
	}
	if l.Lingering(id) {
		if mark == "*" {
			return "*~"
		}
		return "~ "
	}
	return mark + " "
}

func (l *List) hasGutter() bool { return len(l.marks) > 0 || len(l.linger.leaving) > 0 }

func (l *List) rowTones(item ListItem) []ToneSpan {
	if !l.Lingering(item.ID) {
		return item.Tones
	}
	tones := slices.Clone(item.Tones)
	for cell := range max(1, len(item.Cells)) {
		tones = append(tones, ToneSpan{Cell: cell, Tone: ToneLingering})
	}
	return tones
}
