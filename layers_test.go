package ochazuke

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"testing"
)

var updateLayers = flag.Bool("update-layers", false, "rewrite testdata/layers.golden")

// layerRows is a table with a row-number column (Column.Part), a column
// highlighted as the layer "column" on every row and on its header, and
// the second row also a "row" layer (a field row a cursor lights).
func layerRows() ([]Column, []ListItem) {
	cols := []Column{{Title: "#", Align: AlignRight, Part: PartRowNumber}, {Title: "name"}, {Title: "size"}}
	var items []ListItem
	for i, row := range [][]string{{"1", "alpha", "7kb"}, {"2", "beta", "40kb"}, {"3", "gamma", "9mb"}} {
		it := ListItem{ID: "r" + row[0], Label: row[1], Noun: "row", Cells: row, Tones: []ToneSpan{LayerTone(2, ToneQueued, "column")}}
		if i == 1 {
			for c := range row {
				it.Tones = append(it.Tones, LayerTone(c, ToneQueued, "row"))
			}
		}
		items = append(items, it)
	}
	return cols, items
}

// The layer frames: in each built-in theme (and with list.intersect and
// list.part-tones off) the header band, the row numbers, the selected
// row crossing the column (list.cell.intersect), the header of the
// column (list.header.intersect), the selected row's number
// (list.rownum.intersect) and a lit row crossing the column with no
// selection. The SGR is written out, so a colour change shows in the
// golden's diff.
func TestLayerFramesGolden(t *testing.T) {
	cols, items := layerRows()
	var b strings.Builder
	frame := func(name string, l *List) {
		b.WriteString("== " + name + "\n")
		for _, line := range strings.Split(l.View(36, 4), "\n") {
			b.WriteString(strconv.Quote(line) + "\n")
		}
	}
	for _, theme := range []string{"dark", "light", "mono"} {
		_, l := toneFixture(t, theme, cols, items)
		l.SetHeaderTones(LayerTone(2, ToneQueued, "column"))
		frame(theme, l)
	}
	for _, off := range []string{IntersectSetting, PartTonesSetting} {
		m, l := toneFixture(t, "dark", cols, items)
		l.SetHeaderTones(LayerTone(2, ToneQueued, "column"))
		m.Runtime.EnsureConfigSettings()
		if err := m.Runtime.Settings().Set(off, false); err != nil {
			t.Fatal(err)
		}
		frame("dark, "+off+" off", l)
	}
	path := "testdata/layers.golden"
	if *updateLayers {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != string(want) {
		t.Fatalf("layer frames changed (go test -run TestLayerFramesGolden -update-layers):\n%s", got)
	}
}

// Where two layers cross, the cell takes the part's intersect tone, and
// one layer alone keeps its tone: the selected row on the column draws
// list.cell.intersect out of the reverse bar; the column's header
// list.header.intersect; the selected row's number list.rownum.intersect;
// the lit row crossing the column (no selection) list.cell.intersect;
// the lit row's other cells and the column's other cells their tone.
func TestLayersCrossInTheirPartsIntersectTone(t *testing.T) {
	cols, items := layerRows()
	_, l := toneFixture(t, "dark", cols, items)
	l.SetHeaderTones(LayerTone(2, ToneQueued, "column"))
	lines := strings.Split(l.View(36, 4), "\n")
	cross := "\x1b[1;" + fg("#1A1A1A") + ";" + bgSGR("#E0B243") + "m"
	if !strings.Contains(lines[1], cross+"7kb") || !strings.Contains(lines[1], "\x1b[7m") {
		t.Fatalf("selected row × column: %q", lines[1])
	}
	if !strings.Contains(lines[2], cross+"40kb") {
		t.Fatalf("row layer × column: %q", lines[2])
	}
	if !strings.Contains(lines[0], bgSGR("#C99A2E")+"msize") {
		t.Fatalf("header × column: %q", lines[0])
	}
	if !strings.Contains(lines[0], bgSGR("#2B323B")) {
		t.Fatalf("header band: %q", lines[0])
	}
	if !strings.Contains(lines[1], bgSGR("#93A7A1")) {
		t.Fatalf("selected row × row number: %q", lines[1])
	}
	if strings.Contains(lines[3], "48;") {
		t.Fatalf("one layer draws no fill: %q", lines[3])
	}
}
