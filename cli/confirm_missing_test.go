package cli

import (
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// A confirmation never asks about a thing that does not exist: delete
// --target typo (a target read as the key) and delete --id typo fail with
// the store's answer before any prompt, and nothing is deleted; a thing
// that exists still asks.
func TestConfirmRefusesAMissingThing(t *testing.T) {
	f := newFixture(t)
	things := map[string]ochazuke.Thing{"t1": {"id": "t1", "title": "Call the plumber"}}
	var deleted []string
	f.r.Define("chore").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
	).Store(ochazuke.Store{
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			if t, ok := things[id]; ok {
				return t, nil
			}
			return nil, fmt.Errorf("unknown chore %q", id)
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{things["t1"]}, nil
		},
		Delete: func(_ ochazuke.Ctx, id string) error {
			deleted = append(deleted, id)
			return nil
		},
	})
	for _, args := range [][]string{{"chore", "delete", "--target", "typo"}, {"chore", "delete", "--id", "typo"}} {
		_, errOut, err := f.run("", args...)
		if err == nil || !strings.Contains(err.Error(), `"typo"`) || strings.Contains(errOut, "[y/N]") {
			t.Fatalf("%v: err %v, stderr %q", args, err, errOut)
		}
	}
	if len(deleted) != 0 || f.r.TopModal() != "" {
		t.Fatalf("deleted %v, modal %q", deleted, f.r.TopModal())
	}
	if _, errOut, err := f.run("n\n", "chore", "delete", "--target", "t1"); err == nil || !strings.Contains(errOut, "Call the plumber") || !strings.Contains(errOut, "[y/N]") {
		t.Fatalf("an existing chore still asks: %v %q", err, errOut)
	}
}
