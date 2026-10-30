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

// helpID is the bindings help modal; helpTreeID the read-only tree in it
// (R6.10: a prefix row opens onto its sequences, a sequence onto its
// steps) and helpListID the tree's list, the rows drawn.
const (
	helpID     = "bindings-help"
	helpTreeID = "__bindings-help"
	helpListID = helpTreeID + "/list"
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
	// enter opens or closes a help row as the tree's l and h do (R6.10),
	// in the views drawn as a tree; hidden, so the footer is unchanged
	// (bindings/help.json).
	r.DefineGuard("help-tree", func(r *Runtime, _ KeyEvent) bool { return r.Tree(helpTreeID) != nil })
	r.Register(Operation{Verb: "help", Noun: "ochazuke.bindings", Description: "Open the keys the focus answers to, with the operation each runs and its description", Result: "help row[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		focus := focusOf(c)
		mode := ModeState{Name: r.BaseMode()}
		// Opened again while it is the top modal (? or :bindings help
		// inside the help), the help shows again in its place, for the
		// focus and mode it opened over (ReplaceOpen, R4.8).
		if r.TopModal() == helpID {
			focus, mode = r.ModalReturn(helpID)
		}
		rows, home := BindingHelp(r, focus), helpHome(r, focus)
		inst, err := r.OpenModal(helpID, Payload{}, ReplaceOpen())
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

// helpColumns are the help table's columns: the key, the whole sequence
// (R6.10: "ctrl+a v", "g c"), as wide as its widest cell, the operation
// fixed, the arguments on one line (cut, at most 22 cells, the first to
// drop when narrow) and the description the rest; the key, operation and
// description wrap inside their columns; the key column is pinned, so
// z l scrolls the others.
var helpColumns = []Column{
	{Title: "key", Wrap: true, Pinned: true},
	{Title: "operation", Width: helpOpWidth, Wrap: true},
	{Title: "arguments", Max: 22, Priority: 2},
	{Title: "description", Wrap: true, Priority: 1},
}

// helpItems is one table row per help row, each in its level's section,
// and the columns they draw in (helpColumns), in helpOrder.
func helpItems(rows []HelpRow) ([]ListItem, []Column) {
	rows = helpOrder(rows)
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		key := helpKeyText(row)
		args := helpArgsText(row.Args)
		label := strings.Join([]string{key, row.Operation, args, row.Description}, "  ")
		cells := []string{key, row.Operation, args, row.Description}
		items[i] = ListItem{ID: fmt.Sprintf("__bindings-help/%d", i), Label: label, Noun: "ochazuke.binding", Data: row, Cells: cells, Section: row.Section}
	}
	return items, helpColumns
}

// helpNodes are the help's rows as a tree (R6.10): a prefix row (Group)
// holds the rows listed Under it, a sequence row its steps, one node per
// key, each step's operation and what it does there. collapsed are the
// nodes that start closed: every sequence's steps (a prefix row starts
// open, so the help reads as the flat list it was, its sequences under
// it). A row's label, which the filter reads, holds its steps' and its
// sequences' text too, so a closed row is found by what is inside it.
func helpNodes(rows []HelpRow) (nodes []TreeNode, collapsed []string) {
	items, _ := helpItems(rows)
	parent := map[string]string{} // section + key of a prefix row -> node id
	labels := make([]string, len(items))
	parents := make([]int, len(items))
	index := map[string]int{}
	for i, item := range items {
		row := item.Data.(HelpRow)
		index[item.ID] = i
		parents[i] = -1
		labels[i] = item.Label
		node := TreeNode{ID: item.ID, Label: item.Label, Noun: item.Noun, Data: row, Cells: item.Cells, Section: item.Section}
		if row.Under != "" {
			node.Parent = parent[row.Section+"\x00"+row.Under]
			if j, ok := index[node.Parent]; ok {
				parents[i] = j
			}
		}
		if row.Group {
			parent[row.Section+"\x00"+row.Key] = item.ID
		}
		nodes = append(nodes, node)
		if row.Group || len(row.Steps) == 0 {
			continue
		}
		collapsed = append(collapsed, item.ID)
		for k, step := range row.Steps {
			text := step.Description
			id := fmt.Sprintf("%s/step%d", item.ID, k)
			cells := []string{step.Key, step.Operation, "", text}
			label := strings.Join([]string{step.Key, step.Operation, text}, "  ")
			labels[i] += "  " + label
			nodes = append(nodes, TreeNode{ID: id, Parent: item.ID, Label: label, Noun: "ochazuke.binding", Data: step, Cells: cells, Section: item.Section})
		}
	}
	// A prefix row's label gathers its sequences' (innermost first, so a
	// longer prefix's own sequences reach the outer row).
	for i := len(items) - 1; i >= 0; i-- {
		if p := parents[i]; p >= 0 {
			labels[p] += "  " + labels[i]
		}
	}
	for n := range nodes {
		if i, ok := index[nodes[n].ID]; ok {
			nodes[n].Label = labels[i]
		}
	}
	return nodes, collapsed
}

// helpKeyText is a row's key cell: the whole sequence, a pattern final key
// in its short form ("ctrl+a 0-9").
func helpKeyText(row HelpRow) string {
	final, _ := helpKey(row.Final)
	return strings.TrimSpace(row.Prefix + " " + final)
}

// helpOrder is rows with each section's rows as helpItems lists them
// (R6.9, R6.10): at the top of a section the keys that are no prefix first,
// in the order they answer, then the prefix rows (Group) in the order they
// answer; under each prefix row the rows listed Under it, sorted by their
// final key (keyOrder: characters case folded, then named keys, so the
// generated cancel, esc, comes last), each prefix row among them followed
// by its own. The sections keep their order.
func helpOrder(rows []HelpRow) []HelpRow {
	out := make([]HelpRow, 0, len(rows))
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].Section == rows[start].Section {
			end++
		}
		var plain, groups []HelpRow
		under := map[string][]HelpRow{}
		for _, row := range rows[start:end] {
			switch {
			case row.Under != "":
				under[row.Under] = append(under[row.Under], row)
			case row.Group:
				groups = append(groups, row)
			default:
				plain = append(plain, row)
			}
		}
		out = append(out, plain...)
		for _, row := range groups {
			out = appendUnder(out, row, under)
		}
		start = end
	}
	return out
}

// appendUnder appends row and, for a prefix row, the rows under it in
// key order, each followed by its own.
func appendUnder(out []HelpRow, row HelpRow, under map[string][]HelpRow) []HelpRow {
	out = append(out, row)
	if !row.Group {
		return out
	}
	kids := slices.Clone(under[row.Key])
	slices.SortStableFunc(kids, func(a, b HelpRow) int { return keyOrder(a.Final, b.Final) })
	for _, kid := range kids {
		out = appendUnder(out, kid, under)
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
