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

// Destructive marks the last verb as destroying user data that cannot be recovered.
func (b *NounBuilder) Destructive() *NounBuilder {
	return b.lastOp("Destructive", func(o *Operation) { o.Destructive = true })
}

// Agent declares the last verb's agent right. An empty right restores the default.
func (b *NounBuilder) Agent(right string) *NounBuilder {
	switch right {
	case "", AgentAllow, AgentConfirm, AgentNever:
	default:
		panic("Agent: expected allow, confirm or never")
	}
	return b.lastOp("Agent", func(o *Operation) { o.Agent = right })
}

// Example adds an example to the last verb. The caller keeps ownership of args.
func (b *NounBuilder) Example(args Arguments, note string) *NounBuilder {
	return b.lastOp("Example", func(o *Operation) {
		o.Examples = append(slices.Clone(o.Examples), Example{Args: maps.Clone(args), Note: note})
	})
}

// AgentRightOf refuses destructive verbs before consulting declarations, then
// defaults prompted verbs to confirm and everything else to allow.
// This describes a default only; owner overrides are resolved by AgentRight.
func (o Operation) AgentRightOf(live bool) string {
	if o.Destructive {
		return AgentNever
	}
	if o.Agent != "" {
		return o.Agent
	}
	if o.Confirm != "" {
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
		if s := kindOf(p.Type).sample; s != nil {
			v = s
		}
		if len(p.Enum) > 0 {
			v = p.Enum[0]
		}
		args[name] = v
	}
	return []Example{{Args: args, Note: "Template: replace placeholder values before running"}}
}
