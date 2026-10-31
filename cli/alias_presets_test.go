package cli

import (
	"bytes"
	"context"
	"fmt"
	stdmaps "maps"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
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

// An old parameter name the alias declares (ochazuke.ParamAlias) is a
// flag of its command, typed as its parameter, with a one-time notice on
// stderr; help never lists it.
func TestCLIParamAlias(t *testing.T) {
	r, things := starRuntime(t)
	old := []ochazuke.ParamAlias{{Alias: "starred", Param: "value", Since: "v0.57.0", RemoveAfter: "v0.58.0"}}
	if err := r.AddActionAlias(ochazuke.ActionAlias{Alias: "task.mark", Action: "task.set", Args: ochazuke.Arguments{"prop": "starred", "toggle": true}, ParamAliases: old}); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	run := func(args ...string) error {
		var out bytes.Buffer
		return New(r, "tool", strings.NewReader(""), &out, &errOut).Run(context.Background(), args)
	}
	if err := run("task", "mark", "t1", "--starred=false"); err != nil || things["t1"]["starred"] != false {
		t.Fatalf("mark --starred=false: %v, starred %v\n%s", err, things["t1"]["starred"], errOut.String())
	}
	if err := run("task", "mark", "t1", "--starred", "true"); err != nil || things["t1"]["starred"] != true {
		t.Fatalf("mark --starred true: %v, starred %v\n%s", err, things["t1"]["starred"], errOut.String())
	}
	if got := strings.Count(errOut.String(), "notice: task.mark: starred is an old name of value (renamed in v0.57.0; removed after v0.58.0)"); got != 1 {
		t.Fatalf("want one notice, got %d:\n%s", got, errOut.String())
	}
	if err := run("task", "star", "t1", "--starred=false"); err == nil {
		t.Fatal("task.star declares no old names: --starred must be refused")
	}
	var help bytes.Buffer
	_ = New(r, "tool", strings.NewReader(""), &help, &help).Run(context.Background(), []string{"task", "mark", "--help"})
	if strings.Contains(help.String(), "starred is") || strings.Contains(help.String(), "--starred") {
		t.Fatalf("help lists the old name:\n%s", help.String())
	}
}
