package cli

import (
	"fmt"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// Standard create on the CLI takes prop=value words as set and duplicate
// do (standard verbs plan 2.6, NOUN create [p=v ...]), keeping the label
// first as a bare word: mtm stored "text=buy milk" as the label.
func TestCreateTakesPropValueWords(t *testing.T) {
	f := newFixture(t)
	var made []ochazuke.Thing
	f.r.Define("note").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "text", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "project"},
		ochazuke.PropSpec{Name: "size", Parameter: ochazuke.Parameter{Type: ochazuke.SettingInteger}},
	).Store(ochazuke.Store{
		Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return nil, fmt.Errorf("none") },
		Create: func(_ ochazuke.Ctx, v ochazuke.Thing) (ochazuke.Thing, error) {
			t := ochazuke.Thing{"id": fmt.Sprintf("n%d", len(made)+1)}
			for k, x := range v {
				t[k] = x
			}
			made = append(made, t)
			return t, nil
		},
	})
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"text=buy milk", "project=hub"}, "map[id:n1 project:hub text:buy milk]"},
		{[]string{"Buy milk", "project=hub"}, "map[id:n2 project:hub text:Buy milk]"},
		{[]string{"project=hub", "size=3", "text=a b"}, "map[id:n3 project:hub size:3 text:a b]"},
		{[]string{"Plain", "--project", "hub"}, "map[id:n4 project:hub text:Plain]"},
	} {
		if _, errOut, err := f.run("", append([]string{"note", "create"}, c.args...)...); err != nil {
			t.Fatalf("%v: %v\n%s", c.args, err, errOut)
		}
		if got := fmt.Sprint(made[len(made)-1]); got != c.want {
			t.Errorf("%v: created %s, want %s", c.args, got, c.want)
		}
	}
	if _, _, err := f.run("", "note", "create", "text=a", "--text", "b"); err == nil {
		t.Error("a prop given twice must be refused")
	}
}
