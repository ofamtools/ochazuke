// Package trie implements an immutable prefix tree for multi-key command sequences.
package trie

// MatchKind classifies the match state of a key sequence in a Trie.
type MatchKind uint8

const (
	None      MatchKind = iota // no matching prefix or binding
	Prefix                     // valid prefix, no complete binding at this node
	Exact                      // complete binding, no longer continuations
	Ambiguous                  // complete binding AND longer continuations exist
)

// String returns the human-readable name of a MatchKind.
func (k MatchKind) String() string {
	switch k {
	case None:
		return "None"
	case Prefix:
		return "Prefix"
	case Exact:
		return "Exact"
	case Ambiguous:
		return "Ambiguous"
	default:
		return "Unknown"
	}
}

// Entry associates a normalized key sequence with a value in priority order.
// Earlier entries passed to Build take precedence over later entries with the same Keys.
type Entry[T any] struct {
	Keys  []string // non-empty sequence of normalized key names
	Value T
}

// Result describes the outcome of a Step or Lookup query.
// Value and NextKeys are defensive copies that do not alias internal trie state.
type Result[T any] struct {
	Kind     MatchKind
	Value    *T           // set for Exact and Ambiguous
	NextKeys map[string]T // immediate valid next keys (nil when none)
}

type node[T any] struct {
	value    T
	hasValue bool
	children map[string]*node[T]
}

// Trie is an immutable prefix tree mapping normalized key sequences to values of type T.
// A nil *Trie[T] or zero-value Trie[T]{} is safe to query and returns MatchKind None.
type Trie[T any] struct {
	root *node[T]
}

func hasValidKeys(keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		if k == "" {
			return false
		}
	}
	return true
}

// Build constructs an immutable *Trie[T] from priority-ordered entries.
// Entries with empty Keys or any empty key token "" are skipped before creating nodes.
// On duplicate Keys, the first entry wins.
func Build[T any](entries []Entry[T]) *Trie[T] {
	root := &node[T]{}
	for _, e := range entries {
		if !hasValidKeys(e.Keys) {
			continue
		}
		cur := root
		for _, k := range e.Keys {
			if cur.children == nil {
				cur.children = make(map[string]*node[T])
			}
			child := cur.children[k]
			if child == nil {
				child = &node[T]{}
				cur.children[k] = child
			}
			cur = child
		}
		if !cur.hasValue {
			cur.value = e.Value
			cur.hasValue = true
		}
	}
	return &Trie[T]{root: root}
}

func walk[T any](start *node[T], seq []string) *node[T] {
	cur := start
	for _, k := range seq {
		if cur == nil || k == "" || cur.children == nil {
			return nil
		}
		cur = cur.children[k]
	}
	return cur
}

func nodeResult[T any](n *node[T]) Result[T] {
	if n == nil {
		return Result[T]{Kind: None}
	}
	hasChildren := len(n.children) > 0
	var kind MatchKind
	switch {
	case n.hasValue && hasChildren:
		kind = Ambiguous
	case n.hasValue:
		kind = Exact
	case hasChildren:
		kind = Prefix
	default:
		return Result[T]{Kind: None}
	}

	res := Result[T]{Kind: kind}
	if n.hasValue {
		v := n.value
		res.Value = &v
	}
	if hasChildren {
		next := make(map[string]T, len(n.children))
		for k, child := range n.children {
			if child.hasValue {
				next[k] = child.value
			} else {
				var zero T
				next[k] = zero
			}
		}
		res.NextKeys = next
	}
	return res
}

// Lookup evaluates seq in O(len(seq)) without mutating t or seq.
// Passing nil or an empty slice returns MatchKind None.
func (t *Trie[T]) Lookup(seq []string) Result[T] {
	if t == nil || t.root == nil || len(seq) == 0 {
		return Result[T]{Kind: None}
	}
	return nodeResult(walk(t.root, seq))
}

// Step evaluates prefix followed by key in O(len(prefix)+1) without mutating
// prefix's backing array or t.
func (t *Trie[T]) Step(prefix []string, key string) Result[T] {
	if t == nil || t.root == nil || key == "" {
		return Result[T]{Kind: None}
	}
	cur := walk(t.root, prefix)
	if cur == nil || cur.children == nil {
		return Result[T]{Kind: None}
	}
	return nodeResult(cur.children[key])
}
