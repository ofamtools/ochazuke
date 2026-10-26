package main

import (
	"os"
	"path/filepath"
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
