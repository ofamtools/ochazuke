package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// A parameter's from round-trips through the catalog, and a mounted verb
// that declares it fills from the row exactly as a native one: by a bare
// key row, from the help's unbound run, and from a request naming its
// target; with no row the argument is required as before. A bad from in a
// catalog skips the verb with a warning.
func TestMountedVerbParameterSourceFromCatalog(t *testing.T) {
	t.Parallel()
	verb := CatalogVerb{Name: "show", Params: map[string]ochazuke.Parameter{"name": {Type: "string", Required: true, From: "row:name"}}}
	raw, err := json.Marshal(verb)
	if err != nil || !strings.Contains(string(raw), `"from":"row:name"`) {
		t.Fatalf("encoded %s, %v", raw, err)
	}
	var back CatalogVerb
	if err := json.Unmarshal(raw, &back); err != nil || back.Params["name"].From != "row:name" {
		t.Fatalf("decoded %+v, %v", back, err)
	}
	r := ochazuke.NewRuntime()
	var got []ochazuke.Arguments
	mounted := MountNouns(r, []CatalogNoun{{Name: "branch", Verbs: []CatalogVerb{back}}, {Name: "bad", Verbs: []CatalogVerb{{Name: "show", Params: map[string]ochazuke.Parameter{"name": {Type: "string", From: "key"}}}}}}, HostMount{
		Local: func(n string) string { return "git." + n },
		Proxy: func(string, CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result { got = append(got, c.Args); return ochazuke.Done(nil) }
		},
	})
	if len(mounted.Warnings) != 1 || !strings.Contains(mounted.Warnings[0], "bad show skipped") {
		t.Fatalf("warnings %v", mounted.Warnings)
	}
	r.Define("lane").Mode("normal").Bind("s", ochazuke.Do("git.branch.show"))
	r.Instance("l1", ochazuke.Kind("lane")).Data = map[string]any{"name": "main"}
	if err := r.SetFocus("l1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(ochazuke.MacroKey{Key: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RunUnbound("l1", ochazuke.Request{Noun: "git.branch", Verb: "show"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ExecuteAt(r.Context(), ochazuke.Request{Noun: "git.branch", Verb: "show", Target: "l1"}); err != nil {
		t.Fatal(err)
	}
	want := ochazuke.Arguments{"name": "main"}
	if !reflect.DeepEqual(got, []ochazuke.Arguments{want, want, want}) {
		t.Fatalf("arguments %v", got)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "git.branch", Verb: "show"}); err == nil || len(got) != 3 {
		t.Fatalf("a request with no row ran: %v %v", err, got)
	}
}
