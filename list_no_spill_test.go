package ochazuke

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/keytest"
)

// list.no-spill names a list: j at its last row keeps the focus and the
// selection there; taking the id out of the setting restores the spill at
// once (read live, no restart).
func TestListNoSpillSettingClampsAndClears(t *testing.T) {
	m, r := visualPage(t, []string{"a", "b"}, map[string]int{"a": 2, "b": 2}, []string{"a", "b"})
	if err := r.Settings().Set(ListNoSpillSetting, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"j", "j", "j"} {
		keytest.Press(m, key)
		m.View()
	}
	if r.Focus != "a2" || selectedID(r.List("a")) != "a2" {
		t.Fatalf("named list spilled: focus %q", r.Focus)
	}
	if err := r.Settings().Set(ListNoSpillSetting, []string{}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "j")
	if r.Focus != "b1" {
		t.Fatalf("setting cleared: focus %q, want b1", r.Focus)
	}
}

// The prefix form (b*) traps b at its last row.
func TestListNoSpillSettingPrefix(t *testing.T) {
	m, r := visualPage(t, []string{"a", "b"}, map[string]int{"a": 2, "b": 2}, []string{"b", "a"})
	if err := r.Settings().Set(ListNoSpillSetting, []string{"b*"}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "j")
	keytest.Press(m, "j")
	if r.Focus != "b2" {
		t.Fatalf("b* prefix: focus %q, want b2", r.Focus)
	}
}

// A NoSpill() list stays trapped whatever the setting says.
func TestListNoSpillOptionIgnoresSetting(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	if _, err := r.AddList("a", []ListItem{{ID: "a1", Label: "a1", Noun: "ochazuke.list"}}, Parent("page"), NoSpill()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("b", []ListItem{{ID: "b1", Label: "b1", Noun: "ochazuke.list"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("") }, Focus("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(ListNoSpillSetting, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	keytest.Press(m, "j")
	if r.Focus != "a1" {
		t.Fatalf("NoSpill list spilled: focus %q", r.Focus)
	}
}

// A tree named by its own id (not its list's) clamps j at its last row.
func TestTreeNoSpillSetting(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	tree, err := r.AddTree("panes/working", []TreeNode{{ID: "w1", Label: "w1"}, {ID: "w2", Label: "w2"}}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("other", []ListItem{{ID: "o1", Label: "o1", Noun: "ochazuke.list"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	render := func(r *Runtime, width, height int) Block {
		return Text(strings.Join([]string{tree.View(width, 2), r.List("other").View(width, 1)}, "\n"))
	}
	if _, err := r.AddPage("page", "Page", render, Focus("panes/working")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 20
	m.View()
	if err := r.SetFocus("w2"); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(ListNoSpillSetting, []string{"panes/working"}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "j")
	m.View()
	if r.Focus != "w2" || selectedID(tree.List()) != "w2" {
		t.Fatalf("named tree spilled: focus %q", r.Focus)
	}
	if err := r.Settings().Set(ListNoSpillSetting, []string{}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "j")
	if r.Focus != "o1" {
		t.Fatalf("setting cleared: focus %q, want o1", r.Focus)
	}
}

func selectedID(l *List) string {
	item, _ := l.Selected()
	return item.ID
}
