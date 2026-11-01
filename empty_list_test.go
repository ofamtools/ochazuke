package ochazuke

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEmptyListSelectionAndYankLeaveStateAlone(t *testing.T) {
	for _, kind := range []string{"plain", "table", "cell-table", "tree", "filtered-table"} {
		for _, verb := range []string{"visual", "yank"} {
			t.Run(kind+"/"+verb, func(t *testing.T) {
				r := NewRuntime()
				r.Config().Disable()
				var l *List
				var err error
				if kind == "tree" {
					var tree *Tree
					tree, err = r.AddTree("empty", nil)
					if err == nil {
						l = tree.List()
					}
				} else {
					var opts []InstanceOption
					if kind != "plain" {
						opts = append(opts, Columns(Column{Title: "Name"}))
					}
					if kind == "cell-table" {
						opts = append(opts, CellSelect())
					}
					l, err = r.AddList("empty", nil, opts...)
				}
				if err != nil {
					t.Fatal(err)
				}
				if kind == "filtered-table" {
					r.Define("row")
					l.SetItems([]ListItem{{ID: "hidden", Noun: "row", Label: "hidden"}})
					l.marks["hidden"] = true
					l.SetFilter("no matches")
				}
				if err := l.Focus(); err != nil {
					t.Fatal(err)
				}
				regs := r.Registers()
				copied := false
				regs.Copy = func(string) tea.Cmd { copied = true; return nil }
				if err := r.StoreYank(Yank{Text: "keep this", Shape: "line"}, "a", false); err != nil {
					t.Fatal(err)
				}
				// Drain the seed yank through the fake clipboard first.
				_ = regs.PendingCommand()
				copied, regs.Active = false, "+"
				named, history := maps.Clone(regs.Named), slices.Clone(regs.History)
				before, marks := l.Snapshot(), maps.Clone(l.marks)
				focus, mode := r.Focus, r.Mode
				q := Request{Noun: "ochazuke.list", Verb: verb}
				if verb == "yank" {
					q.Args = Arguments{"what": "row"}
				}
				// Plain lists and trees deliberately leave Y free outside a
				// visual selection. Exercise their handler as well as the
				// public operation wherever its existing guard allows it.
				op, _ := r.Operation(q.Key())
				if _, err := op.Run(r, r.Context(), q.Args); err != nil {
					t.Fatal(err)
				}
				if r.Available(q, r.Focus, r.Mode) {
					if _, err := r.Execute(q); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(before, l.Snapshot()) || !reflect.DeepEqual(marks, l.marks) || l.Visual() || l.anchor != "" || l.base != nil || r.Focus != focus || !reflect.DeepEqual(r.Mode, mode) {
					t.Fatalf("empty operation changed selection, focus or mode: %+v", l.Snapshot())
				}
				if !reflect.DeepEqual(named, regs.Named) || !reflect.DeepEqual(history, regs.History) || regs.Active != "+" || regs.clip != nil || regs.PendingCommand() != nil || copied {
					t.Fatal("empty operation changed registers, yank history or clipboard")
				}
			})
		}
	}
}

func TestEmptyListKeepsSelectionErrorsForInvalidTargets(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	l, err := r.AddList("empty", nil, Columns(Column{Title: "Name"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.StartVisual(); err == nil {
		t.Fatal("StartVisual must still refuse a list with no selected row")
	}
	if err := l.Focus(); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"visual", "yank"} {
		op, _ := r.Operation("list." + verb)
		for _, args := range []Arguments{{"id": "missing"}, {"id": "app"}} {
			if _, err := op.Run(r, r.Context(), args); err == nil {
				t.Errorf("%s accepted invalid list id: %v", verb, args)
			}
		}
		if _, err := r.Execute(Request{Noun: "list", Verb: verb, Target: "missing"}); err == nil {
			t.Errorf("%s accepted a missing target", verb)
		}
		if _, err := op.Run(r, Context{Path: []string{"app"}}, nil); err == nil {
			t.Errorf("%s accepted no focused list", verb)
		}
	}
	unmarked, err := r.AddList("unmarked", nil, NoMarks())
	if err != nil {
		t.Fatal(err)
	}
	if err := unmarked.Focus(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "visual"}); err == nil {
		t.Fatal("NoMarks list must keep its visual guard")
	}
}
