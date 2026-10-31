package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Status carries the rendered declaration and the exact next action. Output
// order follows declaration order, including when ids are selected explicitly.
type Status struct {
	Item
	State  State  `json:"state"`
	Reason string `json:"reason,omitempty"`
	Next   string `json:"next,omitempty"`
}

type Selection struct {
	IDs   []string
	Group string
}

// Write is a whole resulting file, or a removal. Its original snapshot stays
// private; applying a plan refuses a target that changed since it was read.
type Write struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Remove  bool   `json:"remove,omitempty"`
	before  snapshot
}
type Invocation struct {
	ID   string   `json:"id"`
	Argv []string `json:"argv"`
}
type Plan struct {
	Operation string       `json:"operation"`
	Items     []Status     `json:"items"`
	Writes    []Write      `json:"writes"`
	Commands  []Invocation `json:"commands"`
	excluded  map[string]bool
}

type snapshot struct {
	data   []byte
	exists bool
	mode   os.FileMode
}

func (s *Setup) List(ctx context.Context) ([]Status, error) {
	p, err := s.plan(ctx, Selection{}, false, true)
	if err != nil {
		return nil, err
	}
	return p.Items, nil
}
func (s *Setup) Plan(ctx context.Context, selection Selection) (*Plan, error) {
	return s.plan(ctx, selection, false, false)
}
func (s *Setup) PlanUninstall(ctx context.Context, selection Selection) (*Plan, error) {
	return s.plan(ctx, selection, true, false)
}

func (s *Setup) plan(ctx context.Context, sel Selection, remove, list bool) (*Plan, error) {
	p := &Plan{Operation: "install", Items: []Status{}, Writes: []Write{}, Commands: []Invocation{}, excluded: map[string]bool{}}
	if remove {
		p.Operation = "uninstall"
	}
	for _, id := range sel.IDs {
		if !slices.ContainsFunc(s.items, func(i Item) bool { return i.ID == id }) {
			return nil, fmt.Errorf("unknown setup item %q", id)
		}
	}
	virtual := map[string]snapshot{}
	originals := map[string]snapshot{}
	paths := []string{}
	for _, raw := range s.items {
		if len(sel.IDs) > 0 && !slices.Contains(sel.IDs, raw.ID) || sel.Group != "" && raw.Group != sel.Group {
			continue
		}
		i, err := s.render(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", raw.ID, err)
		}
		st := Status{Item: i, State: Missing}
		if i.OptIn && len(sel.IDs) == 0 {
			p.excluded[i.ID] = true
		}
		if slices.Contains(s.options.Skip, i.ID) {
			st.State = Skipped
			st.Reason = "listed in setup.skip"
			p.Items = append(p.Items, st)
			continue
		}
		for _, req := range i.Requires {
			kind, value, _ := strings.Cut(req, ":")
			var err error
			if kind == "bin" {
				_, err = s.lookPath(value)
			} else {
				var path string
				path, err = s.expand(value)
				if err == nil {
					var info os.FileInfo
					info, err = os.Stat(path)
					if err == nil && !info.IsDir() {
						err = fmt.Errorf("not a directory")
					}
				}
			}
			if err != nil {
				st.State = NotApplicable
				st.Reason = "requires " + req
				break
			}
		}
		if st.State == NotApplicable {
			st.Next = i.Advice
			p.Items = append(p.Items, st)
			continue
		}
		if i.Kind == Command || i.Kind == Check {
			st.State, st.Reason, err = s.commandStatus(ctx, i)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", i.ID, err)
			}
			st.Next = next(i, s.options.Tool, remove)
			if i.Kind == Check {
				if st.State != Installed && st.State != External {
					st.State = Manual
				}
				st.Next = i.Advice
			}
			if p.excluded[i.ID] && (st.State == Missing || st.State == Outdated) {
				st.State = Manual
			}
			p.Items = append(p.Items, st)
			if list || p.excluded[i.ID] || i.Kind == Check || st.State == External || st.State == Conflict || st.State == Manual || (!remove && st.State == Installed) || (remove && st.State == Missing) {
				continue
			}
			argv := i.Install
			if remove {
				argv = i.Uninstall
			}
			p.Commands = append(p.Commands, Invocation{i.ID, argv})
			continue
		}
		path := i.Path
		if err := regularPath(path); err != nil {
			st.State = External
			st.Reason = err.Error()
			st.Next = i.Advice
			if st.Next == "" {
				st.Next = i.Content
			}
			p.Items = append(p.Items, st)
			continue
		}
		have, ok := virtual[path]
		if !ok {
			have, err = read(path)
			if err != nil {
				return nil, err
			}
			virtual[path] = have
			originals[path] = have
			paths = append(paths, path)
		}
		out, state, err := s.fileEdit(i, have, remove)
		st.State = state
		if err != nil {
			st.State = Conflict
			st.Reason = err.Error()
		}
		st.Next = next(i, s.options.Tool, remove)
		if p.excluded[i.ID] && (st.State == Missing || st.State == Outdated) {
			st.State = Manual
			st.Next = i.Content
			if i.Advice != "" {
				st.Next = i.Advice
			}
		}
		p.Items = append(p.Items, st)
		if list || p.excluded[i.ID] || err != nil || st.State == Conflict || st.State == Manual || (!remove && st.State == Installed) || (remove && st.State == Missing) {
			continue
		}
		// Restore the exact original bytes only when no unrelated edits would
		// be lost. Otherwise remove just this item's owned portion.
		if remove && have.exists {
			backup, bErr := read(path + "." + s.options.Tool + ".bak")
			if bErr != nil {
				return nil, bErr
			}
			if backup.exists {
				expected, backupState, e := s.fileEdit(i, backup, false)
				if e == nil && backupState != Installed && bytes.Equal(expected.data, have.data) {
					out = backup
				}
			}
		}
		virtual[path] = out
	}
	for _, path := range paths {
		before, after := originals[path], virtual[path]
		if before.exists == after.exists && bytes.Equal(before.data, after.data) {
			continue
		}
		p.Writes = append(p.Writes, Write{Path: path, Content: string(after.data), Remove: !after.exists, before: before})
	}
	return p, nil
}

func next(i Item, tool string, remove bool) string {
	if i.Advice != "" && !remove {
		return i.Advice
	}
	argv := i.Install
	if remove {
		argv = i.Uninstall
	}
	if len(argv) > 0 {
		return quoteArgv(argv)
	}
	verb := "install"
	if remove {
		verb = "uninstall"
	}
	return quoteArgv([]string{tool, "setup", verb, i.ID})
}

// quoteArgv is display only. Runner always receives the original argv.
func quoteArgv(argv []string) string {
	words := make([]string, len(argv))
	for n, w := range argv {
		if w != "" && !strings.ContainsAny(w, " \t\n'\"$`\\;&|<>()*?[]{}!") {
			words[n] = w
		} else {
			words[n] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
		}
	}
	return strings.Join(words, " ")
}

func (s *Setup) commandStatus(ctx context.Context, i Item) (State, string, error) {
	var data []byte
	if i.Probe != nil && i.Probe.Path != "" {
		if err := regularPath(i.Probe.Path); err != nil {
			return External, err.Error(), nil
		}
		snap, err := read(i.Probe.Path)
		if err != nil {
			return "", "", err
		}
		if !snap.exists {
			return Missing, "", nil
		}
		data = snap.data
	} else {
		out, err := s.execute(ctx, i.Check)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return Missing, err.Error(), nil
		}
		data = out
	}
	if i.Probe == nil {
		return Installed, "", nil
	}
	value, ok, err := probeValue(*i.Probe, data)
	if err != nil {
		return Conflict, err.Error(), nil
	}
	if !ok {
		return Missing, "", nil
	}
	if i.Content == "" {
		return Installed, "", nil
	}
	state := compare(i, value, []byte(i.Content))
	reason := ""
	if state == Conflict {
		reason = "existing entry does not match this item's ownership fields"
	}
	return state, reason, nil
}

func (s *Setup) execute(ctx context.Context, argv []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty argv")
	}
	argv = slices.Clone(argv)
	path, err := s.lookPath(argv[0])
	if err != nil {
		return nil, err
	}
	argv[0] = path
	return s.options.Runner(ctx, argv, s.environment())
}

func (s *Setup) fileEdit(i Item, have snapshot, remove bool) (snapshot, State, error) {
	out := have
	out.exists = true
	if out.mode == 0 {
		out.mode = 0600
	}
	state := Missing
	switch i.Kind {
	case File:
		comment := i.Comment
		if comment == "" {
			comment = "#"
		}
		owner := comment + " " + s.options.Tool + " setup: " + i.ID
		if comment == "<!--" {
			owner += " -->"
		}
		want := []byte(owner + "\n" + i.Content)
		if have.exists {
			if bytes.Equal(have.data, want) {
				state = Installed
			} else if bytes.HasPrefix(have.data, []byte(owner+"\n")) {
				state = Outdated
			} else {
				return out, Conflict, fmt.Errorf("file does not carry this item's owner mark")
			}
		}
		out.data = want
		if remove {
			out.data = nil
			out.exists = false
		}
	case Block:
		begin, end := markers(i, s.options.Tool)
		a, b, err := blockBounds(have.data, begin, end)
		if err != nil {
			return out, Conflict, err
		}
		want := []byte(begin + "\n" + strings.TrimRight(i.Content, "\r\n") + "\n" + end + "\n")
		if a >= 0 {
			state = Outdated
			if bytes.Equal(have.data[a:b], want) {
				state = Installed
			}
			out.data = append(append(append([]byte{}, have.data[:a]...), want...), have.data[b:]...)
			if remove {
				out.data = append(append([]byte{}, have.data[:a]...), have.data[b:]...)
			}
		} else {
			out.data = append([]byte{}, have.data...)
			if len(out.data) > 0 && out.data[len(out.data)-1] != '\n' {
				out.data = append(out.data, '\n')
			}
			out.data = append(out.data, want...)
			if remove {
				out = have
			}
		}
		if remove && len(out.data) == 0 {
			out.exists = false
		}
	case JSON, JSONArray:
		raw := have.data
		if !have.exists {
			raw = []byte("{}")
		}
		if i.Kind == JSONArray {
			data, st, err := arrayEdit(i, raw, remove)
			out.data = data
			if remove && sameJSON(data, []byte("{}")) {
				out.exists = false
			}
			return out, st, err
		}
		value, exists, err := valueAt(raw, i.Pointer)
		if err != nil {
			return out, Conflict, err
		}
		if exists {
			state = compare(i, value, []byte(i.Content))
			if state == Conflict {
				return out, state, fmt.Errorf("existing JSON value does not match this item's ownership fields")
			}
		}
		out.data, err = editJSON(raw, i.Pointer, []byte(i.Content), remove)
		if err != nil {
			return out, Conflict, err
		}
		if remove && !have.exists {
			out = have
		}
		if remove && sameJSON(out.data, []byte("{}")) {
			out.exists = false
		}
	}
	return out, state, nil
}

// regularPath refuses symlinks at the leaf AND in every parent directory.
func regularPath(path string) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%s is a symlink; managed externally", p)
			}
			if p == path && !info.Mode().IsRegular() {
				return fmt.Errorf("%s is not a regular file", p)
			}
			if p != path && !info.IsDir() {
				return fmt.Errorf("%s is not a directory", p)
			}
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

func read(path string) (snapshot, error) {
	if err := regularPath(path); err != nil {
		return snapshot{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot{}, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	data, err := os.ReadFile(path)
	return snapshot{data: data, exists: true, mode: info.Mode().Perm()}, err
}

func (s *Setup) Install(ctx context.Context, sel Selection) ([]Status, error) {
	p, err := s.Plan(ctx, sel)
	if err != nil {
		return nil, err
	}
	if err = s.Apply(ctx, p); err != nil {
		return nil, err
	}
	return s.selectedStatus(ctx, sel)
}
func (s *Setup) Uninstall(ctx context.Context, sel Selection) ([]Status, error) {
	p, err := s.PlanUninstall(ctx, sel)
	if err != nil {
		return nil, err
	}
	if err = s.Apply(ctx, p); err != nil {
		return nil, err
	}
	return s.selectedStatus(ctx, sel)
}
func (s *Setup) selectedStatus(ctx context.Context, sel Selection) ([]Status, error) {
	p, err := s.plan(ctx, sel, false, true)
	if err != nil {
		return nil, err
	}
	return p.Items, nil
}
