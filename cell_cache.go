package ochazuke

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

type fittedCellKey struct {
	text, tail string
	width      int
}

// Pure text results are shared by nested panels and the outer shell. Bound
// both entries and bytes; clone sliced keys so a short cell cannot retain
// an entire terminal transcript. No style, runtime or application is held.
var fittedCells = struct {
	sync.Mutex
	rows  map[fittedCellKey]string
	bytes int
}{rows: map[fittedCellKey]string{}}

func fitCells(line string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	key := fittedCellKey{line, tail, width}
	cache := len(line) <= 4096 && width <= 4096
	if cache {
		fittedCells.Lock()
		text, ok := fittedCells.rows[key]
		fittedCells.Unlock()
		if ok {
			return text
		}
	}
	text := line
	w := chromeWidth(text)
	if w > width {
		text = ansi.Truncate(text, width, tail)
		w = chromeWidth(text)
	}
	if strings.ContainsRune(text, '\x1b') {
		text += "\x1b[0m"
	}
	text += strings.Repeat(" ", max(0, width-w))
	if cache {
		fittedCells.Lock()
		if len(fittedCells.rows) >= 2048 || fittedCells.bytes+len(line)+len(text) > 2<<20 {
			clear(fittedCells.rows)
			fittedCells.bytes = 0
		}
		key.text = strings.Clone(line)
		if _, exists := fittedCells.rows[key]; !exists {
			fittedCells.rows[key] = strings.Clone(text)
			fittedCells.bytes += len(line) + len(text)
		}
		fittedCells.Unlock()
	}
	return text
}
