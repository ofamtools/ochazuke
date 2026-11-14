package ochazuke

import (
	"fmt"
	"strings"
)

// ChoiceAnswer is the headless "choose one" answer (no-refusals D4): a
// request that could not run as given changed nothing and lists the
// candidates, each with the exact command that runs it. The TUI asks with
// a picker instead; the CLI prints the commands (exit status 3), MCP
// returns it as a result. It is an error so a handler can return it from
// any depth; errors.As finds it.
type ChoiceAnswer struct {
	// Needs names what is to be chosen: a parameter, or "verb".
	Needs   string      `json:"needs,omitempty"`
	Reason  string      `json:"reason"`
	Choices []ChoiceRow `json:"choices"`
}

// ChoiceRow is one candidate of a ChoiceAnswer.
type ChoiceRow struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	Command string `json:"command,omitempty"`
}

func (a *ChoiceAnswer) Error() string {
	var b strings.Builder
	b.WriteString(a.Reason)
	for _, c := range a.Choices {
		fmt.Fprintf(&b, "\n  %s", c.Command)
		if c.Label != "" {
			fmt.Fprintf(&b, "  (%s)", c.Label)
		}
	}
	return b.String()
}
