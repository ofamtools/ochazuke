package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// The codemod rewrites a consumer written against ochazuke v0.99.0 into
// testdata/new.go.txt, and a second run against the same old API changes
// nothing more (the edits are planned from the old types once). The module
// needs ochazuke v0.99.0 and its requirements in the module cache.
func TestRewriteGolden(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("testdata/old.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module consumer\n\ngo 1.26.0\n\nrequire ofam.dev/ochazuke v0.99.0\n")
	write("consumer.go", string(src))
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir, tidy.Env = dir, env
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Skipf("ochazuke v0.99.0 is not in the module cache: %v\n%s", err, out)
	}
	run, err := Rewrite(packages.Config{Dir: dir, Env: env}, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.Write(false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "consumer.go"))
	if os.Getenv("OPNAME_UPDATE") != "" {
		_ = os.WriteFile("testdata/new.go.txt", got, 0o644)
	}
	want, _ := os.ReadFile("testdata/new.go.txt")
	if string(got) != string(want) {
		t.Fatalf("rewrite:\n%s\nwant:\n%s", got, want)
	}
	if len(run.Manual) != 0 {
		t.Fatalf("manual: %s", strings.Join(run.Manual, "\n"))
	}
	// The rewritten file builds against this checkout's ochazuke.
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	write("go.mod", "module consumer\n\ngo 1.26.0\n\nrequire ofam.dev/ochazuke v0.99.0\n\nreplace ofam.dev/ochazuke => "+root+"\n")
	build := exec.Command("go", "build", "./...")
	build.Dir, build.Env = dir, env
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the rewrite does not build on the new API: %v\n%s", err, out)
	}
}
