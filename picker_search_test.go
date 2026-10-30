package ochazuke

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func pickerGitEnvironment(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_CONFIG_COUNT"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func pickerGit(t *testing.T, root string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func pickerFile(t *testing.T, root, path, text string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return full
}

func pickerEntryPaths(entries []PickerEntry) []string {
	paths := make([]string, len(entries))
	for i, entry := range entries {
		paths[i] = entry.Path
	}
	return paths
}

func TestPickerRecursiveIgnoreRulesInsideAndOutsideGit(t *testing.T) {
	pickerGitEnvironment(t)
	for _, repository := range []bool{false, true} {
		t.Run(fmt.Sprintf("repository=%v", repository), func(t *testing.T) {
			root := t.TempDir()
			if repository {
				pickerGit(t, root, "init", "--quiet", "--template=")
			}
			pickerFile(t, root, ".gitignore", "/build/\n*.tmp\n!keep.tmp\n/root-only.txt\n")
			pickerFile(t, root, "build/hidden.go", "hidden")
			pickerFile(t, root, "drop.tmp", "hidden")
			pickerFile(t, root, "keep.tmp", "visible")
			pickerFile(t, root, "root-only.txt", "hidden")
			pickerFile(t, root, "nested/.gitignore", "*.log\n!important.log\n/cache/\n")
			pickerFile(t, root, "nested/drop.log", "hidden")
			pickerFile(t, root, "nested/important.log", "visible")
			pickerFile(t, root, "nested/cache/hidden.go", "hidden")
			pickerFile(t, root, "nested/root-only.txt", "visible")
			pickerFile(t, root, "nested/deeper/cache/visible.go", "visible")
			pickerFile(t, root, "with spaces/target file.go", "visible")
			pickerFile(t, root, ".git/private", "never indexed")
			if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
				t.Fatal(err)
			}
			entries, err := indexPicker(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{".gitignore", "keep.tmp", "loop", "nested", "nested/.gitignore", "nested/deeper", "nested/deeper/cache", "nested/deeper/cache/visible.go", "nested/important.log", "nested/root-only.txt", "with spaces", "with spaces/target file.go"}
			if got := pickerEntryPaths(entries); !reflect.DeepEqual(got, want) {
				t.Fatalf("indexed paths:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestPickerRepositoryAndGlobalExcludes(t *testing.T) {
	pickerGitEnvironment(t)
	global := pickerFile(t, t.TempDir(), "global-ignore", "*.global\n!keep.global\n")
	pickerGit(t, t.TempDir(), "config", "--global", "core.excludesFile", global)
	for _, repository := range []bool{false, true} {
		root := t.TempDir()
		if repository {
			pickerGit(t, root, "init", "--quiet", "--template=")
			pickerFile(t, root, ".git/info/exclude", "local-only\n")
		}
		pickerFile(t, root, "hide.global", "")
		pickerFile(t, root, "keep.global", "")
		pickerFile(t, root, "local-only", "")
		pickerFile(t, root, "visible", "")
		entries, err := indexPicker(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"keep.global", "local-only", "visible"}
		if repository {
			want = []string{"keep.global", "visible"}
		}
		if got := pickerEntryPaths(entries); !reflect.DeepEqual(got, want) {
			t.Fatalf("repo=%v got %q want %q", repository, got, want)
		}
	}
}

func TestPickerFuzzyRelativeFullPathsAndFilters(t *testing.T) {
	pickerGitEnvironment(t)
	root := t.TempDir()
	pickerFile(t, root, "src/main.go", "")
	pickerFile(t, root, "src/maps.go", "")
	pickerFile(t, root, "source/my app.go", "")
	pickerFile(t, root, "unrelated/file.txt", "")
	entries, err := indexPicker(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	results, err := searchPicker(context.Background(), entries, "s/mg", "file", SearchCaseIgnore)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pickerEntryPaths(results), []string{"src/main.go", "src/maps.go", "source/my app.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("relative fuzzy ranking got %q want %q", got, want)
	}
	results, err = searchPicker(context.Background(), entries, "SRC", "dir", SearchCaseIgnore)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pickerEntryPaths(results), []string{"src", "source"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("directory filter got %q want %q", got, want)
	}
	results, err = searchPicker(context.Background(), entries, "my ap", "any", SearchCaseIgnore)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pickerEntryPaths(results), []string{"source/my app.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("spaces got %q want %q", got, want)
	}
}

func TestPickerSearchKeepsEveryMatchAndStableTies(t *testing.T) {
	entries := make([]PickerEntry, 1201)
	for i := range entries {
		entries[i] = PickerEntry{Path: fmt.Sprintf("file-%04d", len(entries)-i)}
	}
	results, err := searchPicker(context.Background(), entries, "file", "file", SearchCaseIgnore)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]string, 1201)
	for i := range want {
		want[i] = fmt.Sprintf("file-%04d", i+1)
	}
	if got := pickerEntryPaths(results); !slices.Equal(got, want) {
		t.Fatalf("full stable match set got %d results, first=%q last=%q", len(got), got[0], got[len(got)-1])
	}
}

func TestPickerIndexCancellationAndMissingRoot(t *testing.T) {
	pickerGitEnvironment(t)
	root := t.TempDir()
	pickerFile(t, root, "visible", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if entries, err := indexPicker(ctx, root); err == nil || len(entries) != 0 {
		t.Fatalf("canceled index returned %v, %v", entries, err)
	}
	if entries, err := indexPicker(context.Background(), filepath.Join(root, "missing")); err == nil || len(entries) != 0 {
		t.Fatalf("missing root returned %v, %v", entries, err)
	}
	entries, err := indexPicker(context.Background(), root)
	if err != nil || !slices.Equal(pickerEntryPaths(entries), []string{"visible"}) {
		t.Fatalf("valid index: %v, %v", entries, err)
	}
	results, err := searchPicker(ctx, entries, "vis", "file", SearchCaseIgnore)
	if err == nil || len(results) != 0 {
		t.Fatalf("canceled search returned %v, %v", results, err)
	}
}

func TestPickerRootResolutionPreservesSpacesAndFuzzySuffix(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "with spaces"), 0700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	gotRoot, query, err := pickerLocation(root, "with spaces/deep/fz")
	if err != nil || gotRoot != filepath.Join(canonical, "with spaces") || query != filepath.Join("deep", "fz") {
		t.Fatalf("location got %q %q %v", gotRoot, query, err)
	}
	pickerFile(t, root, "plain-file", "")
	gotRoot, query, err = pickerLocation(root, "plain-file/query")
	if err != nil || gotRoot != canonical || !strings.HasSuffix(query, "plain-file/query") {
		t.Fatalf("file-prefix location got %q %q %v", gotRoot, query, err)
	}
}

func TestPickerUnreadableSubdirectoryFailsInsteadOfReturningPartialIndex(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode-000 directories")
	}
	pickerGitEnvironment(t)
	root := t.TempDir()
	pickerFile(t, root, "visible", "")
	pickerFile(t, root, "private/secret", "")
	private := filepath.Join(root, "private")
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(private, 0700) })
	if entries, err := indexPicker(context.Background(), root); err == nil || len(entries) != 0 || !strings.Contains(err.Error(), "private") {
		t.Fatalf("unreadable subtree returned partial index %v, error %v", entries, err)
	}
}

func TestPickerShallowIndexUsesNativeIgnoreRulesWithoutDescending(t *testing.T) {
	pickerGitEnvironment(t)
	global := pickerFile(t, t.TempDir(), "global-ignore", "*.global\n")
	pickerGit(t, t.TempDir(), "config", "--global", "core.excludesFile", global)
	for _, repository := range []bool{false, true} {
		t.Run(fmt.Sprintf("repository=%v", repository), func(t *testing.T) {
			root := t.TempDir()
			if repository {
				pickerGit(t, root, "init", "--quiet", "--template=")
				pickerFile(t, root, ".git/info/exclude", "local-only\n")
			}
			pickerFile(t, root, ".gitignore", "/build/\n*.tmp\n!keep.tmp\n")
			pickerFile(t, root, ".git/private", "")
			pickerFile(t, root, "build/hidden", "")
			pickerFile(t, root, "drop.tmp", "")
			pickerFile(t, root, "hide.global", "")
			pickerFile(t, root, "keep.tmp", "")
			pickerFile(t, root, "local-only", "")
			pickerFile(t, root, "nested/deep/file", "")
			if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
				t.Fatal(err)
			}
			entries, err := indexPickerEntries(context.Background(), root, false)
			if err != nil {
				t.Fatal(err)
			}
			want := []PickerEntry{{Path: ".gitignore"}, {Path: "keep.tmp"}, {Path: "local-only"}, {Path: "loop"}, {Path: "nested", Dir: true}}
			if repository {
				want = []PickerEntry{{Path: ".gitignore"}, {Path: "keep.tmp"}, {Path: "loop"}, {Path: "nested", Dir: true}}
			}
			if !reflect.DeepEqual(entries, want) {
				t.Fatalf("shallow entries = %#v, want %#v", entries, want)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if entries, err := indexPickerEntries(ctx, root, false); err == nil || len(entries) != 0 {
				t.Fatalf("canceled shallow scan = %v, %v", entries, err)
			}
		})
	}
}

func TestPickerHomeRootExpansionLeavesOtherShellSyntaxLiteral(t *testing.T) {
	home, base := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	pickerFile(t, home, "proj/target", "")
	pickerFile(t, base, "~someone/file", "")
	pickerFile(t, base, "$HOME/file", "")
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	canonicalBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		value, root, query string
	}{
		{"~", canonicalHome, ""},
		{"~/", canonicalHome, ""},
		{"~/proj", filepath.Join(canonicalHome, "proj"), ""},
		{"~/proj/tgt", filepath.Join(canonicalHome, "proj"), "tgt"},
		{"~someone", filepath.Join(canonicalBase, "~someone"), ""},
		{"$HOME", filepath.Join(canonicalBase, "$HOME"), ""},
	} {
		t.Run(tc.value, func(t *testing.T) {
			root, query, err := pickerLocation(base, tc.value)
			if err != nil || root != tc.root || query != tc.query {
				t.Fatalf("location(%q) = %q, %q, %v; want %q, %q", tc.value, root, query, err, tc.root, tc.query)
			}
		})
	}
	t.Setenv("HOME", "")
	if _, _, err := pickerLocation(base, "~/proj"); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Fatalf("missing HOME silently resolved: %v", err)
	}
}
