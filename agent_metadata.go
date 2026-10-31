package ochazuke

import (
	"maps"
	"slices"
)

// Agent rights are declarations for agent callers, enforced by the runtime gate.
const (
	AgentAllow   = "allow"
	AgentConfirm = "confirm"
	AgentNever   = "never"
)

// Example is an invocation's arguments and a short explanation.
type Example struct {
	Args Arguments `json:"args"`
	Note string    `json:"note,omitempty"`
}

// About describes the noun's purpose for catalog readers.
func (b *NounBuilder) About(text string) *NounBuilder { b.n.About = text; return b }

// Destructive marks the last verb as deleting, archiving or otherwise destroying data.
func (b *NounBuilder) Destructive() *NounBuilder {
	if b.last == "" {
		panic("Destructive must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Destructive = true
	b.r.Operations[b.last] = op
	return b
}

// Agent declares the last verb's agent right. An empty right restores the default.
func (b *NounBuilder) Agent(right string) *NounBuilder {
	if b.last == "" {
		panic("Agent must follow Verb")
	}
	switch right {
	case "", AgentAllow, AgentConfirm, AgentNever:
	default:
		panic("Agent: expected allow, confirm or never")
	}
	op := b.r.Operations[b.last]
	op.Agent = right
	b.r.Operations[b.last] = op
	return b
}

// Example adds an example to the last verb. The caller keeps ownership of args.
func (b *NounBuilder) Example(args Arguments, note string) *NounBuilder {
	if b.last == "" {
		panic("Example must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Examples = append(slices.Clone(op.Examples), Example{Args: maps.Clone(args), Note: note})
	b.r.Operations[b.last] = op
	return b
}

// AgentRightOf is the declared right, or confirm for a destructive or confirmed
// verb, else allow. Live screen control is allowed when the host enables live.
// This describes a default only; it does not enforce rights or read overrides.
func (o Operation) AgentRightOf(live bool) string {
	if o.Agent != "" {
		return o.Agent
	}
	if o.Destructive || o.Confirm != "" {
		return AgentConfirm
	}
	return AgentAllow
}

// ExamplesOf returns declared examples, else a template of the required params.
// Templates with placeholder strings must be filled before running them.
func (o Operation) ExamplesOf() []Example {
	if len(o.Examples) > 0 {
		out := slices.Clone(o.Examples)
		for i := range out {
			out[i].Args = maps.Clone(out[i].Args)
		}
		return out
	}
	args := Arguments{}
	for name, p := range o.Parameters {
		if !p.Required {
			continue
		}
		var v any = "<" + name + ">"
		switch p.Type {
		case "boolean":
			v = true
		case "integer", "number":
			v = 1
		case "duration":
			v = "1s"
		}
		if len(p.Enum) > 0 {
			v = p.Enum[0]
		}
		args[name] = v
	}
	return []Example{{Args: args, Note: "Template: replace placeholder values before running"}}
}
