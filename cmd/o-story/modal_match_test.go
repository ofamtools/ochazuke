package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	story "ofam.dev/monogatari"
)

func TestChooserMatchKeepsPanelInkAndOneBar(t *testing.T) {
	t.Parallel()
	path := "stories/chrome/bar-one-run-dark.json"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := story.Parse(data, path)
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
