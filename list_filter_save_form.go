package ochazuke

import (
	"errors"
	"slices"
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
	label := "Save as"
	saved, err := v.l.SavedFilters()
	if err != nil {
		label = "Save · " + err.Error()
	}
	for _, f := range saved {
		if f.Name == name {
			label = "Replace “" + name + "”"
			break
		}
	}
	if v.l.vs.Saved != "" {
		label = "Applied: " + v.l.vs.Saved + " · " + label
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

func (v *filterViewForm) deleteSaved() error {
	filters, err := v.l.SavedFilters()
	if err != nil {
		return err
	}
	user, err := v.l.r.Settings().savedFilters()
	if err != nil {
		return err
	}
	// Only the user's own definitions can be deleted; an app default is not
	// offered, since picking it could only fail.
	var rows []ChooserRow
	for _, f := range filters {
		if slices.ContainsFunc(user, f.sameDefinition) {
			rows = append(rows, ChooserRow{ID: f.Name, Label: f.Name})
		}
	}
	_, err = v.l.r.OpenChooser(ChooserSpec{Kind: "list-filter-delete", Title: "Delete saved filter", Plural: "saved filters", Rows: rows, NoPropFilters: true, NoSort: true, Choose: func(row ChooserRow) Result {
		_, err := v.l.r.Execute(Request{Noun: "ochazuke.list", Verb: "filter-saved-delete", Args: Arguments{"id": v.l.id, "name": row.ID}})
		if err != nil {
			return Failed(err)
		}
		return Done(nil)
	}})
	return err
}
