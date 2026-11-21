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

func isForbiddenImport(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "bubbletea") ||
		strings.Contains(lower, "lipgloss") ||
		strings.Contains(lower, "charm.land/") ||
		strings.Contains(lower, "charmbracelet/")
}

func TestTier0DependencyBoundaries(t *testing.T) {
	pkgs := runGoList(t, "list", "-json", "./...")

	byPath := make(map[string]pkgInfo, len(pkgs))
	uniqueDeps := make(map[string]struct{})

	for _, p := range pkgs {
		if p.Error != nil {
			t.Fatalf("package %s error: %s", p.ImportPath, p.Error.Err)
		}
		if p.Incomplete || len(p.DepsErrors) > 0 {
			t.Fatalf("package %s has incomplete metadata or deps errors: %+v", p.ImportPath, p.DepsErrors)
		}
		byPath[p.ImportPath] = p

		collect := func(list []string) {
			for _, imp := range list {
				if imp == "C" {
					t.Fatalf("package %s imports forbidden cgo pseudo-package %q", p.ImportPath, imp)
				}
				if isForbiddenImport(imp) {
					t.Fatalf("package %s has forbidden import %q", p.ImportPath, imp)
				}
				uniqueDeps[imp] = struct{}{}
			}
		}
		collect(p.Imports)
		collect(p.TestImports)
		collect(p.XTestImports)
		collect(p.Deps)
	}

	requiredTier0 := []string{
		"ofam.dev/ochazuke/trie",
		"ofam.dev/ochazuke/expr",
		"ofam.dev/ochazuke/layout",
		"ofam.dev/ochazuke/buffer",
	}
	for _, req := range requiredTier0 {
		if _, ok := byPath[req]; !ok {
			t.Fatalf("required Tier 0 package %q missing from go list ./...", req)
		}
	}

	depPaths := make([]string, 0, len(uniqueDeps))
	for dep := range uniqueDeps {
		depPaths = append(depPaths, dep)
	}
	slices.Sort(depPaths)

	meta := make(map[string]pkgInfo, len(depPaths))
	if len(depPaths) > 0 {
		args := append([]string{"list", "-deps", "-json", "--"}, depPaths...)
		resolved := runGoList(t, args...)
		for _, r := range resolved {
			if r.Error != nil {
				t.Fatalf("dependency %s error: %s", r.ImportPath, r.Error.Err)
			}
			if r.Incomplete || len(r.DepsErrors) > 0 {
				t.Fatalf("dependency %s has incomplete metadata or deps errors: %+v", r.ImportPath, r.DepsErrors)
			}
			if isForbiddenImport(r.ImportPath) {
				t.Fatalf("forbidden transitive dependency %q", r.ImportPath)
			}
			for _, d := range r.Deps {
				if d == "C" || isForbiddenImport(d) {
					t.Fatalf("dependency %s has forbidden transitive dep %q", r.ImportPath, d)
				}
			}
			meta[r.ImportPath] = r
		}
	}

	assertStdlibOnly := func(pkg pkgInfo) {
		checkSlice := func(kind string, list []string, allowSelf bool) {
			for _, imp := range list {
				if allowSelf && imp == pkg.ImportPath {
					continue
				}
				m, ok := meta[imp]
				if !ok {
					t.Fatalf("%s %s %q missing from resolved metadata", pkg.ImportPath, kind, imp)
				}
				if !m.Standard {
					t.Errorf("%s %s %q is non-stdlib (Standard=false)", pkg.ImportPath, kind, imp)
				}
				if kind == "TestImports" || kind == "XTestImports" {
					for _, td := range m.Deps {
						if tm, ok := meta[td]; !ok || !tm.Standard {
							t.Errorf("%s %s %q has non-stdlib transitive dep %q", pkg.ImportPath, kind, imp, td)
						}
					}
				}
			}
		}
		checkSlice("Imports", pkg.Imports, false)
		checkSlice("Deps", pkg.Deps, false)
		checkSlice("TestImports", pkg.TestImports, false)
		checkSlice("XTestImports", pkg.XTestImports, true)
	}

	allowedBufferDep := func(path string) bool {
		if path == "ofam.dev/ochazuke/layout" || path == "github.com/rivo/uniseg" {
			return true
		}
		m, ok := meta[path]
		return ok && m.Standard
	}

	assertBufferDeps := func(pkg pkgInfo) {
		checkSlice := func(kind string, list []string, allowSelf bool) {
			for _, imp := range list {
				if allowSelf && imp == pkg.ImportPath {
					continue
				}
				if !allowedBufferDep(imp) {
					t.Errorf("%s %s %q violates Tier 0 buffer allowlist", pkg.ImportPath, kind, imp)
				}
				if kind == "TestImports" || kind == "XTestImports" {
					m, ok := meta[imp]
					if !ok {
						t.Fatalf("%s %s %q missing from resolved metadata", pkg.ImportPath, kind, imp)
					}
					for _, td := range m.Deps {
						if !allowedBufferDep(td) {
							t.Errorf("%s %s %q has disallowed transitive dep %q", pkg.ImportPath, kind, imp, td)
						}
					}
				}
			}
		}
		checkSlice("Imports", pkg.Imports, false)
		checkSlice("Deps", pkg.Deps, false)
		checkSlice("TestImports", pkg.TestImports, false)
		checkSlice("XTestImports", pkg.XTestImports, true)
	}

	for _, pkg := range pkgs {
		switch pkg.ImportPath {
		case "ofam.dev/ochazuke/trie",
			"ofam.dev/ochazuke/expr",
			"ofam.dev/ochazuke/layout",
			"ofam.dev/ochazuke":
			assertStdlibOnly(pkg)
		case "ofam.dev/ochazuke/buffer":
			assertBufferDeps(pkg)
		default:
			t.Errorf("unexpected package %q in Tier 0 module", pkg.ImportPath)
		}
	}
}
