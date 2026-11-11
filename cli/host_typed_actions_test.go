package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountTypedPathKeepsFreeVerbsAndNativeOpen(t *testing.T) {
	t.Parallel()
	producer := ochazuke.NewRuntime()
	producer.Config().Disable()
	thing := ochazuke.Thing{"id": "d1", "name": "Plan", "file": "/fixture/plan.txt"}
	producer.Define("document").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "name", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "file", Parameter: ochazuke.Parameter{Type: ochazuke.PropPath, Path: "file"}},
	).Store(ochazuke.Store{
		Get:  func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return thing, nil },
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return []ochazuke.Thing{thing}, nil },
	}).
		Verb("open", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("native") }).OpensPage("documents")
	raw, err := Catalog(producer, "example", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	ochazuke.NewModel(r)
	var runs []string
	r.SetOpener(ochazuke.Opener{GOOS: "darwin", Start: func(name string, args ...string) error {
		runs = append(runs, fmt.Sprint(append([]string{name}, args...)))
		return nil
	}})
	mounted := MountCatalog(r, doc, HostMount{Local: func(noun string) string { return "tool-" + noun }, Proxy: func(noun string, v CatalogVerb) any {
		return func(c ochazuke.Ctx) ochazuke.Result {
			value, err := producer.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args})
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(value)
		}
	}})
	if len(mounted.Warnings) > 0 {
		t.Fatal(mounted.Warnings)
	}
	props := r.Props("tool-document")
	file := slices.IndexFunc(props, func(p ochazuke.PropSpec) bool { return p.Name == "file" })
	if file < 0 || props[file].Type != ochazuke.PropPath || props[file].Path != "file" {
		t.Fatalf("typed file prop: %+v", props)
	}
	for _, verb := range []string{"show", "reveal", "yank-path", "open-with", "open"} {
		if _, ok := r.Operation("tool-document." + verb); !ok {
			t.Fatalf("missing %s", verb)
		}
	}
	for _, verb := range []string{"reveal", "yank-path", "open-with"} {
		args := []string{"tool-document", verb, "--id", "d1"}
		if verb == "open-with" {
			args = append(args, "--with", "Preview")
		}
		helpOut(t, r, args...)
	}
	if !slices.Contains(runs, "[open -R -- /fixture/plan.txt]") || !slices.Contains(runs, "[open -a Preview -- /fixture/plan.txt]") {
		t.Fatalf("free verb effects: %v", runs)
	}
	before := len(runs)
	if got := helpOut(t, r, "tool-document", "open"); !strings.Contains(got, "native") {
		t.Fatalf("native open: %q", got)
	}
	if len(runs) != before {
		t.Fatal("native open fell through to OS opener")
	}
	restore := r.Sourced("palette")
	defer restore()
	if _, err := r.Execute(ochazuke.Request{Noun: "tool-document", Verb: "show", Args: ochazuke.Arguments{"id": "d1"}}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if r.Page != ochazuke.IndexPageID("tool-document") || r.ShowFormOf(r.Page+"/show") == nil {
		t.Fatalf("mounted show did not open its form: %s", r.Page)
	}
}

func TestMountReciprocalForwardsTypedSubjectsAcrossNamespaces(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.DefineOwn("receiver").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{})
	var calls []string
	for _, namespace := range []string{"alpha", "beta"} {
		producer := ochazuke.NewRuntime()
		producer.Config().Disable()
		producer.Define("document").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{}).
			Verb("send", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }).
			Reciprocal("destination", "receive-"+namespace, "document")
		op, _ := producer.Operation("document.send")
		op.Parameters = map[string]ochazuke.Parameter{"id": {Type: "string", Noun: "document", Required: true}, "destination": {Type: "string", Noun: "receiver", HostNoun: "receiver", Required: true}, "text": {Type: "string"}}
		producer.Operations.Set(op)
		raw, err := Catalog(producer, namespace, "1")
		if err != nil {
			t.Fatal(err)
		}
		var doc CatalogDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		mounted := MountCatalog(r, doc, HostMount{Local: func(noun string) string { return namespace + "-" + noun }, Proxy: func(noun string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				calls = append(calls, fmt.Sprintf("%s %s %v", namespace, v.Name, c.Args))
				return ochazuke.Done(c.Args)
			}
		}})
		if len(mounted.Warnings) > 0 {
			t.Fatal(mounted.Warnings)
		}
		reverse, _ := r.Operation("receiver.receive-" + namespace)
		if reverse.Parameters["document"].Noun != namespace+"-document" {
			t.Fatalf("wrong subject namespace: %+v", reverse.Parameters)
		}
		helpOut(t, r, "receiver", "receive-"+namespace, "--document", "d1", "--id", "r2", "--text", "literal")
		if err := r.AddVerbCheck(namespace+"-document.send", ochazuke.VerbCheck{Name: "permission", Check: func(_ *ochazuke.Runtime, _ ochazuke.Context, args ochazuke.Arguments) ochazuke.Verdict {
			if args["text"] == "refuse" {
				return ochazuke.Refuse("not permitted")
			}
			return ochazuke.Allow()
		}}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Execute(ochazuke.Request{Noun: "receiver", Verb: "receive-" + namespace, Args: ochazuke.Arguments{"document": "d1", "id": "r2", "text": "refuse"}}); err == nil || !strings.Contains(err.Error(), "not permitted") {
			t.Fatalf("reverse bypassed original check: %v", err)
		}
	}
	want := []string{"alpha send map[destination:r2 id:d1 text:literal]", "beta send map[destination:r2 id:d1 text:literal]"}
	if !slices.Equal(calls, want) {
		t.Fatalf("forwarded %v, want %v", calls, want)
	}
	if err := r.SetConfirm("alpha-document.send", "Send {{id}} to {{destination}}?"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "receiver", Verb: "receive-alpha", Args: ochazuke.Arguments{"document": "d1", "id": "r2", "text": "confirmed"}}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() == "" || !slices.Equal(calls, want) {
		t.Fatalf("reverse bypassed confirmation: %v", calls)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.confirm", Verb: "accept"}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, append(want, "alpha send map[destination:r2 id:d1 text:confirmed]")) {
		t.Fatalf("confirmed arguments changed: %v", calls)
	}
}
