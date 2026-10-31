package ochazuke

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRecordInvertedToneCoversFieldKinds(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, tc := range []struct {
		name, value string
		field       RecordField
		words       []string
	}{
		{"text", "sample", RecordField{}, []string{"sample"}},
		{"multiline", "first\nsecond", RecordField{Multiline: true}, []string{"first", "second"}},
		{"placeholder", "", RecordField{Placeholder: "type here"}, []string{"type here"}},
		{"choice", "first", RecordField{Parameter: Parameter{Enum: []string{"first", "second"}}}, []string{"first"}},
		{"chips", ChipsValue([]string{"first"}), RecordField{Chips: true, Parameter: Parameter{Enum: []string{"first", "second"}}}, []string{"● first", "○ second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRuntime()
			r.Config().Disable()
			tc.field.Name, tc.field.Invertible = "field", true
			e, err := r.AddRecordEditor("record", RecordSpec{Fields: []RecordField{tc.field}, Load: func() (Record, error) {
				return Record{"field": tc.value}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			e.Styles = NewStyles(Appearance{Theme: "dark"})
			if err := r.SetFocus(e.field("field").id); err != nil {
				t.Fatal(err)
			}
			plain := ansi.Strip(e.Block(60, 20).Text)
			if err := e.invert(e.field("field")); err != nil {
				t.Fatal(err)
			}
			body := e.Block(60, 20).Text
			for _, word := range append([]string{"field · not"}, tc.words...) {
				assertRecordRunColor(t, body, word, "#FF9B70")
			}
			if got := ansi.Strip(body); got != strings.Replace(plain, "field · is", "field · not", 1) {
				t.Fatal("the tone changed field content or layout")
			}
			if err := r.Settings().Set(FormsInvertedToneSetting, false); err != nil {
				t.Fatal(err)
			}
			if body := e.Block(60, 20).Text; strings.Contains(body, fg("#FF9B70")) || !strings.Contains(ansi.Strip(body), "field · not") {
				t.Fatal("opt-out changed inversion or kept its colour")
			}
			if err := r.Settings().Set(FormsInvertedToneSetting, true); err != nil {
				t.Fatal(err)
			}
			if err := e.invert(e.field("field")); err != nil {
				t.Fatal(err)
			}
			if body := e.Block(60, 20).Text; ansi.Strip(body) != plain || strings.Contains(body, fg("#FF9B70")) {
				t.Fatal("normal field retained inverted styling")
			}
		})
	}
}

func assertRecordRunColor(t *testing.T, body, word, hex string) {
	t.Helper()
	run := regexp.MustCompile(`\x1b\[([0-9;:]*)m\s*` + regexp.QuoteMeta(word))
	for _, match := range run.FindAllStringSubmatch(body, -1) {
		if strings.Contains(";"+match[1]+";", ";"+fg(hex)+";") {
			return
		}
	}
	t.Fatalf("%q does not draw in %s: %q", word, hex, body)
}

func TestRecordInvertedToneThemesAndOverrides(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, theme := range BuiltinThemes {
		style := NewStyles(Appearance{Theme: theme, Dark: true}).Tone(ToneRecordInverted)
		if theme == "mono" {
			if !isNoColor(style) {
				t.Fatal("mono gained a colour")
			}
			continue
		}
		want := "#FF9B70"
		if theme == "light" {
			want = "#B54708"
		}
		if !sameColor(style.GetForeground(), lipgloss.Color(want)) {
			t.Fatalf("%s inverted foreground: %v", theme, style.GetForeground())
		}
	}
	schemes, problems := ParseSchemes([]byte(`{"dark":{"tones":{"record.inverted":{"fg":"#AB1234","bold":true}}}}`))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	r := NewRuntime()
	r.Config().Disable()
	e, err := r.AddRecordEditor("record", RecordSpec{Fields: []RecordField{{Name: "field", Invertible: true}}, Load: func() (Record, error) {
		return Record{"field": "sample", "field!": "true"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	e.Styles = NewStyles(Appearance{Theme: "dark", Schemes: NewSchemeSet(schemes)})
	for _, word := range []string{"field · not", "sample"} {
		assertRecordRunColor(t, e.Block(60, 10).Text, word, "#AB1234")
	}
	t.Setenv("NO_COLOR", "1")
	e.Styles = NewStyles(Appearance{Theme: "dark", Schemes: NewSchemeSet(schemes)})
	if strings.Contains(e.Block(60, 10).Text, "38;") {
		t.Fatal("NO_COLOR gained a foreground")
	}
}
