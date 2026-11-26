package tree

import (
	"strings"

	"ofam.dev/ochazuke/traits"
)

var (
	_ traits.Filterable     = (*Model)(nil)
	_ traits.FilterableNode = Node{}
)

// FilterableText returns the concatenated searchable text for n. If SearchText
// is pre-populated (e.g. "status:todo\ntitle:..."), it is returned directly;
// otherwise n synthesizes searchable text from its visible label, badges,
// metadata, scope, and ref.
func (n Node) FilterableText() string {
	if n.SearchText != "" {
		return n.SearchText
	}
	var parts []string
	if n.Label != "" {
		parts = append(parts, n.Label)
	}
	if n.Badge != "" {
		parts = append(parts, n.Badge)
	}
	for _, b := range n.Badges {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	for _, m := range n.Meta {
		if m.Text != "" {
			parts = append(parts, m.Text)
		}
	}
	if n.Scope != "" {
		parts = append(parts, "scope:"+n.Scope)
	}
	if n.Ref != nil {
		if n.Ref.Kind != "" {
			parts = append(parts, "kind:"+n.Ref.Kind)
		}
		if n.Ref.ID != "" {
			parts = append(parts, "id:"+n.Ref.ID)
		}
	}
	return strings.Join(parts, "\n")
}

// Filter applies query under mode in place without deleting underlying nodes
// and reports whether at least one node remains visible.
func (m *Model) Filter(query string, mode traits.MatchMode) bool {
	m.Query = query
	if mode != "" {
		m.MatchMode = mode
	}
	_, errMsg := traits.CompileMultiTerm(m.Query, m.MatchMode, traits.FilterConfig{})
	m.FilterErr = errMsg
	m.Offset = 0
	m.Clamp()
	return len(m.Flatten()) > 0
}

// SetQuery applies query using the tree's current MatchMode.
func (m *Model) SetQuery(query string) bool {
	return m.Filter(query, m.MatchMode)
}

// CycleMatchMode advances substring -> fuzzy -> regex and re-evaluates Filter.
func (m *Model) CycleMatchMode() {
	m.MatchMode = traits.CycleMatchMode(m.MatchMode, traits.FilterConfig{})
	m.Filter(m.Query, m.MatchMode)
}

func (m *Model) FilterQuery() string { return m.Query }

func (m *Model) FilterMode() traits.MatchMode {
	if m.MatchMode == "" {
		return traits.MatchSubstring
	}
	return m.MatchMode
}

func (m *Model) FilterError() string { return m.FilterErr }
func (m *Model) IsFiltered() bool    { return strings.TrimSpace(m.Query) != "" }

// filteredRoots returns the visible node forest under m.Query. When unfiltered,
// it returns m.activeRoots() directly with zero allocations. When filtered,
// every branch is evaluated recursively: a node stays visible if its own
// FilterableText matches OR any descendant matches.
func (m *Model) filteredRoots() []Node {
	roots := m.activeRoots()
	if !m.IsFiltered() {
		m.FilterErr = ""
		return roots
	}
	mm, errMsg := traits.CompileMultiTerm(m.Query, m.MatchMode, traits.FilterConfig{})
	m.FilterErr = errMsg
	return filterNodeSlice(roots, mm)
}

func filterNodeSlice(ns []Node, mm traits.MultiMatcher) []Node {
	var out []Node
	for _, nd := range ns {
		visKids := filterNodeSlice(nd.Children, mm)
		selfMatch := mm.Match(nd.FilterableText())
		if !selfMatch && len(visKids) == 0 {
			continue
		}
		cp := nd
		if len(visKids) > 0 {
			cp.Children = visKids
		}
		out = append(out, cp)
	}
	return out
}
