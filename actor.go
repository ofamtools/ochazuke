package ochazuke

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	ActorOwner   = "owner"
	ActorAgent   = "agent"
	ActorUnknown = "unknown"
)

// Actor identifies who issued a request. The zero value is the owner, preserving
// requests and history written before attribution existed. Identity is advisory,
// not authentication. Servers bind it once at startup, never from tool arguments.
type Actor struct {
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Harness string `json:"harness,omitempty"`
	Pane    string `json:"pane,omitempty"`
	Session string `json:"session,omitempty"`
	Model   string `json:"model,omitempty"`
}

// Owner is the owner as an actor.
func Owner() Actor { return Actor{Kind: ActorOwner} }

func (a Actor) normalized() Actor {
	if a == (Actor{}) {
		return Owner()
	}
	return a
}

func (a Actor) validate() error {
	switch a.normalized().Kind {
	case ActorOwner, ActorAgent, ActorUnknown:
		return nil
	}
	return fmt.Errorf("unknown actor kind %q (owner, agent or unknown)", a.Kind)
}

// IsAgent reports whether the actor is an agent.
func (a Actor) IsAgent() bool { return a.Kind == ActorAgent }

// String describes the actor without including task titles or pane labels.
func (a Actor) String() string {
	a = a.normalized()
	if !a.IsAgent() {
		if a.Kind == ActorOwner {
			return ActorOwner
		}
		return ActorUnknown
	}
	out := "agent"
	if a.Name != "" {
		out += " " + a.Name
	}
	if a.Harness != "" {
		out += " " + a.Harness
	}
	if a.Pane != "" {
		out += " (pane " + a.Pane + ")"
	}
	return out
}

// ParseActor accepts owner, agent, unknown or an actor as JSON.
func ParseActor(s string) (Actor, error) {
	s = strings.TrimSpace(s)
	switch s {
	case ActorOwner, ActorAgent, ActorUnknown:
		return Actor{Kind: s}, nil
	}
	var a Actor
	if err := json.Unmarshal([]byte(s), &a); err != nil || a.Kind == "" || a.validate() != nil {
		return Actor{}, fmt.Errorf("actor must be owner, agent, unknown or an actor JSON object")
	}
	return a, nil
}

// SameAgent reports the same identified agent. Anonymous agents cannot claim
// entries: at least a pane or a name must identify it, and all identity fields
// must match (including session, so a reused pane id is not another session).
func (a Actor) SameAgent(b Actor) bool {
	return a.IsAgent() && b.IsAgent() && a == b && (a.Pane != "" || a.Name != "")
}

// ActorFromEnv reads explicit CLI identity from OCHAZUKE_ACTOR (a kind or JSON),
// filling an agent's missing fields from OCHAZUKE_HARNESS, OCHAZUKE_MODEL,
// TUIOS_PANE_ID and TUIOS_SESSION. A terminal pane alone does not imply an agent.
func ActorFromEnv(getenv func(string) string) (Actor, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	raw := strings.TrimSpace(getenv("OCHAZUKE_ACTOR"))
	if raw == "" {
		return Owner(), nil
	}
	a, err := ParseActor(raw)
	if err != nil {
		return Actor{}, fmt.Errorf("OCHAZUKE_ACTOR: %w", err)
	}
	if a.IsAgent() {
		if a.Harness == "" {
			a.Harness = getenv("OCHAZUKE_HARNESS")
		}
		if a.Model == "" {
			a.Model = getenv("OCHAZUKE_MODEL")
		}
		if a.Pane == "" {
			a.Pane = getenv("TUIOS_PANE_ID")
		}
		if a.Session == "" {
			a.Session = getenv("TUIOS_SESSION")
		}
	}
	return a, nil
}

// Actor is the current request's actor, the owner outside any request.
func (r *Runtime) Actor() Actor { return r.history.actor.normalized() }

// WithActor scopes direct API requests, including nested requests, to actor.
// API adapters should validate external identity with ParseActor first.
func (r *Runtime) WithActor(actor Actor) (restore func()) {
	before := r.history.actor
	r.history.actor = actor.normalized()
	return func() { r.history.actor = before }
}

// Actor is the actor captured for an application handler or pending callback.
func (c Ctx) Actor() Actor {
	if c.actor != (Actor{}) {
		return c.actor
	}
	if c.Runtime != nil {
		return c.Runtime.Actor()
	}
	return Owner()
}
