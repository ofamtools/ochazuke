package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// One failure, one wording: the shared errors every place that cannot
// find a thing, does not know a name or cannot write a prop returns, so
// the CLI, MCP, the palette and the keys say the same words.

// NearShown is how many close names a did-you-mean lists.
const NearShown = 3

// NearNames are up to limit of names close to word, closest first: a name
// the word is a prefix of or holds (or that holds the word), or one a few
// edits away (at most a third of the word's length, and at least one). A
// name equal to word is left out. Case is ignored.
func NearNames(word string, names []string, limit int) []string {
	w := strings.ToLower(strings.TrimSpace(word))
	if w == "" || limit <= 0 {
		return nil
	}
	type near struct {
		name string
		rank int
	}
	most := nearEdits(w)
	var nears []near
	seen := map[string]bool{}
	for _, name := range names {
		n := strings.ToLower(name)
		if n == w || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		rank := -1
		switch {
		case strings.HasPrefix(n, w), strings.HasPrefix(w, n):
			rank = 0
		case strings.Contains(n, w), strings.Contains(w, n) && len(n) > 2:
			rank = 1
		default:
			head := string([]rune(n)[:min(len([]rune(n)), len([]rune(w)))])
			// A word no longer than the edits is any name of its length.
			if d := min(editDistance(w, n), editDistance(w, head)+1); d <= most && d < len([]rune(w)) {
				rank = 1 + d
			}
		}
		if rank >= 0 {
			nears = append(nears, near{name, rank})
		}
	}
	slices.SortStableFunc(nears, func(a, b near) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return strings.Compare(a.name, b.name)
	})
	out := make([]string, 0, min(limit, len(nears)))
	for _, n := range nears[:min(limit, len(nears))] {
		out = append(out, n.name)
	}
	return out
}

// nearEdits is how many edits away a name may be from word and still be
// near: one for a word under six letters, else two or a third of its
// length.
func nearEdits(word string) int {
	n := len([]rune(word))
	if n < 6 {
		return 1
	}
	return max(2, n/3)
}

// didYouMean is ": did you mean A, B?" for near names, "" for none.
func didYouMean(near []string) string {
	if len(near) == 0 {
		return ""
	}
	return ": did you mean " + strings.Join(near, ", ") + "?"
}

// NotFoundError is the one error for a name that reaches no thing: no
// NOUN "ID", the close ids when some are (did you mean), and where every
// id is listed. Noun is the noun as drawn (DisplayNoun); List is the hint,
// "NOUN list shows ids" when empty.
type NotFoundError struct {
	Noun string   `json:"noun"`
	ID   string   `json:"id"`
	Near []string `json:"near,omitempty"`
	List string   `json:"list,omitempty"`
}

func (e *NotFoundError) Error() string {
	list := e.List
	if list == "" {
		list = e.Noun + " list shows ids"
	}
	return fmt.Sprintf("no %s %q%s; %s", e.Noun, e.ID, didYouMean(e.Near), list)
}

// NotFound is the NotFoundError for id of noun (as drawn), with the close
// names among known.
func NotFound(noun, id string, known []string) error {
	return &NotFoundError{Noun: noun, ID: id, Near: NearNames(id, known, NearShown)}
}

// NotFound is the NotFoundError for id of noun, the noun drawn by its
// display name (DisplayNoun), the close ids among known.
func (r *Runtime) NotFound(noun, id string, known []string) error {
	return NotFound(r.DisplayNoun(noun), id, known)
}

// UnknownNameError is the one error for a name of a kind (a setting, a
// verb, a parameter) that names none: "unknown KIND NAME", the close
// names, and where every name is listed. Of, when set, is whose name it
// is ("task.list has no parameter \"bogus\"").
type UnknownNameError struct {
	Kind string   `json:"kind"`
	Name string   `json:"name"`
	Of   string   `json:"of,omitempty"`
	Near []string `json:"near,omitempty"`
	List string   `json:"list,omitempty"`
}

func (e *UnknownNameError) Error() string {
	var b strings.Builder
	if e.Of != "" {
		fmt.Fprintf(&b, "%s has no %s %q", e.Of, e.Kind, e.Name)
	} else {
		fmt.Fprintf(&b, "unknown %s %s", e.Kind, e.Name)
	}
	b.WriteString(didYouMean(e.Near))
	if e.List != "" {
		b.WriteString("; " + e.List)
	}
	return b.String()
}

// NoParameter is the error for an argument op does not declare: "KEY has
// no parameter NAME", the close parameter names, and where they are
// listed (every parameter it takes). The CLI, MCP and Runtime.Execute all
// return it.
func NoParameter(op Operation, name string) error {
	params := op.ParameterOrder()
	takes := "it takes no parameters"
	if len(params) > 0 {
		takes = "it takes " + strings.Join(params, ", ")
	}
	return &UnknownNameError{Kind: "parameter", Name: name, Of: op.Key.Short(), Near: NearNames(name, params, NearShown), List: takes}
}

// ReadOnlyError is the one error for a write to what cannot be written:
// "NOUN PROP is read-only" (or "NOUN is read-only"), with the reason when
// one is declared.
type ReadOnlyError struct {
	Noun   string `json:"noun"`
	Prop   string `json:"prop,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func (e *ReadOnlyError) Error() string {
	text := e.Noun
	if e.Prop != "" {
		text += " " + e.Prop
	}
	text += " is read-only"
	if e.Reason != "" {
		text += ": " + e.Reason
	}
	return text
}

// DecodeKnown decodes the JSON object data into v (a pointer to a struct)
// as encoding/json does, and reports each key v has no field for as a
// warning, never an error, with the close field names: "unknown key
// \"statsu\" in lane: did you mean status?". what names the object in the
// warning. The known keys are saved whatever the unknown ones are (never
// blocks); an error is only a JSON error. A verb's sub-objects (a lane
// spec, a question's options) decode with it; the owner's config files
// keep refusing an unknown key.
func DecodeKnown(data []byte, v any, what string) ([]string, error) {
	if err := json.Unmarshal(data, v); err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return nil, nil // not an object: nothing to check
	}
	known := jsonFieldNames(reflect.TypeOf(v))
	var warnings []string
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for k := range raw {
			if !yield(k) {
				return
			}
		}
	}) {
		if slices.ContainsFunc(known, func(n string) bool { return strings.EqualFold(n, key) }) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("unknown key %q in %s%s", key, what, didYouMean(NearNames(key, known, NearShown))))
	}
	return warnings, nil
}

// jsonFieldNames are the JSON names of t's fields (t a struct or a
// pointer to one), embedded structs' fields included.
func jsonFieldNames(t reflect.Type) []string {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			out = append(out, jsonFieldNames(f.Type)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, name)
	}
	return out
}

// ErrNotFound is what a store's Get returns (or wraps) for a key that
// names no thing; the standard verbs answer it, and a nil thing, with the
// shared not-found error (Runtime.ThingNotFound), so an application's
// stores word "not found" as every other store does.
var ErrNotFound = errors.New("not found")

// getThing reads key of n through its store's Get; no thing (nil, or
// ErrNotFound) is the not-found error for typed, the word as it was given.
func (r *Runtime) getThing(c Ctx, n *Noun, key, typed string) (Thing, error) {
	t, err := n.Store.Get(c, key)
	if errors.Is(err, ErrNotFound) || err == nil && t == nil {
		return nil, r.ThingNotFound(c, n.Name, typed)
	}
	return t, err
}

// nearKeysRead is how many of a noun's things a not-found error reads for
// its did-you-mean ids.
const nearKeysRead = 500

// ThingNotFound is the NotFoundError for id of noun, the close ids read
// from the noun's store (its first things; none when the noun has no
// store or reading fails).
func (r *Runtime) ThingNotFound(c Ctx, noun, id string) error {
	return r.NotFound(noun, id, r.thingKeys(c, noun))
}

// thingKeys are the keys of noun's first things, nil when it has no store.
func (r *Runtime) thingKeys(c Ctx, noun string) []string {
	n := r.Noun(noun)
	if n == nil || n.Store == nil || n.Store.List == nil && n.Store.Query == nil {
		return nil
	}
	kp := n.keyProp()
	if kp == nil {
		return nil
	}
	if c.Runtime == nil {
		c.Runtime = r
	}
	c.Noun = n.Name
	page, err := r.queryThings(c, n, ListQuery{Page: 1, PerPage: nearKeysRead})
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(page.Items))
	for _, t := range page.Items {
		if k := thingKey(kp, t); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}
