package ochazuke

import (
	"slices"
	"strings"
)

// Actor labels: where a person reads who did something (the restart
// dialog and notice, an agent's confirm, the agent segment, a related
// history row), an agent's pane is named by the pane's label, not its
// window id. The noun whose things an Actor.Pane names is data: the noun
// declares it (Noun.ActorPane, a noun file's "actorPane": true), and its
// store's Get reads the thing. The setting actor.pane-labels (default on)
// turns it off; a pane the store cannot read keeps its id. Actor.String
// stays the plain, stable form (logs, files, JSON).

// ActorPaneLabelsSetting turns pane names in actor labels on (default) or
// off.
const ActorPaneLabelsSetting = "actor.pane-labels"

// ActorLabel is the actor as a person reads it: Actor.String with an
// agent's pane given by the label of the thing it names in the noun that
// declares ActorPane, when actor.pane-labels is on.
func (r *Runtime) ActorLabel(a Actor) string {
	text := a.String()
	a = a.normalized()
	if !a.IsAgent() || a.Pane == "" || !r.Settings().Bool(ActorPaneLabelsSetting) {
		return text
	}
	name := r.actorPaneLabel(a.Pane)
	if name == "" || name == a.Pane {
		return text
	}
	return strings.Replace(text, "(pane "+a.Pane+")", "(pane "+name+")", 1)
}

// actorPaneLabel is the label of the thing key names in the first noun
// (by name) that declares ActorPane, "" when none reads it.
func (r *Runtime) actorPaneLabel(key string) string {
	names := make([]string, 0, len(r.Nouns))
	for name, n := range r.Nouns {
		if n.ActorPane && n.Store != nil && n.Store.Get != nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		n := r.Nouns[name]
		t, err := n.Store.Get(Ctx{Runtime: r, Noun: name}, key)
		if err != nil || t == nil {
			continue
		}
		if label := n.displayLabel(func(p string) any { return t[p] }); label != "" {
			return label
		}
	}
	return ""
}
