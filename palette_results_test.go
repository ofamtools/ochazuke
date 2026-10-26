package ochazuke

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func paletteResultFixture(t *testing.T, value any) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	r.Define("record").Focusable()
	r.Instance("origin", Kind("record"))
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	r.Register(Operation{Verb: "list", Noun: "record", Parameters: map[string]Parameter{
		"filter": {Type: "string"},
	}, Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
		if filter := StringArg(args, "filter", ""); filter != "" && filter != "active" {
			t.Errorf("unexpected filter %q", filter)
		}
		return value, nil
	}})
	return r, NewModel(r)
}

func runPaletteResults(t *testing.T, r *Runtime, line string) {
	t.Helper()
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, line)
}

func paletteResultTable(t *testing.T, r *Runtime) *List {
	t.Helper()
	if got := r.TopModal(); got != "palette-results" {
		t.Fatalf("top modal = %q, want results", got)
	}
	list := r.List("__palette-results/list")
	if list == nil {
		t.Fatal("results modal has no list")
	}
	return list
}

func assertPaletteResultCells(t *testing.T, r *Runtime, titles []string, cells [][]string) *List {
	t.Helper()
	list := paletteResultTable(t, r)
	var gotTitles []string
	for _, col := range list.Columns() {
		gotTitles = append(gotTitles, col.Title)
	}
	if !slices.Equal(gotTitles, titles) {
		t.Fatalf("columns = %v, want %v", gotTitles, titles)
	}
	var gotCells [][]string
	for _, item := range list.Items() {
		gotCells = append(gotCells, item.Cells)
	}
	if !reflect.DeepEqual(gotCells, cells) {
		t.Fatalf("cells = %#v, want %#v", gotCells, cells)
	}
	return list
}

func TestPaletteResultsSparseObjects(t *testing.T) {
	rows := []map[string]any{
		{"id": "second", "z": nil, "b": true, "nested": map[string]any{"value": []int{1, 2}}, "number": int64(9007199254740993)},
		{"id": "first", "z": "present", "a": "alpha"},
		{"id": "third", "b": false},
	}
	r, m := paletteResultFixture(t, rows)
	runPaletteResults(t, r, "list record active")
	assertPaletteResultCells(t, r,
		[]string{"id", "b", "z", "a", "nested", "number"},
		[][]string{
			{"second", "true", "null", "", `{"value":[1,2]}`, "9007199254740993"},
			{"first", "", "present", "alpha", "", ""},
			{"third", "false", "", "", "", ""},
		})
	m.Width, m.Height = 180, 30
	view := ansi.Strip(m.View().Content)
	for _, text := range []string{"Results", "second", "first", "9007199254740993"} {
		if !strings.Contains(view, text) {
			t.Errorf("results frame lacks %q:\n%s", text, view)
		}
	}
}

func TestPaletteResultsTypedStructsAndEncodedArrays(t *testing.T) {
	type row struct {
		ID      string `json:"id"`
		Count   int64  `json:"count"`
		Name    string `json:"name,omitempty"`
		Ignored string `json:"-"`
	}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"struct slice", []row{{ID: "b", Count: 9007199254740993, Name: "Bee", Ignored: "secret"}, {ID: "a", Count: 2}}},
		{"struct array", [2]row{{ID: "b", Count: 9007199254740993, Name: "Bee"}, {ID: "a", Count: 2}}},
		{"raw message", json.RawMessage(`[{"id":"b","count":9007199254740993,"name":"Bee"},{"id":"a","count":2}]`)},
		{"JSON string", `[{"id":"b","count":9007199254740993,"name":"Bee"},{"id":"a","count":2}]`},
		{"JSON bytes", []byte(`[{"id":"b","count":9007199254740993,"name":"Bee"},{"id":"a","count":2}]`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := paletteResultFixture(t, tc.value)
			// Object-array results are displayed even without a list verb or Rows declaration.
			r.Register(Operation{Verb: "inspect", Noun: "record", Run: func(*Runtime, Context, Arguments) (any, error) {
				return tc.value, nil
			}})
			runPaletteResults(t, r, "inspect record")
			assertPaletteResultCells(t, r, []string{"count", "id", "name"}, [][]string{
				{"9007199254740993", "b", "Bee"}, {"2", "a", ""},
			})
		})
	}
}

func TestPaletteResultsEmptyLists(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		verb  string
		rows  string
	}{
		{"empty array", []any{}, "list", ""},
		{"nil slice", []map[string]any(nil), "list", ""},
		{"nil list result", nil, "list", ""},
		{"declared rows result", nil, "search", "record"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, m := paletteResultFixture(t, tc.value)
			if tc.verb != "list" {
				r.Register(Operation{Verb: tc.verb, Noun: "record", Rows: tc.rows, Run: func(*Runtime, Context, Arguments) (any, error) { return tc.value, nil }})
			}
			runPaletteResults(t, r, tc.verb+" record")
			if list := paletteResultTable(t, r); list.Len() != 0 {
				t.Fatalf("empty results contain %d rows", list.Len())
			}
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "No rows") {
				t.Fatalf("empty results frame:\n%s", view)
			}
		})
	}
}

func TestPaletteResultsLeaveOtherJSONAlone(t *testing.T) {
	for _, value := range []any{
		map[string]any{"id": "one"}, []string{"one"}, []any{map[string]any{"id": "one"}, nil},
		"plain text", 42, true, json.RawMessage(`{"id":"one"}`), "[{broken", `[{"id":"one"}] trailing`,
	} {
		t.Run(jsonText(value), func(t *testing.T) {
			r, _ := paletteResultFixture(t, value)
			runPaletteResults(t, r, "list record")
			if got := r.TopModal(); got != "" || r.Focus != "origin" {
				t.Fatalf("non-table result changed origin: modal=%q focus=%q", got, r.Focus)
			}
		})
	}
}

func TestPaletteResultsArePaletteOnly(t *testing.T) {
	rows := []map[string]string{{"id": "one"}}
	r, _ := paletteResultFixture(t, rows)
	r.Define("record").Mode("normal").Bind("L", Do("record.list"))
	got := paletteRequest(t, r, "list", "record", nil)
	if !reflect.DeepEqual(got, rows) || r.TopModal() != "" {
		t.Fatalf("API list = %#v, modal=%q", got, r.TopModal())
	}
	if _, err := r.Dispatch(MacroKey{Key: "L"}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "" || r.Focus != "origin" {
		t.Fatalf("key list opened results: modal=%q focus=%q", r.TopModal(), r.Focus)
	}
	runPaletteResults(t, r, "list record")
	assertPaletteResultCells(t, r, []string{"id"}, [][]string{{"one"}})
}

func TestPaletteResultsReadOnlyNavigationAndClose(t *testing.T) {
	r, m := paletteResultFixture(t, []map[string]string{{"id": "one"}, {"id": "two"}})
	var applied []string
	r.Define("record").Mode("normal").Bind("enter", Do("record.apply")).
		Verb("apply", func(c Ctx) Result {
			applied = append(applied, c.Instance.ID)
			return Done(nil)
		})
	runPaletteResults(t, r, "list record")
	list := assertPaletteResultCells(t, r, []string{"id"}, [][]string{{"one"}, {"two"}})
	for _, key := range []string{"j", "enter"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("results key %q: %v", key, err)
		}
	}
	selected, ok := list.Selected()
	if !ok || !slices.Equal(selected.Cells, []string{"two"}) || len(applied) != 0 || r.TopModal() != "palette-results" {
		t.Fatalf("read-only selection=%+v applied=%v modal=%q", selected, applied, r.TopModal())
	}
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "record.apply") {
		t.Fatalf("application action offered on results:\n%s", view)
	}
	if _, err := r.Dispatch(MacroKey{Key: "esc"}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "" || r.Focus != "origin" {
		t.Fatalf("close restored modal=%q focus=%q", r.TopModal(), r.Focus)
	}
	if _, err := r.Dispatch(MacroKey{Key: "enter"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied, []string{"origin"}) {
		t.Fatalf("origin application action = %v", applied)
	}
}

func TestPaletteResultsPendingCompletion(t *testing.T) {
	for _, otherModal := range []bool{false, true} {
		t.Run(map[bool]string{false: "opens results", true: "preserves newer modal"}[otherModal], func(t *testing.T) {
			r := NewRuntime()
			r.Define("record").Focusable()
			r.Instance("origin", Kind("record"))
			if err := r.SetFocus("origin"); err != nil {
				t.Fatal(err)
			}
			m := NewModel(r)
			var landed [][]map[string]string
			r.Define("record").Verb("list", func(c Ctx) Result {
				return Pending(func(context.Context) (any, error) {
					return []map[string]string{{"id": "later"}}, nil
				}).Then(func(c Ctx, value any, err error) {
					if err != nil {
						t.Errorf("completion: %v", err)
						return
					}
					if c.Instance.ID != "origin" {
						t.Errorf("completion owner = %q", c.Instance.ID)
					}
					landed = append(landed, value.([]map[string]string))
				})
			})
			runPaletteResults(t, r, "list record")
			if r.TopModal() != "" || r.Focus != "origin" {
				t.Fatalf("pending list did not return to origin: modal=%q focus=%q", r.TopModal(), r.Focus)
			}
			cmd := r.PendingCommands()
			if cmd == nil {
				t.Fatal("pending list scheduled no work")
			}
			if otherModal {
				if _, err := r.OpenModal("question", Payload{Prompt: "Keep this question"}); err != nil {
					t.Fatal(err)
				}
				m.ModalViews["question"] = ModalView{Title: "Question", Render: func(*Runtime, *Instance, int, int) Block { return Text("Keep this question") }}
			}
			for _, msg := range runCmd(cmd) {
				if handled, err := r.Deliver(msg); !handled || err != nil {
					t.Fatalf("deliver pending list: handled=%v err=%v", handled, err)
				}
			}
			if !reflect.DeepEqual(landed, [][]map[string]string{{{"id": "later"}}}) {
				t.Fatalf("existing Then received %#v", landed)
			}
			if otherModal {
				if r.TopModal() != "question" || !strings.Contains(ansi.Strip(m.View().Content), "Keep this question") {
					t.Fatalf("pending result replaced question: modal=%q", r.TopModal())
				}
			} else {
				assertPaletteResultCells(t, r, []string{"id"}, [][]string{{"later"}})
				if view := ansi.Strip(m.View().Content); !strings.Contains(view, "later") {
					t.Fatalf("pending results frame:\n%s", view)
				}
			}
		})
	}
}
