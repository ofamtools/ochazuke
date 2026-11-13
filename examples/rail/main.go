// Command rail is a page with a rail: a list of projects as the main
// area and, at its right, the rail listing the selected project's tasks
// (split by kind into Links and Files) and notes, and a list section of
// today's tasks whatever the selection. Z toggles the rail; } enters it,
// { leaves; enter or l unfolds a heading, h folds it; set rail.look float
// (ochazuke.rail.look float, from the palette) for the float look. Try it:
//
//	go run ./examples/rail
//	go run ./examples/rail --smoke     # print both looks at 80, 120 and 200 columns
package main

import (
	"fmt"
	"maps"
	"os"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

func main() {
	m, err := build()
	if err != nil {
		fmt.Fprintln(os.Stderr, "rail:", err)
		os.Exit(1)
	}
	if len(os.Args) == 2 && os.Args[1] == "--smoke" {
		smoke(m)
		return
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "rail:", err)
		os.Exit(1)
	}
}

// mem is an in-memory store of things keyed by id.
func mem(things ...ochazuke.Thing) ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.KindCollection,
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			out := make([]ochazuke.Thing, len(things))
			for i, t := range things {
				out[i] = maps.Clone(t)
			}
			return out, nil
		},
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			if i := slices.IndexFunc(things, func(t ochazuke.Thing) bool { return t["id"] == id }); i >= 0 {
				return maps.Clone(things[i]), nil
			}
			return nil, fmt.Errorf("no thing %q", id)
		},
	}
}

func build() (*ochazuke.Model, error) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	id := ochazuke.PropSpec{Name: "id", Key: true}
	title := ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel}
	project := ochazuke.PropSpec{Name: "project", Role: ochazuke.RoleOwner, Parameter: ochazuke.Parameter{Noun: "project"}}
	r.Define("project").Props(id, ochazuke.PropSpec{Name: "name", Role: ochazuke.RoleLabel}).
		Store(mem(ochazuke.Thing{"id": "p1", "name": "Alpha"}, ochazuke.Thing{"id": "p2", "name": "Beta"}))
	r.Define("task").Props(id, title, ochazuke.PropSpec{Name: "kind"}, ochazuke.PropSpec{Name: "today", Parameter: ochazuke.Parameter{Type: "boolean"}}, project).
		Filters(ochazuke.SavedFilter{Name: "today", Where: map[string]any{"today": true}}).
		Store(mem(ochazuke.Thing{"id": "t1", "title": "read the docs", "kind": "link", "project": "p1"},
			ochazuke.Thing{"id": "t2", "title": "main.go", "kind": "file", "project": "p1", "today": true},
			ochazuke.Thing{"id": "t3", "title": "the PR", "kind": "link", "project": "p2"}))
	r.Define("note").Props(id, title, ochazuke.PropSpec{Name: "project", Parameter: ochazuke.Parameter{Noun: "project"}}).
		Store(mem(ochazuke.Thing{"id": "n1", "title": "an idea", "project": "p1"}))
	r.Instance("projects")
	list, err := r.AddList("projects/list", []ochazuke.ListItem{
		{ID: "p1", Label: "Alpha", Noun: "project", Data: ochazuke.Thing{"id": "p1"}},
		{ID: "p2", Label: "Beta", Noun: "project", Data: ochazuke.Thing{"id": "p2"}},
	}, ochazuke.Parent("projects"))
	if err != nil {
		return nil, err
	}
	rail, err := r.AddRail("projects/rail", ochazuke.RailSpec{Stack: ochazuke.RelatedSpec{
		Target:  r.SelectionTarget(func() *ochazuke.Collection { return list }),
		GroupBy: map[string]string{"task.project": "kind"},
		Lists:   []ochazuke.RelatedList{{ID: "today", Noun: "task", Filter: "today", Label: "Today"}},
	}}, ochazuke.Parent("projects"))
	if err != nil {
		return nil, err
	}
	var m *ochazuke.Model
	if _, err := r.AddPage("projects", "Projects", func(r *ochazuke.Runtime, w, h int) ochazuke.Block {
		fit := rail.Fit(w, h)
		main := ochazuke.Text(list.View(fit.MainWidth, fit.MainHeight))
		return rail.Compose(ochazuke.Layout{Width: w, Height: h, Styles: m.Styles()}, fit, main)
	}, ochazuke.Focus(list.FocusTarget())); err != nil {
		return nil, err
	}
	if err := r.Bind(ochazuke.Binding{Noun: "app", Mode: "normal", Keys: "Z", Request: ochazuke.Request{Noun: ochazuke.RailNoun, Verb: "toggle"}}); err != nil {
		return nil, err
	}
	if err := r.OpenPage("projects"); err != nil {
		return nil, err
	}
	m = ochazuke.NewModel(r)
	return m, nil
}

// smoke prints the page in both looks at three widths.
func smoke(m *ochazuke.Model) {
	for _, look := range []string{ochazuke.RailLookFull, ochazuke.RailLookFloat} {
		if err := m.Runtime.Settings().Set(ochazuke.RailLookSetting, look); err != nil {
			fmt.Fprintln(os.Stderr, "rail:", err)
			os.Exit(1)
		}
		for _, w := range []int{80, 120, 200} {
			m.Width, m.Height = w, 20
			fmt.Printf("--- %s, %d columns\n%s\n", look, w, ansi.Strip(m.View().Content))
		}
	}
}
