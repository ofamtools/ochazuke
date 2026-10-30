package ochazuke_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nestedTestsEnv is set by make test, which runs every module's tests;
// a bare go test ./... at the root runs only the root module.
const nestedTestsEnv = "OCHAZUKE_NESTED_TESTS"

// TestNestedModulesRunToo fails a root-only go test ./... so it cannot
// pass while the nested modules' tests (o-story's stories, the examples)
// went unrun: make test sets OCHAZUKE_NESTED_TESTS=1 and runs every
// module. The modules it names are the go.mod files under the root, so a
// new nested module is named without editing this test.
func TestNestedModulesRunToo(t *testing.T) {
	if os.Getenv(nestedTestsEnv) != "" {
		return
	}
	var nested []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" && p != "go.mod" {
			nested = append(nested, filepath.Dir(p))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(nested) == 0 {
		return
	}
	t.Fatalf("go test ./... ran the root module only; the stories live in the cmd/o-story module and the nested modules (%s) have their own tests: run make test (it sets %s=1 and runs every module)", strings.Join(nested, ", "), nestedTestsEnv)
}
