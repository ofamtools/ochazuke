package cli

import (
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// o-git's branch rows use an opaque id while their binding reads name and repo.
func TestMountedRowFieldsOGitBranch(t *testing.T) {
	t.Parallel()
	doc := CatalogDoc{Nouns: []CatalogNoun{{Name: "branch", Verbs: []CatalogVerb{{Name: "list", Rows: "branch"}}}}, Pages: []CatalogPage{{ID: "git", Title: "Git", Lists: []CatalogList{{ID: "branches", Noun: "branch"}}}}}
	m, v := mountedPage(t, doc, PageConfig{}, 80, 20)
	r := m.Runtime
	var got ochazuke.Arguments
	r.Define("branch").Verb("inspect", func(c ochazuke.Ctx) ochazuke.Result { got = c.Args; return ochazuke.Done(nil) })
	op := r.Operations.At(ochazuke.Key("branch.inspect"))
	op.Parameters = map[string]ochazuke.Parameter{"name": {Type: "string"}, "repo": {Type: "string"}, "handle": {Type: "string"}}
	r.Operations.Set(op)
	r.Define("branch").Mode("normal").Bind("v", ochazuke.Do("branch.inspect").FromRow("name", "name").FromRow("repo", "repo").FromPayload("handle", ochazuke.SourceRow))
	if err := v.SetRows("branches", []Row{{ID: "repo:main", Label: "main", Data: map[string]any{"name": "main", "repo": "/fixture/repo"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("branches/repo:main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(ochazuke.MacroKey{Key: "v"}); err != nil {
		t.Fatal(err)
	}
	want := ochazuke.Arguments{"name": "main", "repo": "/fixture/repo", "handle": "repo:main"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mounted branch arguments = %#v, want %#v", got, want)
	}
}

func TestMountedProxyTargetSource(t *testing.T) {
	t.Parallel()
	for _, declared := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "parameter"}[declared], func(t *testing.T) {
			t.Parallel()
			r := ochazuke.NewRuntime()
			var got ochazuke.Arguments
			verb := CatalogVerb{Name: "show"}
			if declared {
				verb.Params = map[string]ochazuke.Parameter{"target": {Type: "string"}}
			}
			mounted := MountNouns(r, []CatalogNoun{{Name: "doc", Verbs: []CatalogVerb{verb}}}, HostMount{
				Local: func(n string) string { return "md." + n },
				Proxy: func(string, CatalogVerb) any {
					return func(c ochazuke.Ctx) ochazuke.Result { got = c.Args; return ochazuke.Done(nil) }
				},
			})
			if len(mounted.Warnings) > 0 {
				t.Fatal(mounted.Warnings)
			}
			r.Define("lane").Mode("normal").Bind("s", ochazuke.Do("md.doc.show").FromRow("target", "doc"))
			r.Instance("lane1", ochazuke.Kind("lane")).Data = map[string]any{"doc": "/fixture/report.md"}
			if err := r.SetFocus("lane1"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Dispatch(ochazuke.MacroKey{Key: "s"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, ochazuke.Arguments{"target": "/fixture/report.md"}) {
				t.Fatalf("arguments: %#v", got)
			}
			_, has := r.Operations.At(ochazuke.Key("md.doc.show")).Parameters["target"]
			if has != declared {
				t.Fatal("transport target changed the verb's parameter schema")
			}
		})
	}
}

func TestMountedRowEmptyTargetRefuses(t *testing.T) {
	t.Parallel()
	for _, declared := range []bool{false, true} {
		r := ochazuke.NewRuntime()
		var sent []string
		verb := CatalogVerb{Name: "send"}
		if declared {
			verb.Params = map[string]ochazuke.Parameter{"target": {Type: "string"}}
		}
		mounted := MountNouns(r, []CatalogNoun{{Name: "doc", Verbs: []CatalogVerb{verb}}}, HostMount{
			Local: func(n string) string { return "md." + n },
			Proxy: func(string, CatalogVerb) any {
				return func(c ochazuke.Ctx) ochazuke.Result {
					sent = append(sent, ochazuke.StringArg(c.Args, "target", "UNTARGETED"))
					return ochazuke.Done(nil)
				}
			},
		})
		if len(mounted.Warnings) > 0 {
			t.Fatal(mounted.Warnings)
		}
		m := ochazuke.NewModel(r)
		doc := CatalogDoc{Nouns: []CatalogNoun{{Name: "lane", Verbs: []CatalogVerb{{Name: "list", Rows: "lane"}}}}, Pages: []CatalogPage{{ID: "lanes", Title: "Lanes", Lists: []CatalogList{{ID: "rows", Noun: "lane"}}}}}
		v, err := MountPage(m, doc, doc.Pages[0], PageConfig{})
		if err != nil {
			t.Fatal(err)
		}
		r.Define("lane").Mode("normal").Bind("s", ochazuke.Do("md.doc.send").FromRow("target", "doc"))
		for _, data := range []map[string]any{{}, {"doc": ""}, {"doc": "/fixture/report.md"}} {
			if err := v.SetRows("rows", []Row{{ID: "lane1", Label: "Work", Data: data}}); err != nil {
				t.Fatal(err)
			}
			if err := r.SetFocus("rows/lane1"); err != nil {
				t.Fatal(err)
			}
			_, err := r.Dispatch(ochazuke.MacroKey{Key: "s"})
			if data["doc"] == "/fixture/report.md" {
				if err != nil || !reflect.DeepEqual(sent, []string{"/fixture/report.md"}) {
					t.Fatalf("valid mounted target: %v %v", sent, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), `"doc"`) || !strings.Contains(err.Error(), `"rows/lane1"`) || len(sent) != 0 {
				t.Fatalf("empty mounted target: %v %v", sent, err)
			}
		}
	}
}
