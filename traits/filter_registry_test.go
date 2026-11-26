package traits

import "testing"

type stubFieldFilterable struct {
	query  string
	mode   MatchMode
	err    string
	fields []string
	idx    int
}

func (s *stubFieldFilterable) Filter(q string, m MatchMode) bool {
	s.query = q
	if m != "" {
		s.mode = m
	}
	return true
}
func (s *stubFieldFilterable) FilterQuery() string { return s.query }
func (s *stubFieldFilterable) FilterMode() MatchMode {
	if s.mode == "" {
		return MatchSubstring
	}
	return s.mode
}
func (s *stubFieldFilterable) FilterError() string { return s.err }
func (s *stubFieldFilterable) IsFiltered() bool    { return s.query != "" }
func (s *stubFieldFilterable) FilterFieldName() string {
	if len(s.fields) == 0 {
		return ""
	}
	return s.fields[s.idx%len(s.fields)]
}
func (s *stubFieldFilterable) CycleFilterField() string {
	if len(s.fields) == 0 {
		return ""
	}
	s.idx = (s.idx + 1) % len(s.fields)
	return s.FilterFieldName()
}

func TestFilterRegistrySnapshotCancelAcceptClearAndFacts(t *testing.T) {
	reg := NewFilterRegistry()
	tbl := &stubFieldFilterable{query: "kept", mode: MatchFuzzy, fields: []string{"any", "title", "status"}}
	reg.Register("work.table", tbl)

	// Begin edit snapshots kept filter and return mode.
	f, ok := reg.BeginEdit("work.table", "outline")
	if !ok || f != tbl {
		t.Fatalf("BeginEdit failed: ok=%v", ok)
	}
	tbl.Filter("draft", MatchRegex)

	facts := reg.Facts("work.table", "work ", true, FilterConfig{})
	if facts["filtering"] != true || facts["query"] != "draft" || facts["matchMode"] != string(MatchRegex) {
		t.Fatalf("unexpected active facts: %+v", facts)
	}
	if facts["hasFilterFields"] != true || facts["filterField"] != "any" || facts["filterPrefix"] != "work " {
		t.Fatalf("unexpected field facts: %+v", facts)
	}

	// CancelEdit restores prior kept filter + mode and returns caller mode.
	ret, hadSnap := reg.CancelEdit("work.table")
	if !hadSnap || ret != "outline" {
		t.Fatalf("CancelEdit = (%q, %v), want (outline, true)", ret, hadSnap)
	}
	if tbl.FilterQuery() != "kept" || tbl.FilterMode() != MatchFuzzy {
		t.Fatalf("CancelEdit did not restore snapshot: q=%q mode=%q", tbl.FilterQuery(), tbl.FilterMode())
	}

	// AcceptEdit commits new query and returns caller mode.
	_, _ = reg.BeginEdit("work.table", "normal")
	tbl.Filter("final", MatchSubstring)
	if got := reg.AcceptEdit("work.table"); got != "normal" {
		t.Fatalf("AcceptEdit returnMode = %q, want normal", got)
	}
	if tbl.FilterQuery() != "final" {
		t.Fatalf("AcceptEdit query = %q, want final", tbl.FilterQuery())
	}

	// ClearFilter empties query cleanly.
	if !reg.ClearFilter("work.table") || tbl.IsFiltered() {
		t.Fatalf("ClearFilter failed: q=%q", tbl.FilterQuery())
	}
}

func TestModeStackPushPopAndFallback(t *testing.T) {
	var st ModeStack
	if got := st.Pop("normal"); got != "normal" {
		t.Fatalf("empty Pop = %q, want normal", got)
	}
	st.Push("outline")
	st.Push("filter")
	if st.Len() != 2 || st.Peek() != "filter" {
		t.Fatalf("unexpected stack state: len=%d peek=%q", st.Len(), st.Peek())
	}
	if got := st.Pop("normal"); got != "filter" {
		t.Fatalf("first Pop = %q, want filter", got)
	}
	if got := st.Pop("normal"); got != "outline" {
		t.Fatalf("second Pop = %q, want outline", got)
	}
	if got := st.Pop("normal"); got != "normal" {
		t.Fatalf("fallback Pop = %q, want normal", got)
	}
}
