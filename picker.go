package ochazuke

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Picker struct {
	Runtime   *Runtime
	Origin    Context
	Directory string
	Kind      string
	Entries   []os.DirEntry
	Selected  int
	frame     *Frame
}

func NewPicker(r *Runtime) *Picker {
	p := &Picker{Runtime: r}
	r.Layers["picker"] = &Layer{ID: "picker", Noun: "picker", Actions: map[string]Handler{}}
	for _, b := range []struct {
		key, verb string
		args      Arguments
	}{
		{"j", "move", Arguments{"delta": 1}}, {"down", "move", Arguments{"delta": 1}},
		{"k", "move", Arguments{"delta": -1}}, {"up", "move", Arguments{"delta": -1}},
		{"enter", "enter", nil}, {"-", "parent", nil}, {"backspace", "parent", nil},
		{"ctrl+s", "choose", nil}, {"esc", "close", nil},
	} {
		r.Layers["picker"].Bindings = append(r.Layers["picker"].Bindings, Binding{Keys: b.key, Request: Request{Verb: b.verb, Noun: "picker", Args: b.args}})
	}
	register := func(verb, desc string, params map[string]Parameter, fn func(Arguments) (any, error)) {
		r.Register(Operation{Verb: verb, Noun: "picker", Description: desc, Parameters: params, Result: "picker state", Run: func(_ *Runtime, _ Context, a Arguments) (any, error) { return fn(a) }})
	}
	register("open", "Open a file or directory selector", map[string]Parameter{"kind": {Type: "string", Description: "file or directory (default directory)"}, "path": {Type: "string"}}, func(a Arguments) (any, error) {
		if p.IsOpen() {
			return nil, errors.New("picker is already open")
		}
		kind := StringArg(a, "kind", "directory")
		if kind != "file" && kind != "directory" {
			return nil, errors.New("kind must be file or directory")
		}
		path := StringArg(a, "path", "")
		if path == "" {
			var err error
			path, err = os.Getwd()
			if err != nil {
				return nil, err
			}
		}
		p.Kind = kind
		if err := p.load(path); err != nil {
			return nil, err
		}
		p.Origin = r.Context()
		p.frame = &Frame{ID: "picker", Context: Context{Path: []string{"app", "picker"}}, State: map[string]any{}}
		if err := r.Push(p.frame); err != nil {
			return nil, err
		}
		p.sync()
		return p.state(), nil
	})
	register("move", "Move the picker selection", map[string]Parameter{"delta": {Type: "integer", Required: true}}, func(a Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		p.Selected = min(max(0, p.Selected+IntArg(a, "delta", 0)), max(0, len(p.Entries)-1))
		p.sync()
		return p.state(), nil
	})
	register("parent", "Browse the parent directory", nil, func(a Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		err := p.load(filepath.Dir(p.Directory))
		p.sync()
		return p.state(), err
	})
	register("enter", "Open a directory or select a file", nil, func(a Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		if len(p.Entries) == 0 {
			return nil, errors.New("directory is empty")
		}
		entry := p.Entries[p.Selected]
		path := filepath.Join(p.Directory, entry.Name())
		if entry.IsDir() {
			err := p.load(path)
			p.sync()
			return p.state(), err
		}
		return p.choose(path)
	})
	register("choose", "Choose the current directory or highlighted file", nil, func(a Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		if p.Kind == "directory" {
			return p.choose(p.Directory)
		}
		if len(p.Entries) == 0 || p.Entries[p.Selected].IsDir() {
			return nil, errors.New("select a file first")
		}
		return p.choose(filepath.Join(p.Directory, p.Entries[p.Selected].Name()))
	})
	register("close", "Cancel the selector without changing its originating input", nil, func(a Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		if err := r.Pop(); err != nil {
			return nil, err
		}
		return r.Context(), nil
	})
	return p
}
func (p *Picker) IsOpen() bool {
	for _, frame := range p.Runtime.Frames {
		if frame == p.frame {
			return true
		}
	}
	return false
}
func (p *Picker) require() error {
	if !p.IsOpen() || p.Runtime.Frames[len(p.Runtime.Frames)-1] != p.frame {
		return errors.New("picker is not the active modal")
	}
	return nil
}
func (p *Picker) load(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return err
	}
	filtered := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if p.Kind == "file" || entry.IsDir() {
			filtered = append(filtered, entry)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].IsDir() != filtered[j].IsDir() {
			return filtered[i].IsDir()
		}
		return filtered[i].Name() < filtered[j].Name()
	})
	p.Directory = absolute
	p.Entries = filtered
	p.Selected = 0
	return nil
}
func (p *Picker) choose(path string) (any, error) {
	in := p.Runtime.Inputs[p.Origin.InputID]
	if in == nil {
		return nil, errors.New("picker has no originating input")
	}
	if _, err := p.Runtime.ExecuteAt(p.Origin, Request{Verb: "replace", Noun: "input", Args: Arguments{"start": 0, "end": len(in.Text), "text": path}}); err != nil {
		return nil, err
	}
	if err := p.Runtime.Pop(); err != nil {
		return nil, err
	}
	return path, nil
}
func (p *Picker) state() map[string]any {
	names := make([]string, len(p.Entries))
	for i, e := range p.Entries {
		names[i] = e.Name()
	}
	return map[string]any{"directory": p.Directory, "kind": p.Kind, "selected": p.Selected, "entries": names}
}
func (p *Picker) sync() {
	if p.IsOpen() {
		p.frame.State = p.state()
	}
}
func (p *Picker) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Choose %s: %s\n\nj/k move · Enter open/select · - parent · Ctrl-S choose · Esc cancel\n\n", p.Kind, p.Directory)
	if len(p.Entries) == 0 {
		b.WriteString("(empty directory)\n")
	}
	start := max(0, p.Selected-5)
	end := min(len(p.Entries), start+10)
	for i := start; i < end; i++ {
		mark := "  "
		if i == p.Selected {
			mark = "> "
		}
		suffix := ""
		if p.Entries[i].IsDir() {
			suffix = "/"
		}
		fmt.Fprintf(&b, "%s%s%s\n", mark, p.Entries[i].Name(), suffix)
	}
	return b.String()
}
