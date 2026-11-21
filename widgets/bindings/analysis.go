package bindings

import (
	"reflect"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/expr"
)

// Conflict is an advisory finding on one declaration. Other is the index of
// the related declaration, or -1 for a finding about just this row.
type Conflict struct {
	Kind    string
	Other   int
	Message string
}

// Analysis is indexed in declaration order. A row may participate in multiple
// leader layers and have more than one relationship to normal mode.
type Analysis struct {
	Conflicts []Conflict
	Leader    []string
}

// AnalyzeBindings inspects every row, including malformed and shadowed ones.
// specs extend the built-in registry, as in core.NewKeymap. Pairwise scope
// analysis uses relationships known without a view tree (self, app, any).
func AnalyzeBindings(modes []core.Mode, bindings []core.Binding, reserved []string, specs ...core.CommandSpec) []Analysis {
	km, _ := core.NewKeymap(modes, bindings, specs...)
	return analyze(modes, km, bindings, reserved)
}

func analyze(modes []core.Mode, km *core.Keymap, bindings []core.Binding, reserved []string) []Analysis {
	out := make([]Analysis, len(bindings))
	add := func(index int, kind string, other int, message string) {
		out[index].Conflicts = append(out[index].Conflicts, Conflict{kind, other, message})
	}
	if reserved == nil {
		reserved = DefaultReserved()
	}
	for i, b := range bindings {
		if _, err := expr.Parse(b.When); err != nil {
			add(i, "bad-when", -1, err.Error())
		}
		if _, ok := km.Commands.Lookup(b.Command); b.Command != "" && !ok {
			add(i, "unknown-command", -1, b.Command)
		}
		for _, k := range b.Keys {
			if reservedKey(k, reserved) {
				add(i, "reserved", -1, k)
				break
			}
		}
		for j, a := range bindings[:i] {
			same := len(a.Keys) > 0 && slices.Equal(a.Keys, b.Keys) || a.Class != "" && a.Class == b.Class
			prefix := !same && (startsWith(a.Keys, b.Keys) || startsWith(b.Keys, a.Keys))
			if !same && !prefix {
				continue
			}
			if !overlapScopes(a.Scope, b.Scope) || !overlapModes(km, a.Mode, b.Mode) {
				continue
			}
			both := func(kind string) {
				add(j, kind, i, "")
				add(i, kind, j, "")
			}
			if prefix {
				both("prefix-collision")
				continue
			}
			if strings.TrimSpace(a.When) != strings.TrimSpace(b.When) {
				both("same-key-diff-when")
				continue
			}
			captureA, captureB := a.Phase == core.PhaseCapture, b.Phase == core.PhaseCapture
			if a.Scope == b.Scope && a.Mode == b.Mode && captureA == captureB {
				both("duplicate")
			}
			// Scope precedence comes before mode specificity. Capture walks
			// scopes outer-to-inner and bubble walks them inner-to-outer.
			winner, loser := j, i
			switch {
			case captureA != captureB:
				if captureB {
					winner, loser = i, j
				}
			case a.Scope != b.Scope:
				if (scopeRank(b.Scope) > scopeRank(a.Scope)) != captureB {
					winner, loser = i, j
				}
			case a.Mode != b.Mode:
				if a.Mode == "" || slices.Contains(km.ModeChain(b.Mode), a.Mode) {
					winner, loser = i, j
				}
			}
			add(loser, "shadowed", winner, "")
		}
	}
	analyzeLeaders(modes, km, bindings, out)
	return out
}

func scopeRank(scope string) int {
	switch scope {
	case "":
		return 0
	case "app":
		return 1
	default:
		return 2
	}
}

func overlapScopes(a, b string) bool { return a == b || scopeRank(a) < 2 || scopeRank(b) < 2 }

func overlapModes(km *core.Keymap, a, b string) bool {
	return a == "" || b == "" || slices.Contains(km.ModeChain(a), b) || slices.Contains(km.ModeChain(b), a)
}

func scopeChain(scope string) []core.ContextFrame {
	chain := []core.ContextFrame{{Scope: "app"}}
	if scope != "" && scope != "app" {
		chain = append([]core.ContextFrame{{Scope: scope}}, chain...)
	}
	return chain
}

// winner returns the effective binding of a complete sequence, including a
// held fallback. With a pending prefix, it starts at the next stroke.
func winner(km *core.Keymap, q core.KeyQuery, keys []string) *core.Binding {
	prefix := q.Input.Prefix
	if len(keys) <= len(prefix) || !slices.Equal(prefix, keys[:len(prefix)]) {
		return nil
	}
	for i := len(prefix); i < len(keys); i++ {
		q.Key = keys[i]
		res := km.Resolve(q)
		if i == len(keys)-1 {
			if res.Replay {
				return nil
			}
			return res.Trace.Winner
		}
		if res.Outcome != core.Pending {
			return nil
		}
		q.Input = res.Input
	}
	return nil
}

func sameAction(a, b *core.Binding) bool {
	return a.Command == b.Command && reflect.DeepEqual(a.Args, b.Args) && reflect.DeepEqual(a.Layer, b.Layer)
}

func analyzeLeaders(modes []core.Mode, km *core.Keymap, bindings []core.Binding, out []Analysis) {
	var leaders []core.Binding
	for _, b := range bindings {
		if b.Layer != nil && b.Layer.Name == "leader" && !b.Unbind {
			leaders = append(leaders, b)
		}
	}
	// A dedicated leader mode may also be entered directly via mode.set.
	if _, ok := km.Mode("leader"); ok && len(leaders) == 0 {
		leaders = append(leaders, core.Binding{Layer: &core.Layer{Name: "leader", Modes: []string{"leader"}}})
	}
	// The editor has no runtime facts. Compare each guard with itself and
	// unconditional fallbacks, giving the guard the opportunity to match.
	// HelpView evaluates the actual predicates against the live query.
	byGuard := map[string]*core.Keymap{}
	if !slices.ContainsFunc(bindings, func(b core.Binding) bool { return b.When != "" }) {
		byGuard[""] = km
	}
	var samples []string
	for _, trigger := range leaders {
		for i, b := range bindings {
			if !overlapScopes(b.Scope, trigger.Scope) {
				continue
			}
			structural := byGuard[b.When]
			if structural == nil {
				var active []core.Binding
				for _, candidate := range bindings {
					if candidate.When != "" && candidate.When != b.When {
						continue
					}
					candidate.When = ""
					active = append(active, candidate)
				}
				structural, _ = core.NewKeymap(modes, active, km.Commands.List()...)
				byGuard[b.When] = structural
			}
			b.When = ""
			scope := b.Scope
			if scopeRank(trigger.Scope) > scopeRank(scope) {
				scope = trigger.Scope
			}
			q := core.KeyQuery{Mode: "normal", Chain: scopeChain(scope)}
			compare := func(keys []string) {
				kind := leaderRelationship(structural, q, trigger, &b, keys)
				if kind != "" && !slices.Contains(out[i].Leader, kind) {
					out[i].Leader = append(out[i].Leader, kind)
				}
			}
			if len(b.Keys) > 0 {
				compare(b.Keys)
				continue
			}
			if b.Class == "" {
				continue
			}
			if samples == nil {
				samples = classSamples(bindings)
			}
			for _, key := range samples {
				if core.ClassMatches(b.Class, key) {
					compare([]string{key})
				}
			}
		}
	}
}

func leaderRelationship(km *core.Keymap, q core.KeyQuery, trigger core.Binding, b *core.Binding, keys []string) string {
	normal := winner(km, q, keys)
	if trigger.Mode != "" {
		q.Mode = trigger.Mode
	}
	q.Input.Layer = trigger.Layer
	leader := winner(km, q, keys)
	if leader == nil || !(reflect.DeepEqual(b, normal) || reflect.DeepEqual(b, leader)) {
		return ""
	}
	if normal == nil {
		return "leader-only"
	}
	if sameAction(normal, leader) {
		return "mirror"
	}
	return "differs"
}
