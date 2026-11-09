package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func thingChoice() Choice {
	return Choice{ID: "question/q-1", Label: "Ship the page?"}
}

// ParseChoices reads cells, tones and the row flags a source row carries,
// from a map, a JSON-decoded []any and a list row.
func TestParseChoicesReadsCellsTonesAndFlags(t *testing.T) {
	t.Parallel()
	got, err := ParseChoices([]any{map[string]any{
		"id": "a", "label": "Alpha", "cells": []any{"Alpha", "busy"},
		"tones":  []any{map[string]any{"cell": 1, "tone": "errored"}},
		"opens":  true,
		"noPick": true, "refuse": "no", "trailing": true, "fixed": true,
	}})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	want := Choice{ID: "a", Label: "Alpha", Cells: []string{"Alpha", "busy"},
		Tones:    []ToneSpan{{Cell: 1, Tone: "errored"}},
		RowFlags: RowFlags{Opens: true, NoPick: true, Refuse: "no", Trailing: true, Fixed: true}}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
	rows, err := ParseChoices([]ListItem{{ID: "t1", Label: "alpha", Cells: []string{"alpha", "seeding"}}})
	if err != nil || !slices.Equal(rows[0].Cells, []string{"alpha", "seeding"}) {
		t.Errorf("a list row's cells: %+v %v", rows, err)
	}
}

// Choice.Row carries cells, tones and flags across, and dims the id only
// on a row drawn as one label.
func TestChoiceRowCarriesDataAcross(t *testing.T) {
	t.Parallel()
	c := thingChoice()
	c.Cells = []string{"Ship the page?", "open"}
	c.Tones = []ToneSpan{CellTone(1, ToneErrored)}
	c.RowFlags = RowFlags{Opens: true, NoPick: true, Refuse: "why", Trailing: true, Fixed: true}
	row := c.Row()
	if row.Label != "Ship the page?  q-1" || !slices.Equal(row.Cells, c.Cells) || !row.Opens || !row.NoPick || row.Refuse != "why" || !row.Trailing || !row.Fixed {
		t.Errorf("row %+v", row)
	}
	if len(row.Tones) != 1 || row.Tones[0].Tone != ToneErrored {
		t.Errorf("a row with cells has only the choice's own tones: %+v", row.Tones)
	}
	plain := thingChoice().Row()
	if len(plain.Tones) != 1 || plain.Tones[0].Tone != ToneChoiceID {
		t.Errorf("plain row tones %+v", plain.Tones)
	}
	if bare := (Choice{ID: "x", Label: "x"}).Row(); bare.Label != "x" || len(bare.Tones) != 0 {
		t.Errorf("a choice without a label of its own: %+v", bare)
	}
}

// Entry is the one label rule: label first with the id dim, or the id first.
func TestChoiceEntryOrders(t *testing.T) {
	t.Parallel()
	c := thingChoice()
	first := c.Entry(ChoiceLabelFirst)
	if first.Text != "Ship the page?  q-1" || len(first.Tones) != 1 || first.Tones[0].Tone != ToneChoiceID || first.Tones[0].From != len([]rune("Ship the page?"))+2 {
		t.Errorf("label-first %+v", first)
	}
	if first.Text != c.Row().Label {
		t.Errorf("entry %q and row %q differ", first.Text, c.Row().Label)
	}
	if got := c.Entry(ChoiceIDFirst); got.Text != "question/q-1  Ship the page?" || len(got.Tones) != 0 {
		t.Errorf("id-first %+v", got)
	}
	if got := (Choice{ID: "a", Label: "a"}).Entry(ChoiceLabelFirst); got.Text != "a" {
		t.Errorf("unlabelled %+v", got)
	}
}

func choiceLabelPalette(t *testing.T) (*Runtime, *Palette) {
	t.Helper()
	r := NewRuntime()
	p := NewPalette(r)
	if _, err := r.AddPage("ext/json", "JSON", func(*Runtime, int, int) Block { return Block{} }); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(PaletteChoiceWordsSetting, "id"); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	typeLine(t, p, "page open ")
	return r, p
}

func choiceEntryLabel(p *Palette, id string) (PaletteEntry, bool) {
	for _, e := range p.completions() {
		if e.ID == id {
			return e, true
		}
	}
	return PaletteEntry{}, false
}

// The palette's choice completion shows the label first by default and the
// id first under palette.choice-label id-first.
func TestPaletteChoiceLabelSetting(t *testing.T) {
	t.Parallel()
	r, p := choiceLabelPalette(t)
	e, ok := choiceEntryLabel(p, "ext/json")
	if !ok || e.Label != "JSON  json" || len(e.Tones) != 1 || e.Tones[0].Tone != ToneChoiceID {
		t.Fatalf("default entry %+v %v", e, ok)
	}
	if err := r.Settings().Set(PaletteChoiceLabelSetting, "id-first"); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "fill", "palette", Arguments{"text": "page open "})
	if e, ok := choiceEntryLabel(p, "ext/json"); !ok || e.Label != "ext/json  JSON" || len(e.Tones) != 0 {
		t.Errorf("id-first entry %+v %v", e, ok)
	}
}

// Chips use the same rule.
func TestChipTextUsesTheEntryRule(t *testing.T) {
	t.Parallel()
	c := thingChoice()
	s := Styles{}
	if got := strings.TrimSpace(ansi.Strip(s.chipText(c, true, ChoiceLabelFirst))); !strings.HasSuffix(got, "Ship the page?  q-1") {
		t.Errorf("label-first chip %q", got)
	}
	if got := strings.TrimSpace(ansi.Strip(s.chipText(c, false, ChoiceIDFirst))); !strings.HasSuffix(got, "question/q-1  Ship the page?") {
		t.Errorf("id-first chip %q", got)
	}
}

func choiceChooser(t *testing.T, choices []Choice) (*Runtime, *Chooser) {
	t.Helper()
	rows := make([]ChooserRow, len(choices))
	for i, c := range choices {
		rows[i] = c.Row()
	}
	r, _, c, _ := chooserTest(t, ChooserSpec{Rows: rows})
	return r, c
}

// picker.show-ids off hides the dim id from the drawn row; the filter still
// matches it.
func TestPickerShowIDsSetting(t *testing.T) {
	t.Parallel()
	r, c := choiceChooser(t, []Choice{thingChoice(), {ID: "question/q-2", Label: "Rename it?"}})
	if v := ansi.Strip(c.View(60, 8).Text); !strings.Contains(v, "Ship the page?  q-1") {
		t.Fatalf("default:\n%s", v)
	}
	if err := r.Settings().Set(ChooserShowIDsSetting, false); err != nil {
		t.Fatal(err)
	}
	v := ansi.Strip(c.View(60, 8).Text)
	if !strings.Contains(v, "Ship the page?") || strings.Contains(v, "q-1") || strings.Contains(v, "q-2") {
		t.Fatalf("show-ids off:\n%s", v)
	}
	c.SetQuery("q-2")
	if ids := chooserIDs(c); !slices.Equal(ids, []string{"question/q-2"}) {
		t.Errorf("the filter should still read the id: %v", ids)
	}
}

// picker.columns off draws the label alone for a row with cells.
func TestPickerColumnsSetting(t *testing.T) {
	t.Parallel()
	a := Choice{ID: "a", Label: "alpha", Cells: []string{"alpha", "seeding", "2d"}}
	b := Choice{ID: "b", Label: "beta", Cells: []string{"beta", "stalled", "5h"}}
	r, c := choiceChooser(t, []Choice{a, b})
	v := ansi.Strip(c.View(60, 8).Text)
	if !strings.Contains(v, "alpha  seeding  2d") && !strings.Contains(v, "alpha  seeding") {
		t.Fatalf("default draws columns:\n%s", v)
	}
	if !strings.Contains(v, "beta   stalled") {
		t.Errorf("columns not aligned:\n%s", v)
	}
	if err := r.Settings().Set(ChooserColumnsSetting, false); err != nil {
		t.Fatal(err)
	}
	v = ansi.Strip(c.View(60, 8).Text)
	if strings.Contains(v, "seeding") || !strings.Contains(v, "alpha") {
		t.Errorf("columns off:\n%s", v)
	}
}

// A record field of any kind opens its choices as Choice.Row (no special
// case for a field without a noun).
func TestRecordFieldChoiceRowIsChoiceRow(t *testing.T) {
	t.Parallel()
	c := Choice{ID: "todo", Label: "To do"}
	for _, f := range []RecordField{{Name: "status"}, {Name: "target", Parameter: Parameter{Noun: "question"}}} {
		if got, want := f.choiceRow(c), c.Row(); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %+v, want %+v", f.Name, got, want)
		}
	}
}
