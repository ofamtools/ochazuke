package ochazuke_deps_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var nonDomainFilenamePattern = regexp.MustCompile(`(?i)(^|_)(wave|sprint|phase|milestone)[a-z0-9]*(_|\.go$)`)

const (
	maxProductionFileLines  = 1000
	maxCyclomaticComplexity = 12

	ruleCoreStructBudget  = "library.core_struct_budget"
	ruleCustomModalChrome = "reuse.custom_modal_chrome"
	ruleHardcodedWidgetID = "identity.hardcoded_widget_id"
	ruleCyclomatic        = "complexity.cyclomatic"
)

// forbiddenLibraryDomainTokens are app-specific domain words that must never
// appear in ochazuke's exported struct field names (outside examples/).
var forbiddenLibraryDomainTokens = []string{
	"pane", "lane", "task", "harness", "zsh", "claude",
	"culprit", "scratch", "daemon", "mtm",
}

var widgetIDNames = map[string]bool{
	"FocusID": true,
	"focusID": true,
	"region":  true,
	"Region":  true,
	"pageID":  true,
	"PageID":  true,
	"modalID": true,
	"ModalID": true,
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

func walkNonTestProdGoFiles(t *testing.T, fn func(relPath string)) {
	t.Helper()
	walkGoFiles(t, func(relPath, base string) {
		if strings.HasSuffix(base, "_test.go") || strings.HasPrefix(relPath, "examples/") {
			return
		}
		fn(relPath)
	})
}

func parseFileWithLines(t *testing.T, fset *token.FileSet, relPath string) (*ast.File, []string) {
	t.Helper()
	src, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	file, err := parser.ParseFile(fset, relPath, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", relPath, err)
	}
	return file, strings.Split(string(src), "\n")
}

func snippetAtLine(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	s := strings.TrimSpace(lines[line-1])
	runes := []rune(s)
	if len(runes) > 120 {
		return string(runes[:117]) + "..."
	}
	return s
}

func formatFuncDeclName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := types.ExprString(fn.Recv.List[0].Type)
	return fmt.Sprintf("(%s).%s", recv, fn.Name.Name)
}

func TestDomainFilenames(t *testing.T) {
	walkGoFiles(t, func(relPath, base string) {
		if nonDomainFilenamePattern.MatchString(base) {
			t.Errorf("%s: filename contains sprint/wave/phase/milestone marker; use a domain-specific name", relPath)
		}
	})
}

func TestNoGodFiles(t *testing.T) {
	seen := map[string]int{}
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
		seen[relPath] = lines
		limit := maxProductionFileLines
		if ceiling, ok := legacyLineCeilings[relPath]; ok {
			limit = ceiling
		}
		if lines > limit {
			t.Errorf("%s has %d lines (limit %d); split by domain responsibility", relPath, lines, limit)
		}
	})
	for _, relPath := range sortedMapKeys(legacyLineCeilings) {
		ceiling := legacyLineCeilings[relPath]
		actual, ok := seen[relPath]
		if !ok || actual <= maxProductionFileLines {
			t.Errorf("stale legacyLineCeilings entry %s (%d <= %d); remove it from hygiene_ceilings_test.go", relPath, actual, maxProductionFileLines)
		} else if actual < ceiling {
			t.Errorf("%s shrank to %d lines (ceiling %d); tighten legacyLineCeilings in hygiene_ceilings_test.go", relPath, actual, ceiling)
		}
	}
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
	walkNonTestProdGoFiles(t, func(relPath string) {
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

func scanCoreStructSurfaceBudget(t *testing.T) ([]hygieneFinding, []string) {
	t.Helper()
	fset := token.NewFileSet()
	var findings []hygieneFinding
	var missing []string
	for _, key := range sortedMapKeys(coreStructFieldCeilings) {
		ceiling := coreStructFieldCeilings[key]
		parts := strings.SplitN(key, ":", 2)
		relPath, typeName := parts[0], parts[1]
		file, lines := parseFileWithLines(t, fset, relPath)
		found := false
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			found = true
			count := 0
			for _, f := range st.Fields.List {
				if len(f.Names) == 0 {
					count++ // embedded field
				} else {
					count += len(f.Names)
				}
			}
			line := fset.Position(ts.Pos()).Line
			severity := "ratcheted"
			if count > ceiling {
				severity = "blocked"
			}
			findings = append(findings, hygieneFinding{
				Rule:     ruleCoreStructBudget,
				Category: "Core Surface Budget",
				File:     relPath,
				Line:     line,
				Func:     typeName,
				Detail:   fmt.Sprintf("struct %s has %d fields (ceiling %d)", typeName, count, ceiling),
				Snippet:  snippetAtLine(lines, line),
				Reusable: "compose smaller value structs or declarative metadata",
				Severity: severity,
				Value:    count,
				Cap:      ceiling,
			})
			return false
		})
		if !found {
			missing = append(missing, key)
		}
	}
	return findings, missing
}

// TestCoreStructSurfaceBudget enforces shrink-only field-count ceilings on core
// shared structs so single-caller options cannot bloat shared library surfaces.
func TestCoreStructSurfaceBudget(t *testing.T) {
	findings, missing := scanCoreStructSurfaceBudget(t)
	for _, key := range missing {
		t.Errorf("core struct %s not found; update coreStructFieldCeilings in hygiene_ceilings_test.go", key)
	}
	for _, f := range findings {
		key := f.File + ":" + f.Func
		if f.Value > f.Cap {
			t.Errorf("%s has %d fields (ceiling %d); consolidate or prove Rule-of-Two before expanding core surface", key, f.Value, f.Cap)
		} else if f.Value < f.Cap {
			t.Errorf("%s shrank to %d fields (ceiling %d); tighten coreStructFieldCeilings in hygiene_ceilings_test.go", key, f.Value, f.Cap)
		}
	}
}

func isKindOverlayExpr(e ast.Expr) bool {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "KindOverlay"
	case *ast.Ident:
		return x.Name == "KindOverlay"
	default:
		return false
	}
}

func isViewNodeExpr(e ast.Expr) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "ViewNode"
	case *ast.Ident:
		return x.Name == "ViewNode"
	default:
		return false
	}
}

func isStringLiteral(e ast.Expr, want string) bool {
	val, ok := nonEmptyStringLit(e)
	return ok && val == want
}

// scanCustomModalChrome finds constructions of ui.KindOverlay outside ui/ and
// widgets/modal/frame.go (plus widgets/modal/modal.go:Overlay, which Frame.Overlay
// delegates to).
func scanCustomModalChrome(t *testing.T) []hygieneFinding {
	t.Helper()
	fset := token.NewFileSet()
	var findings []hygieneFinding
	walkNonTestProdGoFiles(t, func(relPath string) {
		if strings.HasPrefix(relPath, "ui/") || relPath == "widgets/modal/frame.go" {
			return
		}
		file, lines := parseFileWithLines(t, fset, relPath)
		for _, decl := range file.Decls {
			funcName := ""
			if fn, ok := decl.(*ast.FuncDecl); ok {
				funcName = formatFuncDeclName(fn)
			}
			// widgets/modal/modal.go:Overlay is the canonical constructor called by Frame.Overlay.
			if relPath == "widgets/modal/modal.go" && funcName == "Overlay" {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CompositeLit:
					for _, elt := range node.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						keyIdent, ok := kv.Key.(*ast.Ident)
						if !ok || keyIdent.Name != "Kind" {
							continue
						}
						if isKindOverlayExpr(kv.Value) || (isViewNodeExpr(node.Type) && isStringLiteral(kv.Value, "overlay")) {
							line := fset.Position(kv.Pos()).Line
							findings = append(findings, hygieneFinding{
								Rule:     ruleCustomModalChrome,
								Category: "Parallel Reimplementation",
								File:     relPath,
								Line:     line,
								Func:     funcName,
								Detail:   "constructs ui.KindOverlay directly instead of delegating to modal.Frame",
								Snippet:  snippetAtLine(lines, line),
								Reusable: "widgets/modal.Frame (BuildOverlay / FitHeightOverlay)",
							})
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range node.Lhs {
						if i >= len(node.Rhs) {
							continue
						}
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "Kind" {
							continue
						}
						if isKindOverlayExpr(node.Rhs[i]) {
							line := fset.Position(node.Pos()).Line
							findings = append(findings, hygieneFinding{
								Rule:     ruleCustomModalChrome,
								Category: "Parallel Reimplementation",
								File:     relPath,
								Line:     line,
								Func:     funcName,
								Detail:   "assigns ui.KindOverlay directly instead of delegating to modal.Frame",
								Snippet:  snippetAtLine(lines, line),
								Reusable: "widgets/modal.Frame (BuildOverlay / FitHeightOverlay)",
							})
						}
					}
				}
				return true
			})
		}
	})
	annotatePerFileSeverity(findings, legacyCustomModalChromeCeilings)
	return findings
}

// TestModalsUseModalFrame bans constructing ui.KindOverlay outside ui/ and
// widgets/modal/frame.go (and widgets/modal/modal.go:Overlay). Every modal widget
// must delegate framing to modal.Frame (BuildOverlay/FitHeightOverlay).
func TestModalsUseModalFrame(t *testing.T) {
	findings := scanCustomModalChrome(t)
	enforcePerFileRatchet(t, "legacyCustomModalChromeCeilings", findings, legacyCustomModalChromeCeilings)
}

func extractWidgetIDName(e ast.Expr) (string, bool) {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	switch x := e.(type) {
	case *ast.Ident:
		if widgetIDNames[x.Name] {
			return x.Name, true
		}
	case *ast.SelectorExpr:
		if widgetIDNames[x.Sel.Name] {
			return x.Sel.Name, true
		}
	case *ast.CallExpr:
		if len(x.Args) == 0 {
			return extractWidgetIDName(x.Fun)
		}
	}
	return "", false
}

func nonEmptyStringLit(e ast.Expr) (string, bool) {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil || val == "" {
		return "", false
	}
	return val, true
}

// scanHardcodedWidgetIDs detects identity comparisons (==, !=, switch case,
// strings.HasPrefix, strings.HasSuffix) against non-empty string literals on
// widget/focus/region/page/modal ID identifiers or selectors.
func scanHardcodedWidgetIDs(t *testing.T) []hygieneFinding {
	t.Helper()
	fset := token.NewFileSet()
	var findings []hygieneFinding
	walkNonTestProdGoFiles(t, func(relPath string) {
		file, lines := parseFileWithLines(t, fset, relPath)
		for _, decl := range file.Decls {
			funcName := ""
			if fn, ok := decl.(*ast.FuncDecl); ok {
				funcName = formatFuncDeclName(fn)
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BinaryExpr:
					if node.Op != token.EQL && node.Op != token.NEQ {
						return true
					}
					if name, ok := extractWidgetIDName(node.X); ok {
						if lit, ok := nonEmptyStringLit(node.Y); ok {
							line := fset.Position(node.Pos()).Line
							findings = append(findings, hygieneFinding{
								Rule:     ruleHardcodedWidgetID,
								Category: "Identity Dispatch",
								File:     relPath,
								Line:     line,
								Func:     funcName,
								Detail:   fmt.Sprintf("compares %s %s %q", name, node.Op, lit),
								Snippet:  snippetAtLine(lines, line),
								Reusable: "ViewNode.Scope / ViewNode.Facts + core.Keymap when-guards",
							})
						}
					} else if name, ok := extractWidgetIDName(node.Y); ok {
						if lit, ok := nonEmptyStringLit(node.X); ok {
							line := fset.Position(node.Pos()).Line
							findings = append(findings, hygieneFinding{
								Rule:     ruleHardcodedWidgetID,
								Category: "Identity Dispatch",
								File:     relPath,
								Line:     line,
								Func:     funcName,
								Detail:   fmt.Sprintf("compares %q %s %s", lit, node.Op, name),
								Snippet:  snippetAtLine(lines, line),
								Reusable: "ViewNode.Scope / ViewNode.Facts + core.Keymap when-guards",
							})
						}
					}
				case *ast.SwitchStmt:
					if node.Tag == nil {
						return true
					}
					name, ok := extractWidgetIDName(node.Tag)
					if !ok || node.Body == nil {
						return true
					}
					for _, stmt := range node.Body.List {
						cc, ok := stmt.(*ast.CaseClause)
						if !ok {
							continue
						}
						for _, expr := range cc.List {
							if lit, ok := nonEmptyStringLit(expr); ok {
								line := fset.Position(expr.Pos()).Line
								findings = append(findings, hygieneFinding{
									Rule:     ruleHardcodedWidgetID,
									Category: "Identity Dispatch",
									File:     relPath,
									Line:     line,
									Func:     funcName,
									Detail:   fmt.Sprintf("switches on %s case %q", name, lit),
									Snippet:  snippetAtLine(lines, line),
									Reusable: "ViewNode.Scope / ViewNode.Facts + core.Keymap when-guards",
								})
							}
						}
					}
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok || len(node.Args) != 2 {
						return true
					}
					if sel.Sel.Name != "HasPrefix" && sel.Sel.Name != "HasSuffix" {
						return true
					}
					pkgIdent, ok := sel.X.(*ast.Ident)
					if !ok || pkgIdent.Name != "strings" {
						return true
					}
					if name, ok := extractWidgetIDName(node.Args[0]); ok {
						if lit, ok := nonEmptyStringLit(node.Args[1]); ok {
							line := fset.Position(node.Pos()).Line
							findings = append(findings, hygieneFinding{
								Rule:     ruleHardcodedWidgetID,
								Category: "Identity Dispatch",
								File:     relPath,
								Line:     line,
								Func:     funcName,
								Detail:   fmt.Sprintf("calls strings.%s(%s, %q)", sel.Sel.Name, name, lit),
								Snippet:  snippetAtLine(lines, line),
								Reusable: "ViewNode.Scope / ViewNode.Facts + core.Keymap when-guards",
							})
						}
					}
				}
				return true
			})
		}
	})
	annotatePerFileSeverity(findings, legacyHardcodedWidgetIDCeilings)
	return findings
}

// TestNoHardcodedWidgetIDs blocks hardcoded string identity dispatch on widget/focus/
// region/page/modal IDs, with a two-way shrink-only per-file ratchet for legacy code.
func TestNoHardcodedWidgetIDs(t *testing.T) {
	findings := scanHardcodedWidgetIDs(t)
	enforcePerFileRatchet(t, "legacyHardcodedWidgetIDCeilings", findings, legacyHardcodedWidgetIDCeilings)
}

func cyclomaticComplexity(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 1
	}
	complexity := 1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if len(x.List) > 0 {
				complexity++
			}
		case *ast.CommClause:
			if x.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}

// scanCyclomaticComplexity returns findings for every non-test FuncDecl
// (excluding examples/) whose McCabe cyclomatic complexity exceeds maxCyclomaticComplexity.
func scanCyclomaticComplexity(t *testing.T) []hygieneFinding {
	t.Helper()
	fset := token.NewFileSet()
	var findings []hygieneFinding
	walkNonTestProdGoFiles(t, func(relPath string) {
		file, lines := parseFileWithLines(t, fset, relPath)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			c := cyclomaticComplexity(fn)
			if c <= maxCyclomaticComplexity {
				continue
			}
			funcName := formatFuncDeclName(fn)
			key := relPath + ":" + funcName
			line := fset.Position(fn.Pos()).Line
			severity := "blocked"
			if ceiling, ok := legacyCyclomaticCeilings[key]; ok && c <= ceiling {
				severity = "ratcheted"
			}
			findings = append(findings, hygieneFinding{
				Rule:     ruleCyclomatic,
				Category: "Cyclomatic Complexity",
				File:     relPath,
				Line:     line,
				Func:     funcName,
				Detail:   fmt.Sprintf("cyclomatic complexity %d exceeds cap %d", c, maxCyclomaticComplexity),
				Snippet:  snippetAtLine(lines, line),
				Reusable: "extract pure helpers / table-driven dispatch",
				Severity: severity,
				Value:    c,
				Cap:      maxCyclomaticComplexity,
			})
		}
	})
	return findings
}

// TestMaxCyclomaticComplexity enforces a strict McCabe complexity cap of <= 12 on
// all new non-test functions, with a two-way shrink-only ratchet for legacy functions.
func TestMaxCyclomaticComplexity(t *testing.T) {
	findings := scanCyclomaticComplexity(t)
	byKey := make(map[string]hygieneFinding, len(findings))
	for _, f := range findings {
		key := f.File + ":" + f.Func
		byKey[key] = f
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f := byKey[key]
		ceiling, ratcheted := legacyCyclomaticCeilings[key]
		if !ratcheted {
			t.Errorf("%s (line %d): cyclomatic complexity %d exceeds cap %d; decompose into smaller pure helpers", key, f.Line, f.Value, maxCyclomaticComplexity)
			continue
		}
		if f.Value > ceiling {
			t.Errorf("%s (line %d): cyclomatic complexity %d exceeds legacy ceiling %d; do not increase complexity", key, f.Line, f.Value, ceiling)
		} else if f.Value < ceiling {
			t.Errorf("%s (line %d): cyclomatic complexity shrank to %d (ceiling %d); tighten legacyCyclomaticCeilings in hygiene_ceilings_test.go", key, f.Line, f.Value, ceiling)
		}
	}
	for _, key := range sortedMapKeys(legacyCyclomaticCeilings) {
		if _, ok := byKey[key]; !ok {
			t.Errorf("stale legacyCyclomaticCeilings entry %s (deleted or now <= %d); remove it from hygiene_ceilings_test.go", key, maxCyclomaticComplexity)
		}
	}
}

func annotatePerFileSeverity(findings []hygieneFinding, ceilings map[string]int) {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.File]++
	}
	for i := range findings {
		file := findings[i].File
		if ceiling, ok := ceilings[file]; ok && counts[file] <= ceiling {
			findings[i].Severity = "ratcheted"
		} else {
			findings[i].Severity = "blocked"
		}
	}
}

func enforcePerFileRatchet(t *testing.T, mapName string, findings []hygieneFinding, ceilings map[string]int) {
	t.Helper()
	byFile := map[string][]hygieneFinding{}
	for _, f := range findings {
		byFile[f.File] = append(byFile[f.File], f)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		list := byFile[file]
		actual := len(list)
		ceiling, ratcheted := ceilings[file]
		if !ratcheted {
			for _, f := range list {
				t.Errorf("%s:%d (%s): %s; use %s", f.File, f.Line, f.Func, f.Detail, f.Reusable)
			}
			continue
		}
		if actual > ceiling {
			t.Errorf("%s has %d findings for %s (ceiling %d); do not add new violations", file, actual, list[0].Rule, ceiling)
			for _, f := range list {
				t.Errorf("  %s:%d (%s): %s", f.File, f.Line, f.Func, f.Detail)
			}
		} else if actual < ceiling {
			t.Errorf("%s shrank to %d findings (ceiling %d); tighten %s in hygiene_ceilings_test.go", file, actual, ceiling, mapName)
		}
	}
	for _, file := range sortedMapKeys(ceilings) {
		if len(byFile[file]) == 0 {
			t.Errorf("stale %s entry %s (now 0 findings); remove it from hygiene_ceilings_test.go", mapName, file)
		}
	}
}

func sortedMapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
