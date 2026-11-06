package ochazuke

import (
	"encoding/json"
	"fmt"
	"time"
)

// ListFacts declares a thing noun over the noun's own list verb: the
// standard get and list read the rows that verb answers, so the verb stays
// what it was (its rows, its parameters, its TUI list) and the noun gains
// the core verbs. The things are read-only (a facts store): create, set,
// delete and archive are on the noun, unavailable, unless the application
// registers a verb of that name itself, which then wins. The store is
// Remote: List runs off the loop through Ctx.Await, which takes the verb's
// pending work and leaves its Then (the TUI list update) alone.
//
// A thing's key is its row's id (also the human key, found with or without
// the "noun/" prefix), its label the row's label, and its other props the
// fields of the row's Data (or its Props, when the row carries them).
type ListFacts struct {
	// Noun is the application's own noun (declared with DefineOwn).
	Noun string
	// Sample is a value of the struct the rows carry as Data: its json tags
	// are the props (embedded structs flattened, "-" skipped). nil: the
	// rows' data has no fixed fields, so it is one object prop, data.
	Sample any
	// Rename gives a field another prop name where its own is a verb of the
	// noun (a prop and a verb never share a word): json name to prop name.
	Rename map[string]string
	// Docs describes the props by name.
	Docs map[string]string
	// ListParams are list's parameters beside where, props and limit.
	ListParams map[string]Parameter
	// List is the noun's list verb. It runs with the list's arguments (get
	// passes its own), and answers rows {id, label, data}: ListItem or any
	// value that encodes as them.
	List func(Ctx) Result
	// Limit bounds the wait for List's work (0 none).
	Limit time.Duration
}

// DefineListFacts declares each spec's noun, its props (the row's id as the
// key, its label, then the sample's fields) and its store. Call it after
// the nouns' list verbs are registered.
func (r *Runtime) DefineListFacts(specs ...ListFacts) {
	for _, f := range specs {
		extra := []PropSpec{
			{Name: "id", Parameter: Parameter{Description: "The row's id"}},
			{Name: "label", Parameter: Parameter{Description: "The row's label"}},
		}
		if f.Sample == nil {
			extra = append(extra, PropSpec{Name: "data", Parameter: Parameter{Type: PropObject, Description: "The row's fields"}})
		}
		r.defineFacts(factsSpec{
			Noun: f.Noun, Sample: f.Sample, Key: "id", Label: "label", Extra: extra,
			Rename: f.Rename, Docs: f.Docs, ListParams: f.ListParams,
			KeyPrefix: f.Noun + "/", Own: true, Remote: true,
			List: f.list,
		})
	}
}

// list runs the list verb through Ctx.Await and reads its answer.
func (f ListFacts) list(c Ctx, q ListQuery) ([]Thing, error) {
	if len(q.Args) > 0 {
		c.Args = q.Args
	}
	v, err := c.Await(StoreCall{Store: f.Noun, Op: "list"}, func() Result { return f.List(c) }, f.Limit)
	if err != nil {
		return nil, err
	}
	return f.things(v)
}

// things reads a list verb's answer as the noun's things: each row's Props,
// else its data's fields (with a Sample) or its data whole (without), then
// its id and label. Any answer that encodes as rows reads, whatever its Go
// type.
func (f ListFacts) things(v any) ([]Thing, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Data  any    `json:"data"`
		Props Thing  `json:"props"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("%s list answered %T, not rows", f.Noun, v)
	}
	out := make([]Thing, 0, len(rows))
	for _, row := range rows {
		t := Thing{}
		switch {
		case f.Sample == nil:
			t["data"] = row.Data
		case row.Props != nil:
			t = row.Props
		case row.Data != nil:
			if t, err = ThingOf(row.Data); err != nil {
				return nil, err
			}
		}
		t["id"], t["label"] = row.ID, row.Label
		out = append(out, t)
	}
	return out, nil
}
