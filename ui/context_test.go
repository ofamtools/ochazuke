package ui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
)

func scopes(chain []core.ContextFrame) []string {
	out := make([]string, len(chain))
	for i, f := range chain {
		out[i] = f.Scope
	}
	return out
}

func feedTree() ViewNode {
	card := func(id, scope, kind string, kids ...ViewNode) ViewNode {
		return ViewNode{ID: id, Kind: KindCard, Focusable: true, Scope: scope, Ref: &core.Ref{Kind: kind, ID: id}, Title: id,
			Facts: map[string]any{"scope": scope}, Children: kids}
	}
	return ViewNode{Kind: KindVStack, Scope: "page", ID: "page", Children: []ViewNode{
		{Kind: KindVStack, ID: "feed", Scope: "feed", Children: []ViewNode{
			card("b1", "git.branch", "git.branch"),
			card("t1", "task", "task"),
			card("q1", "question", "question", ViewNode{Kind: KindInput, ID: "q1.answer", Focusable: true, Scope: "input"}),
		}},
	}}
}

func TestContextChainHeterogeneousCards(t *testing.T) {
	root := feedTree()
	facts := map[string]any{"user": "u1"}
	cases := []struct {
		focus string
		want  []string
		ref   core.Ref
	}{
		{"b1", []string{"git.branch", "feed", "page", "app"}, core.Ref{Kind: "git.branch", ID: "b1"}},
		{"t1", []string{"task", "feed", "page", "app"}, core.Ref{Kind: "task", ID: "t1"}},
		{"q1", []string{"question", "feed", "page", "app"}, core.Ref{Kind: "question", ID: "q1"}},
		{"q1.answer", []string{"input", "question", "feed", "page", "app"}, core.Ref{Kind: "question", ID: "q1"}},
	}
	for _, tc := range cases {
		chain := ContextChain(root, tc.focus, facts)
		if got := scopes(chain); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("focus %s: scopes = %v, want %v", tc.focus, got, tc.want)
			continue
		}
		subject := chain[0].Subject
		if tc.focus == "q1.answer" {
			subject = chain[1].Subject
		}
		if subject == nil || *subject != tc.ref {
			t.Errorf("focus %s: subject = %v, want %v", tc.focus, subject, tc.ref)
		}
		if chain[0].NodeID != tc.focus {
			t.Errorf("focus %s: NodeID = %q", tc.focus, chain[0].NodeID)
		}
		if last := chain[len(chain)-1]; last.Facts["user"] != "u1" {
			t.Errorf("focus %s: app frame facts = %v", tc.focus, last.Facts)
		}
	}
	if chain := ContextChain(root, "b1", nil); chain[0].Facts["scope"] != "git.branch" {
		t.Errorf("node facts missing from frame: %+v", chain[0])
	}
}

func TestContextChainNestedAndFallbacks(t *testing.T) {
	root := ViewNode{Kind: KindSplit, Children: []ViewNode{ // no scope: skipped
		{Kind: KindPanel, Scope: "sidebar", Children: []ViewNode{
			{Kind: KindTree, ID: "tree", Focusable: true, Scope: "tree"},
		}},
		{Kind: KindPanel, Children: []ViewNode{ // Ref without Scope: scope defaults to Ref.Kind
			{Kind: KindTable, ID: "tbl", Focusable: true, Ref: &core.Ref{Kind: "git.log"}},
		}},
	}}
	if got, want := scopes(ContextChain(root, "tree", nil)), []string{"tree", "sidebar", "app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tree chain = %v, want %v", got, want)
	}
	if got, want := scopes(ContextChain(root, "tbl", nil)), []string{"git.log", "app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("table chain = %v, want %v", got, want)
	}
	for _, id := range []string{"", "nope"} {
		if got := scopes(ContextChain(root, id, nil)); !reflect.DeepEqual(got, []string{"app"}) {
			t.Errorf("focus %q chain = %v, want [app]", id, got)
		}
	}
}

func TestOwnerID(t *testing.T) {
	root := ViewNode{Kind: KindVStack, ID: "root", Children: []ViewNode{
		{Kind: KindPanel, Children: []ViewNode{{Kind: KindText}}},
		{Kind: KindCard, ID: "c", Children: []ViewNode{{Kind: KindText}}},
	}}
	cases := map[string]string{"c": "c", "~": "root", "~/0": "root", "~/0/0": "root", "~/1/0": "c", "~/1#head": "c", "~/9": "root"}
	for id, want := range cases {
		if got := OwnerID(root, id); got != want {
			t.Errorf("OwnerID(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestContextChainTraitsOrderAndDedup(t *testing.T) {
	ref := &core.Ref{Kind: "task", ID: "t1"}
	root := ViewNode{
		Kind:  KindVStack,
		ID:    "page",
		Scope: "page",
		Children: []ViewNode{
			{
				Kind:      KindCard,
				ID:        "t1",
				Scope:     "task",
				Traits:    []string{"scroll", "fold", "trait.scroll", "", "trait.fold"},
				Ref:       ref,
				Facts:     map[string]any{"folded": false},
				Focusable: true,
			},
			{
				Kind:      KindScroll,
				ID:        "bare-scroll",
				Traits:    []string{"scroll"},
				Facts:     map[string]any{"atTop": true},
				Focusable: true,
			},
		},
	}

	// Node with Scope + Traits: own scope first, then normalized deduped trait scopes, then outer scopes.
	chain := ContextChain(root, "t1", map[string]any{"user": "u1"})
	want := []string{"task", "trait.scroll", "trait.fold", "page", "app"}
	if got := scopes(chain); !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
	for i := 0; i <= 2; i++ {
		if chain[i].NodeID != "t1" {
			t.Errorf("frame %d NodeID = %q, want t1", i, chain[i].NodeID)
		}
		if chain[i].Subject == nil || *chain[i].Subject != *ref {
			t.Errorf("frame %d Subject = %v, want %v", i, chain[i].Subject, *ref)
		}
		if chain[i].Facts["folded"] != false {
			t.Errorf("frame %d missing Facts: %+v", i, chain[i].Facts)
		}
	}
	// Each frame carries an independent copy of Ref.
	chain[0].Subject.ID = "mutated"
	if chain[1].Subject.ID != "t1" {
		t.Errorf("trait frame Subject aliased node frame Subject: %+v", chain[1].Subject)
	}

	// Node with no Scope/Ref but with Traits still emits its trait frames.
	bareChain := ContextChain(root, "bare-scroll", nil)
	wantBare := []string{"trait.scroll", "page", "app"}
	if got := scopes(bareChain); !reflect.DeepEqual(got, wantBare) {
		t.Fatalf("bare-scroll scopes = %v, want %v", got, wantBare)
	}
	if bareChain[0].NodeID != "bare-scroll" || bareChain[0].Facts["atTop"] != true || bareChain[0].Subject != nil {
		t.Errorf("bare-scroll frame = %+v", bareChain[0])
	}
}

func TestContextChainTraitsKeymapIntegration(t *testing.T) {
	root := ViewNode{
		Kind:  KindVStack,
		ID:    "page",
		Scope: "page",
		Children: []ViewNode{
			{Kind: KindCard, ID: "t1", Scope: "task", Traits: []string{"scroll", "fold"}, Focusable: true},
			{Kind: KindPanel, ID: "doc1", Scope: "doc", Traits: []string{"scroll"}, Focusable: true},
		},
	}

	modes := []core.Mode{{Name: "normal"}}
	baseBindings := []core.Binding{
		{Scope: "trait.scroll", Mode: "normal", Keys: []string{"ctrl+d"}, Command: "motion.half.down"},
		{Scope: "trait.scroll", Mode: "normal", Keys: []string{"ctrl+u"}, Command: "motion.half.up"},
		{Scope: "task", Mode: "normal", Keys: []string{"ctrl+d"}, Command: "task.custom.down"},
		{Scope: "page", Mode: "normal", Keys: []string{"ctrl+u"}, Command: "page.fallback.up"},
	}
	km, err := core.NewKeymap(modes, baseBindings)
	if err != nil {
		t.Fatalf("NewKeymap: %v", err)
	}

	resolve := func(km *core.Keymap, focusID, key string) core.Resolution {
		return km.Resolve(core.KeyQuery{
			Chain: ContextChain(root, focusID, nil),
			Mode:  "normal",
			Key:   key,
		})
	}

	// 1. Binding on scope "trait.scroll" fires automatically on any node declaring "scroll"
	// and beats outer "page" scope.
	res := resolve(km, "doc1", "ctrl+d")
	if res.Outcome != core.Matched || res.Command == nil || res.Command.ID != "motion.half.down" {
		t.Fatalf("doc1 ctrl+d = %+v, want motion.half.down", res)
	}
	res = resolve(km, "doc1", "ctrl+u")
	if res.Outcome != core.Matched || res.Command == nil || res.Command.ID != "motion.half.up" {
		t.Fatalf("doc1 ctrl+u = %+v, want motion.half.up (trait beats outer page)", res)
	}

	// 2. Specific scope "task" overrides "trait.scroll" on the same key, while
	// non-overridden trait keys still fire.
	res = resolve(km, "t1", "ctrl+d")
	if res.Outcome != core.Matched || res.Command == nil || res.Command.ID != "task.custom.down" {
		t.Fatalf("t1 ctrl+d = %+v, want task.custom.down", res)
	}
	res = resolve(km, "t1", "ctrl+u")
	if res.Outcome != core.Matched || res.Command == nil || res.Command.ID != "motion.half.up" {
		t.Fatalf("t1 ctrl+u = %+v, want motion.half.up", res)
	}

	// 3. Rebinding scope "trait.scroll" propagates to all nodes declaring "scroll".
	rebound := []core.Binding{
		{Scope: "trait.scroll", Mode: "normal", Keys: []string{"J"}, Command: "motion.half.down"},
	}
	kmRebound, err := core.NewKeymap(modes, rebound)
	if err != nil {
		t.Fatalf("NewKeymap rebound: %v", err)
	}
	for _, id := range []string{"t1", "doc1"} {
		res = resolve(kmRebound, id, "J")
		if res.Outcome != core.Matched || res.Command == nil || res.Command.ID != "motion.half.down" {
			t.Fatalf("%s J after rebind = %+v, want motion.half.down", id, res)
		}
	}

	// 4. JSON round-trip preserves "traits" and omits when empty.
	withTraits := ViewNode{Kind: KindCard, ID: "t1", Scope: "task", Traits: []string{"scroll", "fold"}}
	raw, err := json.Marshal(withTraits)
	if err != nil {
		t.Fatalf("Marshal withTraits: %v", err)
	}
	if !strings.Contains(string(raw), `"traits":["scroll","fold"]`) {
		t.Fatalf("marshaled JSON missing traits: %s", raw)
	}
	var decoded ViewNode
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal withTraits: %v", err)
	}
	if !reflect.DeepEqual(decoded, withTraits) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", decoded, withTraits)
	}

	noTraits := ViewNode{Kind: KindCard, ID: "t2", Scope: "task"}
	rawEmpty, err := json.Marshal(noTraits)
	if err != nil {
		t.Fatalf("Marshal noTraits: %v", err)
	}
	if strings.Contains(string(rawEmpty), `"traits"`) {
		t.Fatalf("empty traits should be omitted from JSON: %s", rawEmpty)
	}
}
