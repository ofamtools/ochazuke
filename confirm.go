package ochazuke

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The confirm noun: a framework modal a verb with a Confirm prompt opens
// before it runs. Its body instance is parented at the focus that issued the
// request and carries Payload{Prompt, Request}; y runs the stored request
// (marked Confirmed so the prompt does not reopen) in the context it came
// from, n or esc pops, every other key is swallowed.
func registerConfirm(r *Runtime) {
	r.Define("confirm").Mode("").
		Bind("y", Do("confirm.accept")).
		Bind("enter", Do("confirm.accept")).
		Bind("n", Do("modal.pop")).
		Bind("esc", Do("modal.pop")).
		Pattern(Any, Do("key.ignore"))
	r.Register(Operation{Verb: "accept", Noun: "confirm", Description: "Run the request the open confirm modal holds and close it", Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		inst := r.Instances[r.top().ID]
		if inst == nil || inst.Noun != "confirm" || inst.Payload.Request == nil {
			return nil, errors.New("no confirm modal is open")
		}
		q := *inst.Payload.Request
		if err := r.Pop(); err != nil {
			return nil, err
		}
		return r.Execute(q)
	}})
	r.Register(Operation{Verb: "set", Noun: "confirm", Description: "Add or remove the confirm prompt of an operation ({{id}}, {{noun}} and {{<arg>}} expand); an empty prompt removes it", Parameters: map[string]Parameter{"operation": {Type: "string", Required: true, Description: "noun.verb"}, "prompt": {Type: "string"}}, Result: "operation", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		key := StringArg(a, "operation", "")
		if err := r.SetConfirm(key, StringArg(a, "prompt", "")); err != nil {
			return nil, err
		}
		return r.Operations[key], nil
	}})
}

// SetConfirm sets or clears (empty prompt) the confirm prompt of noun.verb.
func (r *Runtime) SetConfirm(key, prompt string) error {
	op, ok := r.Operations[key]
	if !ok {
		return fmt.Errorf("unknown operation %q", key)
	}
	op.Confirm = prompt
	r.Operations[key] = op
	return nil
}

var promptField = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)

// openConfirm opens the confirm modal for a request instead of running it.
// The stored request targets the instance the request addressed, so y runs
// it there even though the confirm body is focused.
func (r *Runtime) openConfirm(c Context, q Request, op Operation) (any, error) {
	inst := r.Instances[focusOf(c)]
	stored := q
	stored.Confirmed = true
	stored.Target = inst.ID
	prompt := promptField.ReplaceAllStringFunc(op.Confirm, func(m string) string {
		name := promptField.FindStringSubmatch(m)[1]
		switch name {
		case "id":
			return inst.ID
		case "noun":
			return inst.Noun
		}
		if v, ok := q.Args[name]; ok {
			return fmt.Sprint(v)
		}
		return m
	})
	if strings.TrimSpace(prompt) == "" {
		prompt = "Run " + q.Key() + "?"
	}
	body, err := r.OpenModal("confirm", Payload{Prompt: prompt, Request: &stored})
	if err != nil {
		return nil, err
	}
	return map[string]any{"confirm": body.ID, "prompt": prompt, "request": stored}, nil
}

// confirmView renders the confirm modal body.
func confirmView(r *Runtime, f *Frame, width, height int) Block {
	inst := r.Instances[f.ID]
	if inst == nil {
		return Text("")
	}
	return Text(inst.Payload.Prompt + "\n\ny yes · n no")
}
