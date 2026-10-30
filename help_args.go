package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// The tones a binding's arguments draw in, in the ? help (its arguments
// column and the table view's detail): the name bold, the value plain in
// every built-in theme; a themes.json "tones" row restyles either.
const (
	ToneHelpArgKey   = "help.arg.key"
	ToneHelpArgValue = "help.arg.value"
)

// argLine is one argument as the help shows it: its name and its value
// as text.
type argLine struct{ key, value string }

// helpArgLines are a binding's arguments one per line, by name: a nested
// object is flattened into dotted names (request.verb), as a row field is
// named (Binding.FromRow), so each line is one name and one plain value;
// an array of plain values is one line, its values joined by ", ", and an
// array holding objects is flattened by index (steps.0.key). A string
// draws without quotes, the empty string as "", null as null, an empty
// object or array as {} or [].
func helpArgLines(args Arguments) []argLine {
	var out []argLine
	for _, name := range slices.Sorted(maps.Keys(args)) {
		out = flattenArg(out, name, plainValue(args[name]))
	}
	return out
}

// plainValue is v as JSON decodes it (map[string]any, []any, float64,
// string, bool, nil), so a Go map, slice or struct flattens as its JSON
// does; a value JSON cannot carry stays as it is.
func plainValue(v any) any {
	switch v.(type) {
	case nil, string, bool, float64, map[string]any, []any:
		return v
	}
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(data, &out) != nil {
		return v
	}
	return out
}

func flattenArg(out []argLine, key string, v any) []argLine {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			return append(out, argLine{key, "{}"})
		}
		for _, name := range slices.Sorted(maps.Keys(t)) {
			out = flattenArg(out, key+"."+name, plainValue(t[name]))
		}
		return out
	case []any:
		if len(t) == 0 {
			return append(out, argLine{key, "[]"})
		}
		texts := make([]string, len(t))
		for i, item := range t {
			item = plainValue(item)
			switch item.(type) {
			case map[string]any, []any:
				for i, item := range t {
					out = flattenArg(out, key+"."+strconv.Itoa(i), plainValue(item))
				}
				return out
			}
			texts[i] = argText(item)
		}
		return append(out, argLine{key, strings.Join(texts, ", ")})
	}
	return append(out, argLine{key, argText(v)})
}

// argText is a plain value as text, on one line.
func argText(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		if t == "" {
			return `""`
		}
		return modalLabel(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return modalLabel(fmt.Sprint(v))
}

// helpArgsCell is the help's arguments cell (column cell): a line per
// argument, the names padded so the values start in one column, and the
// tones of each name and value. No arguments is the empty cell.
func helpArgsCell(args Arguments, cell int) (string, []ToneSpan) {
	lines := helpArgLines(args)
	if len(lines) == 0 {
		return "", nil
	}
	widest := 0
	for _, l := range lines {
		widest = max(widest, len([]rune(l.key)))
	}
	var b strings.Builder
	var tones []ToneSpan
	at := 0
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
			at++
		}
		key := len([]rune(l.key))
		pad := widest - key + 1
		b.WriteString(l.key + strings.Repeat(" ", pad) + l.value)
		tones = append(tones, SpanTone(cell, at, at+key, ToneHelpArgKey))
		value := len([]rune(l.value))
		tones = append(tones, SpanTone(cell, at+key+pad, at+key+pad+value, ToneHelpArgValue))
		at += key + pad + value
	}
	return b.String(), tones
}

// helpArgsBlock is the arguments as styled lines, a line per argument
// after indent, the values in one column; the table view's detail draws
// them under their row. A tone without a colour keeps the text's.
func helpArgsBlock(s Styles, args Arguments, indent string) []string {
	lines := helpArgLines(args)
	widest := 0
	for _, l := range lines {
		widest = max(widest, len([]rune(l.key)))
	}
	base := lipgloss.NewStyle()
	if s.text != nil && !s.plain {
		base = base.Foreground(s.text)
	}
	key := s.Tone(ToneHelpArgKey).Inherit(base)
	value := s.Tone(ToneHelpArgValue).Inherit(base)
	out := make([]string, len(lines))
	for i, l := range lines {
		pad := strings.Repeat(" ", widest-len([]rune(l.key))+1)
		out[i] = indent + key.Render(l.key) + pad + value.Render(l.value)
	}
	return out
}

// helpArgsLabel is the arguments on one line, for a row's filter label.
func helpArgsLabel(args Arguments) string {
	var parts []string
	for _, l := range helpArgLines(args) {
		parts = append(parts, l.key+" "+l.value)
	}
	return strings.Join(parts, "  ")
}
