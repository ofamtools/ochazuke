package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// Completion of a :set line (Vim's cmdline-completion and
// complete-set-option), the choices source of settings.apply's line
// (settings.complete). The word under the cursor (setCursorAt) decides
// what is offered:
//
//	a name           the settings whose name starts with it, then noNAME
//	                 and invNAME for the booleans whose spelling does (a
//	                 typed word only), and all and all& as the first word
//	opt=, opt:       empty, the current value first; an enum's values,
//	                 true and false for a boolean, narrowed by what is typed
//	opt-=            a list's items (a text's whole value when empty)
//	anything else    nothing
//
// Each choice's id is the line up to the cursor with the completion in
// place (Parameter.Rest), its label the completion and what it is.

// settingsCompletions are the completions of a :set line at cursor (a
// rune offset).
func settingsCompletions(s *Settings, line string, cursor int) []Choice {
	s.ensure()
	at := setCursorAt(line, cursor)
	rs := []rune(line)
	before := string(rs[:at.Start])
	var out []Choice
	seen := map[string]bool{}
	add := func(text, label string) {
		if seen[text] {
			return
		}
		seen[text] = true
		out = append(out, Choice{ID: before + text, Label: label})
	}
	declared := s.Declared()
	switch at.Part {
	case setPartName:
		typed := strings.ToLower(at.Typed)
		slices.SortFunc(declared, func(a, b Setting) int { return strings.Compare(a.Name, b.Name) })
		for _, one := range declared {
			if hasPrefixFold(one.Name, typed) {
				add(one.Name, completionLabel(s, one, one.Name))
			}
		}
		for _, prefix := range setNamePrefixes {
			if typed == "" {
				break
			}
			for _, one := range declared {
				if one.Type == SettingBoolean && hasPrefixFold(prefix+one.Name, typed) {
					add(prefix+one.Name, prefix+one.Name+" · "+map[string]string{"no": "off", "inv": "invert"}[prefix])
				}
			}
		}
		if at.Word == 0 {
			forms := make([]string, 0, len(setLineForms))
			for form := range setLineForms {
				forms = append(forms, form)
			}
			slices.Sort(forms)
			for _, form := range forms {
				if hasPrefixFold(form, typed) {
					add(form, form+" · "+map[string]string{"all": "every setting", "all&": "every setting to its default"}[form])
				}
			}
		}
	case setPartValue:
		w := at.Words[at.Word]
		one, prefix, err := resolveSetName(w.Name, declared)
		if err != nil || prefix != "" {
			return nil
		}
		current := s.current(one)
		class := setClassOf(one)
		typed := at.Typed
		offer := func(values []string) {
			for _, v := range values {
				if hasPrefixFold(v, typed) {
					add(v, v)
				}
			}
		}
		currentText := setValueText(one, current)
		switch w.Op {
		case "=", ":":
			if typed == "" {
				add(currentText, currentText+" · current")
			}
			switch class {
			case SettingBoolean:
				offer([]string{"true", "false"})
			case setEnum:
				offer(one.Enum)
			}
		case "-=":
			switch class {
			case SettingList:
				items, _ := current.([]string)
				escaped := make([]string, len(items))
				for i, it := range items {
					escaped[i] = setEscape(it, true)
				}
				offer(escaped)
			case SettingString:
				if typed == "" && currentText != "" {
					add(currentText, currentText+" · current")
				}
			}
		}
	}
	return out
}

// completionLabel is a name's label: the name, its class and its value.
func completionLabel(s *Settings, one Setting, name string) string {
	class := setClassOf(one)
	if class == setEnum {
		class = "one of " + strings.Join(one.Enum, ", ")
	}
	return fmt.Sprintf("%s · %s · %s", name, class, setValueText(one, s.current(one)))
}

// setValueText is a value as the line reads it back after =: a boolean
// true or false, a list comma-separated, text with its spaces, quotes and
// backslashes escaped.
func setValueText(one Setting, v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case []string:
		items := make([]string, len(t))
		for i, it := range t {
			items[i] = setEscape(it, true)
		}
		return strings.Join(items, ",")
	case string:
		return setEscape(t, false)
	}
	return settingWordArg(v)
}

// setEscape escapes what setDecode reads specially: a backslash, a space,
// a double quote, and in a list item a comma.
func setEscape(text string, item bool) string {
	var b strings.Builder
	for _, r := range text {
		if r == '\\' || r == ' ' || r == '\t' || r == '"' || item && r == ',' {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// registerSettingsComplete registers settings.complete, the :set line's
// completion source.
func registerSettingsComplete(r *Runtime) {
	r.Register(Operation{Verb: "complete", Noun: "ochazuke.settings", HistoryKind: HistoryFetch,
		Description: "Complete a :set line at the cursor: setting names (noNAME and invNAME for on/off ones, all, all&), the current value after =, an enum's values, true and false, a list's items after -=; each id is the line up to the cursor completed",
		Parameters: map[string]Parameter{
			ChoicesQueryParameter:  {Type: "string", Description: "The :set line (the text after set)"},
			ChoicesCursorParameter: {Type: "integer", Description: "The cursor, a character offset into the line (default: its end)"},
		},
		Positional: []string{ChoicesQueryParameter, ChoicesCursorParameter}, Result: "choice[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			line := StringArg(a, ChoicesQueryParameter, "")
			out := settingsCompletions(r.Settings(), line, IntArg(a, ChoicesCursorParameter, len([]rune(line))))
			if out == nil {
				out = []Choice{}
			}
			return out, nil
		}})
}
