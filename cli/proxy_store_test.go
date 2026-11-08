package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gomaps "maps"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// proxyHost mounts an extension noun "parcel" with all five store calls
// as ext-parcel; calls records the verbs the extension was asked to run.
func proxyHost(t *testing.T) (r *ochazuke.Runtime, things map[string]ochazuke.Thing, calls *[]string) {
	t.Helper()
	things = map[string]ochazuke.Thing{"p1": {"id": "p1", "title": "One", "done": false}}
	ext := ochazuke.NewRuntime()
	ext.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "done", Parameter: ochazuke.Parameter{Type: "boolean"}},
	).Store(ochazuke.Store{
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			t, ok := things[id]
			if !ok {
				return nil, fmt.Errorf("no parcel %q", id)
			}
			return gomaps.Clone(t), nil
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			var out []ochazuke.Thing
			for _, id := range slices.Sorted(gomaps.Keys(things)) {
				out = append(out, gomaps.Clone(things[id]))
			}
			return out, nil
		},
		Create: func(_ ochazuke.Ctx, t ochazuke.Thing) (ochazuke.Thing, error) {
			id := fmt.Sprintf("p%d", len(things)+1)
			t["id"] = id
			things[id] = gomaps.Clone(t)
			return t, nil
		},
		Patch: func(_ ochazuke.Ctx, id string, c ochazuke.Thing) (ochazuke.Thing, error) {
			gomaps.Copy(things[id], c)
			return gomaps.Clone(things[id]), nil
		},
		Delete: func(_ ochazuke.Ctx, id string) error { delete(things, id); return nil },
	})
	raw, err := Catalog(ext, "extension", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r = ochazuke.NewRuntime()
	var log []string
	got := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(n string) string { return "ext-" + n },
		Proxy: func(noun string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				log = append(log, v.Name)
				value, err := ext.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args, Confirmed: true}) // the host confirmed already
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Pending(func(context.Context) (any, error) { return value, nil })
			}
		},
	})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	return r, things, &log
}

// run runs the verb as the host's CLI does, settling its job, and answers
// what it printed.
func run(t *testing.T, r *ochazuke.Runtime, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := New(r, "host", nil, &out, &out).Run(context.Background(), args); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out.String())
	}
	t.Logf("%v: %s", args, out.String())
	return out.String()
}

func TestProxyStoreSendsEachStandardCallToTheExtension(t *testing.T) {
	r, things, calls := proxyHost(t)
	if n := r.Noun("ext-parcel"); n == nil || n.Store == nil || !n.Store.Remote || len(n.Store.Missing) != 0 {
		t.Fatalf("a mounted noun with props has a remote store: %+v", n)
	}
	if got := run(t, r, "ext-parcel", "get", "--id", "p1"); !strings.Contains(got, `"title": "One"`) || !slices.Contains(*calls, "get") {
		t.Fatalf("get %q calls %v", got, *calls)
	}
	*calls = nil
	if got := run(t, r, "ext-parcel", "list"); !strings.Contains(got, `"id": "p1"`) || !slices.Contains(*calls, "list") {
		t.Fatalf("list %q calls %v", got, *calls)
	}
	*calls = nil
	if got := run(t, r, "ext-parcel", "create", "--title", "Two"); !strings.Contains(got, `"id": "p2"`) || things["p2"]["title"] != "Two" || !slices.Contains(*calls, "create") {
		t.Fatalf("create %q things %v calls %v", got, things, *calls)
	}
	*calls = nil
	run(t, r, "ext-parcel", "delete", "--id", "p2", "--yes")
	if _, ok := things["p2"]; ok || !slices.Contains(*calls, "delete") {
		t.Fatalf("delete: things %v calls %v", things, *calls)
	}
}

func TestProxyStoreSetAndEditUndoLikeLocal(t *testing.T) {
	r, things, calls := proxyHost(t)
	run(t, r, "ext-parcel", "set", "--id", "p1", "--prop", "title", "--value", "Changed")
	if things["p1"]["title"] != "Changed" || !slices.Contains(*calls, "set") {
		t.Fatalf("set did not reach the extension: %v %v", things, *calls)
	}
	undo := func() {
		t.Helper()
		if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
			t.Fatal(err)
		}
		if err := Settle(r, nil); err != nil {
			t.Fatal(err)
		}
	}
	undo()
	if things["p1"]["title"] != "One" {
		t.Fatalf("undo of set: %v", things["p1"])
	}
	run(t, r, "ext-parcel", "edit", "--id", "p1", "--title", "Edited", "--done", "true")
	if things["p1"]["title"] != "Edited" || things["p1"]["done"] != true {
		t.Fatalf("edit: %v", things["p1"])
	}
	undo()
	if things["p1"]["title"] != "One" || things["p1"]["done"] != false {
		t.Fatalf("undo of edit: %v", things["p1"])
	}
}

func TestProxyStoreMissingCapability(t *testing.T) {
	r, _, _ := mountHost(t, hostTestCatalog(t)) // the task extension has get, list and set only
	n := r.Noun("ext-task")
	for _, verb := range []string{"create", "delete"} {
		if n.Store.Missing[verb] == "" {
			t.Fatalf("%s should be named missing: %v", verb, n.Store.Missing)
		}
	}
	for _, verb := range []string{"get", "list", "set"} {
		if n.Store.Missing[verb] != "" {
			t.Fatalf("%s is offered: %v", verb, n.Store.Missing)
		}
	}
	_, err := r.Execute(ochazuke.Request{Noun: "ext-task", Verb: "create", Args: ochazuke.Arguments{"title": "x"}})
	if err == nil || !strings.Contains(err.Error(), "extension offers no create") {
		t.Fatalf("create should refuse with the reason: %v", err)
	}
	// A verb the extension lacks is registered but unavailable, with the reason.
	if op := r.Operations.At(ochazuke.Key("ext-project.get")); op.Unavailable == "" || !strings.Contains(op.Unavailable, "offers no get") {
		t.Fatalf("project has no get in the extension: %q", op.Unavailable)
	}
}
