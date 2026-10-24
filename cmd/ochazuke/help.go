package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"ochazuke"
)

// choicesFunc resolves a parameter Source (noun.verb) to its choices.
type choicesFunc func(source string) ([]ochazuke.Choice, error)

// operationHelp prints NOUN VERB's description and parameters, with their
// choices, from the running app's operations.list.
func operationHelp(ctx context.Context, socket, noun, verb string, out io.Writer) error {
	result, err := ochazuke.Call(ctx, socket, ochazuke.Request{Noun: "operations", Verb: "list"})
	if err != nil {
		fmt.Fprintf(out, "\n(parameters unavailable: %v)\n", err)
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var catalog []ochazuke.Operation
	if err := json.Unmarshal(data, &catalog); err != nil {
		return err
	}
	for _, op := range catalog {
		if op.Noun == noun && op.Verb == verb {
			writeOperationHelp(out, op, func(source string) ([]ochazuke.Choice, error) {
				n, v, _ := strings.Cut(source, ".")
				result, err := ochazuke.Call(ctx, socket, ochazuke.Request{Noun: n, Verb: v})
				if err != nil {
					return nil, err
				}
				return ochazuke.ParseChoices(result)
			})
			return nil
		}
	}
	return fmt.Errorf("unknown operation %q", noun+"."+verb)
}

func writeOperationHelp(out io.Writer, op ochazuke.Operation, choices choicesFunc) {
	fmt.Fprintf(out, "\n%s.%s (%s): %s\n", op.Noun, op.Verb, op.Kind, op.Description)
	if len(op.Parameters) == 0 {
		fmt.Fprintln(out, "No parameters.")
		return
	}
	names := make([]string, 0, len(op.Parameters))
	for name := range op.Parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(out, "Parameters:")
	for _, name := range names {
		p := op.Parameters[name]
		kind := "optional"
		if p.Required {
			kind = "required"
		}
		fmt.Fprintf(out, "  %s (%s, %s)", name, p.Type, kind)
		if p.Description != "" {
			fmt.Fprintf(out, ": %s", p.Description)
		}
		fmt.Fprintln(out)
		if len(p.Enum) == 0 && p.Source == "" {
			continue
		}
		ids := append([]string(nil), p.Enum...)
		if p.Source != "" {
			more, err := choices(p.Source)
			if err != nil {
				fmt.Fprintf(out, "    choices from %s unavailable: %v\n", p.Source, err)
			}
			for _, c := range more {
				ids = append(ids, c.ID)
			}
		}
		free := ""
		if p.Free {
			free = " (or any value)"
		}
		fmt.Fprintf(out, "    choices%s: %s\n", free, strings.Join(ids, ", "))
	}
}
