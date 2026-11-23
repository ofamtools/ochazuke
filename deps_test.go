package ochazuke_deps_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const mod = "ofam.dev/ochazuke"

type pkgError struct {
	Err string `json:"Err"`
}

type pkgInfo struct {
	ImportPath   string     `json:"ImportPath"`
	Standard     bool       `json:"Standard"`
	Imports      []string   `json:"Imports"`
	TestImports  []string   `json:"TestImports"`
	XTestImports []string   `json:"XTestImports"`
	Deps         []string   `json:"Deps"`
	Incomplete   bool       `json:"Incomplete"`
	Error        *pkgError  `json:"Error"`
	DepsErrors   []pkgError `json:"DepsErrors"`
}

func runGoList(t *testing.T, args ...string) []pkgInfo {
	t.Helper()
	cmd := exec.Command("go", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go %s failed: %v\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}

	var pkgs []pkgInfo
	dec := json.NewDecoder(&stdout)
	for {
		var p pkgInfo
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode go %s output: %v", strings.Join(args, " "), err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// policy states what a package may import outside the standard library.
type policy struct {
	// allow lists the exact non-stdlib imports the package and tests may use.
	allow []string
	// allowTests adds direct imports allowed only from test files.
	allowTests []string
	// closed also restricts the transitive dependencies to stdlib + allow.
	closed bool
	// forbidDeps lists substrings no transitive dependency may contain.
	forbidDeps []string
}

const (
	lipgloss  = "charm.land/lipgloss/v2"
	bubbletea = "charm.land/bubbletea/v2"
	uniseg    = "github.com/rivo/uniseg"
	oden      = "ofam.dev/oden"
)

var policies = map[string]policy{
	// Tier 0: stdlib only, except buffer (layout + uniseg).
	mod:               {closed: true, allowTests: []string{mod + "/core", mod + "/ui", mod + "/tea", mod + "/widgets/table", bubbletea}},
	mod + "/trie":     {closed: true},
	mod + "/expr":     {closed: true},
	mod + "/layout":   {closed: true},
	mod + "/buffer":   {closed: true, allow: []string{mod + "/layout", uniseg}},
	mod + "/recorder": {closed: true},
	// Tier 1: the kernel is pure; ui renders with Lip Gloss; tea owns Bubble Tea.
	mod + "/core": {closed: true, allow: []string{mod + "/trie", mod + "/expr", mod + "/layout"}},
	mod + "/ui": {
		allow:      []string{mod + "/core", mod + "/layout", mod + "/buffer", lipgloss},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/tea": {
		allow: []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/buffer", mod + "/recorder", bubbletea},
	},
	// Tier 2: opt-in widgets and config; never imported by Tier 0/1, never import Bubble Tea or Lip Gloss.
	mod + "/widgets/table": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/tree": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/widgets/input"},
		allowTests: []string{mod + "/buffer"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/picker": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/widgets/input"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/pathpicker": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/widgets/picker", mod + "/widgets/input"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/form": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/expr", mod + "/widgets/input"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/config": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/expr", mod + "/layout"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/input": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/buffer"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/bindings": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/expr", mod + "/config", mod + "/widgets/table"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/diff": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/markdown": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/commandline": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/widgets/input"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/palette": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/widgets/picker", mod + "/widgets/form", mod + "/widgets/input"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/toast": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/hints": {
		allow:      []string{mod + "/core", mod + "/ui", mod + "/layout", mod + "/buffer"},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/widgets/modal": {
		allow: []string{
			mod + "/core",
			mod + "/ui",
			mod + "/layout",
			mod + "/widgets/commandline",
			mod + "/widgets/form",
			mod + "/widgets/input",
			mod + "/widgets/markdown",
			mod + "/widgets/palette",
			mod + "/widgets/pathpicker",
			mod + "/widgets/picker",
			mod + "/widgets/toast",
		},
		forbidDeps: []string{"bubbletea", mod + "/tea"},
	},
	mod + "/tutorial": {
		allow:      []string{mod + "/core"},
		forbidDeps: []string{"bubbletea", "lipgloss", mod + "/tea", mod + "/ui"},
	},
	// Tier 3: opt-in CLI bridge connecting oden with Ochazuke; never imported by Tier 0/1/2.
	mod + "/cli": {
		allow: []string{
			oden,
			bubbletea,
			mod + "/core",
			mod + "/ui",
			mod + "/tea",
			mod + "/config",
			mod + "/widgets/picker",
			mod + "/widgets/bindings",
			mod + "/widgets/input",
		},
	},
}

func TestDependencyBoundaries(t *testing.T) {
	pkgs := runGoList(t, "list", "-json", "./...")

	// Standard-library membership, including everything tests pull in.
	std := map[string]bool{}
	for _, p := range runGoList(t, "list", "-deps", "-test", "-json", "./...") {
		if p.Standard {
			std[p.ImportPath] = true
		}
	}

	byPath := map[string]pkgInfo{}
	for _, p := range pkgs {
		if p.Error != nil {
			t.Fatalf("package %s error: %s", p.ImportPath, p.Error.Err)
		}
		if p.Incomplete || len(p.DepsErrors) > 0 {
			t.Fatalf("package %s has incomplete metadata or deps errors: %+v", p.ImportPath, p.DepsErrors)
		}
		byPath[p.ImportPath] = p
	}

	for _, req := range []string{"trie", "expr", "layout", "buffer", "recorder", "core", "ui", "tea"} {
		if _, ok := byPath[mod+"/"+req]; !ok {
			t.Fatalf("required package %q missing from go list ./...", mod+"/"+req)
		}
	}

	for _, p := range pkgs {
		pol, governed := policies[p.ImportPath]
		isExample := strings.HasPrefix(p.ImportPath, mod+"/examples/")
		if !governed && !isExample {
			t.Errorf("unexpected package %q: add a policy to deps_test.go", p.ImportPath)
			continue
		}

		for kind, list := range map[string][]string{"Imports": p.Imports, "Deps": p.Deps, "TestImports": p.TestImports, "XTestImports": p.XTestImports} {
			allowed := pol.allow
			if (kind == "TestImports" || kind == "XTestImports") && len(pol.allowTests) > 0 {
				allowed = slices.Concat(allowed, pol.allowTests)
			}
			for _, imp := range list {
				if imp == "C" {
					t.Errorf("%s %s imports cgo pseudo-package %q", p.ImportPath, kind, imp)
				}
				if isExample || imp == p.ImportPath { // examples are free; xtests import their own package
					continue
				}
				if std[imp] {
					continue
				}
				if kind == "Deps" && !pol.closed {
					for _, f := range pol.forbidDeps {
						if strings.Contains(imp, f) {
							t.Errorf("%s has forbidden transitive dependency %q", p.ImportPath, imp)
						}
					}
				} else if !slices.Contains(allowed, imp) {
					t.Errorf("%s %s %q is outside its allowlist %v", p.ImportPath, kind, imp, allowed)
				}
			}
		}
	}
}
