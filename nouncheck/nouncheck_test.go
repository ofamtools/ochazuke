package nouncheck

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// A thing noun with every core verb and a key is clean; a command noun
// with a core verb's name is reported, and so is a thing noun without a
// key.
func TestRowsAndCompare(t *testing.T) {
	r := ochazuke.NewRuntime()
	list := func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return nil, nil }
	r.Define("task").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{List: list})
	r.Define("tool").Verb("delete", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) })
	rows := Rows(r)
	if i := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "\ttask\t") }); i >= 0 {
		t.Fatalf("task is a clean thing noun: %q", rows[i])
	}
	if !slices.Contains(rows, "command-core-verb\ttool\tdelete") {
		t.Fatalf("rows %q lack the command noun's delete", rows)
	}
	fresh, stale := Compare([]string{"a", "b"}, []string{"b", "c"})
	if !reflect.DeepEqual(fresh, []string{"a"}) || !reflect.DeepEqual(stale, []string{"c"}) {
		t.Fatalf("fresh %v stale %v", fresh, stale)
	}
	if got := Parse(Format([]string{"b", "a"})); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("round trip %v", got)
	}
}
