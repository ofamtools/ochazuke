package ochazuke

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// pageTestRegister builds a page holding one panel with one input: the page
// instance (noun page), a panel instance beneath it and the input beneath that.
func pageTestRegister(t *testing.T, r *Runtime, id string) *Page {
	t.Helper()
	r.Instance(id)
	r.Instance(id+"-editor", Parent(id))
	if err := r.AddInput(NewInput(id+"-input", id, false), Parent(id+"-editor")); err != nil {
		t.Fatal(err)
	}
	page := &Page{ID: id, Title: strings.ToUpper(id), Render: func(*Runtime, int, int) Block { return Text(id) }}
	if err := r.RegisterPage(page); err != nil {
		t.Fatal(err)
	}
	return page
}

func pageTestSnapshot(t *testing.T, r *Runtime) string {
	t.Helper()
	data, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPageRegistrationAndPublicSnapshot(t *testing.T) {
	r := NewRuntime()
	original := r.Frames[0]
	r.Pending = "g"
	alpha := pageTestRegister(t, r, "alpha")
	pageTestRegister(t, r, "beta")
	if r.Page != "" || len(r.Frames) != 1 || r.Frames[0] != original || r.Pending != "g" {
		t.Fatal("registering pages changed the active frame or pending sequence")
	}
	if pages := r.PageList(); len(pages) != 2 || pages[0] != alpha || pages[1].ID != "beta" || r.PageOf("alpha") != alpha {
		t.Fatalf("registration did not retain page order and identity: %v", pages)
	}
	want := []map[string]any{{"id": "alpha", "title": "ALPHA", "active": false}, {"id": "beta", "title": "BETA", "active": false}}
	if got := paletteRequest(t, r, "list", "pages", nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("pages=%v; want %v", got, want)
	}
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	want[0]["active"] = true
	if got := paletteRequest(t, r, "list", "pages", nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("pages=%v; want %v", got, want)
	}
	for path, want := range map[string]any{"/page": "alpha", "/pages/alpha/id": "alpha", "/pages/alpha/title": "ALPHA", "/pages/alpha/frame/focus": "alpha", "/instances/alpha/noun": "alpha", "/instances/alpha-input/parent": "alpha-editor"} {
		if got := paletteRequest(t, r, "get", "state", Arguments{"path": path}); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s=%v; want %v", path, got, want)
		}
	}
	if _, err := r.Execute(Request{Verb: "get", Noun: "state", Args: Arguments{"path": "/pages/alpha/render"}}); err == nil {
		t.Fatal("renderer must not be exposed through public state")
	}
}

func TestPageSwitchRestoresFrameFocusAndEditingState(t *testing.T) {
	r := NewRuntime()
	alpha := pageTestRegister(t, r, "alpha")
	beta := pageTestRegister(t, r, "beta")
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	paletteRequest(t, r, "focus", "input", Arguments{"id": "alpha-input"})
	in := r.Inputs["alpha-input"]
	in.Cursor = 2
	paletteRequest(t, r, "set", "mode", Arguments{"mode": "insert"})
	paletteRequest(t, r, "insert", "input", Arguments{"text": "X"})
	before := paletteInputSnapshot(t, in)
	context := r.Context()
	paletteRequest(t, r, "open", "page", Arguments{"id": "beta"})
	paletteRequest(t, r, "set", "mode", Arguments{"mode": "insert"})
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	if len(r.Frames) != 1 || r.Frames[0] != alpha.Frame || r.Inputs[r.Context().InputID] != in || !reflect.DeepEqual(r.Context(), context) {
		t.Fatalf("page identity or nested focus was lost: %+v", r.Context())
	}
	if got := paletteInputSnapshot(t, in); got != before || r.Mode(r.Context()) != "insert" {
		t.Fatalf("page editing state was lost: %s", got)
	}
	paletteRequest(t, r, "undo", "input", nil)
	if string(in.Text) != "alpha" || string(r.Inputs["beta-input"].Text) != "beta" {
		t.Fatal("page switch lost the originating undo history or changed another input")
	}
	paletteRequest(t, r, "open", "page", Arguments{"id": "beta"})
	if r.Frames[0] != beta.Frame || r.Mode(r.Context()) != "insert" || r.Context().InputID != "" {
		t.Fatal("page-owned frame mode, state, or non-input context was lost")
	}
}

func TestPageBindingsAndActionsFollowActiveContext(t *testing.T) {
	r := NewRuntime()
	r.Register(Operation{Verb: "inspect", Noun: "owner", Run: func(*Runtime, Context, Arguments) (any, error) { return "app", nil }})
	for _, id := range []string{"alpha", "beta"} {
		pageTestRegister(t, r, id)
		if err := r.Bind(Binding{Instance: id, Keys: "x", Request: Request{Verb: "insert", Noun: "input", Args: Arguments{"text": strings.ToUpper(id)}}}); err != nil {
			t.Fatal(err)
		}
		r.Define(id+"-editor").Action("inspect.owner", func(*Runtime, Context, Arguments) (any, error) { return id, nil })
	}
	for _, id := range []string{"alpha", "beta"} {
		paletteRequest(t, r, "open", "page", Arguments{"id": id})
		paletteRequest(t, r, "focus", "input", Arguments{"id": id + "-input"})
		if handled, err := r.HandleBinding("x"); !handled || err != nil {
			t.Fatalf("%s binding: handled=%v error=%v", id, handled, err)
		}
		if got := paletteRequest(t, r, "inspect", "owner", nil); got != id {
			t.Fatalf("owner=%v; want %s", got, id)
		}
	}
	if string(r.Inputs["alpha-input"].Text) != "ALPHAalpha" || string(r.Inputs["beta-input"].Text) != "BETAbeta" {
		t.Fatal("page binding was resolved against an inactive page")
	}
}

func TestPageModalNavigationErrorsAreAtomic(t *testing.T) {
	r := NewRuntime()
	alpha := pageTestRegister(t, r, "alpha")
	pageTestRegister(t, r, "beta")
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	r.Instance("dialog")
	dialog := &Frame{ID: "dialog", Focus: "dialog"}
	if err := r.Push(dialog); err != nil {
		t.Fatal(err)
	}
	r.Pending = "g"
	before := pageTestSnapshot(t, r)
	for _, q := range []Request{{Verb: "open", Noun: "page", Args: Arguments{"id": "beta"}}, {Verb: "open", Noun: "page", Args: Arguments{"id": "alpha"}}, {Verb: "next", Noun: "page"}, {Verb: "previous", Noun: "page"}} {
		if _, err := r.Execute(q); err == nil {
			t.Fatalf("%s switched pages under a modal", q.Key())
		}
		if pageTestSnapshot(t, r) != before || r.Frames[0] != alpha.Frame || r.Frames[1] != dialog {
			t.Fatalf("%s changed state despite rejecting navigation", q.Key())
		}
	}
	paletteRequest(t, r, "pop", "modal", nil)
	paletteRequest(t, r, "open", "page", Arguments{"id": "beta"})
	if r.Page != "beta" || r.Pending != "" {
		t.Fatal("navigation did not resume after closing the modal")
	}
}

func TestPageRegistrationRejectsInvalidPagesAtomically(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Page) *Page
	}{
		{"nil", func(*Page) *Page { return nil }},
		{"empty ID", func(p *Page) *Page { p.ID = " "; return p }},
		{"empty title", func(p *Page) *Page { p.Title = " "; return p }},
		{"duplicate ID", func(p *Page) *Page { p.ID = "existing"; return p }},
		{"duplicate title", func(p *Page) *Page { p.Title = "EXISTING"; return p }},
		{"nil renderer", func(p *Page) *Page { p.Render = nil; return p }},
		{"unknown focus", func(p *Page) *Page { p.Focus = "missing"; return p }},
		{"focus on another page", func(p *Page) *Page { p.Focus = "existing-input"; return p }},
		{"nested instance", func(p *Page) *Page { p.ID = "existing-editor"; p.Focus = "existing-editor"; return p }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRuntime()
			pageTestRegister(t, r, "existing")
			candidate := r.Instance("candidate")
			if err := r.Bind(Binding{Noun: "candidate", Keys: "q", Request: Request{Verb: "quit", Noun: "app"}}); err != nil {
				t.Fatal(err)
			}
			r.Pending = "g"
			page := tc.change(&Page{ID: "candidate", Title: "Candidate", Focus: "candidate", Render: func(*Runtime, int, int) Block { return Text("candidate") }})
			before := pageTestSnapshot(t, r)
			if err := r.RegisterPage(page); err == nil {
				t.Fatal("invalid page was registered")
			}
			if pageTestSnapshot(t, r) != before || len(r.PageList()) != 1 || r.Instances["candidate"] != candidate || candidate.Component != nil {
				t.Fatal("failed registration mutated runtime state or replaced an existing instance")
			}
		})
	}
}

func TestPageNavigationWrapsAndRejectsInvalidRequests(t *testing.T) {
	r := NewRuntime()
	for _, verb := range []string{"next", "previous"} {
		if _, err := r.Execute(Request{Verb: verb, Noun: "page"}); err == nil {
			t.Fatalf("%s accepted an empty page registry", verb)
		}
	}
	pageTestRegister(t, r, "alpha")
	pageTestRegister(t, r, "beta")
	pageTestRegister(t, r, "gamma")
	for _, step := range []struct{ verb, want string }{{"next", "alpha"}, {"previous", "gamma"}, {"next", "alpha"}, {"next", "beta"}, {"next", "gamma"}, {"previous", "beta"}} {
		paletteRequest(t, r, step.verb, "page", nil)
		if r.Page != step.want || r.Frames[0] != r.PageOf(step.want).Frame {
			t.Fatalf("%s: active=%s; want %s", step.verb, r.Page, step.want)
		}
	}
	r.Pending = "g"
	before := pageTestSnapshot(t, r)
	for _, q := range []Request{{Verb: "open", Noun: "page"}, {Verb: "open", Noun: "page", Args: Arguments{"id": 1}}, {Verb: "open", Noun: "page", Args: Arguments{"id": "missing"}}, {Verb: "list", Noun: "pages", Args: Arguments{"extra": true}}, {Verb: "next", Noun: "page", Args: Arguments{"id": "alpha"}}, {Verb: "previous", Noun: "page", Args: Arguments{"extra": true}}} {
		if _, err := r.Execute(q); err == nil || pageTestSnapshot(t, r) != before {
			t.Fatalf("%+v must fail without mutation: %v", q, err)
		}
	}
	single := NewRuntime()
	pageTestRegister(t, single, "only")
	for _, verb := range []string{"previous", "next", "previous"} {
		paletteRequest(t, single, verb, "page", nil)
		if single.Page != "only" {
			t.Fatalf("single-page %s: active=%s", verb, single.Page)
		}
	}
	reverse := NewRuntime()
	pageTestRegister(t, reverse, "alpha")
	pageTestRegister(t, reverse, "beta")
	paletteRequest(t, reverse, "previous", "page", nil)
	if reverse.Page != "beta" {
		t.Fatalf("previous from no active page: active=%s; want beta", reverse.Page)
	}
}

func TestPageFocusIsolationAllowsExplicitOffscreenEdits(t *testing.T) {
	r := NewRuntime()
	alpha := pageTestRegister(t, r, "alpha")
	pageTestRegister(t, r, "beta")
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	paletteRequest(t, r, "focus", "input", Arguments{"id": "alpha-input"})
	r.Pending = "g"
	before := pageTestSnapshot(t, r)
	if err := r.Focus("beta-input"); err == nil || pageTestSnapshot(t, r) != before {
		t.Fatalf("cross-page Focus must fail without mutation: %v", err)
	}
	for _, q := range []Request{
		{Verb: "focus", Noun: "input", Args: Arguments{"id": "beta-input"}},
		{Verb: "select", Noun: "context", Args: Arguments{"path": `["app","beta"]`}},
		{Verb: "select", Noun: "context", Args: Arguments{"path": `["app","beta","beta-editor"]`, "input": "beta-input"}},
		{Verb: "select", Noun: "context", Args: Arguments{"path": `["app","alpha","alpha-editor"]`, "input": "beta-input"}},
	} {
		if _, err := r.Execute(q); err == nil || pageTestSnapshot(t, r) != before {
			t.Fatalf("cross-page selection must fail without mutation: %+v: %v", q, err)
		}
	}
	offscreen := Context{Path: []string{"app", "beta", "beta-editor"}, InputID: "beta-input"}
	if _, err := r.ExecuteAt(offscreen, Request{Verb: "insert", Noun: "input", Args: Arguments{"text": "off-"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "insert", Noun: "input", Target: "beta-input", Args: Arguments{"text": "target-"}}); err != nil {
		t.Fatal(err)
	}
	if r.Page != "alpha" || r.Frames[0] != alpha.Frame || r.Context().InputID != "alpha-input" || string(r.Inputs["alpha-input"].Text) != "alpha" || string(r.Inputs["beta-input"].Text) != "off-target-beta" || r.Pending != "g" {
		t.Fatal("targeted edits navigated or edited the wrong input")
	}
	paletteRequest(t, r, "select", "context", Arguments{"path": `["app","alpha","alpha-editor"]`})
	if r.Context().InputID != "" || !reflect.DeepEqual(r.Context().Path, []string{"app", "alpha", "alpha-editor"}) {
		t.Fatal("same-page nested noun selection failed")
	}
	for _, id := range []string{"palette", "dialog", "picker"} {
		r.Instance(id)
		if err := r.AddInput(NewInput(id+"-field", "", false), Parent(id)); err != nil {
			t.Fatal(err)
		}
		if err := r.Push(&Frame{ID: id, Focus: id}); err != nil {
			t.Fatal(err)
		}
		paletteRequest(t, r, "focus", "input", Arguments{"id": id + "-field"})
		path, err := json.Marshal([]string{"app", id})
		if err != nil {
			t.Fatal(err)
		}
		paletteRequest(t, r, "select", "context", Arguments{"path": string(path), "input": id + "-field"})
		if r.Context().InputID != id+"-field" || r.Page != "alpha" {
			t.Fatalf("valid modal context %s was not selected", id)
		}
		paletteRequest(t, r, "pop", "modal", nil)
	}
}

func TestPagePaletteClosesBeforeNavigationDispatch(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "alpha")
	beta := pageTestRegister(t, r, "beta")
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "verb:open", "page")
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "id", "value": "beta"})
	paletteRequest(t, r, "execute", "palette", nil)
	if p.IsOpen() || len(r.Frames) != 1 || r.Frames[0] != beta.Frame || r.Page != "beta" {
		t.Fatal("palette navigation was blocked or switched a discarded overlay")
	}
}
