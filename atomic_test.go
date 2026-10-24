package ochazuke_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"ochazuke"
	"ochazuke/examples/demo"
)

// A binding row is atomic: a Request to one registered operation with its
// arguments from the row, the key or the payload, a mode transition, or an
// unbind. No row runs Go code with the key (Binding has no func field), so
// composite behaviour is an operation that calls operations and a condition
// is a named guard. Every table of the framework, the model, the picker and
// the demo validates, and the resolver names no key: the only code that
// compares key names is the classifier and the key-text table in noun.go.
func TestBindingRowsAreAtomic(t *testing.T) {
	binding := reflect.TypeOf(ochazuke.Binding{})
	for i := 0; i < binding.NumField(); i++ {
		if f := binding.Field(i); f.Type.Kind() == reflect.Func {
			t.Fatalf("Binding.%s is a func: a row may not run Go code", f.Name)
		}
	}
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	for name, rt := range map[string]*ochazuke.Runtime{"runtime+model+picker": r, "demo": demo.New().Runtime} {
		warnings, err := rt.ValidateBindings()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(warnings) != 0 {
			t.Fatalf("%s: rows still use the verb.noun spelling: %v", name, warnings)
		}
	}

	keyNames := map[string]bool{}
	for _, noun := range r.Nouns {
		for _, rows := range noun.Bindings {
			for _, row := range rows {
				for _, key := range strings.Fields(row.Keys) {
					keyNames[key] = true
				}
			}
		}
	}
	for _, key := range []string{"esc", "enter", "tab", "space", "backspace", "delete", "insert", "home", "end", "pgup", "pgdown", "up", "down", "left", "right"} {
		keyNames[key] = true
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "resolve.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		// An operation's Verb, Noun and prose are names, not keys.
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && (id.Name == "Verb" || id.Name == "Noun" || id.Name == "Description" || id.Name == "Result") {
				return false
			}
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"ctrl+", "alt+", "shift+"} {
			if strings.HasPrefix(value, prefix) {
				keyNames[value] = true
			}
		}
		if keyNames[value] {
			t.Errorf("%s: the resolver names the key %q; a key belongs in a row", fset.Position(lit.Pos()), value)
		}
		return true
	})
}

// A config row carries what the Go constructors carry: an Enter row with a
// prefix and a stored request (the terminal leader's shape) installs from
// JSON and arms the sequence timer the same way.
func TestEnterRowFromConfigCarriesPrefixAndRequest(t *testing.T) {
	r := ochazuke.NewRuntime()
	var sent []string
	r.Register(ochazuke.Operation{Verb: "forward", Noun: "pane", Parameters: map[string]ochazuke.Parameter{"keys": {Type: "string", Required: true}}, Run: func(_ *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) (any, error) {
		sent = append(sent, ochazuke.StringArg(a, "keys", ""))
		return nil, nil
	}})
	r.DefineMode("leader_pending", true)
	r.Define("term")
	r.Instance("pane", ochazuke.Kind("term"))
	rows := []ochazuke.Binding{
		{Noun: "term", Mode: "normal", Keys: "ctrl+b", Request: ochazuke.Request{Verb: "enter", Noun: "mode", Args: ochazuke.Arguments{"mode": "leader_pending", "prefix": "ctrl+b", "request": map[string]any{"verb": "forward", "noun": "pane", "args": map[string]any{"keys": "ctrl+b"}}}}},
		{Noun: "term", Mode: "leader_pending", Pattern: ochazuke.Any, Request: ochazuke.Request{Verb: "forward", Noun: "pane"}, From: map[string]string{"keys": "prefix key"}},
	}
	if err := r.Bind(rows...); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("pane"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ctrl+b", "z"} {
		if _, err := r.Dispatch(ochazuke.MacroKey{Key: key}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if _, err := r.Dispatch(ochazuke.MacroKey{Key: "ctrl+b"}); err != nil || r.Pending != "ctrl+b" {
		t.Fatalf("leader alone: err=%v pending=%q", err, r.Pending)
	}
	if !reflect.DeepEqual(sent, []string{"ctrl+b z"}) {
		t.Fatalf("sent %v", sent)
	}
}
