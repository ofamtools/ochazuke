package ochazuke

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// helpID is the bindings help modal; helpListID the read-only list in it.
const (
	helpID     = "bindings-help"
	helpListID = "__bindings-help/list"
)

// registerBindingHelp adds bindings.help: a modal listing BindingHelp for
// the focus it opens over, one row per binding (key, noun.verb, arguments,
// description), grouped by level from the innermost instance outward, each
// level under its noun's label and the motions last under "movement". The
// rows are a list of the "binding" noun, which has no verbs: j/k move,
// esc closes (the modal noun's row). NewModel binds ? to it in normal mode.
func registerBindingHelp(r *Runtime) {
	r.Define("binding")
	r.Register(Operation{Verb: "help", Noun: "bindings", Description: "Open the keys the focus answers to, with the operation each runs and its description", Result: "help row[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		rows := BindingHelp(r, focusOf(c))
		if _, err := r.OpenModal(helpID, Payload{}); err != nil {
			return nil, err
		}
		list, err := r.AddList(helpListID, helpItems(rows), Parent(helpID), Columns(helpColumns...))
		if err != nil {
			_ = r.Pop()
			return nil, err
		}
		return rows, list.Focus()
	}})
}

// bindingHelpArgs previews arguments without executing the binding. Pattern
// keys are unknown until pressed; keep those sources explicit instead.
func bindingHelpArgs(r *Runtime, b Binding, focus string) Arguments {
	args := Arguments{}
	for name, value := range b.Request.Args {
		args[name] = value
	}
	key := ""
	if keys := strings.Fields(b.Keys); len(keys) > 0 {
		key = keys[len(keys)-1]
	}
	ev := KeyEvent{Key: MacroKey{Key: key}, Mode: r.Mode, Focus: focus}
	for name, source := range b.From {
		parts := strings.Fields(source)
		values := make([]any, len(parts))
		for i, part := range parts {
			if b.Pattern != "" && (part == SourceKey || part == SourceDigit || part == SourceText || part == SourceLetter) {
				values[i] = "$" + part
			} else {
				values[i] = r.argument(part, ev)
			}
		}
		if len(values) == 1 {
			if values[0] != nil {
				args[name] = values[0]
			}
		} else {
			var texts []string
			for _, value := range values {
				if value != nil {
					texts = append(texts, fmt.Sprint(value))
				}
			}
			args[name] = strings.Join(texts, " ")
		}
	}
	return args
}

func helpArgsText(args Arguments) string {
	if len(args) == 0 {
		return ""
	}
	text, err := json.Marshal(args)
	if err != nil {
		return safeText(fmt.Sprint(args))
	}
	return string(text)
}

// helpOpWidth is the help's fixed operation column; a longer operation
// wraps into it.
const helpOpWidth = 18

// helpColumns are the help table's columns: the key as wide as its widest
// key, the operation fixed, the arguments on one line (cut, at most 22
// cells, the first to drop when narrow) and the description the rest; the
// key, operation and description wrap inside their columns.
var helpColumns = []Column{
	{Title: "key", Wrap: true},
	{Title: "operation", Width: helpOpWidth, Wrap: true},
	{Title: "arguments", Max: 22, Priority: 2},
	{Title: "description", Wrap: true, Priority: 1},
}

// helpItems is one table row per help row, each in its level's section.
func helpItems(rows []HelpRow) []ListItem {
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		key, _ := helpKey(row.Key)
		args := helpArgsText(row.Args)
		label := strings.Join([]string{row.Key, row.Operation, args, row.Description}, "  ")
		items[i] = ListItem{ID: fmt.Sprintf("__bindings-help/%d", i), Label: label, Noun: "binding", Data: row, Cells: []string{key, row.Operation, args, row.Description}, Section: row.Section}
	}
	return items
}

// helpKey is the key column's text for a row: a literal key as it is, a
// pattern row (rowKey's "<digit-0>") in its short form.
func helpKey(key string) (text string, literal bool) {
	inner, ok := strings.CutPrefix(key, "<")
	if inner, ok2 := strings.CutSuffix(inner, ">"); ok && ok2 && isPattern(inner) {
		return patternLabel(inner), false
	}
	return key, true
}

// patternLabel is a key class in a few cells: digit is 0-9 (1-9 without
// 0), alpha a-z, printable char, one-of:a b the keys joined by /; other
// excluded keys follow as -x.
func patternLabel(pattern string) string {
	if set, ok := strings.CutPrefix(pattern, "one-of:"); ok {
		return strings.Join(strings.Fields(set), "/")
	}
	class, minus, _ := strings.Cut(pattern, "-")
	var except []string
	if minus != "" {
		except = strings.Split(minus, "-")
	}
	if class == Digit {
		var keep []int
		for d := 0; d <= 9; d++ {
			if !slicesContains(except, strconv.Itoa(d)) {
				keep = append(keep, d)
			}
		}
		if len(keep) > 0 && keep[len(keep)-1]-keep[0] == len(keep)-1 {
			return fmt.Sprintf("%d-%d", keep[0], keep[len(keep)-1])
		}
		parts := make([]string, len(keep))
		for i, d := range keep {
			parts[i] = strconv.Itoa(d)
		}
		return strings.Join(parts, "/")
	}
	label := map[string]string{Alpha: "a-z", Upper: "A-Z", CtrlAlpha: "ctrl+a-z", Printable: "char"}[class]
	if label == "" {
		label = class
	}
	for _, key := range except {
		label += " -" + key
	}
	return label
}

// bindingHelpView draws the help list: a table (helpColumns) whose rows
// are grouped under their sections.
func bindingHelpView(r *Runtime, _ *Instance, width, height int) Block {
	list := r.List(helpListID)
	if list == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	return Text(list.View(width, height))
}
