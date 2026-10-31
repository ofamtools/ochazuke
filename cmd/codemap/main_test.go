package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestBuildConstrainedFilesMapTheSameEveryRun pins the fix for a map that
// flipped between runs: two build-tagged files declare the same names, and
// the owner of a name was whichever file Go's map iteration visited last.
func TestBuildConstrainedFilesMapTheSameEveryRun(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pty.go", "package p\n\nfunc Start() { configure(); end() }\n")
	write("pty_unix.go", "//go:build !windows\n\npackage p\n\nfunc configure() { end() }\n\nfunc end() {}\n")
	write("pty_windows.go", "//go:build windows\n\npackage p\n\nfunc configure() {}\n\nfunc end() { Start() }\n")

	want := map[string]string{
		"pty.go":         "pty_unix.go(2), pty_windows.go(2)",
		"pty_unix.go":    "",
		"pty_windows.go": "pty.go(1)",
	}
	for run := 0; run < 50; run++ {
		files, _, _ := mapDir(dir)
		for name, w := range want {
			if got := topN(files[name].refs, 4); got != w {
				t.Fatalf("run %d: %s leans on %q, want %q", run, name, got, w)
			}
		}
	}
}

// Nested importable packages belong in the agent's map, while fixture and
// separately versioned module code have their own documentation boundaries.
func TestSubpackagesIncludeNested(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"cli/shell.go", "cli/mcp/server.go", "cli/inspect/page.go", "cli/testdata/fake/no.go", "examples/demo/main.go", "separate/module.go", "separate/go.mod"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package fixture\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got := subpackages(root)
	want := []string{"cli", "cli/inspect", "cli/mcp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages %v want %v", got, want)
	}
}
