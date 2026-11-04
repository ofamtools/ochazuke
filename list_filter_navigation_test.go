package ochazuke

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilterFormTabFollowsDrawing(t *testing.T) {
	r, m, _ := compactFixture(t)
	m.Width, m.Height = 100, 24
	compactKeys(m, "z", "/")
	want := []string{"list-filter-form/conditions", "list-filter-form/save", "list-filter-form/sort/query", "list-filter-form/.direction", "list-filter-form/field/query"}
	var got []string
	for range want {
		compactKeys(m, "tab")
		got = append(got, r.Focus)
	}
	if !reflect.DeepEqual(got, want) {
		ring, rank := r.units(r.TopModal(), r.focusable)
		t.Fatalf("Tab = %v; want %v; ring %v ranks %v drawn %v", got, want, ring, rank, r.Instances[r.TopModal()].drawn)
	}
}

func TestFilterFormSizeSteadyOnTab(t *testing.T) {
	_, m, _ := compactFixture(t)
	compactKeys(m, "z", "/")
	_, before := m.modalContent(listFilterFormID, 70, 18)
	compactKeys(m, "shift+tab")
	_, after := m.modalContent(listFilterFormID, 70, 18)
	if strings.Count(before.Text, "\n") != strings.Count(after.Text, "\n") {
		t.Fatalf("modal grew on focus: %d -> %d", strings.Count(before.Text, "\n"), strings.Count(after.Text, "\n"))
	}
}
