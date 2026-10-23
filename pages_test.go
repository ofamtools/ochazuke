package ochazuke

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func pageTestRegister(t *testing.T, r *Runtime, id string) *Page {
	t.Helper()
	r.Layers[id] = &Layer{ID: id, Noun: id}
	r.Layers[id+"-editor"] = &Layer{ID: id + "-editor", Noun: "editor"}
	if err := r.AddInput(NewInput(id+"-input", id, false), []string{"app", id, id + "-editor"}); err != nil {
		t.Fatal(err)
	}
	page := &Page{ID: id, Title: strings.ToUpper(id), Frame: &Frame{ID: id, Context: Context{Path: []string{"app", id}}}, Render: func(*Runtime, int, int) string { return id }}
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
	if r.ActivePage != "" || len(r.Frames) != 1 || r.Frames[0] != original || r.Pending != "g" {
		t.Fatal("registering pages changed the active frame or pending sequence")
	}
	if !reflect.DeepEqual(r.PageOrder, []string{"alpha", "beta"}) || r.Pages["alpha"] != alpha {
		t.Fatalf("registration did not retain page order and identity: %v", r.PageOrder)
	}
	want := []map[string]any{{"id": "alpha", "title": "ALPHA", "active": false}, {"id": "beta", "title": "BETA", "active": false}}
	if got := paletteRequest(t, r, "list", "pages", nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("pages=%v; want %v", got, want)
	}
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	alpha.Frame.State["draft"] = "retained"
	want[0]["active"] = true
	if got := paletteRequest(t, r, "list", "pages", nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("pages=%v; want %v", got, want)
	}
	for path, want := range map[string]any{"/activePage": "alpha", "/pages/alpha/id": "alpha", "/pages/alpha/title": "ALPHA", "/pages/alpha/frame/context/path/1": "alpha", "/pages/alpha/frame/state/draft": "retained"} {
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
	in.Kill, in.Count, in.Pending = "clipboard", 2, "g"
	alpha.Frame.State["draft"] = "page draft"
	before := paletteInputSnapshot(t, in)
	context := r.Context()
	paletteRequest(t, r, "open", "page", Arguments{"id": "beta"})
	paletteRequest(t, r, "set", "mode", Arguments{"mode": "insert"})
	beta.Frame.State["selection"] = "second row"
	paletteRequest(t, r, "open", "page", Arguments{"id": "alpha"})
	if len(r.Frames) != 1 || r.Frames[0] != alpha.Frame || r.Inputs[r.Context().InputID] != in || !reflect.DeepEqual(r.Context(), context) {
		t.Fatalf("page identity or nested focus was lost: %+v", r.Context())
	}
	if got := paletteInputSnapshot(t, in); got != before || alpha.Frame.State["draft"] != "page draft" || r.Mode(r.Context()) != "insert" {
		t.Fatalf("page editing state was lost: %s", got)
	}
	paletteRequest(t, r, "undo", "input", nil)
	if string(in.Text) != "alpha" || string(r.Inputs["beta-input"].Text) != "beta" {
		t.Fatal("page switch lost the originating undo history or changed another input")
	}
	paletteRequest(t, r, "open", "page", Arguments{"id": "beta"})
	if r.Frames[0] != beta.Frame || r.Mode(r.Context()) != "insert" || beta.Frame.State["selection"] != "second row" || r.Context().InputID != "" {
		t.Fatal("page-owned frame mode, state, or non-input context was lost")
	}
}

func TestPageBindingsAndActionsFollowActiveContext(t *testing.T) {
	r := NewRuntime()
	r.Register(Operation{Verb: "inspect", Noun: "owner", Run: func(*Runtime, Context, Arguments) (any, error) { return "app", nil }})
	for _, id := range []string{"alpha", "beta"} {
		pageTestRegister(t, r, id)
		r.Layers[id].Bindings = []Binding{{Keys: "x", Request: Request{Verb: "insert", Noun: "input", Args: Arguments{"text": strings.ToUpper(id)}}}}
		r.Layers[id].Actions = map[string]Handler{"inspect.owner": func(*Runtime, Context, Arguments) (any, error) { return id, nil }}
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
	r.Layers["dialog"] = &Layer{ID: "dialog"}
	dialog := &Frame{ID: "dialog", Context: Context{Path: []string{"app", "dialog"}}, State: map[string]any{"draft": "unsaved"}}
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
	if r.ActivePage != "beta" || r.Pending != "" {
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
		{"nil frame", func(p *Page) *Page { p.Frame = nil; return p }},
		{"nil renderer", func(p *Page) *Page { p.Render = nil; return p }},
		{"missing page path", func(p *Page) *Page { p.Frame.Context.Path = []string{"app"}; return p }},
		{"wrong root", func(p *Page) *Page { p.Frame.Context.Path[0] = "candidate"; return p }},
		{"wrong page", func(p *Page) *Page { p.Frame.Context.Path[1] = "existing"; return p }},
		{"missing page layer", func(p *Page) *Page {
			p.ID = "missing"
			p.Frame.Context.Path = []string{"app", "missing"}
			return p
		}},
		{"unknown layer", func(p *Page) *Page { p.Frame.Context.Path = append(p.Frame.Context.Path, "missing"); return p }},
		{"repeated layer", func(p *Page) *Page { p.Frame.Context.Path = append(p.Frame.Context.Path, "candidate"); return p }},
		{"unknown input", func(p *Page) *Page { p.Frame.Context.InputID = "missing"; return p }},
		{"wrong input path", func(p *Page) *Page { p.Frame.Context.InputID = "existing-input"; return p }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRuntime()
			pageTestRegister(t, r, "existing")
			layer := &Layer{ID: "candidate", Noun: "candidate", Bindings: []Binding{{Keys: "q", Request: Request{Verb: "quit", Noun: "app"}}}}
			r.Layers["candidate"] = layer
			r.Pending = "g"
			candidate := tc.change(&Page{ID: "candidate", Title: "Candidate", Frame: &Frame{Context: Context{Path: []string{"app", "candidate"}}}, Render: func(*Runtime, int, int) string { return "candidate" }})
			before := pageTestSnapshot(t, r)
			if err := r.RegisterPage(candidate); err == nil {
				t.Fatal("invalid page was registered")
			}
			if pageTestSnapshot(t, r) != before || !reflect.DeepEqual(r.PageOrder, []string{"existing"}) || r.Layers["candidate"] != layer {
				t.Fatal("failed registration mutated runtime state or replaced an existing layer")
			}
			if candidate != nil && candidate.Frame != nil && candidate.Frame.State != nil {
				t.Fatal("failed registration mutated the candidate frame")
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
		if r.ActivePage != step.want || r.Frames[0] != r.Pages[step.want].Frame {
			t.Fatalf("%s: active=%s; want %s", step.verb, r.ActivePage, step.want)
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
		if single.ActivePage != "only" {
			t.Fatalf("single-page %s: active=%s", verb, single.ActivePage)
		}
	}
	reverse := NewRuntime()
	pageTestRegister(t, reverse, "alpha")
	pageTestRegister(t, reverse, "beta")
	paletteRequest(t, reverse, "previous", "page", nil)
	if reverse.ActivePage != "beta" {
		t.Fatalf("previous from no active page: active=%s; want beta", reverse.ActivePage)
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
	if r.ActivePage != "alpha" || r.Frames[0] != alpha.Frame || r.Context().InputID != "alpha-input" || string(r.Inputs["alpha-input"].Text) != "alpha" || string(r.Inputs["beta-input"].Text) != "off-target-beta" || r.Pending != "g" {
		t.Fatal("targeted edits navigated or edited the wrong input")
	}
	paletteRequest(t, r, "select", "context", Arguments{"path": `["app","alpha","alpha-editor"]`})
	if r.Context().InputID != "" || !reflect.DeepEqual(r.Context().Path, []string{"app", "alpha", "alpha-editor"}) {
		t.Fatal("same-page nested noun selection failed")
	}
	for _, id := range []string{"palette", "dialog", "picker"} {
		r.Layers[id] = &Layer{ID: id}
		if err := r.AddInput(NewInput(id+"-field", "", false), []string{"app", id}); err != nil {
			t.Fatal(err)
		}
		if err := r.Push(&Frame{ID: id, Context: Context{Path: []string{"app", id}}}); err != nil {
			t.Fatal(err)
		}
		paletteRequest(t, r, "focus", "input", Arguments{"id": id + "-field"})
		path, err := json.Marshal([]string{"app", id})
		if err != nil {
			t.Fatal(err)
		}
		paletteRequest(t, r, "select", "context", Arguments{"path": string(path), "input": id + "-field"})
		if r.Context().InputID != id+"-field" || r.ActivePage != "alpha" {
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
	if p.IsOpen() || len(r.Frames) != 1 || r.Frames[0] != beta.Frame || r.ActivePage != "beta" {
		t.Fatal("palette navigation was blocked or switched a discarded overlay")
	}
}
