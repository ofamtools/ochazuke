package ochazuke

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sourceRuntime is lineRuntime plus row.send, whose required `to` declares
// From "row:host", on a row r1 whose Data holds host h1 and alt h2.
func sourceRuntime(t *testing.T) (*Runtime, *Palette, *[]string) {
	t.Helper()
	r, p, ran := lineRuntime(t)
	run := r.Operations.At(Key("row.delete")).Run
	r.Register(Operation{Key: "row.send", Parameters: map[string]Parameter{
		"to": {Type: "string", Required: true, From: "row:host"},
	}, Run: run})
	r.Instances["r1"].Data = map[string]any{"host": "h1", "alt": "h2"}
	paletteRequest(t, r, "close", "palette", nil)
	if err := r.SetFocus("r1"); err != nil {
		t.Fatal(err)
	}
	return r, p, ran
}

func wantRan(t *testing.T, ran *[]string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
}

// A declared From fills by key (a bare row), from the help's unbound run,
// from the palette, and from a request that names its target: the same
// value each time.
func TestParameterSourceFillsOnEveryRoute(t *testing.T) {
	r, _, ran := sourceRuntime(t)
	if err := r.Bind(Binding{Noun: "row", Mode: "normal", Keys: "x", Request: Request{Noun: "row", Verb: "send"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "x"}); err != nil {
		t.Fatal(err)
	}
	wantRan(t, ran, `r1 to="h1"`)
	if err := r.RunUnbound("r1", Request{Noun: "row", Verb: "send"}); err != nil {
		t.Fatal(err)
	}
	wantRan(t, ran, `r1 to="h1"`, `r1 to="h1"`)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "row send")
	wantRan(t, ran, `r1 to="h1"`, `r1 to="h1"`, `r1 to="h1"`)
	// A program names its target.
	if _, err := r.Execute(Request{Noun: "row", Verb: "send", Target: "r1"}); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 4 || (*ran)[3] != `r1 to="h1"` {
		t.Fatalf("ran %v", *ran)
	}
}

// An argument given wins; a binding row's From wins over the parameter's.
func TestParameterSourceOrder(t *testing.T) {
	r, _, ran := sourceRuntime(t)
	if err := r.Bind(Binding{Noun: "row", Mode: "normal", Keys: "y", Request: Request{Noun: "row", Verb: "send"}}.FromRow("to", "alt")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "y"}); err != nil {
		t.Fatal(err)
	}
	wantRan(t, ran, `r1 to="h2"`)
	if _, err := r.Execute(Request{Noun: "row", Verb: "send", Target: "r1", Args: Arguments{"to": "given"}}); err != nil {
		t.Fatal(err)
	}
	wantRan(t, ran, `r1 to="h2"`, `r1 to="given"`)
}

// A program that names no target (the API, the CLI, MCP) gets nothing from
// the owner's focus: the argument is required as before.
func TestParameterSourceNeedsATargetForAProgram(t *testing.T) {
	r, _, ran := sourceRuntime(t)
	_, err := r.Execute(Request{Noun: "row", Verb: "send"})
	if err == nil || !strings.Contains(err.Error(), "to") || len(*ran) != 0 {
		t.Fatalf("err %v ran %v", err, *ran)
	}
}

// A row without the field fills nothing.
func TestParameterSourceMissingFieldFillsNothing(t *testing.T) {
	r, _, ran := sourceRuntime(t)
	r.Instances["r1"].Data = map[string]any{"alt": "h2"}
	if err := r.RunUnbound("r1", Request{Noun: "row", Verb: "send"}); err == nil && len(*ran) != 0 {
		t.Fatalf("ran %v", *ran)
	}
}

// A bad From fails at registration.
func TestParameterSourceIsValidated(t *testing.T) {
	for _, bad := range []string{"key", "row: x", "row:", "row:a b", "focus", "row row"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("from %q registered", bad)
				}
			}()
			r := NewRuntime()
			r.Register(Operation{Key: "n.x", Parameters: map[string]Parameter{"a": {Type: "string", From: bad}}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
		}()
	}
}

// The palette slot says what the row fills, and the args form opens
// holding it.
func TestParameterSourceIsShown(t *testing.T) {
	r, _, _ := sourceRuntime(t)
	op := r.Operations.At(Key("row.send"))
	if v, ok := r.sourceValue(op, "to", "r1"); !ok || v != "h1" {
		t.Fatalf("source value %v %v", v, ok)
	}
	r.Operations.Set(withParams(op, map[string]Parameter{"to": {Type: "string", Required: true, From: "row:host"}, "via": {Type: "string", Required: true}}))
	ed, err := r.OpenArgsForm(Request{Noun: "row", Verb: "send"}, func(Request) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := ed.Values()["to"]; got != "h1" {
		t.Fatalf("form to=%v", got)
	}
}

func withParams(op Operation, params map[string]Parameter) Operation {
	op.Parameters = params
	return op
}

// bindings.bind writes a bare action row, and the key it binds fills the
// parameter from its source like any other route.
func TestBoundUnboundVerbInheritsTheParameterSource(t *testing.T) {
	r, _, ran := sourceRuntime(t)
	r.Config().Dir = t.TempDir()
	if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "bind", Args: Arguments{"key": "z", "action": "row.send"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(r.Config().Dir, BindingsFile))
	if err != nil || strings.Contains(string(data), `"from"`) {
		t.Fatalf("file %q, %v", data, err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "z"}); err != nil {
		t.Fatal(err)
	}
	wantRan(t, ran, `r1 to="h1"`)
}
