package expr

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func mustParse(t *testing.T, src string) Expr {
	t.Helper()
	e, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", src, err)
	}
	return e
}

func TestEmptyZeroValueAndSource(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		e := mustParse(t, "")
		if e.Source() != "" {
			t.Fatalf("Source() = %q, want %q", e.Source(), "")
		}
		if !e.Eval(nil) {
			t.Fatalf("Eval(nil) = false, want true")
		}
		if !e.Eval(map[string]any{"a": false}) {
			t.Fatalf("Eval(non-empty) = false, want true")
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		src := "  \t\n\r "
		e := mustParse(t, src)
		if e.Source() != src {
			t.Fatalf("Source() = %q, want %q", e.Source(), src)
		}
		if !e.Eval(nil) {
			t.Fatalf("Eval(nil) = false, want true")
		}
		if !e.Eval(map[string]any{"a": false}) {
			t.Fatalf("Eval(non-empty) = false, want true")
		}
	})

	t.Run("zero-value Expr", func(t *testing.T) {
		var e Expr
		if e.Source() != "" {
			t.Fatalf("Source() = %q, want %q", e.Source(), "")
		}
		if !e.Eval(nil) {
			t.Fatalf("Eval(nil) = false, want true")
		}
		if !e.Eval(map[string]any{"a": 1}) {
			t.Fatalf("Eval(non-empty) = false, want true")
		}
	})

	t.Run("exact spacing preserved in Source", func(t *testing.T) {
		src := "  mode == \"normal\" \n\t"
		e := mustParse(t, src)
		if e.Source() != src {
			t.Fatalf("Source() = %q, want %q", e.Source(), src)
		}
		if !e.Eval(map[string]any{"mode": "normal"}) {
			t.Fatalf("Eval = false, want true")
		}
	})
}

func TestLiterals(t *testing.T) {
	env := map[string]any{
		"sQuote":    "a\"b",
		"sSlash":    "a\\b",
		"sEndSlash": "a\\",
		"sMixed":    "\\\" ",
		"sCtrl":     "x\ny\tz\r\b\f/",
		"sUTF8":     "日本語 🚀 café",
		"maxI64":    int64(math.MaxInt64),
		"minI64":    int64(math.MinInt64),
	}

	cases := []struct {
		name string
		src  string
		want bool
	}{
		// Strings & escapes
		{"quoted empty literal is falsy", `""`, false},
		{"quoted empty eq", `"" == ""`, true},
		{"quoted empty neq", `"" != ""`, false},
		{"quoted empty or false", `"" || false`, false},
		{"string eq", `"normal" == "normal"`, true},
		{"string vs bool", `"true" == true`, false},
		{"string vs int", `"123" == 123`, false},
		{"string vs null", `"null" == null`, false},
		{"escape quote", `"a\"b" == sQuote`, true},
		{"escape backslash", `"a\\b" == sSlash`, true},
		{"escape trailing backslash", `"a\\" == sEndSlash`, true},
		{"escape mixed", `"\\\" " == sMixed`, true},
		{"escape ctrl and slash", `"x\ny\tz\r\b\f\/" == sCtrl`, true},
		{"unicode escape ascii", `"\u0041" == "A"`, true},
		{"unicode escape lower", `"\u00e9" == "é"`, true},
		{"unicode escape upper", `"\u00E9" == "é"`, true},
		{"unicode escape cjk", `"\u6f22" == "漢"`, true},
		{"unicode surrogate pair", `"\ud83d\ude80" == "🚀"`, true},
		{"direct utf8", `"日本語 🚀 café" == sUTF8`, true},

		// Integers
		{"zero falsy", `0`, false},
		{"not zero", `!0`, true},
		{"zero eq zero", `0 == 0`, true},
		{"positive int truthy", `42`, true},
		{"negative int truthy", `-7`, true},
		{"negative lt zero", `-7 < 0`, true},
		{"negative lt negative", `-7 < -2`, true},
		{"max int64", `9223372036854775807 == maxI64`, true},
		{"min int64", `-9223372036854775808 == minI64`, true},
		{"min int64 lt 0", `-9223372036854775808 < 0`, true},

		// Floats & scientific notation
		{"float truthy", `3.14`, true},
		{"float gt int", `3.14 > 3`, true},
		{"negative float truthy", `-0.5`, true},
		{"negative float lt 0", `-0.5 < 0`, true},
		{"zero float falsy", `0.0`, false},
		{"not zero float", `!0.0`, true},
		{"zero float eq zero int", `0.0 == 0`, true},
		{"neg zero float eq zero float", `-0.0 == 0.0`, true},
		{"not neg zero float", `!-0.0`, true},
		{"sci positive", `1e3 == 1000`, true},
		{"sci fractional", `2.5e-2 == 0.025`, true},
		{"sci neg exponent", `-1.5E+2 == -150`, true},

		// Booleans & null
		{"true", `true`, true},
		{"false", `false`, false},
		{"null", `null`, false},
		{"not null", `!null`, true},
		{"null eq null", `null == null`, true},
		{"null eq false", `null == false`, false},
		{"null eq zero", `null == 0`, false},
		{"null eq empty str", `null == ""`, false},

		// List literals
		{"empty list falsy", `[]`, false},
		{"not empty list", `![]`, true},
		{"empty list eq", `[] == []`, true},
		{"nonempty list truthy", `["a", "b"] && true`, true},
		{"mixed list str", `"a" in ["a", 1, -2, 3.5, true, null]`, true},
		{"mixed list int", `1 in ["a", 1, -2, 3.5, true, null]`, true},
		{"mixed list neg", `-2 in ["a", 1, -2, 3.5, true, null]`, true},
		{"mixed list float", `3.5 in ["a", 1, -2, 3.5, true, null]`, true},
		{"mixed list bool", `true in ["a", 1, -2, 3.5, true, null]`, true},
		{"mixed list null", `null in ["a", 1, -2, 3.5, true, null]`, true},
		{"nested lists", `[1, 2] in [[1, 2], ["x"]]`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := mustParse(t, tc.src)
			if got := e.Eval(env); got != tc.want {
				t.Fatalf("Eval(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestDottedPaths(t *testing.T) {
	type customFacts map[string]any
	type customKey string

	var nilAnyMap map[string]any
	realisticEnv := map[string]any{
		"mode": "normal",
		"facts": map[string]any{
			"editable": true,
		},
		"modal": map[string]any{
			"open": false,
		},
		"subject": map[string]any{
			"kind": "task",
		},
		"count": 3,
	}

	if !mustParse(t, `mode == "normal" && facts.editable && !modal.open && subject.kind == "task" && count >= 1`).Eval(realisticEnv) {
		t.Fatalf("realistic core When expression failed")
	}

	cases := []struct {
		name string
		src  string
		env  map[string]any
		want bool
	}{
		{"nil env path", "a.b.c", nil, false},
		{"nil env not path", "!a.b.c", nil, true},
		{"nil env path eq null", "a.b.c == null", nil, true},
		{"empty env path", "a.b.c", map[string]any{}, false},
		{"4-level path", "frame.subject.meta.kind == \"task\"", map[string]any{
			"frame": map[string]any{
				"subject": map[string]any{
					"meta": map[string]any{"kind": "task"},
				},
			},
		}, true},
		{"underscores and digits", "item_2._flag", map[string]any{
			"item_2": map[string]any{"_flag": true},
		}, true},
		{"missing intermediate", "a.b.c", map[string]any{"a": map[string]any{"x": 1}}, false},
		{"present leaf", "a.x", map[string]any{"a": map[string]any{"x": 1}}, true},
		{"missing leaf eq null", "a.y == null", map[string]any{"a": map[string]any{"x": 1}}, true},
		{"explicit nil intermediate", "a.b", map[string]any{"a": nil}, false},
		{"explicit nil intermediate eq null", "a.b == null", map[string]any{"a": nil}, true},
		{"typed nil map intermediate", "a.b", map[string]any{"a": nilAnyMap}, false},
		{"typed nil map intermediate eq null", "a.b == null", map[string]any{"a": nilAnyMap}, true},
		{"string intermediate", "s.b", map[string]any{"s": "str"}, false},
		{"string intermediate eq null", "s.b == null", map[string]any{"s": "str"}, true},
		{"int intermediate", "n.b == null", map[string]any{"n": 42}, true},
		{"bool intermediate", "b.b == null", map[string]any{"b": true}, true},
		{"slice intermediate", "xs.b == null", map[string]any{"xs": []string{"a"}}, true},
		{"struct intermediate", "st.b == null", map[string]any{"st": struct{ B int }{B: 1}}, true},
		{"map[string]string hit", `sub.kind == "task"`, map[string]any{"sub": map[string]string{"kind": "task"}}, true},
		{"map[string]string missing is null", `sub.missing == null`, map[string]any{"sub": map[string]string{"kind": "task"}}, true},
		{"map[string]string missing not empty str", `sub.missing == ""`, map[string]any{"sub": map[string]string{"kind": "task"}}, false},
		{"map[string]int zero hit", `stats.zero == 0`, map[string]any{"stats": map[string]int{"zero": 0, "n": 5}}, true},
		{"map[string]int zero falsy", `!stats.zero`, map[string]any{"stats": map[string]int{"zero": 0}}, true},
		{"map[string]int missing is null", `stats.missing == null`, map[string]any{"stats": map[string]int{"zero": 0}}, true},
		{"map[string]int missing not 0", `stats.missing == 0`, map[string]any{"stats": map[string]int{"zero": 0}}, false},
		{"map[string]bool false hit", `cfg.flag == false`, map[string]any{"cfg": map[string]bool{"flag": false}}, true},
		{"map[string]bool missing is null", `cfg.missing == null`, map[string]any{"cfg": map[string]bool{"flag": false}}, true},
		{"map[string]bool missing not false", `cfg.missing == false`, map[string]any{"cfg": map[string]bool{"flag": false}}, false},
		{"named map type", `facts.editable`, map[string]any{"facts": customFacts{"editable": true}}, true},
		{"named string key map", `m.foo == "bar"`, map[string]any{"m": map[customKey]string{"foo": "bar"}}, true},
		{"non-string key map yields nil", `m.x == null`, map[string]any{"m": map[int]string{1: "one"}}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := mustParse(t, tc.src)
			if got := e.Eval(tc.env); got != tc.want {
				t.Fatalf("Eval(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestOperatorsAndPrecedence(t *testing.T) {
	cases := []struct {
		name string
		src  string
		env  map[string]any
		want bool
	}{
		// Unary !
		{"double bang true", `!!true`, nil, true},
		{"double bang empty str", `!!""`, nil, false},
		{"triple bang nonempty str", `!!!"x"`, nil, false},

		// Grouping
		{"nested parens", `((a || b) && !(c || !d))`, map[string]any{"a": false, "b": true, "c": false, "d": true}, true},

		// Precedence: ! < && < ||
		{"!a && b || c true", `!a && b || c`, map[string]any{"a": true, "b": false, "c": true}, true},
		{"!a && b || c false", `!a && b || c`, map[string]any{"a": true, "b": true, "c": false}, false},
		{"a || b && c", `a || b && c`, map[string]any{"a": true, "b": false, "c": false}, true},
		{"(a || b) && c", `(a || b) && c`, map[string]any{"a": true, "b": false, "c": false}, false},

		// Precedence: ! tighter than ==
		{"!x == false for string", `!x == false`, map[string]any{"x": "nonempty"}, true},
		{"!x == false for nil", `!x == false`, map[string]any{"x": nil}, false},
		{"!(x == false) for nil", `!(x == false)`, map[string]any{"x": nil}, true},

		// Precedence: ! tighter than in
		{"!a in [b]", `!"a" in ["b"]`, nil, false},
		{"!(a in [b])", `!("a" in ["b"])`, nil, true},
		{"!a in [a, false]", `!"a" in ["a", false]`, nil, true},
		{"!(a in [a, false])", `!("a" in ["a", false])`, nil, false},

		// Precedence: comparison tighter than ==/!=
		{"lt tighter than eq", `1 < 2 == true`, nil, true},
		{"in tighter than eq", `"a" in ["a", "b"] == true`, nil, true},
		{"in tighter than neq", `"c" in ["a", "b"] != true`, nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := mustParse(t, tc.src)
			if got := e.Eval(tc.env); got != tc.want {
				t.Fatalf("Eval(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestShortCircuit(t *testing.T) {
	cases := []struct {
		name string
		src  string
		env  map[string]any
		want bool
	}{
		{"false && missing", `false && missing.a.b`, nil, false},
		{"true || missing", `true || missing.a.b`, nil, true},
		{"null guard &&", `obj != null && obj.enabled`, map[string]any{"obj": nil}, false},
		{"null guard ||", `obj == null || obj.enabled`, map[string]any{"obj": nil}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := mustParse(t, tc.src)
			if got := e.Eval(tc.env); got != tc.want {
				t.Fatalf("Eval(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

type spyNode struct {
	calls int
	val   any
}

func (s *spyNode) eval(_ map[string]any) any {
	s.calls++
	return s.val
}

func TestShortCircuitSpy(t *testing.T) {
	t.Run("false && spy skips RHS", func(t *testing.T) {
		spy := &spyNode{val: true}
		e := Expr{root: binaryNode{op: tokAnd, left: literalNode{val: false}, right: spy}}
		if e.Eval(nil) {
			t.Fatalf("got true, want false")
		}
		if spy.calls != 0 {
			t.Fatalf("spy.calls = %d, want 0", spy.calls)
		}
	})

	t.Run("true && spy evaluates RHS", func(t *testing.T) {
		spy := &spyNode{val: true}
		e := Expr{root: binaryNode{op: tokAnd, left: literalNode{val: true}, right: spy}}
		if !e.Eval(nil) {
			t.Fatalf("got false, want true")
		}
		if spy.calls != 1 {
			t.Fatalf("spy.calls = %d, want 1", spy.calls)
		}
	})

	t.Run("true || spy skips RHS", func(t *testing.T) {
		spy := &spyNode{val: false}
		e := Expr{root: binaryNode{op: tokOr, left: literalNode{val: true}, right: spy}}
		if !e.Eval(nil) {
			t.Fatalf("got false, want true")
		}
		if spy.calls != 0 {
			t.Fatalf("spy.calls = %d, want 0", spy.calls)
		}
	})

	t.Run("false || spy evaluates RHS", func(t *testing.T) {
		spy := &spyNode{val: true}
		e := Expr{root: binaryNode{op: tokOr, left: literalNode{val: false}, right: spy}}
		if !e.Eval(nil) {
			t.Fatalf("got false, want true")
		}
		if spy.calls != 1 {
			t.Fatalf("spy.calls = %d, want 1", spy.calls)
		}
	})

	t.Run("nested short circuit", func(t *testing.T) {
		spy1 := &spyNode{val: true}
		spy2 := &spyNode{val: false}
		e := Expr{root: binaryNode{
			op:    tokOr,
			left:  binaryNode{op: tokAnd, left: literalNode{val: false}, right: spy1},
			right: binaryNode{op: tokOr, left: literalNode{val: true}, right: spy2},
		}}
		if !e.Eval(nil) {
			t.Fatalf("got false, want true")
		}
		if spy1.calls != 0 || spy2.calls != 0 {
			t.Fatalf("spy1.calls=%d spy2.calls=%d, want 0, 0", spy1.calls, spy2.calls)
		}
	})
}

func TestTruthiness(t *testing.T) {
	type customStr string
	type customInt int
	var nilSlice []string
	var nilMap map[string]any
	var nilIntPtr *int
	nonNilInt := 7

	cases := []struct {
		name string
		val  any
		want bool
	}{
		{"nil", nil, false},
		{"nil ptr", nilIntPtr, false},
		{"non-nil ptr", &nonNilInt, true},
		{"bool false", false, false},
		{"bool true", true, true},
		{"empty string", "", false},
		{"space string", " ", true},
		{"zero string", "0", true},
		{"false string", "false", true},
		{"custom string empty", customStr(""), false},
		{"custom string nonempty", customStr("a"), true},
		{"int 0", int(0), false},
		{"int -1", int(-1), true},
		{"int8 0", int8(0), false},
		{"int8 1", int8(1), true},
		{"int16 0", int16(0), false},
		{"int16 -2", int16(-2), true},
		{"int32 0", int32(0), false},
		{"int32 3", int32(3), true},
		{"int64 0", int64(0), false},
		{"int64 4", int64(4), true},
		{"custom int 0", customInt(0), false},
		{"custom int 5", customInt(5), true},
		{"uint 0", uint(0), false},
		{"uint 1", uint(1), true},
		{"uint8 0", uint8(0), false},
		{"uint16 0", uint16(0), false},
		{"uint32 0", uint32(0), false},
		{"uint64 0", uint64(0), false},
		{"uint64 1", uint64(1), true},
		{"float64 0", 0.0, false},
		{"float64 -0.0", math.Copysign(0, -1), false},
		{"float32 0", float32(0), false},
		{"float64 NaN", math.NaN(), false},
		{"float32 NaN", float32(math.NaN()), false},
		{"float32 -0.25", float32(-0.25), true},
		{"float64 +Inf", math.Inf(1), true},
		{"float64 -Inf", math.Inf(-1), true},
		{"nil slice", nilSlice, false},
		{"empty slice", []string{}, false},
		{"slice of empty string", []string{""}, true},
		{"slice of zero int", []int{0}, true},
		{"nil map", nilMap, false},
		{"empty map", map[string]any{}, false},
		{"map with nil value", map[string]any{"k": nil}, true},
		{"empty array", [0]int{}, false},
		{"non-empty array", [1]int{0}, true},
		{"struct", struct{}{}, true},
	}

	e := mustParse(t, "v")
	eNot := mustParse(t, "!v")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]any{"v": tc.val}
			if got := e.Eval(env); got != tc.want {
				t.Fatalf("v Eval(%T) = %v, want %v", tc.val, got, tc.want)
			}
			if got := eNot.Eval(env); got != !tc.want {
				t.Fatalf("!v Eval(%T) = %v, want %v", tc.val, got, !tc.want)
			}
		})
	}
}

func TestNumericComparisonsAndNaN(t *testing.T) {
	env := map[string]any{
		"i3":        int(3),
		"i64_3":     int64(3),
		"i32_3":     int32(3),
		"i8_10":     int8(10),
		"f64_3":     float64(3.0),
		"f32_3":     float32(3.0),
		"f64_35":    float64(3.5),
		"u5":        uint(5),
		"i64_5":     int64(5),
		"u64_5":     uint64(5),
		"f64_5":     float64(5.0),
		"u10":       uint(10),
		"u0":        uint(0),
		"neg1":      int64(-1),
		"big53":     int64(1 << 53),
		"big53p1":   int64((1 << 53) + 1),
		"fbig53":    float64(1 << 53),
		"maxI64":    int64(math.MaxInt64),
		"fMaxI64":   float64(math.MaxInt64), // rounds up to 2^63
		"minI64":    int64(math.MinInt64),
		"fMinI64":   float64(math.MinInt64), // exact -2^63
		"maxU64":    uint64(math.MaxUint64),
		"fMaxU64":   float64(math.MaxUint64), // rounds up to 2^64
		"nan":       math.NaN(),
		"nan32":     float32(math.NaN()),
		"pinf":      math.Inf(1),
		"ninf":      math.Inf(-1),
		"negZero":   math.Copysign(0, -1),
		"s3":        "3",
		"sA":        "a",
		"sB":        "b",
		"nilVal":    nil,
		"boolTrue":  true,
		"boolFalse": false,
	}

	cases := []struct {
		src  string
		want bool
	}{
		// Cross-type numeric equality
		{"i3 == i64_3", true},
		{"i3 == f64_3", true},
		{"i32_3 == f32_3", true},
		{"u5 == i64_5", true},
		{"u64_5 == f64_5", true},
		{"negZero == 0", true},
		{"negZero == u0", true},

		// Cross-type numeric ordering
		{"i3 < f64_35", true},
		{"f64_35 > i64_3", true},
		{"neg1 < u0", true},
		{"u10 >= i8_10", true},

		// >2^53 precision & signed/unsigned/float64 edge bounds
		{"big53 == big53p1", false},
		{"big53 < big53p1", true},
		{"big53p1 == fbig53", false},
		{"big53p1 > fbig53", true},
		{"big53 == fbig53", true},
		{"maxI64 < fMaxI64", true},
		{"maxI64 == fMaxI64", false},
		{"minI64 == fMinI64", true},
		{"minI64 < fMinI64", false},
		{"neg1 < maxU64", true},
		{"neg1 == maxU64", false},
		{"maxU64 < fMaxU64", true},
		{"maxU64 == fMaxU64", false},

		// NaN & Inf safety
		{"nan == nan", false},
		{"nan == 0", false},
		{"nan < 1", false},
		{"nan <= 1", false},
		{"nan > 1", false},
		{"nan >= 1", false},
		{"nan < nan", false},
		{"nan32 == nan32", false},
		{"nan != nan", true},
		{"nan != 0", true},
		{"nan32 != 0", true},
		{"pinf > 1000000", true},
		{"ninf < -1000000", true},
		{"pinf == pinf", true},
		{"pinf > maxU64", true},
		{"ninf < minI64", true},

		// Non-numeric / nil / mismatched types
		{"nilVal == false", false},
		{"nilVal == 0", false},
		{"nilVal == \"\"", false},
		{"3 == s3", false},
		{"true == 1", false},
		{"false == 0", false},
		{"nilVal != false", true},
		{"3 != s3", true},
		{"nilVal < 5", false},
		{"nilVal <= 0", false},
		{"nilVal > -1", false},
		{"nilVal >= nilVal", false},
		{"nilVal < nilVal", false},
		{"s3 < 5", false},
		{"3 < s3", false},
		{"sA < sB", false},
		{"sA <= sA", false},
		{"sB > sA", false},
		{"boolTrue < boolFalse", false},
		{"[] < []", false},
	}

	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			e := mustParse(t, tc.src)
			if got := e.Eval(env); got != tc.want {
				t.Fatalf("Eval(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestInMembership(t *testing.T) {
	env := map[string]any{
		"anySlice": []any{"x", 1, true},
		"tags":     []string{"motion", "destructive"},
		"ints":     []int{1, 2, 3},
		"int64s":   []int64{10, 20},
		"uints":    []uint{7, 8},
		"floats64": []float64{1.0, 1.5, 2.0},
		"floats32": []float32{0.5},
		"arr":      [2]string{"a", "b"},
		"nan":      math.NaN(),
		"str":      "xyz",
		"mp":       map[string]any{"k": 1},
	}

	cases := []struct {
		src  string
		want bool
	}{
		// List literals
		{`"a" in ["a", "b"]`, true},
		{`"c" in ["a", "b"]`, false},
		{`"a" in []`, false},
		{`null in [1, null]`, true},
		{`null in [1, 2]`, false},

		// Typed & untyped slices / arrays
		{`"x" in anySlice`, true},
		{`1 in anySlice`, true},
		{`true in anySlice`, true},
		{`"destructive" in tags`, true},
		{`"other" in tags`, false},
		{`3 in tags`, false},
		{`2 in ints`, true},
		{`2.0 in ints`, true},
		{`"2" in ints`, false},
		{`10 in int64s`, true},
		{`7 in uints`, true},
		{`1 in floats64`, true},
		{`1.5 in floats64`, true},
		{`0.5 in floats32`, true},
		{`"b" in arr`, true},

		// Numeric interop & NaN in list literals
		{`2 in [1.0, 2.0, 3.0]`, true},
		{`3 in ["3"]`, false},
		{`nan in [1.0, nan]`, false},
		{`nan in floats64`, false},

		// nil / missing / non-collection RHS (including string & map per SCOPE.md #6)
		{`"a" in missing`, false},
		{`"a" in null`, false},
		{`"a" in 42`, false},
		{`"a" in true`, false},
		{`"x" in str`, false},
		{`"k" in mp`, false},
	}

	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			e := mustParse(t, tc.src)
			if got := e.Eval(env); got != tc.want {
				t.Fatalf("Eval(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestConcurrentEval(t *testing.T) {
	exprs := []struct {
		src  string
		want bool
		env  map[string]any
	}{
		{
			src:  "",
			want: true,
			env:  nil,
		},
		{
			src:  `mode == "normal" && count >= 2`,
			want: true,
			env:  map[string]any{"mode": "normal", "count": 5},
		},
		{
			src:  `"destructive" in tags && !modal.open`,
			want: true,
			env:  map[string]any{"tags": []string{"destructive"}, "modal": map[string]any{"open": false}},
		},
		{
			src:  `(a || b) && !(c == 1) && facts.editable`,
			want: false,
			env:  map[string]any{"a": true, "b": false, "c": 1, "facts": map[string]any{"editable": true}},
		},
	}

	compiled := make([]Expr, len(exprs))
	for i, item := range exprs {
		compiled[i] = mustParse(t, item.src)
	}

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for iter := 0; iter < 100; iter++ {
				for i, e := range compiled {
					if e.Source() != exprs[i].src {
						t.Errorf("goroutine %d: Source() = %q, want %q", gid, e.Source(), exprs[i].src)
					}
					if got := e.Eval(exprs[i].env); got != exprs[i].want {
						t.Errorf("goroutine %d: Eval(%q) = %v, want %v", gid, exprs[i].src, got, exprs[i].want)
					}
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestParseErrors(t *testing.T) {
	badInputs := []struct {
		name string
		src  string
	}{
		// Strings
		{"unterminated string", `"unterminated`},
		{"escaped quote unterminated", `"escaped\"`},
		{"bad escape", `"bad\q"`},
		{"short unicode escape", `"short\u12"`},
		{"non-hex unicode escape", `"nonhex\uZZZZ"`},
		{"lone high surrogate", `"\ud800"`},
		{"lone low surrogate", `"\udc00"`},
		{"eof backslash", `"eof\`},
		{"unescaped newline", "\"line\n\""},
		{"invalid utf8 outside string", string([]byte{0xff, 0xfe})},
		{"invalid utf8 inside string", `"` + string([]byte{0xff}) + `"`},

		// Trailing tokens
		{"two idents", `a b`},
		{"trailing ident", `a == 1 b`},
		{"two bools", `true false`},
		{"two ints", `1 2`},
		{"extra rparen", `(a) )`},

		// Bad/incomplete operators
		{"single eq", `a = 1`},
		{"single amp", `a & b`},
		{"single pipe", `a | b`},
		{"leading and", `&& a`},
		{"leading or", `|| a`},
		{"leading eq", `== 1`},
		{"leading in", `in [1]`},
		{"dangling and", `a &&`},
		{"dangling or", `a ||`},
		{"dangling eq", `a ==`},
		{"dangling neq", `a !=`},
		{"dangling lt", `a <`},
		{"dangling lte", `a <=`},
		{"dangling gt", `a >`},
		{"dangling gte", `a >=`},
		{"dangling in", `a in`},
		{"dangling not", `!`},

		// Parens & brackets
		{"unclosed paren", `(a && b`},
		{"unmatched rparen", `a && b)`},
		{"empty parens", `()`},
		{"not empty parens", `!()`},
		{"unclosed bracket", `["a", "b"`},
		{"extra rbracket", `["a", "b"]]`},
		{"missing comma in list", `["a" "b"]`},
		{"only comma in list", `[,]`},
		{"trailing comma in list", `["a",]`},
		{"leading comma in list", `[,"a"]`},
		{"bare rbracket", `]`},

		// Dotted paths
		{"trailing dot", `a.`},
		{"nested trailing dot", `a.b.`},
		{"leading dot", `.a`},
		{"double dot", `a..b`},
		{"numeric segment", `a.1`},
		{"string segment", `a."b"`},
		{"keyword segment", `a.true`},
		{"int dot ident", `1.b`},
		{"string dot ident", `"s".b`},

		// Invalid numbers
		{"trailing dot number", `1.`},
		{"leading dot number", `.5`},
		{"double dot float", `1.2.3`},
		{"ident attached to int", `123abc`},
		{"double minus", `--1`},
		{"bare minus", `-`},
		{"minus ident", `-a`},
		{"subtraction", `1 - 2`},
		{"incomplete exponent", `1e`},
		{"incomplete signed exponent", `1e+`},
		{"int64 overflow", `99999999999999999999999`},
		{"float overflow", `1e500`},

		// Unsupported syntax
		{"single quotes", `'single'`},
		{"plus op", `a + b`},
		{"mul op", `a * b`},
		{"div op", `a / b`},
		{"at sign", `@foo`},
		{"dollar sign", `$bar`},

		// Deep nesting exceeding maxDepth (256)
		{"excessive paren depth", strings.Repeat("(", 2000) + "true" + strings.Repeat(")", 2000)},
		{"excessive bang depth", strings.Repeat("!", 300) + "true"},
		{"excessive list depth", strings.Repeat("[", 300) + "1" + strings.Repeat("]", 300)},
	}

	for _, tc := range badInputs {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.src); err == nil {
				t.Fatalf("Parse(%q) succeeded, expected error", tc.src)
			}
		})
	}

	t.Run("depth 64 succeeds", func(t *testing.T) {
		src := strings.Repeat("(", 64) + "true" + strings.Repeat(")", 64)
		e := mustParse(t, src)
		if !e.Eval(nil) {
			t.Fatalf("Eval(depth 64) = false, want true")
		}
	})

	t.Run("boundary depth 256 succeeds and 257 fails", func(t *testing.T) {
		okSrc := strings.Repeat("(", maxDepth) + "true" + strings.Repeat(")", maxDepth)
		e := mustParse(t, okSrc)
		if !e.Eval(nil) {
			t.Fatalf("Eval(depth %d) = false, want true", maxDepth)
		}
		failSrc := strings.Repeat("(", maxDepth+1) + "true" + strings.Repeat(")", maxDepth+1)
		if _, err := Parse(failSrc); err == nil {
			t.Fatalf("Parse(depth %d) succeeded, expected error", maxDepth+1)
		}
	})

	t.Run("large int boundary strings", func(t *testing.T) {
		// Verify exact MinInt64/MaxInt64 round-trip and +1 overflow errors
		eMin := mustParse(t, strconv.FormatInt(math.MinInt64, 10)+" < 0")
		if !eMin.Eval(nil) {
			t.Fatalf("MinInt64 < 0 = false, want true")
		}
		if _, err := Parse("9223372036854775808"); err == nil {
			t.Fatalf("MaxInt64+1 parsed without overflow error")
		}
		if _, err := Parse("-9223372036854775809"); err == nil {
			t.Fatalf("MinInt64-1 parsed without overflow error")
		}
	})
}
