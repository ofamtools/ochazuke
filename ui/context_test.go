package ui

import (
	"reflect"
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
