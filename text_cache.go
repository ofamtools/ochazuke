package ochazuke

import (
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// Chrome reuses short labels and styled rule lines across frames. Bound
// retained text and entries; large bodies use their component/viewport cache.
var chromeWidths = struct {
	sync.Mutex
	rows map[string]int
}{rows: map[string]int{}}

func chromeWidth(text string) int {
	ascii := true
	for i := range len(text) {
		if text[i] < 32 || text[i] > 126 {
			ascii = false
			break
		}
	}
	if ascii {
		return len(text)
	}
	if len(text) > 512 {
		return ansi.StringWidth(text)
	}
	chromeWidths.Lock()
	w, ok := chromeWidths.rows[text]
	chromeWidths.Unlock()
	if ok {
		return w
	}
	w = ansi.StringWidth(text)
	chromeWidths.Lock()
	if len(chromeWidths.rows) >= 1024 {
		clear(chromeWidths.rows)
	}
	chromeWidths.rows[text] = w
	chromeWidths.Unlock()
	return w
}
