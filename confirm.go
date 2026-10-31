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
	r.Register(Operation{Verb: "accept", Noun: "ochazuke.confirm", Description: "Run the request the open confirm modal holds and close it", Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		inst := r.Instances[r.TopModal()]
		if inst == nil || inst.Noun != "ochazuke.confirm" || inst.Payload.Request == nil {
			return nil, errors.New("no confirm modal is open")
		}
		q := *inst.Payload.Request
		if err := r.Pop(); err != nil {
			return nil, err
		}
		return r.Execute(q)
	}})
	r.Register(Operation{Verb: "set", Noun: "ochazuke.confirm", Description: "Add or remove the confirm prompt of an operation ({{id}}, {{noun}}, {{label}} and {{<arg>}} expand; a label argument wins over the instance label); an empty prompt removes it", Parameters: map[string]Parameter{"operation": {Type: "string", Required: true, Description: "noun.verb", Noun: "ochazuke.operations"}, "prompt": {Type: "string"}}, Result: "operation", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
// {{noun}} is always the verb's noun (the request's), never the focused
// or targeted instance's. A request that names its thing by the noun's key
// argument (task delete --id t2) is about that thing: {{id}} and
// {{target}} are the key, {{label}} its label read from the store. A
// request over several subjects names the first by its store label and
// counts the rest ("Water the plants and 1 more"). Else
// {{id}}, {{target}} and {{label}} describe the instance the request
// addresses: an explicit target, or an instance of the verb's noun on the
// focus path. A request that addresses none (a CLI call with positional
// words, the palette away from any such row) would name app, so they read
// the request instead: the argument of that name,
// else the request's first argument in parameter order (ParameterOrder,
// then the others by name), and the focus as before when there are no
// arguments. A label argument always wins for {{label}};
// {{<arg>}} is that argument everywhere.
func (r *Runtime) openConfirm(c Context, q Request, op Operation) (any, error) {
	return r.openConfirmAbout(c, q, q, op)
}

// openConfirmAbout is openConfirm for a request run on another's behalf:
// the prompt's fields read q, the request the prompt is about (task.delete
// over the rows), and y runs run (the list.apply that asked, confirmed),
// so {{label}} never reads the asking verb's own arguments.
func (r *Runtime) openConfirmAbout(c Context, q, run Request, op Operation) (any, error) {
	inst := r.Instances[focusOf(c)]
	stored := run
	actor := r.Actor()
	if actor.IsAgent() && r.Settings().Bool(AgentRightsGateSetting) {
		policy := op
		if registered, ok := r.Operation(op.Key()); ok {
			policy = registered
		}
		right := r.AgentRight(policy, r.agentLive())
		if right.Right == AgentNever {
			return nil, &RefusedError{Verb: policy.Key(), Rule: right.Rule, Reason: "agents may never run this verb; ask the owner"}
		}
		if !r.agentLive() && !r.agentApproved {
			return nil, &RefusedError{Verb: policy.Key(), Rule: right.Rule, Reason: "needs the owner's confirmation; ask them to run this command"}
		}
	}
	stored.Actor, stored.source = &actor, r.history.source
	stored.Confirmed = true
	stored.agentApproved = true
	stored.Target = inst.ID
	// A request that names its thing by the noun's key argument (task
	// delete --id t2) is about that thing, whatever instance it targets
	// (the CLI's owner of the noun's rows, a list) or the focus is.
	named := ""
	if n := r.Nouns[op.Noun]; n != nil {
		if kp := n.keyProp(); kp != nil && !emptyProp(q.Args[kp.Name]) {
			named = fmt.Sprint(q.Args[kp.Name])
		}
	}
	addressed := named == "" && (q.Target != "" || inst.Noun == op.Noun)
	prompt := promptField.ReplaceAllStringFunc(op.Confirm, func(m string) string {
		name := promptField.FindStringSubmatch(m)[1]
		if _, given := q.Args["label"]; name == "label" && !given && len(q.Subjects) <= 1 {
			// A standard verb's thing is shown by its label prop, else
			// its Fallback, read from the store (a key argument or the
			// instance the request acts for).
			if label := r.storeLabel(c, q, inst); label != "" {
				return label
			}
		}
		if _, given := q.Args["label"]; name == "label" && !given && named == "" && len(q.Subjects) > 1 {
			// Several subjects name the first by its store label, as one
			// does, and count the rest (subjectsLabel's form).
			if first := r.Instances[q.Subjects[0]]; first != nil {
				if label := r.storeLabel(c, q, first); label != "" {
					return fmt.Sprintf("%s and %d more", label, len(q.Subjects)-1)
				}
			}
		}
		if name == "noun" {
			return r.DisplayNoun(op.Noun) // the request's noun, never the focus's
		}
		if named != "" && (name == "id" || name == "target") {
			return named
		}
		if !addressed {
			switch name {
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
		case "label":
			if v, ok := q.Args["label"]; ok {
				return fmt.Sprint(v)
			}
			if len(q.Subjects) > 1 {
				return r.subjectsLabel(q.Subjects)
			}
			if label := r.thingLabel(inst); label != "" {
				return label
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
	if actor.IsAgent() && r.Settings().Bool(AgentConfirmAttributionSetting) {
		prompt = actor.String() + " asks:\n" + prompt
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

// storeLabel is the label of the thing a standard verb's request acts on,
// read from its noun's store: the label prop, else its Fallback. "" when
// the request's operation is not a standard verb, the store reads no
// things, or the thing has no label.
func (r *Runtime) storeLabel(c Context, q Request, inst *Instance) string {
	op := r.Operations[q.Key()]
	n := r.Nouns[op.Noun]
	if op.Standard == "" || n == nil || n.Store == nil || n.Store.Get == nil || n.roleProp(RoleLabel) == nil {
		return ""
	}
	ctx := Ctx{actor: r.Actor(), Runtime: r, Context: c, Args: q.Args, Instance: inst}
	key, err := r.subjectKey(n, ctx)
	if err != nil {
		return ""
	}
	t, err := n.Store.Get(ctx, key)
	if err != nil || t == nil {
		return ""
	}
	return n.displayLabel(func(name string) any { return t[name] })
}
