package form

import (
	"reflect"
	"time"
)

// DefaultMaxDraftsPerKey is the default ring capacity per form key.
const DefaultMaxDraftsPerKey = 20

// Draft is a saved snapshot of form field values.
type Draft struct {
	Values  map[string]any `json:"values"`
	SavedAt time.Time      `json:"savedAt"`
	Label   string         `json:"label,omitempty"`
}

// DraftStore is a pure, JSON-serializable ring of drafts keyed by form ID.
// It performs no I/O; host apps persist it however they choose.
type DraftStore struct {
	Drafts    map[string][]Draft `json:"drafts"`
	MaxPerKey int                `json:"maxPerKey,omitempty"`
}

// NewDraftStore builds an empty DraftStore with the default per-key capacity.
func NewDraftStore() *DraftStore {
	return &DraftStore{
		Drafts:    map[string][]Draft{},
		MaxPerKey: DefaultMaxDraftsPerKey,
	}
}

// Save stores values as the newest draft for key. Empty values maps are skipped.
// When the newest draft for key already has identical values, its SavedAt and
// Label are updated in place instead of pushing a duplicate.
func (ds *DraftStore) Save(key string, values map[string]any, label string, now time.Time) bool {
	if ds == nil || key == "" || len(values) == 0 {
		return false
	}
	if ds.Drafts == nil {
		ds.Drafts = map[string][]Draft{}
	}
	cloned := cloneValueMap(values)
	existing := ds.Drafts[key]
	if len(existing) > 0 && reflect.DeepEqual(existing[0].Values, cloned) {
		existing[0].SavedAt = now
		existing[0].Label = label
		return true
	}
	limit := ds.MaxPerKey
	if limit <= 0 {
		limit = DefaultMaxDraftsPerKey
	}
	next := make([]Draft, 0, min(len(existing)+1, limit))
	next = append(next, Draft{Values: cloned, SavedAt: now, Label: label})
	for i := 0; i < len(existing) && len(next) < limit; i++ {
		next = append(next, existing[i])
	}
	ds.Drafts[key] = next
	return true
}

// List returns a cloned slice of drafts for key, newest first.
func (ds *DraftStore) List(key string) []Draft {
	if ds == nil || len(ds.Drafts[key]) == 0 {
		return nil
	}
	return cloneDrafts(ds.Drafts[key])
}

// Remove deletes the draft at idx for key.
func (ds *DraftStore) Remove(key string, idx int) bool {
	if ds == nil {
		return false
	}
	list := ds.Drafts[key]
	if idx < 0 || idx >= len(list) {
		return false
	}
	list = append(list[:idx], list[idx+1:]...)
	if len(list) == 0 {
		delete(ds.Drafts, key)
	} else {
		ds.Drafts[key] = list
	}
	return true
}

// Clone returns a deep copy of the DraftStore.
func (ds *DraftStore) Clone() *DraftStore {
	if ds == nil {
		return nil
	}
	out := &DraftStore{
		Drafts:    make(map[string][]Draft, len(ds.Drafts)),
		MaxPerKey: ds.MaxPerKey,
	}
	for k, list := range ds.Drafts {
		out.Drafts[k] = cloneDrafts(list)
	}
	return out
}

func cloneDrafts(in []Draft) []Draft {
	if len(in) == 0 {
		return nil
	}
	out := make([]Draft, len(in))
	for i, d := range in {
		out[i] = Draft{
			Values:  cloneValueMap(d.Values),
			SavedAt: d.SavedAt,
			Label:   d.Label,
		}
	}
	return out
}

func cloneValueMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneValueMap(t)
	case []string:
		cp := make([]string, len(t))
		copy(cp, t)
		return cp
	case []any:
		cp := make([]any, len(t))
		for i, e := range t {
			cp[i] = cloneValue(e)
		}
		return cp
	default:
		return v
	}
}
