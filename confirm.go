package ochazuke

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
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
		Pattern(Any, Do("key.ignore").Hide())
	r.Register(Operation{Verb: "accept", Noun: "confirm", Description: "Run the request the open confirm modal holds and close it", Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		inst := r.Instances[r.TopModal()]
		if inst == nil || inst.Noun != "confirm" || inst.Payload.Request == nil {
			return nil, errors.New("no confirm modal is open")
		}
		q := *inst.Payload.Request
		if err := r.Pop(); err != nil {
			return nil, err
		}
		return r.Execute(q)
	}})
	r.Register(Operation{Verb: "set", Noun: "confirm", Description: "Add or remove the confirm prompt of an operation ({{id}}, {{noun}}, {{label}} and {{<arg>}} expand; a label argument wins over the instance label); an empty prompt removes it", Parameters: map[string]Parameter{"operation": {Type: "string", Required: true, Description: "noun.verb"}, "prompt": {Type: "string"}}, Result: "operation", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
//
// The prompt's {{id}}, {{target}}, {{noun}} and {{label}} describe that
// instance when the request addresses one: an explicit target, or an
// instance of the verb's noun on the focus path. A request that addresses
// none (a CLI call with positional words, the palette away from any such
// row) would name app, so they read the request instead: {{noun}} the
// verb's noun, {{id}}, {{target}} and {{label}} the argument of that name,
// else the request's first argument in parameter order (ParameterOrder,
// then the others by name), and the focus as before when there are no
// arguments. A label argument always wins for {{label}};
// {{<arg>}} is that argument everywhere.
func (r *Runtime) openConfirm(c Context, q Request, op Operation) (any, error) {
	inst := r.Instances[focusOf(c)]
	stored := q
	stored.Confirmed = true
	stored.Target = inst.ID
	addressed := q.Target != "" || inst.Noun == op.Noun
	prompt := promptField.ReplaceAllStringFunc(op.Confirm, func(m string) string {
		name := promptField.FindStringSubmatch(m)[1]
		if !addressed {
			switch name {
			case "noun":
				return op.Noun
			case "id", "target", "label":
				if v, ok := q.Args[name]; ok {
					return fmt.Sprint(v)
				}
				if v, ok := firstArgument(op, q.Args); ok {
					return fmt.Sprint(v)
				}
			}
		}
		switch name {
		case "id", "target":
			return inst.ID
		case "noun":
			return inst.Noun
		case "label":
			if v, ok := q.Args["label"]; ok {
				return fmt.Sprint(v)
			}
			if inst.Label != "" {
				return inst.Label
			}
			return inst.ID
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

// firstArgument is the request's first argument with a value: in the
// operation's parameter order, then any other argument by name.
func firstArgument(op Operation, args Arguments) (any, bool) {
	for _, name := range op.ParameterOrder() {
		if v, ok := args[name]; ok {
			return v, true
		}
	}
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	slices.Sort(names)
	if len(names) == 0 {
		return nil, false
	}
	return args[names[0]], true
}

// confirmView renders the confirm modal body: the prompt alone. Its keys
// are the modal's hint line (BindingHints), never a second hand-written
// line; the swallowing any row is hidden from it.
func confirmView(r *Runtime, inst *Instance, width, height int) Block {
	if inst == nil {
		return Text("")
	}
	return Text(inst.Payload.Prompt)
}
