package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// trapResolvePage is a cell-mode table "s" (two rows, two columns) with a
// plain child instance "s/body" under it, focused: the walk from the body
// reaches the table's level, whose list rows (j, k from the list noun; H,
// L bound on the table instance to list.column) act on the table, not on
// the body (runRow's level target).
func trapResolvePage(t *testing.T) (*Model, *Runtime, *List) {
	t.Helper()
	r := NewRuntime()
	r.Instance("page")
	l, err := r.AddList("s", []ListItem{
		{ID: "s1", Label: "s1", Noun: "ochazuke.list", Cells: []string{"s1", "1"}},
		{ID: "s2", Label: "s2", Noun: "ochazuke.list", Cells: []string{"s2", "2"}},
	}, Parent("page"), Columns(Column{Title: "name"}, Column{Title: "n"}), CellSelect())
	if err != nil {
		t.Fatal(err)
	}
	r.Instance("s/body", Parent("s"))
	if err := r.Bind(
		Binding{Instance: "s", Mode: "normal", Keys: "L", Request: Request{Noun: "ochazuke.list", Verb: "column", Args: Arguments{"delta": 1}}},
		Binding{Instance: "s", Mode: "normal", Keys: "H", Request: Request{Noun: "ochazuke.list", Verb: "column", Args: Arguments{"delta": -1}}},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("page", "Page", func(r *Runtime, w, h int) Block { return Text(l.View(w, 3)) }, Focus("s")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 20
	m.View()
	return m, r, l
}

// A list-level row reached from a child instance is judged where it runs:
// at the list it targets. With the list trapped at the edge the key leaves
// by (list.no-spill s:<edge>), the row stands aside, so the key is a
// silent no-op (no "is not applicable now" refusal, the focus, the row
// and the column stay); untrapped, the same key moves the list. Every
// edge goes through the same path (SpillEdges).
func TestTrapResolveJudgesRowAtItsTarget(t *testing.T) {
	keys := map[SpillEdge]string{EdgeFirst: "k", EdgeLast: "j", EdgeFirstColumn: "H", EdgeLastColumn: "L"}
	for _, edge := range SpillEdges {
		for _, trapped := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/trapped=%v", edge, trapped), func(t *testing.T) {
				m, r, l := trapResolvePage(t)
				row, col := "s2", 1
				switch edge {
				case EdgeFirst:
					row = "s1"
				case EdgeFirstColumn:
					col = 0
				}
				if err := l.Select(row); err != nil {
					t.Fatal(err)
				}
				l.SetColumn(col)
				if err := r.SetFocus("s/body"); err != nil {
					t.Fatal(err)
				}
				entries := []string{}
				if trapped {
					entries = []string{"s:" + string(edge)}
				}
				if err := r.Settings().Set(ListNoSpillSetting, entries); err != nil {
					t.Fatal(err)
				}
				m.View()
				// Turn the edge into a move when untrapped: start one step in.
				if !trapped {
					switch edge {
					case EdgeFirst:
						row = "s2"
					case EdgeLast:
						row = "s1"
					case EdgeFirstColumn:
						col = 1
					case EdgeLastColumn:
						col = 0
					}
					if err := l.Select(row); err != nil {
						t.Fatal(err)
					}
					l.SetColumn(col)
				}
				keytest.Press(m, keys[edge])
				if strings.Contains(m.Status, "not applicable") {
					t.Fatalf("status %q: a trapped row was run and refused", m.Status)
				}
				if r.Focus != "s/body" {
					t.Fatalf("focus %q, want s/body", r.Focus)
				}
				gotRow, gotCol := selectedID(l), l.Column()
				if trapped {
					if gotRow != row || gotCol != col {
						t.Fatalf("trapped: row %q col %d, want %q %d", gotRow, gotCol, row, col)
					}
					return
				}
				if gotRow == row && gotCol == col {
					t.Fatalf("untrapped: %s did not move the list (row %q col %d)", keys[edge], gotRow, gotCol)
				}
			})
		}
	}
}

// The hints judge a row where it runs too: at the body over a list
// trapped at its last row, j's list.move is not offered.
func TestTrapResolveHintsJudgeAtTarget(t *testing.T) {
	m, r, l := trapResolvePage(t)
	if err := l.Select("s2"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("s/body"); err != nil {
		t.Fatal(err)
	}
	has := func() bool {
		for _, row := range r.scanRows(r.walk(r.Focus), r.Mode, r.Focus, false) {
			if row.Keys == "j" && row.Request.Key() == "ochazuke.list.move" {
				return true
			}
		}
		return false
	}
	m.View()
	if !has() {
		t.Fatal("untrapped: j list.move not listed at the body")
	}
	if err := r.Settings().Set(ListNoSpillSetting, []string{"s:last"}); err != nil {
		t.Fatal(err)
	}
	m.View()
	if has() {
		t.Fatal("trapped at :last: j list.move still listed at the body")
	}
}
