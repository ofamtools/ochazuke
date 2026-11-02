package ochazuke

import (
	"errors"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (v *filterViewForm) saveHeight() int {
	if v.saveInput == nil {
		return 0
	}
	return 2
}

func (v *filterViewForm) savedAction(verb string) error {
	if v.saveInput == nil {
		return errors.New("save controls are disabled")
	}
	if err := v.e.flush(); err != nil {
		return err
	}
	name := strings.TrimSpace(string(v.saveInput.Text))
	if _, err := v.l.r.Execute(Request{Noun: "ochazuke.list", Verb: verb, Args: Arguments{"id": v.l.id, "name": name}}); err != nil {
		return err
	}
	v.seen = v.signature()
	return nil
}

func (v *filterViewForm) saveBlock(s Styles, width int) Block {
	name := strings.TrimSpace(string(v.saveInput.Text))
	label := "Save as · Enter saves"
	saved, err := v.l.SavedFilters()
	if err != nil {
		label = "Save · " + err.Error()
	}
	for _, f := range saved {
		if f.Name == name {
			label = "Replace “" + name + "” · Enter overwrites · ctrl+alt+x deletes user filter"
			break
		}
	}
	if v.l.savedFilter != "" {
		label = "Applied: " + v.l.savedFilter + " · " + label
	}
	b := InputBlock(v.saveInput, width, 1)
	v.l.r.PlacedAt(v.saveInput.ID, b.Text, 0, 1)
	if v.l.r.Focus != v.saveInput.ID {
		b.Cursor = nil
	}
	if b.Cursor != nil {
		b.Cursor = &Pos{X: b.Cursor.X, Y: b.Cursor.Y + 1}
	}
	return Block{Text: s.Muted.Render(ansi.Truncate(label, width, "…")) + "\n" + b.Text, Cursor: b.Cursor}
}
