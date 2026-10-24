package ochazuke

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// helpID is the bindings help modal; helpListID the read-only list in it.
const (
	helpID     = "bindings-help"
	helpListID = "__bindings-help/list"
)

// registerBindingHelp adds bindings.help: a modal listing BindingHelp for
// the focus it opens over, one row per binding (key, noun.verb,
// description), grouped by noun from the innermost instance outward. The
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

// helpItems lays the rows out in aligned columns: key, operation,
// description.
func helpItems(rows []HelpRow) []ListItem {
	keyWidth, opWidth := 0, 0
	for _, row := range rows {
		keyWidth = max(keyWidth, ansi.StringWidth(row.Key))
		opWidth = max(opWidth, ansi.StringWidth(row.Operation))
	}
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		label := fmt.Sprintf("%-*s  %-*s  %s", keyWidth, row.Key, opWidth, row.Operation, row.Description)
		items[i] = ListItem{ID: fmt.Sprintf("__bindings-help/%d", i), Label: label, Noun: "binding", Data: row}
	}
	return items
}

// bindingHelpView renders the help modal's list.
func bindingHelpView(r *Runtime, _ *Instance, width, height int) Block {
	if list := r.List(helpListID); list != nil {
		return Text(list.View(width, height))
	}
	return Text("")
}
