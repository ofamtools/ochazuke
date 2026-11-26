package ochazuke_deps_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var nonDomainFilenamePattern = regexp.MustCompile(`(?i)(^|_)(wave|sprint|phase|milestone)[a-z0-9]*(_|\.go$)`)

const maxProductionFileLines = 1000

// legacyLineCeilings freezes existing production files above maxProductionFileLines
// so they cannot grow and no new god-files enter. (ui/draw_content.go is now <1000.)
var legacyLineCeilings = map[string]int{}

// forbiddenLibraryDomainTokens are app-specific domain words that must never
// appear in ochazuke's exported struct field names (outside examples/).
var forbiddenLibraryDomainTokens = []string{
	"pane", "lane", "task", "harness", "zsh", "claude",
	"culprit", "scratch", "daemon", "mtm",
}

// coreStructFieldCeilings ratchets the flattened exported+unexported field count
// of ochazuke's core shared structs so "special cases in a trenchcoat" cannot
// creep in silently. Shrink these numbers whenever fields are consolidated.
var coreStructFieldCeilings = map[string]int{
	"ui/viewnode.go:ViewNode":               23,
	"ui/theme.go:Theme":                     14,
	"core/command.go:ParamSpec":             14,
	"widgets/modal/form.go:FormOptions":     24,
	"widgets/modal/picker.go:PickerOptions": 20,
}

func walkGoFiles(t *testing.T, fn func(relPath, base string)) {
	t.Helper()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == ".worktrees" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		fn(filepath.ToSlash(path), name)
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go files: %v", err)
	}
}

func TestDomainFilenames(t *testing.T) {
	walkGoFiles(t, func(relPath, base string) {
		if nonDomainFilenamePattern.MatchString(base) {
			t.Errorf("%s: filename contains sprint/wave/phase/milestone marker; use a domain-specific name", relPath)
		}
	})
}

func TestNoGodFiles(t *testing.T) {
	walkGoFiles(t, func(relPath, base string) {
		if strings.HasSuffix(base, "_test.go") {
			return
		}
		data, err := os.ReadFile(relPath)
		if err != nil {
			t.Fatalf("read %s: %v", relPath, err)
		}
		lines := bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		limit := maxProductionFileLines
		if ceiling, ok := legacyLineCeilings[relPath]; ok {
			limit = ceiling
		}
		if lines > limit {
			t.Errorf("%s has %d lines (limit %d); split by domain responsibility", relPath, lines, limit)
		}
	})
}

func splitCamelWords(s string) []string {
	var words []string
	var cur strings.Builder
	for i, r := range s {
		if r == '_' || r == '-' {
			if cur.Len() > 0 {
				words = append(words, strings.ToLower(cur.String()))
				cur.Reset()
			}
			continue
		}
		if i > 0 && r >= 'A' && r <= 'Z' && cur.Len() > 0 {
			prev := s[i-1]
			if prev >= 'a' && prev <= 'z' {
				words = append(words, strings.ToLower(cur.String()))
				cur.Reset()
			}
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		words = append(words, strings.ToLower(cur.String()))
	}
	return words
}

// TestLibraryDomainPurity guarantees ochazuke library packages stay domain-agnostic
// by forbidding app-specific vocabulary in exported struct fields (excluding examples/).
func TestLibraryDomainPurity(t *testing.T) {
	forbidden := make(map[string]bool, len(forbiddenLibraryDomainTokens))
	for _, tok := range forbiddenLibraryDomainTokens {
		forbidden[tok] = true
	}
	fset := token.NewFileSet()
	walkGoFiles(t, func(relPath, base string) {
		if strings.HasSuffix(base, "_test.go") || strings.HasPrefix(relPath, "examples/") {
			return
		}
		file, err := parser.ParseFile(fset, relPath, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", relPath, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, f := range st.Fields.List {
				for _, name := range f.Names {
					if !name.IsExported() {
						continue
					}
					for _, w := range splitCamelWords(name.Name) {
						if forbidden[w] {
							t.Errorf("%s: exported field %s.%s contains app-domain token %q; keep ochazuke domain-agnostic", relPath, ts.Name.Name, name.Name, w)
						}
					}
				}
			}
			return true
		})
	})
}

// TestCoreStructSurfaceBudget enforces shrink-only field-count ceilings on core
// shared structs so single-caller options cannot bloat shared library surfaces.
func TestCoreStructSurfaceBudget(t *testing.T) {
	fset := token.NewFileSet()
	found := map[string]bool{}
	for key, ceiling := range coreStructFieldCeilings {
		parts := strings.SplitN(key, ":", 2)
		relPath, typeName := parts[0], parts[1]
		file, err := parser.ParseFile(fset, relPath, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", relPath, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			found[key] = true
			count := 0
			for _, f := range st.Fields.List {
				if len(f.Names) == 0 {
					count++ // embedded field
				} else {
					count += len(f.Names)
				}
			}
			if count > ceiling {
				t.Errorf("%s has %d fields (ceiling %d); consolidate or prove Rule-of-Two before expanding core surface", key, count, ceiling)
			}
			return false
		})
		if !found[key] {
			t.Errorf("core struct %s not found; update coreStructFieldCeilings", key)
		}
	}
}
