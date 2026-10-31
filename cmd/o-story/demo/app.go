// Package demo is ochazuke's demo app for stories: a story's synthetic
// pages (its page and pages fields: lists, trees, inputs, a fake terminal,
// fake jobs) built on a fresh runtime. o-story registers it as its first
// (default) app, so a story that names no app is a demo story.
package demo

import (
	"strings"

	"ofam.dev/monogatari"
	"ofam.dev/ochazuke"
)

// App is the demo app: one fixture, pages, that builds the story's pages.
func App() monogatari.App {
	return monogatari.App{
		Name:     Demo,
		Summary:  "ochazuke's components on synthetic pages (the story's page and pages)",
		Fields:   []string{"page", "pages"},
		Expected: Expected,
		Fixtures: []monogatari.Fixture{{
			Name:    "pages",
			Summary: "the story's page and pages on a fresh runtime, every outside effect faked",
			Build:   build,
		}},
	}
}

// Expected reports the errors the demo's fakes mean (plain text, so
// matched by message): finish (W/X/E in the ticker stories) with no job of
// that verb running, a tree or record store a story fails on purpose
// (failWrites), and a binding that runs an input verb on a list row only
// to show its arguments in help (help/arguments).
func Expected(op, err string) bool {
	op = strings.TrimPrefix(op, "app.")
	switch {
	case op == Demo+".finish" && strings.HasPrefix(err, "no running ") && strings.HasSuffix(err, " job"):
		return true
	case err == "store unavailable" || strings.HasSuffix(err, ": store unavailable"):
		return true
	case op == "input.insert" && err == "no focused input":
		return true
	}
	return false
}

// label is what an instance shows: its label, else its id.
func label(inst *ochazuke.Instance) string {
	if inst == nil {
		return ""
	}
	if inst.Label != "" {
		return inst.Label
	}
	return inst.ID
}
