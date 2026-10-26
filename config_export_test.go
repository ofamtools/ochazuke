package ochazuke

import (
	"path/filepath"
	"testing"
)

// config.path prints the config, state and default reference directories;
// a sandbox has none and says so.
func TestConfigPathPrintsTheConfigAndStateDirectories(t *testing.T) {
	r := NewRuntime()
	if _, err := r.Execute(Request{Verb: "path", Noun: "config"}); err == nil {
		t.Fatal("config.path answered without any directory")
	}
	dir, state := t.TempDir(), t.TempDir()
	r.Config().Dir, r.Config().StateDir = dir, state
	got, err := r.Execute(Request{Verb: "path", Noun: "config"})
	if err != nil {
		t.Fatal(err)
	}
	want := ConfigPaths{Config: dir, State: state, Reference: filepath.Join(dir, ReferenceDir)}
	if got != want {
		t.Fatalf("config.path = %+v, want %+v", got, want)
	}
}
