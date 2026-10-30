package cli

import (
	"bytes"
	"context"
	"fmt"
	stdmaps "maps"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// starRuntime is a task store with a boolean starred prop and the alias
// task.star = task.set {prop: starred, toggle: true}.
func starRuntime(t *testing.T) (*ochazuke.Runtime, map[string]ochazuke.Thing) {
	t.Helper()
	things := map[string]ochazuke.Thing{"t1": {"id": "t1", "title": "One", "starred": true}}
	r := ochazuke.NewRuntime()
	r.Define("task").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "starred", Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean}},
	).Store(ochazuke.Store{
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			if t, ok := things[id]; ok {
				return stdmaps.Clone(t), nil
			}
			return nil, fmt.Errorf("no task %q", id)
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{things["t1"]}, nil
		},
		Patch: func(_ ochazuke.Ctx, id string, changes ochazuke.Thing) (ochazuke.Thing, error) {
			stdmaps.Copy(things[id], changes)
			return nil, nil
		},
	})
	if err := r.AddActionAlias(ochazuke.ActionAlias{Alias: "task.star", Action: "task.set", Args: ochazuke.Arguments{"prop": "starred", "toggle": true}}); err != nil {
		t.Fatal(err)
	}
	return r, things
}

// The CLI applies an alias's presets after the caller's flags, so a flag
// that excludes a preset (set's value excludes toggle) drops it, as keys,
// the palette and the API already do: task star t1 --value=false writes
// false; task star t1 alone still toggles.
func TestCLIAliasPresetYieldsToAnExcludingFlag(t *testing.T) {
	r, things := starRuntime(t)
	run := func(args ...string) error {
		var out, errOut bytes.Buffer
		err := New(r, "tool", strings.NewReader(""), &out, &errOut).Run(context.Background(), args)
		if err != nil {
			return fmt.Errorf("%w\n%s", err, errOut.String())
		}
		return nil
	}
	if err := run("task", "star", "t1", "--value=false"); err != nil || things["t1"]["starred"] != false {
		t.Fatalf("star --value=false: %v, starred %v", err, things["t1"]["starred"])
	}
	if err := run("task", "star", "t1"); err != nil || things["t1"]["starred"] != true {
		t.Fatalf("star (toggle): %v, starred %v", err, things["t1"]["starred"])
	}
	if err := run("task", "star", "t1", "--value=false", "--toggle"); err == nil {
		t.Fatal("value and toggle both given by the caller: want a refusal")
	}
}
