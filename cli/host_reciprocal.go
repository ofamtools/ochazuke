package cli

import (
	"fmt"
	stdmaps "maps"

	"ofam.dev/ochazuke"
)

func mountReciprocals(r *ochazuke.Runtime, nouns []CatalogNoun, mounted map[string]string, param func(ochazuke.Parameter) ochazuke.Parameter, warn func(string, ...any)) {
	for _, noun := range nouns {
		local := mounted[noun.Name]
		for _, verb := range noun.Verbs {
			for _, reverse := range verb.Reciprocals {
				if err := mountReciprocal(r, noun, local, verb, reverse, param); err != nil {
					warn("%s %s reciprocal %s skipped: %v", noun.Name, verb.Name, reverse.Verb, err)
				}
			}
		}
	}
}

func mountReciprocal(r *ochazuke.Runtime, noun CatalogNoun, local string, verb CatalogVerb, reverse ochazuke.Reciprocal, param func(ochazuke.Parameter) ochazuke.Parameter) error {
	end, ok := verb.Params[reverse.Parameter]
	if !ok || end.Noun == "" && end.HostNoun == "" {
		return fmt.Errorf("parameter %q must name a noun", reverse.Parameter)
	}
	end = param(end)
	if r.Noun(end.Noun) == nil {
		return nil // the other end is not present in this host
	}
	key := keyOf(noun.Props)
	if key == "" || reverse.Subject == "" || reverse.Verb == "" {
		return fmt.Errorf("a reciprocal needs a keyed source noun, verb and subject parameter")
	}
	receiverKey := ""
	for _, prop := range r.Props(end.Noun) {
		if prop.Key {
			receiverKey = prop.Name
			break
		}
	}
	if receiverKey == "" || receiverKey == reverse.Subject {
		return fmt.Errorf("the receiver needs a distinct key prop")
	}
	forward, ok := r.Operation(local + "." + verb.Name)
	if !ok {
		return fmt.Errorf("the original operation did not mount")
	}
	if _, taken := r.Operation(end.Noun + "." + reverse.Verb); taken {
		return fmt.Errorf("the receiver already has that verb")
	}
	params := stdmaps.Clone(forward.Parameters)
	delete(params, key)
	delete(params, reverse.Parameter)
	if _, taken := params[reverse.Subject]; taken {
		return fmt.Errorf("subject parameter %q is already in use", reverse.Subject)
	}
	if _, taken := params[receiverKey]; taken {
		return fmt.Errorf("receiver key parameter %q is already in use", receiverKey)
	}
	params[reverse.Subject] = ochazuke.Parameter{Type: "string", Required: true, Noun: local, Accept: ochazuke.AcceptKeep, Description: "The " + r.DisplayNoun(local) + " to use"}
	params[receiverKey] = ochazuke.Parameter{Type: "string", Noun: end.Noun, Focus: true, Accept: ochazuke.AcceptKeep, Description: "The receiving " + r.DisplayNoun(end.Noun) + " (default: selected)"}
	handler := func(c ochazuke.Ctx) ochazuke.Result {
		args := stdmaps.Clone(c.Args)
		subject, receiver := args[reverse.Subject], args[receiverKey]
		if receiver == nil || receiver == "" {
			receiver = reciprocalReceiver(c, end.Noun, receiverKey)
		}
		if receiver == nil || receiver == "" {
			return ochazuke.Failed(fmt.Errorf("choose a receiving %s", r.DisplayNoun(end.Noun)))
		}
		delete(args, reverse.Subject)
		delete(args, receiverKey)
		args[key], args[reverse.Parameter] = subject, receiver
		value, err := r.Execute(ochazuke.Request{Noun: local, Verb: verb.Name, Args: args})
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(value)
	}
	if err := try(func() { r.Define(end.Noun).Verb(reverse.Verb, handler, forward.Description) }); err != nil {
		return err
	}
	op, _ := r.Operation(end.Noun + "." + reverse.Verb)
	op.Parameters, op.Positional = params, []string{reverse.Subject}
	op.Destructive, op.Agent, op.HistoryKind = forward.Destructive, forward.Agent, forward.HistoryKind
	r.Operations.Set(op)
	return nil
}

func reciprocalReceiver(c ochazuke.Ctx, noun, key string) any {
	for inst := c.Instance; inst != nil; inst = c.Runtime.Instances[inst.Parent] {
		if c.Runtime.CanonicalNoun(inst.Noun) != noun {
			continue
		}
		if id, ok := inst.Data.(string); ok && id != "" {
			return id
		}
		if thing, err := ochazuke.ThingOf(inst.Data); err == nil && thing[key] != nil {
			return thing[key]
		}
		if n := c.Runtime.Noun(noun); n != nil && n.Store != nil && n.Store.Kind == ochazuke.Instances {
			return inst.ID
		}
	}
	return nil
}
