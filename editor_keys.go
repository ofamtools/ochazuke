package ochazuke

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// editorSpec is the editor noun's mode tables as data: the same row shape
// as a binding config file, plus three generators. "pending" lists the
// exclusive modes, "extends" copies one mode's rows under another after its
// own, and a row with "expand" is emitted once per entry of the named map,
// with the entry's key as the row key and "$" in its arguments replaced by
// the entry's value. Rows for input operations are guarded by "input".
//
//go:embed editor_bindings.json
var editorSpec []byte

type bindingSpec struct {
	Pending []string          `json:"pending"`
	Extends map[string]string `json:"extends"`
	Maps    map[string]map[string]string
	Rows    []specRow `json:"rows"`
}

type specRow struct {
	Binding
	Expand string `json:"expand,omitempty"`
}

// LoadBindingSpec installs a spec onto one noun: modes, then rows.
func (r *Runtime) LoadBindingSpec(noun string, data []byte) error {
	var spec bindingSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("binding spec: %w", err)
	}
	for _, mode := range spec.Pending {
		if err := r.DefineMode(mode, true); err != nil {
			return err
		}
	}
	for mode := range spec.Extends {
		if !r.Modes[mode] && !r.pendingModes[mode] {
			if err := r.DefineMode(mode, false); err != nil {
				return err
			}
		}
	}
	b := r.Define(noun)
	for _, row := range spec.Rows {
		if row.Expand == "" {
			if err := r.installSpecRow(b, row.Binding); err != nil {
				return err
			}
			continue
		}
		entries, ok := spec.Maps[row.Expand]
		if !ok {
			return fmt.Errorf("binding spec: unknown map %q", row.Expand)
		}
		// Keys in sorted order: map order would make the table, and every
		// catalog and hint drawn from it, differ between runs.
		for _, key := range slices.Sorted(maps.Keys(entries)) {
			value := entries[key]
			expanded := row.Binding
			expanded.Keys = key
			expanded.Request.Args = Arguments{}
			for k, v := range row.Request.Args {
				if s, ok := v.(string); ok {
					v = strings.ReplaceAll(s, "$", value)
				}
				expanded.Request.Args[k] = v
			}
			if err := r.installSpecRow(b, expanded); err != nil {
				return err
			}
		}
	}
	// A mode extends another by reading its rows after its own.
	for mode, parent := range spec.Extends {
		b.Mode(mode).Extends(parent)
	}
	return nil
}

// installSpecRow appends a validated row; unlike Bind it keeps earlier rows
// for the same key, so guarded alternatives coexist in spec order.
func (r *Runtime) installSpecRow(b *NounBuilder, row Binding) error {
	row.Noun, row.Instance = b.Noun().Name, ""
	row = withImplicitGuard(row)
	row, err := r.validateRow(row)
	if err != nil {
		return err
	}
	n := b.Noun()
	n.Bindings[row.Mode] = append(n.Bindings[row.Mode], row)
	return nil
}

// registerEditorNoun installs the framework default tables: the editor noun
// a stack with an input falls back to, the prompt noun that pins insert
// mode, and the registers component's rows on the app noun.
func registerEditorNoun(r *Runtime) {
	if err := r.LoadBindingSpec("editor", editorSpec); err != nil {
		panic(err)
	}
	r.Define("prompt").Pin("insert")
	if err := r.LoadBindingSpec("app", registersSpec); err != nil {
		panic(err)
	}
}
