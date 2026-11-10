package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// hostNounCatalog is a tool with a noun of its own called project (not the
// host's) and a lane noun whose project prop declares the host's project as
// its owner, beside a path prop and a list of paths.
func hostNounCatalog(t *testing.T) CatalogDoc {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Define("project").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{})
	r.Define("lane").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "project", Role: ochazuke.RoleOwner, Parameter: ochazuke.Parameter{Noun: "project", HostNoun: "project"}},
		ochazuke.PropSpec{Name: "cwd", Parameter: ochazuke.Parameter{Type: ochazuke.PropPath, Path: "dir"}},
		ochazuke.PropSpec{Name: "dirs", Parameter: ochazuke.Parameter{Type: ochazuke.SettingList, Items: &ochazuke.Parameter{Type: ochazuke.PropPath, Path: "dir"}}},
	).Store(ochazuke.Store{})
	data, err := Catalog(r, "ext", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func mountOn(r *ochazuke.Runtime, doc CatalogDoc) MountedNouns {
	return MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "ext-" + noun },
		Proxy: func(string, CatalogVerb) any {
			return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
		},
	})
}

func TestMountHostNounTakesTheHostsNounWhenItHasOne(t *testing.T) {
	t.Parallel()
	doc := hostNounCatalog(t)
	r := ochazuke.NewRuntime()
	r.Define("project").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{})
	got := mountOn(r, doc)
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings %v", got.Warnings)
	}
	p, _ := r.PropOf("ext-lane", "project")
	if p.Noun != "project" || p.HostNoun != "" || p.Role != ochazuke.RoleOwner {
		t.Fatalf("with a host project: %+v", p)
	}
	found := false
	for _, ref := range r.Referrers("project") {
		found = found || ref.Noun == "ext-lane" && ref.Prop == "project" && ref.Role == ochazuke.RoleOwner
	}
	if !found {
		t.Fatalf("referrers of the host's project: %+v", r.Referrers("project"))
	}
	if refs := r.Referrers("ext-project"); len(refs) != 0 {
		t.Fatalf("the extension's own project is not the owner: %+v", refs)
	}
}

func TestMountHostNounIsDroppedWhenTheHostHasNone(t *testing.T) {
	t.Parallel()
	doc := hostNounCatalog(t)
	r := ochazuke.NewRuntime()
	got := mountOn(r, doc)
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings %v", got.Warnings)
	}
	p, _ := r.PropOf("ext-lane", "project")
	if p.Noun != "ext-project" || p.HostNoun != "" {
		t.Fatalf("without a host project the prop keeps the extension's noun: %+v", p)
	}
}

func TestMountKeepsPathPropsAndListItems(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	if w := mountOn(r, hostNounCatalog(t)).Warnings; len(w) != 0 {
		t.Fatalf("warnings %v", w)
	}
	cwd, _ := r.PropOf("ext-lane", "cwd")
	dirs, _ := r.PropOf("ext-lane", "dirs")
	if !cwd.IsPath() || cwd.Path != "dir" {
		t.Fatalf("cwd %+v", cwd.Parameter)
	}
	if dirs.Items == nil || !dirs.Items.IsPath() {
		t.Fatalf("dirs %+v", dirs.Parameter)
	}
}

func TestMountParameterOnMapsItemsAndNeedsNoHost(t *testing.T) {
	t.Parallel()
	p := ochazuke.Parameter{Type: ochazuke.SettingList, Items: &ochazuke.Parameter{Noun: "project", HostNoun: "project"}}
	nouns := map[string]string{"project": "ext-project"}
	if got := MountParameterOn(nil, p, nouns); got.Items.Noun != "ext-project" || got.Items.HostNoun != "" {
		t.Fatalf("no host: %+v", got.Items)
	}
	r := ochazuke.NewRuntime()
	r.Define("project")
	if got := MountParameterOn(r, p, nouns); got.Items.Noun != "project" {
		t.Fatalf("host project: %+v", got.Items)
	}
	if got := MountParameter(p, nouns); got.Items.HostNoun != "" || got.Items.Noun != "ext-project" {
		t.Fatalf("MountParameter asks no host: %+v", got.Items)
	}
}

func TestMountWarnsOfABadHostNoun(t *testing.T) {
	t.Parallel()
	doc := hostNounCatalog(t)
	for i := range doc.Nouns {
		for j := range doc.Nouns[i].Props {
			if doc.Nouns[i].Props[j].Name == "project" {
				doc.Nouns[i].Props[j].HostNoun = "Not A Noun"
			}
		}
	}
	r := ochazuke.NewRuntime()
	got := mountOn(r, doc)
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "hostNoun") {
		t.Fatalf("warnings %v", got.Warnings)
	}
	if p, _ := r.PropOf("ext-lane", "project"); p.Noun != "ext-project" || p.HostNoun != "" {
		t.Fatalf("the prop still mounts: %+v", p)
	}
}

func TestPropRefusesAHostNounThatIsNotANounName(t *testing.T) {
	t.Parallel()
	for _, p := range []ochazuke.PropSpec{
		{Name: "n", Parameter: ochazuke.Parameter{HostNoun: "Not A Noun"}},
		{Name: "m", Parameter: ochazuke.Parameter{HostNoun: "project/x"}},
	} {
		if p.HostNounError() == nil {
			t.Fatalf("%+v: no error", p)
		}
	}
	if err := (ochazuke.PropSpec{Name: "p", Parameter: ochazuke.Parameter{HostNoun: "project"}}).HostNounError(); err != nil {
		t.Fatal(err)
	}
}
