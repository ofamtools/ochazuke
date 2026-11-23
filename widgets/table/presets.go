package table

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// String formats a SortDir as "asc", "desc", or "none".
func (d SortDir) String() string {
	switch d {
	case SortAsc:
		return "asc"
	case SortDesc:
		return "desc"
	default:
		return "none"
	}
}

// MarshalJSON encodes SortDir as a human-readable string ("asc", "desc", "none")
// for settings.json persistence.
func (d SortDir) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON accepts either a string ("asc", "desc", "none", "") or an
// integer (0, 1, 2) for SortDir.
func (d *SortDir) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "", "none":
			*d = SortNone
		case "asc":
			*d = SortAsc
		case "desc":
			*d = SortDesc
		default:
			return fmt.Errorf("table: unknown sort direction %q", s)
		}
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		switch SortDir(n) {
		case SortNone, SortAsc, SortDesc:
			*d = SortDir(n)
			return nil
		default:
			return fmt.Errorf("table: unknown sort direction %d", n)
		}
	}
	return fmt.Errorf("table: invalid sort direction %s", string(data))
}

// Preset is a saved filter/sort/group configuration for a specific table.
type Preset struct {
	Name        string    `json:"name"`
	Filter      string    `json:"filter,omitempty"`
	FilterField string    `json:"filterField,omitempty"`
	MatchMode   MatchMode `json:"matchMode,omitempty"`
	Sort        []SortKey `json:"sort,omitempty"`
	Group       string    `json:"group,omitempty"`
	Default     bool      `json:"default,omitempty"`
}

func clonePreset(p Preset) Preset {
	p.Sort = slices.Clone(p.Sort)
	return p
}

// CapturePreset snapshots m's current filter, sort, and group settings into a
// named Preset.
func CapturePreset(m *Model, name string) Preset {
	if m == nil {
		return Preset{Name: name}
	}
	p := Preset{
		Name:        name,
		Filter:      m.Query,
		FilterField: m.FilterField,
		Sort:        slices.Clone(m.effectiveSortKeys()),
		Group:       m.GroupField,
	}
	if m.MatchMode != "" && m.MatchMode != MatchSubstring {
		p.MatchMode = m.MatchMode
	}
	return p
}

// ApplyPreset applies p's filter, sort, and group configuration to m.
func ApplyPreset(m *Model, p Preset) {
	if m == nil {
		return
	}
	m.Query = p.Filter
	m.FilterField = p.FilterField
	if p.MatchMode != "" {
		m.MatchMode = p.MatchMode
	} else {
		m.MatchMode = MatchSubstring
	}
	var keys []SortKey
	for _, k := range p.Sort {
		if k.Field != "" && k.Dir != SortNone {
			keys = append(keys, k)
		}
	}
	m.SortKeys = keys
	if len(keys) > 0 {
		m.SortField = keys[0].Field
		m.SortDir = keys[0].Dir
	} else {
		m.SortField = ""
		m.SortDir = SortNone
	}
	m.GroupField = p.Group
	m.Collapsed = nil
	m.VisualActive = false
	m.SelectedIdx = 0
	m.Offset = 0
	m.clamp(len(m.Items()))
}

// PresetStore manages named table presets keyed by tableID (not record kind),
// so different tables showing the same record kind keep independent presets.
type PresetStore struct {
	byTable map[string][]Preset
}

// NewPresetStore returns an empty PresetStore.
func NewPresetStore() *PresetStore {
	return &PresetStore{byTable: map[string][]Preset{}}
}

// Save inserts or replaces a preset by name under tableID. If p.Default is
// true, any previous default for tableID is cleared.
func (s *PresetStore) Save(tableID string, p Preset) error {
	if strings.TrimSpace(tableID) == "" {
		return errors.New("table: preset tableID is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("table: preset name is required")
	}
	if s.byTable == nil {
		s.byTable = map[string][]Preset{}
	}
	cp := clonePreset(p)
	list := s.byTable[tableID]
	if cp.Default {
		for i := range list {
			list[i].Default = false
		}
	}
	for i := range list {
		if list[i].Name == cp.Name {
			list[i] = cp
			s.byTable[tableID] = list
			return nil
		}
	}
	s.byTable[tableID] = append(list, cp)
	return nil
}

// Delete removes the named preset from tableID and reports whether it existed.
func (s *PresetStore) Delete(tableID, name string) bool {
	if s == nil || s.byTable == nil {
		return false
	}
	list := s.byTable[tableID]
	for i := range list {
		if list[i].Name == name {
			list = slices.Delete(list, i, i+1)
			if len(list) == 0 {
				delete(s.byTable, tableID)
			} else {
				s.byTable[tableID] = list
			}
			return true
		}
	}
	return false
}

// SetDefault marks the named preset as the default for tableID (clearing any
// previous default). Pass name == "" to clear the default for tableID.
// Returns false if a non-empty name does not exist under tableID.
func (s *PresetStore) SetDefault(tableID, name string) bool {
	if s == nil || s.byTable == nil {
		return false
	}
	list := s.byTable[tableID]
	if name == "" {
		changed := false
		for i := range list {
			if list[i].Default {
				list[i].Default = false
				changed = true
			}
		}
		return changed
	}
	idx := -1
	for i := range list {
		if list[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	for i := range list {
		list[i].Default = (i == idx)
	}
	return true
}

// Default returns the default preset for tableID, if one is set.
func (s *PresetStore) Default(tableID string) (Preset, bool) {
	if s == nil || s.byTable == nil {
		return Preset{}, false
	}
	for _, p := range s.byTable[tableID] {
		if p.Default {
			return clonePreset(p), true
		}
	}
	return Preset{}, false
}

// Get returns the named preset for tableID, if present.
func (s *PresetStore) Get(tableID, name string) (Preset, bool) {
	if s == nil || s.byTable == nil {
		return Preset{}, false
	}
	for _, p := range s.byTable[tableID] {
		if p.Name == name {
			return clonePreset(p), true
		}
	}
	return Preset{}, false
}

// List returns a defensive copy of all presets saved for tableID.
func (s *PresetStore) List(tableID string) []Preset {
	if s == nil || s.byTable == nil {
		return nil
	}
	list := s.byTable[tableID]
	if len(list) == 0 {
		return nil
	}
	out := make([]Preset, len(list))
	for i, p := range list {
		out[i] = clonePreset(p)
	}
	return out
}

// TableIDs returns the sorted list of tableIDs with at least one saved preset.
func (s *PresetStore) TableIDs() []string {
	if s == nil || len(s.byTable) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.byTable))
	for id, list := range s.byTable {
		if len(list) > 0 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// Apply looks up preset name under tableID and applies it to t.
// Returns false if t is nil or the preset does not exist.
func (s *PresetStore) Apply(t *Model, tableID, name string) bool {
	if t == nil {
		return false
	}
	p, ok := s.Get(tableID, name)
	if !ok {
		return false
	}
	ApplyPreset(t, p)
	return true
}

// ApplyDefault applies the default preset for tableID to t, if one is set.
func (s *PresetStore) ApplyDefault(t *Model, tableID string) bool {
	if t == nil {
		return false
	}
	p, ok := s.Default(tableID)
	if !ok {
		return false
	}
	ApplyPreset(t, p)
	return true
}

// Capture snapshots t's current filter, sort, and group configuration into a
// named Preset.
func (s *PresetStore) Capture(t *Model, name string) Preset {
	return CapturePreset(t, name)
}

// MarshalJSON encodes the store as a JSON object keyed by tableID, suitable
// for embedding directly in settings.json.
func (s PresetStore) MarshalJSON() ([]byte, error) {
	if len(s.byTable) == 0 {
		return []byte("{}"), nil
	}
	clean := make(map[string][]Preset, len(s.byTable))
	for tableID, list := range s.byTable {
		if len(list) == 0 {
			continue
		}
		cp := make([]Preset, len(list))
		for i, p := range list {
			cp[i] = clonePreset(p)
		}
		clean[tableID] = cp
	}
	return json.Marshal(clean)
}

// UnmarshalJSON decodes a JSON object keyed by tableID into s, normalizing
// duplicate names and enforcing at most one default preset per tableID.
func (s *PresetStore) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		s.byTable = map[string][]Preset{}
		return nil
	}
	var raw map[string][]Preset
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	next := make(map[string][]Preset, len(raw))
	for tableID, list := range raw {
		if strings.TrimSpace(tableID) == "" {
			continue
		}
		tmp := &PresetStore{byTable: map[string][]Preset{}}
		for _, p := range list {
			if strings.TrimSpace(p.Name) == "" {
				continue
			}
			_ = tmp.Save(tableID, p)
		}
		if len(tmp.byTable[tableID]) > 0 {
			next[tableID] = tmp.byTable[tableID]
		}
	}
	s.byTable = next
	return nil
}
