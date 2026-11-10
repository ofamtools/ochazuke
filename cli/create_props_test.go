package cli

import (
	"fmt"
	stdmaps "maps"
	"reflect"
	"testing"

	"ofam.dev/ochazuke"
)

func TestCreateCLIPropsAndCollision(t *testing.T) {
	for _, args := range [][]string{
		{"from=project:x", "to=task:y", "size=3"},
		{"size=3", "from=project:x", "to=task:y"},
		{"--from", "project:x", "--to", "task:y", "--size", "3"},
		{"--props", `{"from":"project:x","to":"task:y","size":3}`},
		{"--json", `{"props":{"from":"project:x","to":"task:y","size":3}}`},
		{"--props", `{"from":"project:x"}`, "to=task:y", "size=3"},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			f := newFixture(t)
			var written ochazuke.Thing
			f.r.Define("link").Props(
				ochazuke.PropSpec{Name: "id", Key: true},
				ochazuke.PropSpec{Name: "from", Parameter: ochazuke.Parameter{Required: true, Noun: ochazuke.AnyNoun}},
				ochazuke.PropSpec{Name: "to", Parameter: ochazuke.Parameter{Required: true, Noun: ochazuke.AnyNoun}},
				ochazuke.PropSpec{Name: "size", Parameter: ochazuke.Parameter{Type: ochazuke.SettingInteger, Required: true}},
			).Store(ochazuke.Store{
				Stamps: true,
				Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) {
					return nil, fmt.Errorf("not a copy source")
				},
				Create: func(_ ochazuke.Ctx, v ochazuke.Thing) (ochazuke.Thing, error) {
					written = stdmaps.Clone(v)
					return ochazuke.Thing{"id": "created"}, nil
				},
			})
			_, errOut, err := f.run("", append([]string{"link", "create"}, args...)...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, errOut)
			}
			want := ochazuke.Thing{"from": "project:x", "to": "task:y", "size": 3}
			if !reflect.DeepEqual(written, want) {
				t.Fatalf("wrote %v, want %v", written, want)
			}
		})
	}
}

func TestCreateCLICopySourceWithOwnFrom(t *testing.T) {
	for _, ownFrom := range []bool{false, true} {
		t.Run(fmt.Sprint(ownFrom), func(t *testing.T) {
			f := newFixture(t)
			props := []ochazuke.PropSpec{{Name: "id", Key: true}, {Name: "title", Role: ochazuke.RoleLabel, Parameter: ochazuke.Parameter{Required: true}}}
			copyArg := "--from"
			if ownFrom {
				props = append(props, ochazuke.PropSpec{Name: "from"})
				copyArg = "--create-from"
			}
			var written ochazuke.Thing
			f.r.Define("parcel").Props(props...).Store(ochazuke.Store{
				Stamps: true,
				Get: func(_ ochazuke.Ctx, key string) (ochazuke.Thing, error) {
					if key != "original" {
						return nil, fmt.Errorf("unexpected source %q", key)
					}
					return ochazuke.Thing{"id": "original", "title": "Copied", "from": "project:old"}, nil
				},
				Create: func(_ ochazuke.Ctx, v ochazuke.Thing) (ochazuke.Thing, error) {
					written = stdmaps.Clone(v)
					return ochazuke.Thing{"id": "copy"}, nil
				},
			})
			args := []string{"parcel", "create", copyArg, "original"}
			want := ochazuke.Thing{"title": "Copied"}
			if ownFrom {
				args = append(args, "--props", `{"from":"project:new"}`)
				want["from"] = "project:new"
			}
			if _, errOut, err := f.run("", args...); err != nil {
				t.Fatalf("%v\n%s", err, errOut)
			}
			if !reflect.DeepEqual(written, want) {
				t.Fatalf("wrote %v, want %v", written, want)
			}
		})
	}
}
