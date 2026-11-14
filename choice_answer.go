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
// the thing to create (cli.unknown-name create, or create-human on a human
// key), for execute to create when the request runs (createUnknown), or
// the ChoiceAnswer of the close names, each with the command that runs the
// request with it. The reason is the unknown-name error's words.
func (r *Runtime) unknownName(op Operation, name string, p Parameter, value string, choices []Choice, args Arguments) (unknownCreate, error) {
	unknown := r.choiceError(name, p, value, choices)
	answer := &ChoiceAnswer{Needs: name, Reason: unknown.Error()}
	ids := make([]string, len(choices))
	labels := map[string]string{}
	for i, c := range choices {
		ids[i], labels[c.ID] = c.ID, c.Label
	}
	for _, id := range NearNames(value, ids, NearShown) {
		answer.Choices = append(answer.Choices, ChoiceRow{ID: id, Label: labels[id], Command: r.commandWith(op, args, name, id)})
	}
	source, _ := r.Operation(r.ChoicesSource(p))
	if r.createsUnknown(source.Rows) {
		return unknownCreate{noun: source.Rows, name: value, otherwise: answer}, nil
	}
	return unknownCreate{}, answer
}

// unknownCreate is a thing cli.unknown-name creates for a request: its
// noun and name, and the answer when the create fails.
type unknownCreate struct {
	noun, name string
	otherwise  error
}

// createUnknown creates the things a request named that do not exist yet
// (argumentCreates), each with a notice; a create that fails answers the
// ChoiceAnswer of the close names instead. It runs only for a request
// about to run its handler, its checks (the agent's rights first) passed.
func (r *Runtime) createUnknown(creates []unknownCreate) error {
	for _, c := range creates {
		n := r.Nouns[c.noun]
		if _, err := r.Execute(Request{Noun: c.noun, Verb: "create", Args: Arguments{n.keyProp().Name: c.name}}); err != nil {
			return c.otherwise
		}
		r.notice("created %s %s", r.DisplayNoun(c.noun), c.name)
	}
	return nil
}

// createsUnknown reports whether cli.unknown-name creates a thing of noun
// for a name it does not have (unknownNamePolicies).
func (r *Runtime) createsUnknown(noun string) bool {
	n := r.Nouns[noun]
	if n == nil || n.keyProp() == nil || !r.makesThings(noun) {
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

// makesThings reports whether a name a person types can make a thing of
// noun: its create is offered and its things are not system facts
// (Store.Facts: a fact the overlay "creates" is only a row of the user's
// own beside the registry, not the mode, theme or verb the name asks for).
func (r *Runtime) makesThings(noun string) bool {
	n := r.Nouns[noun]
	return n != nil && n.Store != nil && !n.Store.Facts && r.offers(noun+".create")
}

// PickersCreateRowSetting adds a create "TEXT" row to a field's picker of
// a noun's things when the noun can create (default true).
const PickersCreateRowSetting = "pickers.create-row"

// pickerCreates reports whether a picker of noun's things offers a create
// row: pickers.create-row is on and the noun's create is offered.
func (r *Runtime) pickerCreates(noun string) bool {
	if noun == "" || noun == AnyNoun || !r.makesThings(noun) {
		return false
	}
	s := r.Settings()
	return !s.Has(PickersCreateRowSetting) || s.Bool(PickersCreateRowSetting)
}

// createNamed creates a thing of noun named text (its label prop, else its
// key) and answers the new thing's key.
func (r *Runtime) createNamed(noun, text string) (string, error) {
	n := r.Nouns[noun]
	if n == nil || n.keyProp() == nil {
		return "", r.NotFound("noun", noun, nil)
	}
	name := n.keyProp().Name
	if p := n.roleProp(RoleLabel); p != nil && !p.ReadOnly {
		name = p.Name
	}
	v, err := r.Execute(Request{Noun: noun, Verb: "create", Args: Arguments{name: text}})
	if err != nil {
		return "", err
	}
	if t, ok := v.(map[string]any); ok && t[n.keyProp().Name] != nil {
		return fmt.Sprint(t[n.keyProp().Name]), nil
	}
	return text, nil
}
