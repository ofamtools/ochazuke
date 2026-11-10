package main

import (
	"fmt"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	story "ofam.dev/monogatari"
)

func TestChooserMatchKeepsPanelInkAndOneBar(t *testing.T) {
	t.Parallel()
	s, err := story.Parse([]byte(`{
		"name": "chooser panel ink", "width": 72, "height": 20,
		"theme": "dark", "values": {"chrome.color": "rich"},
		"page": {"layout": "panels", "noHeadings": true,
			"lists": [{"id": "working", "rows": 2, "panelTone": "state.working"}],
			"chooser": {"kind": "pick", "title": "Projects", "rows": [
				{"id": "a", "label": "ochazuke framework"},
				{"id": "b", "label": "ochazuke examples"},
				{"id": "c", "label": "monogatari stories"}
			]}},
		"script": [{"type": "oz"}, "ctrl+n"]
	}`), "chooser panel ink")
	if err != nil {
		t.Fatal(err)
	}
	ss, err := (story.Library{Apps: hostApps}).Build(s, story.Options{Mono: true})
	if err != nil {
		t.Fatal(err)
	}
	d := story.NewDriver(ss, story.Options{Mono: true})
	defer d.Close()
	if err := d.Play(s.Script, nil); err != nil {
		t.Fatal(err)
	}
	frame := d.S.Frame()
	width, height := s.Size()
	screen := vt.NewEmulator(width, height)
	defer screen.Close()
	if _, err := screen.Write([]byte(strings.ReplaceAll(frame, "\n", "\r\n"))); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"uke framework", "ochazuke examples"} {
		found := false
		for y, line := range strings.Split(ansi.Strip(frame), "\n") {
			at := strings.Index(line, text)
			if at < 0 {
				continue
			}
			found = true
			start, end := ansi.StringWidth(line[:at]), ansi.StringWidth(line[:at])+len(text)
			selected := text == "ochazuke examples"
			if selected {
				for x := range width {
					if c := screen.CellAt(x, y); c != nil && c.Style.Attrs&uv.AttrReverse != 0 {
						start, end = min(start, x), max(end, x+1)
					}
				}
			}
			for x := start; x < end; x++ {
				c := screen.CellAt(x, y)
				if c == nil || c.Style.Fg == nil || (c.Style.Attrs&uv.AttrReverse != 0) != selected {
					t.Fatalf("%q cell %d lost foreground or reverse: %+v", text, x, c)
				}
				r, g, b, _ := c.Style.Fg.RGBA()
				if hex := fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8); hex != "#DCE9E3" {
					t.Fatalf("%q cell %d foreground %s, want #DCE9E3", text, x, hex)
				}
			}
		}
		if !found {
			t.Fatalf("frame lacks %q", text)
		}
	}
}

// Compare the visible geometry while scrolling, not a literal border length
// tied to the build stamp or generated reference inventory.
func TestDocsViewerKeepsWidthWhileScrolling(t *testing.T) {
	s, err := story.Parse([]byte(`{
		"name": "docs width", "width": 110, "height": 20,
		"values": {"help.markdown": false},
		"page": {"lists": [{"id": "a", "rows": 2}]},
		"script": [":", {"type": "docs-viewer open index"}, "enter",
			"j", "j", "j", "j", "j", "j", "j", "j", "j", "j"]
	}`), "docs width")
	if err != nil {
		t.Fatal(err)
	}
	width := 0
	res := (story.Library{Apps: hostApps}).Run(s, story.RunOptions{Observe: func(p story.RunPosition) {
		if p.After < 3 {
			return
		}
		if p.Session.R.TopModal() != "docs-viewer" {
			t.Fatalf("after %d: docs viewer closed", p.After)
		}
		for _, line := range strings.Split(ansi.Strip(p.Session.Frame()), "\n") {
			start, end := strings.Index(line, "╭"), strings.LastIndex(line, "╮")
			if start < 0 || end < start {
				continue
			}
			got := ansi.StringWidth(line[start : end+len("╮")])
			if width == 0 {
				width = got
			} else if got != width {
				t.Errorf("after %d: panel width %d, opened at %d", p.After, got, width)
			}
			return
		}
		t.Fatalf("after %d: no visible docs panel", p.After)
	}})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
}
