package main

import (
	_ "embed"
	"flag"

	"github.com/unixsuperhero/ochazuke/internal/storymatrix"
	"github.com/unixsuperhero/ochazuke/story"
)

// componentsJSON is the component map o-story matrix and coverage read.
//
//go:embed components.json
var componentsJSON []byte

// matrixFlags parses --component and --json.
func (t *Tool) matrixFlags(name string, args []string) (component string, asJSON bool, dir string, err error) {
	flags := flag.NewFlagSet(t.Prog+" "+name, flag.ContinueOnError)
	flags.SetOutput(t.Stderr)
	c := flags.String("component", "", "only this component")
	j := flags.Bool("json", false, "print JSON")
	d := flags.String("dir", "", "read the core stories from this directory instead of the embedded copy")
	if _, err := parse(flags, args); err != nil {
		return "", false, "", err
	}
	return *c, *j, *d, nil
}

// Matrix lists the cells: one id per line, or the actions and cells as JSON.
func (t *Tool) Matrix(args []string) error {
	component, asJSON, _, err := t.matrixFlags("matrix", args)
	if err != nil {
		return err
	}
	_, mx, err := storymatrix.Load(componentsJSON)
	if err != nil {
		return err
	}
	if mx, err = mx.Only(component); err != nil {
		return err
	}
	if asJSON {
		return mx.WriteJSON(t.Stdout)
	}
	return mx.WriteText(t.Stdout)
}

// Coverage compares the cells with the core stories' covers.
func (t *Tool) Coverage(args []string) error {
	component, asJSON, dir, err := t.matrixFlags("coverage", args)
	if err != nil {
		return err
	}
	_, mx, err := storymatrix.Load(componentsJSON)
	if err != nil {
		return err
	}
	var core []*story.Story
	if dir != "" {
		core, err = story.LoadDir(dir)
	} else {
		core, err = story.LoadFS(embedded, "stories")
	}
	if err != nil {
		return err
	}
	typos := mx.Typos(core)
	if mx, err = mx.Only(component); err != nil {
		return err
	}
	rows := mx.Coverage(core, component != "")
	if asJSON {
		return storymatrix.WriteCoverageJSON(t.Stdout, rows, typos)
	}
	stories, untagged := len(core), 0
	for _, s := range core {
		if len(s.Covers) == 0 {
			untagged++
		}
	}
	if component != "" {
		stories, untagged = rows[0].Stories, len(rows[0].Untagged)
	}
	storymatrix.WriteCoverageText(t.Stdout, rows, typos, stories, untagged)
	return nil
}
