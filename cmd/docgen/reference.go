package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/markdown"
	"github.com/unixsuperhero/ochazuke/refdoc"
)

// The commands the reference's header names.
const (
	regenerateCommand = "make reference"
	checkCommand      = "go test ./cmd/docgen -run TestReference"
	// referenceExclude is left out of the dirty check, so writing the
	// pages does not make the next generation dirty.
	referenceExclude = "docs/reference"
	// referenceTool is the tool the pages name.
	referenceTool = "ochazuke"
)

// referenceRuntime is the framework runtime the committed reference
// describes: NewRuntime, NewModel (with its palette), NewPicker and
// markdown.Register, no demo or application nouns, its config and state
// in dir so no user file is read. Its App is ochazuke, so the settings a
// config directory brings (the history file's) are declared as in any app.
func referenceRuntime(dir string) *ochazuke.Runtime {
	r := ochazuke.NewRuntime()
	c := r.Config()
	c.App, c.Dir, c.StateDir = referenceTool, filepath.Join(dir, "config"), filepath.Join(dir, "state")
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	markdown.Register(r)
	return r
}

// referenceOptions are the render options of the committed pages: no
// user configuration, the header's commands, and stamp.
func referenceOptions(stamp refdoc.Stamp) refdoc.Options {
	return refdoc.Options{Stamp: stamp, Regenerate: regenerateCommand, Check: checkCommand}
}

// writeReference writes the reference pages into out, stamped from the git
// checkout at repo.
func writeReference(repo, out string) error {
	tmp, err := os.MkdirTemp("", "docgen-ref-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	stamp, err := refdoc.FromGit(repo, referenceTool, referenceExclude)
	if err != nil {
		return err
	}
	m := referenceRuntime(tmp).ReferenceModel()
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for name, text := range refdoc.Render(m, referenceOptions(stamp)) {
		path := filepath.Join(out, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
		fmt.Println("Generated", path)
	}
	return nil
}
