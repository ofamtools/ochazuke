package ochazuke

import (
	"fmt"
	"strings"
	"testing"
)

// paramKindProbes are the texts every route is asked about, per kind.
var paramKindProbes = []string{"x", "7", "1.5", "true", "false", "TRUE", "t", " 7 ", "[1]", `{"a":1}`}

// paramKindRows is every parameter type the routes know, the empty one and
// one nothing knows.
var paramKindRows = []string{"", "string", "path", "any", "integer", "number", "boolean", "array", "object", "duration", "weird"}

func routeShow(v any, err error) string {
	if err != nil {
		return "ERR " + err.Error()
	}
	return fmt.Sprintf("%T:%v", v, v)
}

// kindFormRoute is the args form: formable, then argumentsFrom per probe.
func kindFormRoute(kind string) string {
	p := Parameter{Type: kind}
	if !formable(p) {
		return "not formable"
	}
	op := Operation{Parameters: map[string]Parameter{"x": p}}
	var out []string
	for _, text := range paramKindProbes {
		args, err := argumentsFrom(op, nil, Record{"x": text})
		out = append(out, routeShow(args["x"], err))
	}
	return strings.Join(out, " | ")
}

// kindPaletteRoute is the palette's argument conversion per probe.
func kindPaletteRoute(t *testing.T, kind string) string {
	r := NewRuntime()
	pal := NewPalette(r)
	pal.origin = r.Context()
	op := Operation{Key: "k.v", Parameters: map[string]Parameter{"x": {Type: kind}}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }}
	r.Register(op)
	var out []string
	for _, text := range paramKindProbes {
		v, _, _, err := pal.argument(op, "x", text, Arguments{})
		out = append(out, routeShow(v, err))
	}
	return strings.Join(out, " | ")
}

// kindRuntimeRoute is checkArguments' verdict on each Go value a caller
// might pass.
func kindRuntimeRoute(kind string) string {
	op := Operation{Parameters: map[string]Parameter{"x": {Type: kind}}}
	values := []any{"s", 7, 1.5, 2.0, true, []any{1}, map[string]any{"a": 1}, Arguments{}}
	var out []string
	for _, v := range values {
		err := validateArguments(op, Arguments{"x": v})
		out = append(out, fmt.Sprintf("%T=%v", v, err == nil))
	}
	return strings.Join(out, " ")
}

// kindEditorRoute is the record editor's problem phrase per probe.
func kindEditorRoute(t *testing.T, kind string) string {
	var out []string
	for _, text := range paramKindProbes {
		r := NewRuntime()
		ed, err := r.AddRecordEditor("e", RecordSpec{Fields: []RecordField{{Name: "x", Parameter: Parameter{Type: kind}}}, Load: func() (Record, error) { return Record{"x": text}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%q", ed.Problem("x")))
	}
	return strings.Join(out, " | ")
}

func kindSampleRoute(kind string) string {
	ex := Operation{Parameters: map[string]Parameter{"x": {Type: kind, Required: true}}}.ExamplesOf()
	return routeShow(ex[0].Args["x"], nil)
}

// paramKindPins are today's answers, per route and kind, before the table:
// each route's own switch. The routes disagree, and the differences are
// pinned here on purpose (see the disagreement test below). Probes are
// paramKindProbes in order, joined by " | ".
var paramKindPins = map[string]map[string]string{
	"form": {
		"":         "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"string":   "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"path":     "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"any":      "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"integer":  "ERR x must be integer | int:7 | ERR x must be integer | ERR x must be integer | ERR x must be integer | ERR x must be integer | ERR x must be integer | int:7 | ERR x must be integer | ERR x must be integer",
		"number":   "ERR x must be a number | float64:7 | float64:1.5 | ERR x must be a number | ERR x must be a number | ERR x must be a number | ERR x must be a number | float64:7 | ERR x must be a number | ERR x must be a number",
		"boolean":  "bool:false | bool:false | bool:false | bool:true | bool:false | bool:false | bool:false | bool:false | bool:false | bool:false",
		"array":    "ERR x must be a JSON array such as [1, 2] | ERR x must be a JSON array such as [1, 2] | ERR x must be a JSON array such as [1, 2] | ERR x must be a JSON array such as [1, 2] | ERR x must be a JSON array such as [1, 2] | ERR x must be a JSON array such as [1, 2] | ERR x must be a JSON array such as [1, 2] | ERR x must be a JSON array such as [1, 2] | []interface {}:[1] | ERR x must be a JSON array such as [1, 2]",
		"object":   "ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | ERR x must be a JSON object such as {\"a\": 1} | map[string]interface {}:map[a:1]",
		"duration": "not formable",
		"weird":    "not formable",
	},
	"palette": {
		"":         "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"string":   "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"path":     "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"any":      "string:x | string:7 | string:1.5 | string:true | string:false | string:TRUE | string:t | string: 7  | string:[1] | string:{\"a\":1}",
		"integer":  "ERR x must be integer | int:7 | ERR x must be integer | ERR x must be integer | ERR x must be integer | ERR x must be integer | ERR x must be integer | ERR x must be integer | ERR x must be integer | ERR x must be integer",
		"number":   "ERR x must be a number | float64:7 | float64:1.5 | ERR x must be a number | ERR x must be a number | ERR x must be a number | ERR x must be a number | ERR x must be a number | ERR x must be a number | ERR x must be a number",
		"boolean":  "ERR x must be one of true, false | ERR x must be one of true, false | ERR x must be one of true, false | bool:true | bool:false | bool:true | bool:true | ERR x must be one of true, false | ERR x must be one of true, false | ERR x must be one of true, false",
		"array":    "ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x | ERR unsupported argument type \"array\" for x",
		"object":   "ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x | ERR unsupported argument type \"object\" for x",
		"duration": "ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x | ERR unsupported argument type \"duration\" for x",
		"weird":    "ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x | ERR unsupported argument type \"weird\" for x",
	},
	"runtime": {
		"":         "string=false int=false float64=false float64=false bool=false []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
		"string":   "string=true int=false float64=false float64=false bool=false []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
		"path":     "string=true int=false float64=false float64=false bool=false []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
		"any":      "string=true int=true float64=true float64=true bool=true []interface {}=true map[string]interface {}=true ochazuke.Arguments=true",
		"integer":  "string=false int=true float64=false float64=true bool=false []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
		"number":   "string=false int=true float64=true float64=true bool=false []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
		"boolean":  "string=false int=false float64=false float64=false bool=true []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
		"array":    "string=false int=false float64=false float64=false bool=false []interface {}=true map[string]interface {}=false ochazuke.Arguments=false",
		"object":   "string=false int=false float64=false float64=false bool=false []interface {}=false map[string]interface {}=true ochazuke.Arguments=true",
		"duration": "string=false int=false float64=false float64=false bool=false []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
		"weird":    "string=false int=false float64=false float64=false bool=false []interface {}=false map[string]interface {}=false ochazuke.Arguments=false",
	},
	"editor": {
		"":         "\"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\"",
		"string":   "\"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\"",
		"path":     "\"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\"",
		"any":      "\"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\"",
		"integer":  "\"a whole number\" | \"\" | \"a whole number\" | \"a whole number\" | \"a whole number\" | \"a whole number\" | \"a whole number\" | \"\" | \"a whole number\" | \"a whole number\"",
		"number":   "\"a number\" | \"\" | \"\" | \"a number\" | \"a number\" | \"a number\" | \"a number\" | \"\" | \"a number\" | \"a number\"",
		"boolean":  "\"true or false\" | \"true or false\" | \"true or false\" | \"\" | \"\" | \"true or false\" | \"true or false\" | \"true or false\" | \"true or false\" | \"true or false\"",
		"array":    "\"a JSON array such as [1, 2]\" | \"a JSON array such as [1, 2]\" | \"a JSON array such as [1, 2]\" | \"a JSON array such as [1, 2]\" | \"a JSON array such as [1, 2]\" | \"a JSON array such as [1, 2]\" | \"a JSON array such as [1, 2]\" | \"a JSON array such as [1, 2]\" | \"\" | \"a JSON array such as [1, 2]\"",
		"object":   "\"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"a JSON object such as {\\\"a\\\": 1}\" | \"\"",
		"duration": "\"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\"",
		"weird":    "\"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\" | \"\"",
	},
	"sample": {
		"":         "string:<x>",
		"string":   "string:<x>",
		"path":     "string:<x>",
		"any":      "string:<x>",
		"integer":  "int:1",
		"number":   "int:1",
		"boolean":  "bool:true",
		"array":    "string:<x>",
		"object":   "string:<x>",
		"duration": "string:1s",
		"weird":    "string:<x>",
	},
}

func TestParamKindsPinTodaysRoutes(t *testing.T) {
	routes := map[string]func(string) string{
		"form": kindFormRoute, "palette": func(k string) string { return kindPaletteRoute(t, k) },
		"runtime": kindRuntimeRoute, "editor": func(k string) string { return kindEditorRoute(t, k) },
		"sample": kindSampleRoute,
	}
	for route, kinds := range paramKindPins {
		for kind, want := range kinds {
			if got := routes[route](kind); got != want {
				t.Errorf("%s route, kind %q:\n got %s\nwant %s", route, kind, got, want)
			}
		}
	}
}

// TestParamKindsDisagreements writes down where the routes disagree today,
// by name, so a change to one route shows as a change here. Each line is a
// fact the table rows keep (or a deliberate change, marked so).
func TestParamKindsDisagreements(t *testing.T) {
	probe := func(text string) int {
		for i, p := range paramKindProbes {
			if p == text {
				return i
			}
		}
		t.Fatalf("no probe %q", text)
		return 0
	}
	cell := func(route, kind, text string) string {
		return strings.Split(paramKindPins[route][kind], " | ")[probe(text)]
	}
	// Boolean: the form reads exactly "true" as true and every other text
	// (even "yes" or "TRUE") as false; the palette matches a choice first
	// (a prefix or other case of true or false resolves), the editor and
	// the runtime want exactly true or false.
	for route, want := range map[string]string{"form": "bool:false", "palette": "bool:true", "editor": `"true or false"`} {
		if got := cell(route, "boolean", "TRUE"); got != want {
			t.Errorf("boolean TRUE on %s = %s, want %s", route, got, want)
		}
	}
	// Integer: the form and the editor trim a number's spaces, the palette does not.
	if cell("form", "integer", " 7 ") != "int:7" || cell("editor", "integer", " 7 ") != `""` || !strings.HasPrefix(cell("palette", "integer", " 7 "), "ERR") {
		t.Error("integer spacing no longer differs form/editor vs palette")
	}
	// Number was the one kind some routes took and others refused (the
	// form and the editor took it, the palette called it unsupported, the
	// runtime refused every value, even a float the form had just built,
	// the CLI left a string). The table makes it accepted everywhere:
	// these cells are the deliberate change from the first pins.
	if cell("form", "number", "1.5") != "float64:1.5" || cell("editor", "number", "1.5") != `""` {
		t.Error("number is no longer accepted by the form and the editor")
	}
	if cell("palette", "number", "1.5") != "float64:1.5" {
		t.Errorf("the palette does not read a number: %s", cell("palette", "number", "1.5"))
	}
	if !strings.Contains(paramKindPins["runtime"]["number"], "int=true float64=true") {
		t.Error("the runtime does not accept a number")
	}
	// The empty type reads as a string everywhere text is parsed, yet the
	// runtime refuses every value of it (validateArguments has no case).
	if cell("form", "", "x") != "string:x" || strings.Contains(paramKindPins["runtime"][""], "=true") {
		t.Error("the empty type no longer differs between parse and check")
	}
	// Actions declare only these four types; number and any are not among them.
	for kind, want := range map[string]bool{"string": true, "integer": true, "boolean": true, "path": true, "number": false, "any": false, "": false} {
		if kindOf(kind).action != want {
			t.Errorf("action type %q = %v, want %v", kind, kindOf(kind).action, want)
		}
	}
	// Duration has only a sample: no parse, no check.
	if !strings.Contains(paramKindPins["form"]["duration"], "not formable") || paramKindPins["sample"]["duration"] != "string:1s" {
		t.Error("duration is no longer sample-only")
	}
}
