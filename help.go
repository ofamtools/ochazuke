package ochazuke

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// HelpInsertKey opens the bindings help in insert mode, where ? types
// itself: an app row NewModel adds beside ? in normal mode. ctrl+\ is bound
// by no Readline default, no editor, palette, chooser, completion, filter,
// form or outline row, and the terminal is in raw mode, so it sends no
// SIGQUIT.
const HelpInsertKey = "ctrl+\\"

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
// The modal's instance holds a helpState (help_views.go): S cycles the
// views (here, pick, table), n and m choose the noun and mode of the
// hypothetical ones.
func registerBindingHelp(r *Runtime) {
	r.Define("ochazuke.binding")
	r.Register(Operation{Verb: "help", Noun: "ochazuke.bindings", Description: "Open the keys the focus answers to, with the operation each runs and its description", Result: "help row[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		focus := focusOf(c)
		rows, mode, home := BindingHelp(r, focus), ModeState{Name: r.BaseMode()}, helpHome(r, focus)
		inst, err := r.OpenModal(helpID, Payload{})
		if err != nil {
			return nil, err
		}
		h := newHelpState(r, focus, home, mode, rows)
		inst.Component = h
		if err := h.show(); err != nil {
			_ = r.Pop()
			return nil, err
		}
		return rows, nil
	}})
	registerHelpViews(r)
}

// bindingHelpArgs previews arguments without executing the binding. Pattern
// keys are unknown until pressed; keep those sources explicit instead.
func bindingHelpArgs(r *Runtime, b Binding, focus string) Arguments {
	args := Arguments{}
	for name, value := range b.Request.Args {
		args[name] = value
	}
	_, key := SplitKeys(b.Keys)
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

// helpColumns are the help table's columns: the key prefix (the keys
// before the last, Group: shown once per run of rows sharing it) and the
// final key, each as wide as its widest cell, the operation fixed, the
// arguments on one line (cut, at most 22 cells, the first to drop when
// narrow) and the description the rest; the keys, operation and
// description wrap inside their columns; both key columns are pinned, so
// z l scrolls the others. Without a prefix in any row the prefix column is
// left out.
var helpColumns = []Column{
	{Wrap: true, Group: true, Pinned: true},
	{Title: "key", Wrap: true, Pinned: true},
	{Title: "operation", Width: helpOpWidth, Wrap: true},
	{Title: "arguments", Max: 22, Priority: 2},
	{Title: "description", Wrap: true, Priority: 1},
}

// helpItems is one table row per help row, each in its level's section,
// and the columns they draw in (helpColumns, less the prefix column when
// no row has a prefix). Inside a section the single keys come first in
// the order they answer, then each prefix's rows together, the prefixes
// in the order they first answer (a longer prefix after the shorter one it
// extends: g, then g c) and a prefix's rows sorted by final key (keyOrder;
// the generated cancel, esc, comes last).
func helpItems(rows []HelpRow) ([]ListItem, []Column) {
	rows = helpOrder(rows)
	prefixed := slices.ContainsFunc(rows, func(row HelpRow) bool { return row.Prefix != "" })
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		final, _ := helpKey(row.Final)
		args := helpArgsText(row.Args)
		label := strings.Join([]string{row.Key, row.Operation, args, row.Description}, "  ")
		cells := []string{row.Prefix, final, row.Operation, args, row.Description}
		if !prefixed {
			cells = cells[1:]
		}
		items[i] = ListItem{ID: fmt.Sprintf("__bindings-help/%d", i), Label: label, Noun: "ochazuke.binding", Data: row, Cells: cells, Section: row.Section}
	}
	if !prefixed {
		return items, helpColumns[1:]
	}
	return items, helpColumns
}

// helpOrder is rows with each section's rows as helpItems lists them:
// single keys first, then the prefix groups (SplitKeys). The sections keep
// their order and so do the single keys.
func helpOrder(rows []HelpRow) []HelpRow {
	out := make([]HelpRow, 0, len(rows))
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].Section == rows[start].Section {
			end++
		}
		var groups []string
		byPrefix := map[string][]HelpRow{}
		for _, row := range rows[start:end] {
			if row.Prefix == "" {
				out = append(out, row)
				continue
			}
			if _, seen := byPrefix[row.Prefix]; !seen {
				groups = append(groups, row.Prefix)
			}
			byPrefix[row.Prefix] = append(byPrefix[row.Prefix], row)
		}
		// A prefix's groups sit together, the shorter first: g, then g c.
		first := map[string]int{}
		for i, prefix := range groups {
			word := strings.Fields(prefix)[0]
			if _, ok := first[word]; !ok {
				first[word] = i
			}
		}
		slices.SortStableFunc(groups, func(a, b string) int {
			if c := first[strings.Fields(a)[0]] - first[strings.Fields(b)[0]]; c != 0 {
				return c
			}
			return len(strings.Fields(a)) - len(strings.Fields(b))
		})
		for _, prefix := range groups {
			group := byPrefix[prefix]
			slices.SortStableFunc(group, func(a, b HelpRow) int { return keyOrder(a.Final, b.Final) })
			out = append(out, group...)
		}
		start = end
	}
	return out
}

// keyOrder sorts final keys as a reader looks for them: the characters
// first, case folded with the lower case before its capital (n, N, o),
// then the named keys (enter, esc, ctrl+x) in order.
func keyOrder(a, b string) int {
	if named := utf8.RuneCountInString(a) > 1; named != (utf8.RuneCountInString(b) > 1) {
		if named {
			return 1
		}
		return -1
	}
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return -strings.Compare(a, b)
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
