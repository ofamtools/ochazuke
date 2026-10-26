package ochazuke

import (
	"container/list"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The results table's size and modified cells are statted lazily: the
// index walk never stats, and only the rows the table draws are looked
// up, off the loop, in the picker's "stat" slot, a replacing job per
// window. Answers are cached per absolute path in a bounded LRU that a
// new index (queueIndex's shallow stage) clears. A row whose stat has not
// landed draws blank cells; the columns' widths are fixed, so nothing
// moves when it lands.

// pickerStatLimit bounds the stat cache.
const pickerStatLimit = 4096

// pickerStat is one path's answer; ok is false when the stat failed.
type pickerStat struct {
	size     int64
	modified time.Time
	ok       bool
}

type pickerStatMsg struct {
	picker *Picker
	id     uint64
	stats  map[string]pickerStat
}

// pickerStats is the LRU of stat answers by absolute path.
type pickerStats struct {
	order *list.List // front is the most recent; values are keys
	items map[string]*list.Element
	stats map[string]pickerStat
}

func newPickerStats() *pickerStats {
	return &pickerStats{order: list.New(), items: map[string]*list.Element{}, stats: map[string]pickerStat{}}
}

func (s *pickerStats) get(key string) (pickerStat, bool) {
	if s == nil {
		return pickerStat{}, false
	}
	e, ok := s.items[key]
	if !ok {
		return pickerStat{}, false
	}
	s.order.MoveToFront(e)
	return s.stats[key], true
}

func (s *pickerStats) put(key string, stat pickerStat) {
	if e, ok := s.items[key]; ok {
		s.order.MoveToFront(e)
	} else {
		s.items[key] = s.order.PushFront(key)
	}
	s.stats[key] = stat
	for s.order.Len() > pickerStatLimit {
		old := s.order.Back()
		key := old.Value.(string)
		s.order.Remove(old)
		delete(s.items, key)
		delete(s.stats, key)
	}
}

func (s *pickerStats) len() int { return s.order.Len() }

// statKey is the cache key of an entry: its absolute path.
func (p *Picker) statKey(entry PickerEntry) string {
	return filepath.Join(p.Directory, filepath.FromSlash(entry.Path))
}

// window is the results the table draws for limit rows: the selection
// stays on the last row once it passes the first screen.
func (p *Picker) window(limit int) (start, end int) {
	start = max(0, p.Selected-limit+1)
	return start, min(len(p.Entries), start+limit)
}

// queueStats starts a stat job for the drawn rows (the last frame's row
// count) that are neither cached nor in the current job. Nothing before
// the first frame: an undrawn table stats nothing.
func (p *Picker) queueStats() {
	if p.drawnRows <= 0 || len(p.Entries) == 0 {
		return
	}
	if p.stats == nil {
		p.stats = newPickerStats()
	}
	start, end := p.window(p.drawnRows)
	var missing []string
	fresh := false
	for _, entry := range p.Entries[start:end] {
		key := p.statKey(entry)
		if _, ok := p.stats.items[key]; ok {
			continue
		}
		missing = append(missing, key)
		if !p.statting[key] {
			fresh = true
		}
	}
	if !fresh {
		return
	}
	id, ctx := p.issue("stat")
	p.statting = map[string]bool{}
	for _, key := range missing {
		p.statting[key] = true
	}
	stat := p.statFunc
	if stat == nil {
		stat = os.Lstat
	}
	p.pending = append(p.pending, func() tea.Msg {
		return pickerStatMsg{picker: p, id: id, stats: statPaths(ctx, stat, missing)}
	})
}

// statPaths stats each path until ctx ends.
func statPaths(ctx context.Context, stat func(string) (fs.FileInfo, error), paths []string) map[string]pickerStat {
	out := make(map[string]pickerStat, len(paths))
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		if info, err := stat(path); err == nil {
			out[path] = pickerStat{size: info.Size(), modified: info.ModTime(), ok: true}
		} else {
			out[path] = pickerStat{}
		}
	}
	return out
}

// applyStats caches a current stat job's answers.
func (p *Picker) applyStats(msg pickerStatMsg) {
	if !p.accept("stat", msg.id, nil) {
		return
	}
	if p.stats == nil {
		p.stats = newPickerStats()
	}
	for key, stat := range msg.stats {
		p.stats.put(key, stat)
	}
	p.statting = nil
}

// resetStats forgets every answer and the job in flight (a new index).
func (p *Picker) resetStats() {
	p.cancel("stat")
	p.stats, p.statting = newPickerStats(), nil
}
