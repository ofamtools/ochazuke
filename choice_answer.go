package ochazuke

import (
	"fmt"
	"maps"
	"slices"
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

// Error is the reason alone; the CLI prints each choice's command after
// it, MCP returns the choices as data, the TUI asks with a picker.
func (a *ChoiceAnswer) Error() string { return a.Reason }

// Commands are the choices' commands, one per line.
func (a *ChoiceAnswer) Commands() string {
	var b strings.Builder
	for _, c := range a.Choices {
		fmt.Fprintf(&b, "%s\n", c.Command)
	}
	return b.String()
}

// UnknownNameSetting says what a name no thing has does on a request
// (no-refusals D4; owner 2026-11-13: never create headless by default):
// choices answers a ChoiceAnswer of the close names and changes nothing;
// create-human creates the thing when its noun's key is a name people
// give (PropSpec.Human), else answers the choices; create always creates.
const UnknownNameSetting = "cli.unknown-name"

// unknownName answers value, a name none of the parameter's things has:
// it creates the thing (cli.unknown-name create, or create-human on a
// human key) and lets the request run, or answers the ChoiceAnswer of the
// close names, each with the command that runs the request with it. The
// reason is the unknown-name error's words.
func (r *Runtime) unknownName(op Operation, name string, p Parameter, value string, choices []Choice, args Arguments) error {
	unknown := r.choiceError(name, p, value, choices)
	source, _ := r.Operation(r.ChoicesSource(p))
	if r.createsUnknown(source.Rows) {
		n := r.Nouns[source.Rows]
		if _, err := r.Execute(Request{Noun: source.Rows, Verb: "create", Args: Arguments{n.keyProp().Name: value}}); err == nil {
			r.notice("created %s %s", r.DisplayNoun(source.Rows), value)
			return nil
		}
	}
	answer := &ChoiceAnswer{Needs: name, Reason: unknown.Error()}
	ids := make([]string, len(choices))
	labels := map[string]string{}
	for i, c := range choices {
		ids[i], labels[c.ID] = c.ID, c.Label
	}
	for _, id := range NearNames(value, ids, NearShown) {
		answer.Choices = append(answer.Choices, ChoiceRow{ID: id, Label: labels[id], Command: r.commandWith(op, args, name, id)})
	}
	return answer
}

// createsUnknown reports whether cli.unknown-name creates a thing of noun
// for a name it does not have (unknownNamePolicies).
func (r *Runtime) createsUnknown(noun string) bool {
	n := r.Nouns[noun]
	if n == nil || n.keyProp() == nil || !r.offers(noun+".create") {
		return false
	}
	value := r.Settings().String(UnknownNameSetting)
	for _, p := range unknownNamePolicies {
		if p.value == value {
			return p.creates(n)
		}
	}
	return false
}

// unknownNamePolicies are cli.unknown-name's values: whether each creates
// a thing of a noun for a name it does not have.
var unknownNamePolicies = []struct {
	value   string
	creates func(*Noun) bool
}{
	{"choices", func(*Noun) bool { return false }},
	{"create-human", func(n *Noun) bool { return n.keyProp().Human }},
	{"create", func(*Noun) bool { return true }},
}

// commandWith is the command line that runs op with args, the parameter
// name set to value: NOUN VERB --PARAM VALUE..., the words the CLI and the
// palette both read.
func (r *Runtime) commandWith(op Operation, args Arguments, name, value string) string {
	words := []string{r.DisplayNoun(op.Noun()), op.Name()}
	for _, k := range slices.Sorted(maps.Keys(args)) {
		v := args[k]
		if k == name {
			v = value
		}
		words = append(words, "--"+k, shellWord(fmt.Sprint(v)))
	}
	if _, given := args[name]; !given {
		words = append(words, "--"+name, shellWord(value))
	}
	return strings.Join(words, " ")
}
