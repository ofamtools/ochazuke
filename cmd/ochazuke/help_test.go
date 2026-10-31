package main

import (
	"errors"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestOperationHelpShowsChoices(t *testing.T) {
	op := ochazuke.Operation{Noun: "wall", Verb: "paint", Kind: "interaction", Description: "Paint the wall", Parameters: map[string]ochazuke.Parameter{
		"color": {Type: "string", Required: true, Enum: []string{"red"}, Source: "colors.list"},
		"shade": {Type: "string", Source: "shades.list", Free: true},
		"coats": {Type: "integer"},
	}}
	var out strings.Builder
	writeOperationHelp(&out, op, func(source string) ([]ochazuke.Choice, error) {
		if source == "colors.list" {
			return []ochazuke.Choice{{ID: "cyan", Label: "Cyan"}}, nil
		}
		return nil, errors.New("offline")
	})
	for _, want := range []string{
		"wall.paint (interaction): Paint the wall",
		"  coats (integer, optional)\n  color",
		"  color (string, required)\n    choices: red, cyan\n",
		"    choices from shades.list unavailable: offline\n    choices (or any value): \n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, out.String())
		}
	}
}
