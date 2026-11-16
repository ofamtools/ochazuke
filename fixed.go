package ochazuke

import "fmt"

// A thing that must stay says so as data, and a verb says what it would do
// to its subject as data (ask-not-refuse O2):
//
//   - the thing names the kinds of change it keeps itself from: a store
//     thing or a list row by its prop with RoleFixed (a list of kinds, or
//     an object of kind to reason), a tree node by TreeNode.Fixed;
//   - the verb names the kinds of change it makes (Operation.Changes,
//     NounBuilder.Changes): TreeRename, TreeMove, TreeDelete, or any word
//     an application's things use.
//
// Where a verb's subject keeps itself from one of the verb's changes, the
// request answers nothing / kept with the thing's reason before anything
// else runs (no handler, no fallback, no confirm), from every route; the
// focus stays and nothing is written. Its keys are not offered on the
// thing, as a component's Unoffered verbs are not (offered_here.go): the
// row is skipped and the key falls through. The framework names no verb:
// only the two declarations meet.

// fixedHolder is a component that holds things and answers, by a thing's
// id, the kinds of change it keeps itself from with the reason for each
// (Tree: its nodes' Fixed). nil when it holds no such thing.
type fixedHolder interface {
	fixedOf(id string) map[string]string
}

// keptReason is why the subject of q, acting from focus, keeps itself
// from a change op makes; "" when it does not (or op declares no change).
func (r *Runtime) keptReason(op Operation, q Request, focus string) string {
	if len(op.Changes) == 0 {
		return ""
	}
	fixed, label := r.subjectFixed(op, q, focus)
	for _, change := range op.Changes {
		if reason, ok := fixed[change]; ok {
			if reason == "" {
				reason = fmt.Sprintf("%q refuses %s", label, changeVerb(change))
			}
			return reason
		}
	}
	return ""
}

// keeps answers a request whose subject keeps itself from the change its
// verb makes: nothing / kept, before anything else runs (no handler, no
// fallback, no confirm; the focus stays). A request over several subjects
// is judged per subject where it fans out; a dry run (checkOnly) judges
// nothing here.
func (r *Runtime) keeps(op Operation, q Request, focus string, checkOnly bool) bool {
	if checkOnly || len(q.Subjects) > 1 {
		return false
	}
	reason := r.keptReason(op, q, focus)
	if reason != "" {
		r.answerKept(op, reason)
	}
	return reason != ""
}

// answerKept marks the running request as one that changed nothing
// because its subject keeps itself, and tells the reason as a handler's
// Nothing is told (tellNothing).
func (r *Runtime) answerKept(op Operation, reason string) {
	o := Outcome{Kind: OutcomeNothing, Cause: CauseKept, Reason: reason}
	r.noteNothing(o)
	o.Verb = r.DisplayKey(op.Key.Short())
	r.tellNothing(o)
}

// answerUnavailable is the answer of a request for a verb that cannot run
// for its noun or thing (Operation.Unavailable: a store without the call,
// a singleton's collection verb, a system fact): nothing / not-here with
// the declared reason, one mechanism for every such verb. A run is marked
// as one that changed nothing (no history row, no undo record, no data
// change) and the reason is told as a handler's Nothing is (tellNothing,
// outcome.nothing for a person, the CLI's stderr with exit status 0, MCP's
// status nothing); a dry run (checkOnly) answers the NothingError, so a
// check classifies it as nothing too. Its keys are not offered (the row
// is unavailable and the key falls through); the palette lists it with
// its reason and runs it.
func (r *Runtime) answerUnavailable(op Operation, reason string, checkOnly bool) error {
	if checkOnly {
		return NothingErr(CauseNotHere, reason)
	}
	o := Outcome{Kind: OutcomeNothing, Cause: CauseNotHere, Reason: reason}
	r.noteNothing(o)
	o.Verb = r.DisplayKey(op.Key.Short())
	r.tellNothing(o)
	return nil
}

// subjectFixed is what the subject of q keeps itself from, with its
// label: the thing its key argument names (read through the noun's
// store), else the row it acts on from focus (subjectRowAt), read from the
// component holding it (fixedHolder) or the row's RoleFixed prop.
func (r *Runtime) subjectFixed(op Operation, q Request, focus string) (map[string]string, string) {
	n := r.Noun(op.Noun())
	if key := n.keyProp(); key != nil && n.Store != nil && n.Store.Get != nil {
		if v, ok := q.Args[key.Name]; ok && !emptyProp(v) {
			if p := n.roleProp(RoleFixed); p != nil {
				id := fmt.Sprint(v)
				t, err := n.Store.Get(Ctx{actor: r.Actor(), Runtime: r, Context: r.contextOf(focus), Args: q.Args}, id)
				if err != nil || t == nil {
					return nil, ""
				}
				label := id
				if lp := n.roleProp(RoleLabel); lp != nil && !emptyProp(t[lp.Name]) {
					label = fmt.Sprint(t[lp.Name])
				}
				return fixedChanges(t[p.Name]), label
			}
			return nil, ""
		}
	}
	row := r.Instances[r.subjectRowAt(op.Noun(), focus)]
	if row == nil {
		return nil, ""
	}
	label := row.Label
	if label == "" {
		label = row.ID
	}
	if holder := nearestComponent[fixedHolder](r, row.ID); holder != nil {
		if fixed := holder.fixedOf(row.ID); fixed != nil {
			return fixed, label
		}
	}
	if p := r.Noun(row.Noun).roleProp(RoleFixed); p != nil {
		return fixedChanges(r.rowValue(row, p.Name)), label
	}
	return nil, ""
}

// subjectRowAt is the row a verb of noun acts on from focus: the nearest
// row at or above the focus, when it is a row of the noun or its list is
// (or sits directly in) the noun's nearest instance, as a tree's rows sit
// in the tree an outline verb acts through; "" when there is none.
func (r *Runtime) subjectRowAt(noun, focus string) string {
	for _, id := range r.Ancestry(focus) {
		list := r.rows[id]
		if list == "" {
			continue
		}
		owner := r.nounInstance(noun, id)
		if owner == id || owner != "" && (owner == list || r.Instances[list] != nil && r.Instances[list].Parent == owner) {
			return id
		}
		return ""
	}
	return ""
}

// fixedChanges reads a RoleFixed prop's value: a list of kinds (each with
// no reason of its own), an object of kind to reason, or one kind; nil for
// anything else.
func fixedChanges(v any) map[string]string {
	out := map[string]string{}
	switch v := v.(type) {
	case string:
		if v != "" {
			out[v] = ""
		}
	case []string:
		for _, k := range v {
			out[k] = ""
		}
	case []any:
		for _, k := range v {
			out[fmt.Sprint(k)] = ""
		}
	case map[string]string:
		for k, reason := range v {
			out[k] = reason
		}
	case map[string]any:
		for k, reason := range v {
			s, _ := reason.(string)
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fixedOf is what node id keeps itself from by the tree's Fixed data:
// each kind its Fixed names, and deletion when a node of its subtree
// keeps itself from it, each with the tree's reason (KeptError).
func (t *Tree) fixedOf(id string) map[string]string {
	i := t.index(id)
	if i < 0 {
		return nil
	}
	out := map[string]string{}
	for _, op := range t.nodes[i].Fixed {
		out[op] = t.kept(op, id).Reason
	}
	if _, ok := t.fixedFor(TreeDelete, id); ok {
		out[TreeDelete] = t.kept(TreeDelete, id).Reason
	}
	return out
}

// kept is the KeptError for a change op of node id its Fixed data keeps
// it from (fixedFor: the node itself, or for a delete a node of its
// subtree); nil when nothing keeps it.
func (t *Tree) kept(op, id string) *KeptError {
	i := t.index(id)
	if i < 0 {
		return nil
	}
	blocker, ok := t.fixedFor(op, id)
	if !ok {
		return nil
	}
	node := t.nodes[i]
	reason := fmt.Sprintf("%s refuses %s", nodeName(node), changeVerb(op))
	if blocker.ID != node.ID {
		reason = fmt.Sprintf("%s holds %s, which refuses %s", nodeName(node), nodeName(blocker), changeVerb(op))
	}
	return &KeptError{Thing: node.ID, Change: op, Reason: reason}
}

// KeptError is a change a thing keeps itself from by its fixed data
// (RoleFixed, TreeNode.Fixed): nothing changed and nothing is wrong.
// OutcomeOf answers it nothing / kept; Tree methods return it for a change
// a node's Fixed holds back.
type KeptError struct {
	Thing  string `json:"thing"`  // the thing's id
	Change string `json:"change"` // the kind of change it keeps itself from
	Reason string `json:"reason"` // in the person's words
}

func (e *KeptError) Error() string { return e.Reason }

// offersChange reports whether a key for op is offered at focus as far as
// fixed things go: false where its subject keeps itself from the change.
func (r *Runtime) offersChange(op Operation, focus string) bool {
	return len(op.Changes) == 0 || r.keptReason(op, Request{}, focus) == ""
}
