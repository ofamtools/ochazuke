package cli

import (
	"bytes"
	"context"
	"encoding/json"
	gomaps "maps"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/keytest"
)

func mountedEditFixture(t *testing.T, mode string) (*ochazuke.Runtime, *ochazuke.Model, ochazuke.Thing) {
	t.Helper()
	ext := ochazuke.NewRuntime()
	thing := ochazuke.Thing{"id": "t1", "title": "Original", "status": "todo", "active": false, "stamp": "fixed"}
	ext.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Parameter: ochazuke.Parameter{Required: true}},
		ochazuke.PropSpec{Name: "status", Parameter: ochazuke.Parameter{Enum: []string{"todo", "done"}}},
		ochazuke.PropSpec{Name: "active", Parameter: ochazuke.Parameter{Type: "boolean"}},
		ochazuke.PropSpec{Name: "stamp", ReadOnly: true},
	).Store(ochazuke.Store{
		Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return gomaps.Clone(thing), nil },
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{gomaps.Clone(thing)}, nil
		},
		Patch: func(_ ochazuke.Ctx, _ string, changes ochazuke.Thing) (ochazuke.Thing, error) {
			gomaps.Copy(thing, changes)
			return gomaps.Clone(thing), nil
		},
	})
	raw, err := Catalog(ext, "extension", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	if _, err := r.Settings().SetSession(ochazuke.EditApplySetting, mode); err != nil {
		t.Fatal(err)
	}
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 100, 24
	local := func(noun string) string { return "ext-" + noun }
	mounted := MountNouns(r, doc.AppNouns(), HostMount{Local: local, Proxy: func(noun string, v CatalogVerb) any {
		return func(c ochazuke.Ctx) ochazuke.Result {
			if v.Name == "set" {
				return ochazuke.Pending(func(context.Context) (any, error) {
					return ext.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args})
				})
			}
			value, err := ext.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args})
			if err != nil {
				return ochazuke.Failed(err)
			}
			if v.Name == "get" {
				return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
			}
			return ochazuke.Done(value)
		}
	}})
	if len(mounted.Warnings) > 0 {
		t.Fatal(mounted.Warnings)
	}
	page := CatalogPage{ID: "parcels", Title: "Parcels", Lists: []CatalogList{{ID: "rows", Noun: "parcel"}}}
	pv, err := MountPage(m, doc, page, PageConfig{Noun: local})
	if err != nil {
		t.Fatal(err)
	}
	if err := pv.SetRows("rows", []Row{{ID: "t1", Noun: "parcel", Label: "Original", Data: map[string]any(thing)}}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("parcels"); err != nil {
		t.Fatal(err)
	}
	m.View()
	if err := r.SetFocus("rows/t1"); err != nil {
		t.Fatal(err)
	}
	return r, m, thing
}

func TestMountedPageStandardEdit(t *testing.T) {
	t.Parallel()
	r, m, thing := mountedEditFixture(t, "confirm")
	if handled, err := r.HandleBinding("i"); err != nil || !handled {
		t.Fatalf("edit key: %t %v", handled, err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor("edit-form")
	if e == nil || !reflect.DeepEqual(e.Values(), ochazuke.Record{"title": "Original", "status": "todo", "active": "false"}) {
		t.Fatalf("mounted form: %v", e)
	}
	keytest.Press(m, "A")
	keytest.Press(m, "x")
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	if thing["title"] != "Original" || r.TopModal() != "" {
		t.Fatalf("cancel: %v modal %s", thing, r.TopModal())
	}
	if handled, err := r.HandleBinding("i"); err != nil || !handled {
		t.Fatalf("edit key: %t %v", handled, err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "A")
	keytest.Press(m, "x")
	if _, err := r.HandleBinding("enter"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["title"] != "Originalx" || thing["stamp"] != "fixed" || r.TopModal() != "" {
		t.Fatalf("confirmed %v modal %s", thing, r.TopModal())
	}
	var out bytes.Buffer
	sh := New(r, "host", nil, &out, &out)
	if err := sh.Run(context.Background(), []string{"ext-parcel", "edit", "--id", "t1", "--status", "done", "--active", "true"}); err != nil {
		t.Fatal(err)
	}
	if thing["status"] != "done" || thing["active"] != true || strings.Contains(out.String(), "error:") {
		t.Fatalf("CLI: %v output %s", thing, out.String())
	}
}

func TestMountedStandardEditOptOut(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	if _, err := r.Settings().SetSession(ochazuke.EditFormsSetting, false); err != nil {
		t.Fatal(err)
	}
	doc := hostTestCatalog(t)
	got := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "ext-" + noun },
		Proxy: func(_ string, v CatalogVerb) any {
			return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("proxy " + v.Name) }
		},
	})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-task", Verb: "edit", Args: ochazuke.Arguments{"id": "t1"}}); err == nil {
		t.Fatal("disabled mounted editor ran")
	}
	value, err := r.Execute(ochazuke.Request{Noun: "ext-task", Verb: "set", Args: ochazuke.Arguments{"id": "t1", "prop": "title", "value": "Still writable"}})
	if err != nil || value != "proxy set" {
		t.Fatalf("existing write %v %v", value, err)
	}
}

func TestMountedPageChangeEditsSurviveClose(t *testing.T) {
	t.Parallel()
	r, _, thing := mountedEditFixture(t, "change")
	if handled, err := r.HandleBinding("i"); err != nil || !handled {
		t.Fatalf("edit key %t %v", handled, err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor("edit-form")
	for _, key := range []string{"A", "x", "esc", "j", "space", "j", "enter", "esc"} {
		if _, err := r.HandleBinding(key); err != nil {
			t.Fatal(err)
		}
		e.PendingCommand()
		if thing["title"] != "Original" || thing["status"] != "todo" {
			t.Fatalf("pending write ran inline after %s: %v", key, thing)
		}
	}
	if r.TopModal() != "" {
		t.Fatalf("slow save trapped form: %s", r.TopModal())
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if thing["title"] != "Originalx" || thing["status"] != "done" || thing["stamp"] != "fixed" {
		t.Fatalf("mounted save %v", thing)
	}
}
