package ochazuke

import "testing"

// TestActionCheckMessagesPinned lists, in rule order, an action that fails
// each Action.check rule and the exact message it answers.
func TestActionCheckMessagesPinned(t *testing.T) {
	r := NewRuntime()
	r.Define("worktree")
	r.Operations.Set(Operation{Key: "worktree.frob"})
	r.Nouns["worktree"].Props = append(r.Nouns["worktree"].Props, PropSpec{Name: "branch", Parameter: Parameter{Type: SettingString}})
	cmd := Argv{"true"}
	cases := []struct {
		name string
		a    UserOperation
		want string
	}{
		{"no noun", UserOperation{Verb: "v", Command: cmd}, "noun and verb are required"},
		{"no verb", UserOperation{Noun: "worktree", Command: cmd}, "noun and verb are required"},
		{"verb dot", UserOperation{Noun: "worktree", Verb: "a.b", Command: cmd}, `verb "a.b" must be one word without a dot`},
		{"verb space", UserOperation{Noun: "worktree", Verb: "a b", Command: cmd}, `verb "a b" must be one word without a dot`},
		{"unknown noun", UserOperation{Noun: "nope", Verb: "v", Command: cmd}, `unknown noun "nope" (an action adds a verb to an existing noun)`},
		{"unknown noun spelled", UserOperation{Noun: "nope", Verb: "v", Command: cmd, written: "Nope"}, `unknown noun "Nope" (an action adds a verb to an existing noun)`},
		{"defined", UserOperation{Noun: "worktree", Verb: "frob", Command: cmd}, "worktree.frob is already defined"},
		{"prop", UserOperation{Noun: "worktree", Verb: "branch", Command: cmd}, "branch is a prop of worktree, not a verb"},
		{"both", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Base: Argv{"tool"}}, "command and base exclude each other"},
		{"neither", UserOperation{Noun: "worktree", Verb: "v"}, "command or base is required"},
		{"call with command", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Call: "a b"}, "call and target go with base, not command"},
		{"target with command", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Target: "x"}, "call and target go with base, not command"},
		{"json with base", UserOperation{Noun: "worktree", Verb: "v", Base: Argv{"tool"}, JSON: true}, "json goes with command; base always passes --json"},
		{"foreground timeout", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Foreground: true, Timeout: "1s"}, "foreground takes no timeout: the program runs until it exits"},
		{"foreground json", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Foreground: true, JSON: true}, "json goes with a captured command, not foreground"},
		{"short call", UserOperation{Noun: "worktree", Verb: "v", Base: Argv{"tool"}, Call: "one"}, `call "one" must be the noun's words and the verb`},
		{"param name dot", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Parameters: map[string]Parameter{"a.b": {Type: "string"}}}, `parameter name "a.b" must be one word, not a dot or a reserved name`},
		{"param reserved", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Parameters: map[string]Parameter{"target": {Type: "string"}}}, `parameter name "target" must be one word, not a dot or a reserved name`},
		{"param type", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Parameters: map[string]Parameter{"n": {Type: "float"}}}, "parameter n: type must be string, integer, boolean or path"},
		{"positional", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Positional: []string{"n"}}, `positional "n" is not a declared parameter`},
		{"history", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, History: "x"}, `history "x" must be action, change or fetch`},
		{"timeout bad", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Timeout: "soon"}, `timeout "soon" must be a duration such as "30s"`},
		{"timeout zero", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Timeout: "0s"}, `timeout "0s" must be a duration such as "30s"`},
		{"placeholder", UserOperation{Noun: "worktree", Verb: "v", Command: Argv{"echo", "{{nosuch}}"}}, "unknown placeholder {{nosuch}}"},
		{"placeholder label", UserOperation{Noun: "worktree", Verb: "v", Command: cmd, Label: "{{ x }}"}, "unknown placeholder {{x}}"},
		{"placeholder data.", UserOperation{Noun: "worktree", Verb: "v", Command: Argv{"{{data.}}"}}, "unknown placeholder {{data.}}"},
		{"ok", UserOperation{Noun: "worktree", Verb: "v", Command: Argv{"{{data.x}}", "{{id}}"}}, ""},
		{"order: neither before param", UserOperation{Noun: "worktree", Verb: "v", Positional: []string{"n"}}, "command or base is required"},
	}
	for _, c := range cases {
		got := ""
		if err := c.a.check(r); err != nil {
			got = err.Error()
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestArgumentSourcesPinned pins what Runtime.argument answers per source.
func TestArgumentSourcesPinned(t *testing.T) {
	r := NewRuntime()
	ev := func(key string, paste bool, p Payload) KeyEvent {
		return KeyEvent{Key: MacroKey{Key: key, Paste: paste}, Mode: ModeState{Name: "m", Payload: p}}
	}
	shadowed := KeyEvent{Key: MacroKey{Key: "x"}, Mode: ModeState{Payload: Payload{Count: 9, Shadow: &ModeState{Payload: Payload{Count: 3, OpCount: 4, Op: "d", Prefix: "g"}}}}}
	cases := []struct {
		name, source string
		ev           KeyEvent
		want         any
	}{
		{"key", "key", ev("ctrl+x", false, Payload{}), "ctrl+x"},
		{"digit", "digit", ev("7", false, Payload{}), 7},
		{"digit nan", "digit", ev("a", false, Payload{}), 0},
		{"text", "text", ev("hi", false, Payload{}), "hi"},
		{"text paste", "text", ev("[a\r\nb\rc]", true, Payload{}), "a\nb\nc"},
		{"count default", "count", ev("x", false, Payload{}), 1},
		{"count", "count", ev("x", false, Payload{Count: 5}), 5},
		{"line count", "line", ev("x", false, Payload{Count: 5, OpCount: 2}), 5},
		{"line opcount", "line", ev("x", false, Payload{OpCount: 2}), 2},
		{"line none", "line", ev("x", false, Payload{}), 0},
		{"steps", "steps", ev("x", false, Payload{Count: 3, OpCount: 4}), 12},
		{"steps default", "steps", ev("x", false, Payload{}), 1},
		{"op", "op", ev("x", false, Payload{Op: "d"}), "d"},
		{"opcount", "opcount", ev("x", false, Payload{OpCount: 6}), 6},
		{"prefix", "prefix", ev("x", false, Payload{Prefix: "g"}), "g"},
		{"letter", "letter", ev("ctrl+Q", false, Payload{}), "q"},
		{"letter none", "letter", ev("f5", false, Payload{}), ""},
		{"row none", "row", ev("x", false, Payload{}), nil},
		{"row field none", "row:path", ev("x", false, Payload{}), nil},
		{"unknown", "bogus", ev("x", false, Payload{}), nil},
		{"shadow count", "count", shadowed, 3},
		{"shadow steps", "steps", shadowed, 12},
		{"shadow op", "op", shadowed, "d"},
		{"shadow prefix", "prefix", shadowed, "g"},
		{"compound", "prefix key", ev("x", false, Payload{Prefix: "g"}), "g x"},
		{"compound skips nil", "bogus key", ev("x", false, Payload{}), "x"},
		{"compound empty", "bogus row", ev("x", false, Payload{}), ""},
	}
	for _, c := range cases {
		if got := r.argument(c.source, c.ev); got != c.want {
			t.Errorf("%s: argument(%q) = %#v, want %#v", c.name, c.source, got, c.want)
		}
	}
}
