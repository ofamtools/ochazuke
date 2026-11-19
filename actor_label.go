package ochazuke

import "strings"

// Actor labels: where a person reads who did something (the restart
// dialog and notice, an agent's confirm, the agent segment, a related
// history row), an agent's pane is named by the pane's own name, not its
// window id, when the application can name it. The application gives the
// namer (SetActorLabels); the setting actor.pane-labels (default on) turns
// it off, and an unnamed pane keeps its id. Actor.String stays the plain,
// stable form (logs, files, JSON).

// ActorPaneLabelsSetting turns pane names in actor labels on (default) or
// off.
const ActorPaneLabelsSetting = "actor.pane-labels"

// ActorLabels names the things an actor refers to, for display.
type ActorLabels struct {
	// Pane is the display name of the pane with the given id; "" when it
	// has none (the id is shown).
	Pane func(id string) string
}

const actorLabelsID = "__actor-labels"

// SetActorLabels sets how actor labels name what an actor refers to.
func (r *Runtime) SetActorLabels(l ActorLabels) {
	inst := r.Instance(actorLabelsID, Kind("ochazuke.actor-labels"))
	inst.Component = &l
}

func (r *Runtime) actorLabels() ActorLabels {
	if inst := r.Instances[actorLabelsID]; inst != nil {
		if l, ok := inst.Component.(*ActorLabels); ok {
			return *l
		}
	}
	return ActorLabels{}
}

// ActorLabel is the actor as a person reads it: Actor.String with an
// agent's pane given by its name (ActorLabels.Pane) when the application
// names it and actor.pane-labels is on.
func (r *Runtime) ActorLabel(a Actor) string {
	text := a.String()
	a = a.normalized()
	if !a.IsAgent() || a.Pane == "" || !r.Settings().Bool(ActorPaneLabelsSetting) {
		return text
	}
	namer := r.actorLabels().Pane
	if namer == nil {
		return text
	}
	name := strings.TrimSpace(namer(a.Pane))
	if name == "" || name == a.Pane {
		return text
	}
	return strings.Replace(text, "(pane "+a.Pane+")", "(pane "+name+")", 1)
}
