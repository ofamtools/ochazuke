// Package setup plans and installs declarative tool integrations without a TUI.
// File ownership, backups and atomic replacement adapt matomeru/internal/setup
// (MIT, Copyright Joshua Toyota); markers and probe shapes adapt tuios'
// internal/integration (MIT, Copyright Gaurav Gosain and contributors).
package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Kind string

const (
	File      Kind = "file"
	Block     Kind = "block"
	JSON      Kind = "json"
	JSONArray Kind = "json-array"
	Command   Kind = "command"
	Check     Kind = "check"
)

type State string

const (
	Installed     State = "installed"
	Missing       State = "missing"
	Outdated      State = "outdated"
	Conflict      State = "conflict"
	External      State = "external"
	NotApplicable State = "n/a"
	Manual        State = "manual"
	Skipped       State = "skipped"
)

// Match describes adoption and ownership. Fields are JSON pointers relative to
// the value. Command/Args compare a command's basename and its argument array;
// an absolute command pointing elsewhere (or gone) is outdated. Contains marks
// entries owned by a json-array item, at Field (a relative JSON pointer).
type Match struct {
	Fields   []string `json:"fields,omitempty"`
	Command  string   `json:"command,omitempty"`
	Args     string   `json:"args,omitempty"`
	Field    string   `json:"field,omitempty"`
	Contains string   `json:"contains,omitempty"`
}

// Probe reads Path, or Check's stdout when Path is empty. Format is json
// (default) or toml; Pointer selects JSON, Table selects a TOML table. Where
// selects an object in a JSON array by exact field values before Pointer.
type Probe struct {
	Path    string            `json:"path,omitempty"`
	Format  string            `json:"format,omitempty"`
	Pointer string            `json:"pointer,omitempty"`
	Table   []string          `json:"table,omitempty"`
	Where   map[string]string `json:"where,omitempty"`
}

// Item is the same declaration in Go and setup.json. Content is a Go template
// over Bin, Data, Home and Tool; each argv word and Advice are templates too.
// Comment is a prefix (#, //) or <!--; explicit Begin/End override markers.
type Item struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        Kind     `json:"kind"`
	Path        string   `json:"path,omitempty"`
	Requires    []string `json:"requires,omitempty"`
	Content     string   `json:"content,omitempty"`
	Comment     string   `json:"comment,omitempty"`
	Begin       string   `json:"begin,omitempty"`
	End         string   `json:"end,omitempty"`
	Pointer     string   `json:"pointer,omitempty"`
	Match       Match    `json:"match,omitempty"`
	Probe       *Probe   `json:"probe,omitempty"`
	Check       []string `json:"check,omitempty"`
	Install     []string `json:"install,omitempty"`
	Uninstall   []string `json:"uninstall,omitempty"`
	Advice      string   `json:"advice,omitempty"`
	Group       string   `json:"group,omitempty"`
	OptIn       bool     `json:"opt_in,omitempty"`
}

// Runner executes argv directly, never through a shell. env contains a full
// environment, with HOME and XDG directories isolated when Home was supplied.
// Tests and hosts can inject a runner, including for destructive harness verbs.
type Runner func(context.Context, []string, []string) ([]byte, error)

type Options struct {
	Tool   string
	Home   string
	Bin    string
	Data   string
	Skip   []string
	Env    map[string]string
	Runner Runner
}

type Setup struct {
	options      Options
	items        []Item
	homeOverride bool
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// New validates all declarations, before any reads or writes. Bin defaults to
// the running executable and is always made absolute at declaration time.
func New(items []Item, options Options) (*Setup, error) {
	if !identifier.MatchString(options.Tool) {
		return nil, fmt.Errorf("setup tool must be a simple name")
	}
	s := &Setup{options: options, items: append([]Item(nil), items...), homeOverride: options.Home != ""}
	var err error
	if s.options.Home == "" {
		s.options.Home, err = os.UserHomeDir()
	}
	if err != nil {
		return nil, err
	}
	s.options.Home, err = filepath.Abs(s.options.Home)
	if err != nil {
		return nil, err
	}
	// macOS exposes /tmp and /var through system symlinks. Resolve the
	// explicitly selected home once; links below it remain external.
	for parent, tail := s.options.Home, ""; ; parent = filepath.Dir(parent) {
		if resolved, e := filepath.EvalSymlinks(parent); e == nil {
			s.options.Home = filepath.Join(resolved, tail)
			break
		}
		if filepath.Dir(parent) == parent {
			break
		}
		tail = filepath.Join(filepath.Base(parent), tail)
	}
	if s.options.Bin == "" {
		s.options.Bin, err = os.Executable()
	}
	if err != nil {
		return nil, err
	}
	s.options.Bin, err = filepath.Abs(s.options.Bin)
	if err != nil {
		return nil, err
	}
	if s.options.Runner == nil {
		s.options.Runner = run
	}
	seen := map[string]bool{}
	for _, item := range s.items {
		if err := validate(item); err != nil {
			return nil, fmt.Errorf("setup item %q: %w", item.ID, err)
		}
		if seen[item.ID] {
			return nil, fmt.Errorf("duplicate setup item %q", item.ID)
		}
		seen[item.ID] = true
		rendered, err := s.render(item)
		if err != nil {
			return nil, fmt.Errorf("setup item %q: %w", item.ID, err)
		}
		if item.Kind == Block {
			begin, end := markers(rendered, options.Tool)
			if strings.Contains(rendered.Content, begin) || strings.Contains(rendered.Content, end) {
				return nil, fmt.Errorf("setup item %q: content contains a setup marker", item.ID)
			}
		}
	}
	return s, nil
}

func validate(i Item) error {
	if !identifier.MatchString(i.ID) {
		return fmt.Errorf("id must be a simple name")
	}
	if i.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch i.Kind {
	case File, Block, JSON, JSONArray:
		if i.Path == "" {
			return fmt.Errorf("path is required")
		}
	case Command:
		if len(i.Install) == 0 || len(i.Uninstall) == 0 {
			return fmt.Errorf("install and uninstall argv are required")
		}
		if i.Probe == nil && len(i.Check) == 0 {
			return fmt.Errorf("a read-only probe or check argv is required")
		}
	case Check:
		if i.Probe == nil && len(i.Check) == 0 {
			return fmt.Errorf("a read-only probe or check argv is required")
		}
		if i.Advice == "" {
			return fmt.Errorf("advice is required for a check")
		}
	default:
		return fmt.Errorf("unknown kind %q", i.Kind)
	}
	if (i.Kind == JSON || i.Kind == JSONArray) && (i.Pointer == "" || !strings.HasPrefix(i.Pointer, "/")) {
		return fmt.Errorf("a non-root JSON pointer is required")
	}
	if (i.Begin == "") != (i.End == "") || (i.Begin != "" && i.Begin == i.End) {
		return fmt.Errorf("begin and end must be distinct markers supplied together")
	}
	if i.Kind == JSONArray && (i.Match.Field == "" || i.Match.Contains == "") {
		return fmt.Errorf("json-array needs match.field and match.contains to identify owned entries")
	}
	for _, req := range i.Requires {
		if !strings.HasPrefix(req, "bin:") && !strings.HasPrefix(req, "dir:") {
			return fmt.Errorf("requirement %q must start with bin: or dir:", req)
		}
	}
	if i.Probe != nil && i.Probe.Format != "" && i.Probe.Format != "json" && i.Probe.Format != "toml" {
		return fmt.Errorf("probe format must be json or toml")
	}
	pointers := append([]string{i.Pointer, i.Match.Command, i.Match.Args, i.Match.Field}, i.Match.Fields...)
	if i.Probe != nil {
		pointers = append(pointers, i.Probe.Pointer)
		for p := range i.Probe.Where {
			pointers = append(pointers, p)
		}
	}
	for _, p := range pointers {
		if _, err := pointerParts(p); err != nil {
			return err
		}
	}
	for _, argv := range [][]string{i.Check, i.Install, i.Uninstall} {
		if len(argv) > 0 && argv[0] == "" {
			return fmt.Errorf("argv executable cannot be empty")
		}
	}
	return nil
}

// Decode loads a JSON array of items and refuses misspelled fields and trailing
// documents. Load works directly with an embed.FS or any fs.FS.
func Decode(r io.Reader) ([]Item, error) {
	var items []Item
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&items); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("setup.json must contain exactly one item array")
	}
	return items, nil
}

func Load(files fs.FS, name string) ([]Item, error) {
	f, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Decode(f)
}

func run(ctx context.Context, argv, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
