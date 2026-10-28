// Package strictcheck fails an application's tests when its code uses a
// name ochazuke has deprecated.
//
// ochazuke keeps a deprecated name working for one release, so nothing
// breaks on upgrade, and puts it in a file built only without the
// ochazuke_strict tag (Tag). Check vets the calling module with that tag:
// a use of any deprecated name no longer compiles, and the test fails with
// the compiler's file:line. An application adopts it with one test:
//
//	func TestNoDeprecatedOchazuke(t *testing.T) { strictcheck.Check(t) }
//
// It type-checks, so it finds the deprecated method itself (ctx.Status,
// r.UI().Status) and never a method of the same name on another type.
// It runs go vet, so a vet finding fails it as well.
package strictcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Tag is the build tag that leaves ochazuke's deprecated names out.
const Tag = "ochazuke_strict"

// Check runs go vet -tags ochazuke_strict over patterns (default ./...)
// from the root of the module the test runs in, and fails t with the
// output when it does not pass.
func Check(t testing.TB, patterns ...string) {
	t.Helper()
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("strictcheck: go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(gomod))
	if mod == "" || mod == os.DevNull {
		t.Fatalf("strictcheck: not inside a module")
	}
	if out, err := vet(filepath.Dir(mod), patterns...); err != nil {
		t.Fatalf("strictcheck: code uses a name ochazuke deprecated (or vet failed); replace each with what its Deprecated comment names.\n%s", out)
	}
}

// vet runs go vet with Tag in dir and returns the command line and output.
func vet(dir string, patterns ...string) (string, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	args := append([]string{"vet", "-tags", Tag}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return "go " + strings.Join(args, " ") + ":\n" + string(out), err
}
