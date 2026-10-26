package ochazuke

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// PickerEntry is relative to the indexed root. Symlinks are selectable files,
// never traversal edges. Size and Modified come from the walk's lstat (a
// symlink's own), zero when it failed; the results table shows them.
type PickerEntry struct {
	Path     string    `json:"path"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size,omitempty"`
	Modified time.Time `json:"modified,omitzero"`
}

// One native Git process streams exact ignore decisions for the entire walk.
// A temporary metadata directory gives non-repositories the same parser without
// modifying the directory being browsed.
type pickerIgnore struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
	stderr bytes.Buffer
	temp   string
}

func startPickerIgnore(ctx context.Context, root string) (*pickerIgnore, error) {
	probe := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	probe.Env = append(os.Environ(), "LC_ALL=C")
	var diagnostic bytes.Buffer
	probe.Stderr = &diagnostic
	_, err := probe.Output()
	args := []string{"-C", root}
	checker := &pickerIgnore{}
	isolated := err != nil
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || !strings.Contains(diagnostic.String(), "not a git repository") {
			return nil, fmt.Errorf("locate Git ignore rules: %w: %s", err, strings.TrimSpace(diagnostic.String()))
		}
	} else {
		probe = exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "--no-index", "--quiet", ".")
		probe.Env = append(os.Environ(), "LC_ALL=C")
		data, ignoredErr := probe.CombinedOutput()
		if ignoredErr == nil {
			// Explicitly opening an ignored directory makes it the browsing boundary.
			isolated = true
		} else {
			var exit *exec.ExitError
			if !errors.As(ignoredErr, &exit) || exit.ExitCode() != 1 {
				return nil, fmt.Errorf("check search root ignore rule: %w: %s", ignoredErr, strings.TrimSpace(string(data)))
			}
		}
	}
	if isolated {
		checker.temp, err = os.MkdirTemp("", "ochazuke-ignore-*")
		if err != nil {
			return nil, err
		}
		init := exec.CommandContext(ctx, "git", "init", "--bare", "--quiet", "--template=", checker.temp)
		init.Env = append(os.Environ(), "LC_ALL=C")
		if data, initErr := init.CombinedOutput(); initErr != nil {
			os.RemoveAll(checker.temp)
			return nil, fmt.Errorf("initialize Git ignore parser: %w: %s", initErr, data)
		}
		args = append(args, "--git-dir="+checker.temp, "--work-tree="+root, "-c", "core.bare=false")
	}
	args = append(args, "check-ignore", "--no-index", "--stdin", "-z", "--verbose", "--non-matching")
	checker.cmd = exec.CommandContext(ctx, "git", args...)
	checker.cmd.Env = append(os.Environ(), "GIT_FLUSH=1", "LC_ALL=C")
	checker.cmd.Stderr = &checker.stderr
	checker.input, err = checker.cmd.StdinPipe()
	if err == nil {
		var stdout io.ReadCloser
		stdout, err = checker.cmd.StdoutPipe()
		if err == nil {
			checker.output = bufio.NewReader(stdout)
			err = checker.cmd.Start()
		}
	}
	if err != nil {
		if checker.input != nil {
			checker.input.Close()
		}
		if checker.temp != "" {
			os.RemoveAll(checker.temp)
		}
		return nil, fmt.Errorf("start Git ignore parser: %w", err)
	}
	return checker, nil
}

func (g *pickerIgnore) ignored(path string) (bool, error) {
	// Write one record, then read its response; neither pipe can fill while the
	// other side waits. NUL framing preserves spaces, tabs and newlines.
	if _, err := io.WriteString(g.input, path+"\x00"); err != nil {
		return false, err
	}
	var pattern string
	for field := range 4 {
		value, err := g.output.ReadString(0)
		if err != nil {
			return false, fmt.Errorf("read Git ignore decision for %q: %w", path, err)
		}
		if field == 2 {
			pattern = strings.TrimSuffix(value, "\x00")
		}
	}
	return pattern != "" && !strings.HasPrefix(pattern, "!"), nil
}

func (g *pickerIgnore) close() error {
	g.input.Close()
	err := g.cmd.Wait()
	if g.temp != "" {
		defer os.RemoveAll(g.temp)
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && g.stderr.Len() == 0 {
			return nil // check-ignore returns 1 when nothing matched.
		}
		return fmt.Errorf("Git ignore parser: %w: %s", err, strings.TrimSpace(g.stderr.String()))
	}
	if g.stderr.Len() != 0 {
		return fmt.Errorf("Git ignore parser: %s", strings.TrimSpace(g.stderr.String()))
	}
	return nil
}

func indexPicker(ctx context.Context, root string) ([]PickerEntry, error) {
	return indexPickerEntries(ctx, root, true)
}

// Both stages use native Git's ignore decisions. The shallow stage skips
// traversal after recording each immediate child, including directories.
func indexPickerEntries(ctx context.Context, root string, recursive bool) ([]PickerEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("open search root: %w", err)
	}
	root = canonical
	ignore, err := startPickerIgnore(ctx, root)
	if err != nil {
		return nil, err
	}
	var entries []PickerEntry
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("index %q: %w", path, walkErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		ignored, err := ignore.ignored(path)
		if err != nil {
			return err
		}
		if ignored {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item := PickerEntry{Path: filepath.ToSlash(relative), Dir: entry.IsDir()}
		if info, err := entry.Info(); err == nil {
			item.Size, item.Modified = info.Size(), info.ModTime()
		}
		entries = append(entries, item)
		if !recursive && entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	closeErr := ignore.close()
	if err != nil {
		return nil, errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return entries, nil
}

// Fuzzy subsequences rank by compactness, path/word boundaries, then lexical
// path. Every match remains available; only the rendered viewport is bounded.
func searchPicker(ctx context.Context, entries []PickerEntry, query, filter string) ([]PickerEntry, error) {
	type match struct {
		entry PickerEntry
		score int
	}
	query = strings.ToLower(filepath.ToSlash(query))
	needle := []rune(query)
	matches := make([]match, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if filter == "dir" && !entry.Dir || filter == "file" && entry.Dir {
			continue
		}
		score, ok := pickerFuzzyScore(entry.Path, needle)
		if ok {
			matches = append(matches, match{entry, score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if len(needle) == 0 && matches[i].entry.Dir != matches[j].entry.Dir {
			return matches[i].entry.Dir
		}
		return matches[i].entry.Path < matches[j].entry.Path
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]PickerEntry, len(matches))
	for i, match := range matches {
		result[i] = match.entry
	}
	return result, nil
}

func pickerFuzzyScore(path string, needle []rune) (int, bool) {
	if len(needle) == 0 {
		return 0, true
	}
	matched, position, previous, score := 0, 0, -2, 0
	var before rune
	for _, original := range path {
		ch := unicode.ToLower(original)
		score--
		if matched < len(needle) && ch == needle[matched] {
			if position == previous+1 {
				score += 12
			}
			if position == 0 || before == '/' || before == '-' || before == '_' || unicode.IsSpace(before) {
				score += 16
			}
			score -= position - previous - 1
			previous = position
			matched++
		}
		before = ch
		position++
	}
	return score, matched == len(needle)
}
