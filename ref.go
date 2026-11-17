package ochazuke

import (
	"slices"
	"strings"
)

// Ref is a reference to one thing (spine S5, decision 37): the thing's
// noun and its key in that noun's store. A key is never a name: a name is
// a displayed prop, read through the store (ResolveRef) once, at the edge.
// Its one text form is "noun:key" (String), read back by ParseRef; no
// other code joins or splits ids with ":" or "/". The key may hold colons
// and slashes (a path), so the text splits at the first colon only.
type Ref struct {
	Noun string // canonical noun name
	Key  string // the thing's key in the noun's store
}

// String is the reference's text form, "noun:key".
func (r Ref) String() string { return r.Noun + ":" + r.Key }

// IsZero reports a reference that names nothing (no key).
func (r Ref) IsZero() bool { return r.Key == "" }

// ParseRef reads a reference's text form at its first colon. ok is false
// when there is no noun before a colon.
func ParseRef(text string) (Ref, bool) {
	noun, key, ok := strings.Cut(text, ":")
	if !ok || noun == "" {
		return Ref{}, false
	}
	return Ref{Noun: noun, Key: key}, true
}

// RefOf is ParseRef for text expected to name a thing of noun: the key
// when text is noun's reference form, else text itself (a bare key).
func RefOf(noun, text string) Ref {
	if ref, ok := ParseRef(text); ok && ref.Noun == noun {
		return ref
	}
	return Ref{Noun: noun, Key: text}
}

// Stored is the value a Ref parameter or prop over nouns holds for the
// reference: the key alone when it can name one noun only, else the
// reference's text form.
func (r Ref) Stored(nouns []string) string {
	if len(nouns) == 1 {
		return r.Key
	}
	return r.String()
}

// ResolveRef reads text written at the edge (the CLI, MCP, a hook inside a
// pane) as a reference to a thing of one of nouns: a key, a "noun:key"
// reference, or a label (the noun's human key) that one thing holds,
// resolved once through the noun's store (keyResolvers). A text no store
// resolves is kept as written (record first, never refuse), as a reference
// to the first noun; a label more than one thing holds is kept too, with
// ok false, so the caller can say so.
func (r *Runtime) ResolveRef(c Ctx, nouns []string, text string) (ref Ref, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(nouns) == 0 {
		return Ref{}, text == ""
	}
	if c.Runtime == nil {
		c.Runtime = r
	}
	try := nouns
	if parsed, typed := ParseRef(text); typed && len(nouns) > 1 {
		if noun := r.CanonicalNoun(parsed.Noun); slices.Contains(nouns, noun) {
			try, text = []string{noun}, parsed.Key
		}
	}
	for _, noun := range try {
		n := r.Noun(noun)
		if n == nil || !n.resolvable() {
			continue
		}
		for _, res := range keyResolvers {
			keys, err := res.find(n, c, text)
			if err != nil {
				break // a store that cannot answer (or not yet): the text stands
			}
			switch {
			case len(keys) == 1:
				return Ref{Noun: n.Name, Key: keys[0]}, true
			case len(keys) > 1:
				return Ref{Noun: n.Name, Key: text}, false
			}
		}
	}
	return Ref{Noun: try[0], Key: text}, true
}

// refValue is a Ref parameter's or prop's value read at the edge
// (ResolveRef): the stored form of the reference a key, "noun:key" or a
// label names. A value that is not text, or a parameter that is not a
// reference, is untouched; a label several things hold is kept as written
// with a notice.
func (r *Runtime) refValue(c Ctx, name string, p Parameter, value any) any {
	s, isText := value.(string)
	if !isText || len(p.Ref) == 0 || strings.TrimSpace(s) == "" {
		return value
	}
	nouns := make([]string, len(p.Ref))
	for i, noun := range p.Ref {
		nouns[i] = r.CanonicalNoun(noun)
	}
	ref, ok := r.ResolveRef(c, nouns, s)
	if !ok {
		r.notice("%s: %s names more than one %s; kept as written", name, s, r.DisplayNoun(ref.Noun))
		return s
	}
	return ref.Stored(nouns)
}

// refArguments reads each Ref parameter's argument through refValue, so a
// verb receives the key whatever the caller wrote (spine S5 rule 1).
func (r *Runtime) refArguments(op Operation, args Arguments) {
	var c Ctx
	for name, v := range args {
		p, ok := op.Parameters[name]
		if !ok || len(p.Ref) == 0 {
			continue
		}
		if c.Runtime == nil {
			c = Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), Args: args}
		}
		args[name] = r.refValue(c, name, p, v)
	}
}
