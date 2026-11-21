package ui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const openUICreatePage = `{
  "tool": "ui.create_page",
  "args": {
    "pane": "w12",
    "title": "Compare v2 architectures",
    "root": {
      "kind": "vstack",
      "gap": 1,
      "children": [
        { "kind": "card", "title": "Prompt", "tone": "muted", "props": { "summary": "Propose 4 ways to separate data and behavior" } },
        { "kind": "grid", "minColW": 36, "gap": 1, "children": [
          { "kind": "card", "id": "opt-1", "focusable": true, "scope": "choice", "title": "1. Concentric rings", "badges": [{"text": "0 UI deps in domain"}] },
          { "kind": "card", "id": "opt-2", "focusable": true, "scope": "choice", "title": "2. Data / View planes", "badges": [{"text": "protocol split"}] }
        ]}
      ]
    }
  }
}`

const openUIAsk = `{
  "tool": "ui.ask",
  "args": {
    "pane": "w12",
    "pageId": "questions",
    "mode": "prepend",
    "card": {
      "kind": "card",
      "id": "q-7",
      "focusable": true,
      "scope": "question",
      "unread": true,
      "title": "Keep or drop Saved Panes?",
      "children": [
        { "kind": "form", "props": {
          "fields": [
            { "name": "decision", "type": "string", "enum": ["keep", "reimagine", "drop"], "required": true },
            { "name": "notes", "type": "string" }
          ],
          "onSubmit": "mcp.answer"
        }}
      ]
    }
  }
}`

func roundTrip(t *testing.T, n ViewNode) ViewNode {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var back ViewNode
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	if !reflect.DeepEqual(n, back) {
		t.Fatalf("round trip changed the tree:\n got %+v\nwant %+v\njson %s", back, n, b)
	}
	return back
}

func TestViewNodeJSONOpenUIExamples(t *testing.T) {
	var page struct {
		Args struct{ Root ViewNode }
	}
	if err := json.Unmarshal([]byte(openUICreatePage), &page); err != nil {
		t.Fatal(err)
	}
	root := roundTrip(t, page.Args.Root)
	grid := root.Children[1]
	if grid.Kind != KindGrid || grid.MinColW != 36 || grid.Gap != 1 || len(grid.Children) != 2 {
		t.Fatalf("grid decoded wrong: %+v", grid)
	}
	if c := grid.Children[0]; !c.Focusable || c.Scope != "choice" || c.Badges[0].Text != "0 UI deps in domain" {
		t.Fatalf("card decoded wrong: %+v", c)
	}
	if b, _ := json.Marshal(root); strings.Contains(string(b), `"box"`) || strings.Contains(string(b), `"padding"`) {
		t.Errorf("zero box/padding should be omitted: %s", b)
	}

	var ask struct {
		Args struct{ Card ViewNode }
	}
	if err := json.Unmarshal([]byte(openUIAsk), &ask); err != nil {
		t.Fatal(err)
	}
	card := roundTrip(t, ask.Args.Card)
	if !card.Unread || card.Scope != "question" {
		t.Fatalf("card decoded wrong: %+v", card)
	}
	form := card.Children[0]
	fields := formFields(&form)
	if len(fields) != 2 || !fields[0].Required || len(fields[0].Enum) != 3 || form.propString("onSubmit") != "mcp.answer" {
		t.Fatalf("form fields decoded wrong: %+v", fields)
	}

	// Both examples render, and the question card yields its own context chain.
	f, _ := Render(root, DefaultDarkTheme(), "opt-1", 80, 20)
	if p := stripANSI(f.ANSI); !strings.Contains(p, "Concentric rings") || !strings.Contains(p, "Prompt") {
		t.Errorf("Open UI page did not render:\n%s", p)
	}
	if got := scopes(ContextChain(card, "q-7", nil)); !reflect.DeepEqual(got, []string{"question", "app"}) {
		t.Errorf("question chain = %v", got)
	}
}

func TestViewNodeJSONBoxAndPadding(t *testing.T) {
	in := `{"kind":"vstack","padding":[1,2,3,4],"box":{"fixedH":3,"flex":2},"children":[{"kind":"text"}]}`
	var n ViewNode
	if err := json.Unmarshal([]byte(in), &n); err != nil {
		t.Fatal(err)
	}
	if n.Padding != [4]int{1, 2, 3, 4} || n.Box.FixedH != 3 || n.Box.Flex != 2 {
		t.Fatalf("decoded wrong: %+v", n)
	}
	roundTrip(t, n)
	b, _ := json.Marshal(n)
	if !strings.Contains(string(b), `"fixedH":3`) || !strings.Contains(string(b), `"padding":[1,2,3,4]`) {
		t.Errorf("marshal = %s", b)
	}
}
