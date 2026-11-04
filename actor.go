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
	// Via attributes an extension host call to the actor that launched it.
	// It records provenance, never an independent grant of rights.
	Via *Actor `json:"via,omitempty"`
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
	return a.IsAgent() && b.IsAgent() && sameActor(a, b) && (a.Pane != "" || a.Name != "")
}

func sameActor(a, b Actor) bool {
	av, bv := a.Via, b.Via
	a.Via, b.Via = nil, nil
	if a != b {
		return false
	}
	if av == nil || bv == nil {
		return av == bv
	}
	return sameActor(*av, *bv)
}

// ThroughExtension narrows an actor to the extension's agent identity.
func (a Actor) ThroughExtension(name string) Actor {
	a = a.normalized()
	return Actor{Kind: ActorAgent, Harness: "extension", Name: name, Via: &a}
}

// ActorFromEnv reads CLI identity from OCHAZUKE_ACTOR (a kind or JSON)
// and detects agent harnesses from their environment markers. Detection only
// narrows rights: an explicit owner or unknown kind cannot demote a detected
// agent. Explicit identity fields win over environment defaults. A terminal
// pane or model alone does not imply an agent.
func ActorFromEnv(getenv func(string) string) (Actor, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	a := Owner()
	if raw := strings.TrimSpace(getenv("OCHAZUKE_ACTOR")); raw != "" {
		var err error
		a, err = ParseActor(raw)
		if err != nil {
			return Actor{}, fmt.Errorf("OCHAZUKE_ACTOR: %w", err)
		}
	}
	// Adapted from o-lane's CLI detector. A harness declaration supplies
	// its own name; a harness-specific marker supplies the table's name.
	for _, signal := range []struct{ key, harness string }{
		{"OCHAZUKE_HARNESS", ""},
		{"TUIOS_HARNESS", ""},
		{"AGENT_HARNESS", ""},
		{"CODEX_THREAD_ID", "codex"},
		{"CODEX_CI", "codex"},
		{"CLAUDE_SESSION_ID", "claude-code"},
		{"CLAUDE_CODE_SESSION_ID", "claude-code"},
		{"CLAUDE_CODE_ENTRYPOINT", "claude-code"},
		{"PI_SESSION_ID", "pi"},
	} {
		if value := strings.TrimSpace(getenv(signal.key)); value != "" {
			a.Kind = ActorAgent
			if a.Harness == "" {
				a.Harness = signal.harness
				if a.Harness == "" {
					a.Harness = value
				}
			}
			break
		}
	}
	if a.IsAgent() {
		if a.Name == "" {
			a.Name = getenv("TUIOS_AGENT_NAME")
		}
		if a.Model == "" {
			a.Model = getenv("OCHAZUKE_MODEL")
			if a.Model == "" {
				a.Model = getenv("AGENT_MODEL")
			}
		}
		if a.Pane == "" {
			a.Pane = getenv("TUIOS_PANE_ID")
		}
		if a.Session == "" {
			a.Session = getenv("TUIOS_SESSION")
		}
	}
	if caller := strings.TrimSpace(getenv("OCHAZUKE_CALLER")); caller != "" {
		a = a.ThroughExtension(caller)
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
