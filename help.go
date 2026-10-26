package ochazuke

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
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
		list, err := r.AddList(helpListID, helpItems(rows), Parent(helpID))
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
			if b.Pattern != "" && (part == SourceKey || part == SourceDigit || part == SourceText) {
				values[i] = "$" + part
			} else {
				values[i] = r.argument(part, ev)
			}
		}
		if len(values) == 1 {
			args[name] = values[0]
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
// wraps into it, and the description takes the rest of the width.
const helpOpWidth = 18

// helpItems is one list row per help row; the view lays the columns out at
// the width it is given.
func helpItems(rows []HelpRow) []ListItem {
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		label := strings.Join([]string{row.Key, row.Operation, helpArgsText(row.Args), row.Description}, "  ")
		items[i] = ListItem{ID: fmt.Sprintf("__bindings-help/%d", i), Label: label, Noun: "binding", Data: row}
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
	label := map[string]string{Alpha: "a-z", Printable: "char"}[class]
	if label == "" {
		label = class
	}
	for _, key := range except {
		label += " -" + key
	}
	return label
}

// helpColumn wraps text into a column of width cells.
func helpColumn(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	return strings.Split(ansi.Wrap(text, width, "."), "\n")
}

// bindingHelpView renders key, operation, arguments and description columns.
// Arguments stay on one line and truncate; the other columns wrap.
func bindingHelpView(r *Runtime, _ *Instance, width, height int) Block {
	list := r.List(helpListID)
	if list == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	items := list.items
	if len(items) == 0 {
		return Text(list.View(width, height))
	}
	keyWidth := 1
	for _, it := range items {
		if key, literal := helpKey(it.Data.(HelpRow).Key); literal {
			keyWidth = max(keyWidth, ansi.StringWidth(key))
		}
	}
	const mark, gap = 2, 2
	opWidth := min(helpOpWidth, max(1, width-mark-keyWidth-gap))
	argsWidth := min(22, max(0, (width-mark-keyWidth-gap-opWidth-gap-gap)/2))
	descWidth := width - mark - keyWidth - gap - opWidth - gap
	if argsWidth > 0 {
		descWidth -= argsWidth + gap
	}
	lines := make([][]string, len(items))
	headed := make([]bool, len(items))
	heading := lipgloss.NewStyle().Bold(true).Underline(true)
	section := ""
	for i, it := range items {
		row := it.Data.(HelpRow)
		if row.Section != section {
			// A section label opens each level's rows: the noun, or movement.
			section = row.Section
			headed[i] = true
			lines[i] = append(lines[i], heading.Render(section))
		}
		key, _ := helpKey(row.Key)
		cols := [][]string{helpColumn(key, keyWidth), helpColumn(row.Operation, opWidth), helpColumn(row.Description, descWidth)}
		n := max(len(cols[0]), len(cols[1]), len(cols[2]))
		for j := range n {
			cell := func(c, w int) string {
				text := ""
				if j < len(cols[c]) {
					text = cols[c][j]
				}
				return text + strings.Repeat(" ", max(0, w-ansi.StringWidth(text)))
			}
			prefix := "  "
			if j == 0 && i == list.selected {
				prefix = "> "
			}
			line := prefix + cell(0, keyWidth) + "  " + cell(1, opWidth)
			if argsWidth > 0 {
				args := ""
				if j == 0 {
					args = ansi.Truncate(helpArgsText(row.Args), argsWidth, "…")
				}
				line += "  " + args + strings.Repeat(" ", max(0, argsWidth-ansi.StringWidth(args)))
			}
			if descWidth > 0 {
				line += "  " + cell(2, descWidth)
			}
			lines[i] = append(lines[i], strings.TrimRight(line, " "))
		}
	}
	visible := height
	if len(items) > 1 && height > 1 {
		total := 0
		for _, l := range lines {
			total += len(l)
		}
		if total > height {
			visible--
		}
	}
	list.offset = min(list.offset, list.selected)
	fits := func(from int) bool {
		n := 0
		if visible > 1 && !headed[from] {
			n++
		}
		for i := from; i <= list.selected; i++ {
			n += len(lines[i])
		}
		return n <= visible
	}
	for list.offset < list.selected && !fits(list.offset) {
		list.offset++
	}
	var out []string
	if visible > 1 && !headed[list.offset] {
		out = append(out, heading.Render(items[list.offset].Data.(HelpRow).Section))
	}
	end := list.offset
	for i := list.offset; i < len(items) && len(out) < visible; i++ {
		for j, line := range lines[i] {
			if len(out) == visible {
				break
			}
			line = ansi.Truncate(line, width, "…")
			if i == list.selected && list.Focused() && !(j == 0 && headed[i]) {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			out = append(out, line)
		}
		end = i + 1
	}
	if visible < height {
		out = append(out, fmt.Sprintf("%d–%d / %d", list.offset+1, end, len(items)))
	}
	return Text(strings.Join(layoutRows(strings.Join(out, "\n"), width, height), "\n"))
}
