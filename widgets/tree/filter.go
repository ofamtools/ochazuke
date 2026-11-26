package tree

import (
	"strings"

	"ofam.dev/ochazuke/traits"
)

var (
	_ traits.Filterable     = (*Model)(nil)
	_ traits.FilterableNode = Node{}
)

// compiledFilter caches the last compiled MultiMatcher produced by Filter so
// read/projection paths avoid recompiling on every frame while staying pure.
type compiledFilter struct {
	valid bool
	query string
	mode  traits.MatchMode
	mm    traits.MultiMatcher
	err   string
}

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

// Filter applies query under mode as an explicit mutation without deleting
// underlying nodes, updates compiled matcher/error state, re-clamps selection
// and viewport offset, and reports whether at least one node remains visible.
func (m *Model) Filter(query string, mode traits.MatchMode) bool {
	m.Query = query
	if mode != "" {
		m.MatchMode = mode
	}
	if !m.IsFiltered() {
		m.FilterErr = ""
		m.filter = compiledFilter{valid: true, query: m.Query, mode: m.MatchMode}
	} else {
		mm, errMsg := traits.CompileMultiTerm(m.Query, m.MatchMode, traits.FilterConfig{})
		m.FilterErr = errMsg
		m.filter = compiledFilter{
			valid: true,
			query: m.Query,
			mode:  m.MatchMode,
			mm:    mm,
			err:   errMsg,
		}
	}
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

// FilterError returns the active filter compilation error without mutating m.
func (m *Model) FilterError() string {
	_, errMsg := m.activeMatcher()
	return errMsg
}

func (m *Model) IsFiltered() bool { return strings.TrimSpace(m.Query) != "" }

// activeMatcher resolves the compiled matcher and error string for the current
// Query and MatchMode purely without mutating m. Callers that use Filter reuse
// the cached compilation; callers that assign public Query/MatchMode directly
// still get truthful results.
func (m *Model) activeMatcher() (traits.MultiMatcher, string) {
	if !m.IsFiltered() {
		return traits.MultiMatcher{}, ""
	}
	if m.filter.valid && m.filter.query == m.Query && m.filter.mode == m.MatchMode {
		return m.filter.mm, m.filter.err
	}
	return traits.CompileMultiTerm(m.Query, m.MatchMode, traits.FilterConfig{})
}

// filteredRoots projects the visible node forest under m.Query purely without
// mutating m. When unfiltered, it returns m.activeRoots() directly with zero
// allocations. When filtered, a node stays visible if its own FilterableText
// matches OR any descendant matches, and its projected Children contain only
// visible descendants (never leaking non-matching children of a matched parent).
func (m *Model) filteredRoots() []Node {
	roots := m.activeRoots()
	if !m.IsFiltered() {
		return roots
	}
	mm, _ := m.activeMatcher()
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
		nd.Children = visKids
		out = append(out, nd)
	}
	return out
}
