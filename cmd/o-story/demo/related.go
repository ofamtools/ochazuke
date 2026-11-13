package demo

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

// RelatedSpec is a stack of related sections (ochazuke.AddRelated) drawn in
// the page's order (its id, default "related", names it in Order): for the
// things of Noun, one section per referring noun. The key is the selected
// row of the page list KeyList (default the first list), whose row ids are
// the Noun things' keys. Each referring noun gets props, an in-memory store
// with undo (or, with Hook, no store: its rows come through the Rows hook)
// and, with Columns, a table. u undoes, as an application binds it.
type RelatedSpec struct {
	ID      string `json:"id,omitempty"`
	Noun    string `json:"noun"`
	KeyList string `json:"keyList,omitempty"`
	Height  int    `json:"height,omitempty"`
	// Order, Hidden and Collapsed are the stack's list settings at the
	// start; Empty its empty mode and NoCounts turns the counts off.
	// Detail makes the stack the detail of the selected row of KeyList
	// (ochazuke.AddRelatedDetail): its rows are things of RowNouns (each
	// item's noun, its data or id the key), the sections are those of the
	// selected row's noun, and Noun is not used. detail.related and
	// detail.related-hidden are the settings.
	Detail    bool              `json:"detail,omitempty"`
	RowNouns  []string          `json:"rowNouns,omitempty"`
	Order     []string          `json:"order,omitempty"`
	Hidden    []string          `json:"hidden,omitempty"`
	Collapsed []string          `json:"collapsed,omitempty"`
	Empty     string            `json:"empty,omitempty"`
	NoCounts  bool              `json:"noCounts,omitempty"`
	Nouns     []RelatedNounSpec `json:"nouns"`
	Things    []ochazuke.Thing  `json:"things,omitempty"`
	// Relations gives the runtime a relation store with these kinds and
	// rows (RelationsSpec); Plain names storeless nouns (a key prop id and
	// a title) the kinds' ends may be, whose relation rows are made from
	// the key.
	Relations *RelationsSpec `json:"relations,omitempty"`
	Plain     []string       `json:"plain,omitempty"`
	// Rail draws the stack as a rail (ochazuke.AddRail) at the right of
	// the page's main area, by the rail settings (rail.look, rail.width,
	// rail.collapsed, ...), in place of a place in the page's order; the
	// focus starts in the main. GroupBy splits sections by a prop
	// (RelatedSpec.GroupBy).
	Rail    bool              `json:"rail,omitempty"`
	GroupBy map[string]string `json:"groupBy,omitempty"`
}

// RelatedNounSpec is a noun that names the stack's noun by Prop. Things have
// "id" and "title" and the Prop. Owner gives the prop the owner role;
// ReadOnly makes it read-only; Columns (prop names) makes a table.
type RelatedNounSpec struct {
	Noun  string `json:"noun"`
	Prop  string `json:"prop"`
	Owner bool   `json:"owner,omitempty"`
	// Any makes the prop a reference to a thing of any noun (Noun "*"):
	// things hold "noun:key".
	Any bool `json:"any,omitempty"`
	// Relation backs the prop with relations of that kind: a list of
	// Refs (default the stack's noun) keys read from the relation store,
	// not from the things.
	Relation string `json:"relation,omitempty"`
	// Refs is the noun a plain Prop names (default the stack's Noun).
	Refs     string           `json:"refs,omitempty"`
	ReadOnly bool             `json:"readOnly,omitempty"`
	Hook     bool             `json:"hook,omitempty"`
	Columns  []string         `json:"columns,omitempty"`
	Things   []ochazuke.Thing `json:"things,omitempty"`
	// View names an existing placed component used instead of section rows.
	View string `json:"view,omitempty"`
}

func relatedID(p *Page) string {
	if p.Related.ID != "" {
		return p.Related.ID
	}
	return "related"
}

// relatedMem is one noun's things.
type relatedMem struct{ things []ochazuke.Thing }

func (m *relatedMem) find(id string) int {
	return slices.IndexFunc(m.things, func(t ochazuke.Thing) bool { return fmt.Sprint(t["id"]) == id })
}

func (m *relatedMem) store() ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.KindCollection, Undo: true, AcceptsKey: true, Stamps: true,
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			out := make([]ochazuke.Thing, len(m.things))
			for i, t := range m.things {
				out[i] = maps.Clone(t)
			}
			return out, nil
		},
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			if i := m.find(id); i >= 0 {
				return maps.Clone(m.things[i]), nil
			}
			return nil, fmt.Errorf("no thing %q", id)
		},
		Create: func(_ ochazuke.Ctx, v ochazuke.Thing) (ochazuke.Thing, error) {
			t := maps.Clone(v)
			if id, _ := t["id"].(string); id == "" || m.find(id) >= 0 {
				t["id"] = fmt.Sprintf("n%d", len(m.things)+1)
			}
			if _, ok := t["title"]; !ok {
				t["title"] = "new " + fmt.Sprint(t["id"])
			}
			m.things = append(m.things, t)
			return maps.Clone(t), nil
		},
		Patch: func(_ ochazuke.Ctx, id string, ch ochazuke.Thing) (ochazuke.Thing, error) {
			i := m.find(id)
			if i < 0 {
				return nil, fmt.Errorf("no thing %q", id)
			}
			maps.Copy(m.things[i], ch)
			return maps.Clone(m.things[i]), nil
		},
		Delete: func(_ ochazuke.Ctx, id string) error {
			if i := m.find(id); i >= 0 {
				m.things = slices.Delete(m.things, i, i+1)
			}
			return nil
		},
	}
}

// addRelated defines the nouns, the settings and the stack of a page.
func (ss *session) addRelated(p *Page) (*ochazuke.Related, error) {
	r, spec := ss.R, p.Related
	mems := map[string]*relatedMem{}
	rowNouns := spec.RowNouns
	if !spec.Detail {
		rowNouns = []string{spec.Noun}
	}
	for _, nouns := range [][]string{rowNouns, spec.Plain} {
		for _, noun := range nouns {
			if r.Noun(noun) == nil {
				r.Define(noun).Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel})
			}
		}
	}
	for _, n := range spec.Nouns {
		prop := ochazuke.PropSpec{Name: n.Prop, ReadOnly: n.ReadOnly, Parameter: ochazuke.Parameter{Type: "string", Noun: spec.Noun}}
		if n.Refs != "" {
			prop.Noun = n.Refs
		}
		if n.Any {
			prop.Noun = ochazuke.AnyNoun
		}
		if n.Relation != "" {
			prop.Relation = n.Relation
			prop.Parameter = ochazuke.Parameter{Type: ochazuke.SettingList, Items: &ochazuke.Parameter{Type: "string", Noun: prop.Noun}}
		}
		if n.Owner {
			prop.Role = ochazuke.RoleOwner
		}
		b := r.Define(n.Noun).Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel}, prop)
		if n.View != "" {
			b.RelatedView(n.View)
		}
		if len(n.Columns) > 0 {
			var cols []ochazuke.Column
			for _, c := range n.Columns {
				cols = append(cols, ochazuke.Column{Title: c})
			}
			b.Columns(cols...)
		}
		m := &relatedMem{things: slices.Clone(n.Things)}
		for i := range m.things {
			m.things[i] = maps.Clone(m.things[i])
		}
		mems[n.Noun] = m
		if !n.Hook {
			b.Store(m.store())
		}
	}
	if err := ss.defineRelations(spec.Relations); err != nil {
		return nil, err
	}
	for _, s := range []ochazuke.Setting{
		{Name: "related.enabled", Type: ochazuke.SettingBoolean, Default: true},
		{Name: "related.order", Type: ochazuke.SettingList, Default: orEmpty(spec.Order)},
		{Name: "related.hidden", Type: ochazuke.SettingList, Default: orEmpty(spec.Hidden)},
		{Name: "related.collapsed", Type: ochazuke.SettingList, Default: orEmpty(spec.Collapsed)},
		{Name: "related.counts", Type: ochazuke.SettingBoolean, Default: !spec.NoCounts},
		{Name: "related.empty", Type: ochazuke.SettingString, Default: orDefault(spec.Empty, ochazuke.RelatedEmptyHeader)},
	} {
		if err := r.DeclareSetting(s); err != nil {
			return nil, err
		}
	}
	listID := spec.KeyList
	if listID == "" && len(p.Lists) > 0 {
		listID = p.Lists[0].ID
	}
	rows := func(_ ochazuke.Ctx, ref ochazuke.Referrer, k string) ([]ochazuke.Thing, error) {
		var out []ochazuke.Thing
		for _, t := range mems[ref.Noun].things {
			if t[ref.Prop] == k {
				out = append(out, maps.Clone(t))
			}
		}
		return out, nil
	}
	related := ochazuke.RelatedSpec{Noun: spec.Noun, Rows: rows, Enabled: "related.enabled",
		Order: "related.order", Hidden: "related.hidden", Collapsed: "related.collapsed", Counts: "related.counts", Empty: "related.empty"}
	if spec.Detail {
		related.Target = r.SelectionTarget(func() *ochazuke.Collection { return ss.lists[listID] })
		related.Enabled, related.Hidden = ochazuke.RelatedDetailSetting, ochazuke.RelatedDetailHiddenSetting
	}
	related.Key = func() string {
		id := listID
		if l := ss.lists[id]; l != nil {
			if it, ok := l.Selected(); ok {
				return strings.TrimPrefix(it.ID, id+"/")
			}
		}
		return ""
	}
	if err := r.Bind(ochazuke.Binding{Noun: "app", Mode: "normal", Keys: "u", Request: ochazuke.Request{Noun: "ochazuke.history-entry", Verb: "undo"}}); err != nil {
		return nil, err
	}
	if p.Detail != nil && p.Detail.ShowForm {
		related.History = true
		sf, err := r.AddShowForm(p.ID+"-show", ochazuke.ShowFormSpec{
			List: func() *ochazuke.Collection { return ss.lists[listID] }, Related: &related,
		}, ochazuke.Parent(p.ID))
		if err != nil {
			return nil, err
		}
		return sf.Related(), nil
	}
	if spec.Rail {
		related.Enabled, related.Order, related.Hidden, related.Collapsed, related.Counts, related.Empty = "", "", "", "", "", ""
		related.GroupBy = spec.GroupBy
		rail, err := r.AddRail(relatedID(p), ochazuke.RailSpec{Stack: related}, ochazuke.Parent(p.ID))
		if err != nil {
			return nil, err
		}
		ss.rails[p.ID] = rail
		return rail.Stack(), nil
	}
	related.GroupBy = spec.GroupBy
	return r.AddRelated(relatedID(p), related, ochazuke.Parent(p.ID))
}

func orEmpty(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
