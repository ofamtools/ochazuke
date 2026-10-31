package strictcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ochazuke itself builds without its deprecated names: nothing in the
// library, its commands or its tests leans on them.
func TestOchazukeBuildsStrict(t *testing.T) { Check(t) }

// A module that calls a deprecated name passes the ordinary build and
// fails the strict one, at the call; the replacement passes both.
func TestCheckCatchesADeprecatedCall(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	app := func(call string) string {
		dir := t.TempDir()
		write := func(name, text string) {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("go.mod", "module example.com/app\n\ngo 1.26.0\n\nrequire ofam.dev/ochazuke v0.0.0\n\nreplace ofam.dev/ochazuke => "+root+"\n")
		write("go.sum", string(sum))
		write("main.go", `package main

import "ofam.dev/ochazuke"

func tell(ctx ochazuke.Ctx) { ctx.`+call+`("queued") }

func main() { tell(ochazuke.Ctx{Runtime: ochazuke.NewRuntime()}) }
`)
		return dir
	}
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	build := func(dir string, tags ...string) error {
		cmd := exec.Command("go", append(append([]string{"build"}, tags...), "./...")...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("%s", out)
		}
		return err
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")

	old := app("Status")
	if err := build(old); err != nil {
		t.Fatalf("the deprecated name still builds without the tag: %v", err)
	}
	out, err := vet(old)
	if err == nil || !strings.Contains(out, "main.go:5") || !strings.Contains(out, "Status") {
		t.Fatalf("strict vet passed a deprecated call or did not name it: %v\n%s", err, out)
	}
	if out, err := vet(app("Info")); err != nil {
		t.Fatalf("strict vet fails the replacement: %v\n%s", err, out)
	}
}
