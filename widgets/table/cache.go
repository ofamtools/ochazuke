package table

import (
	"slices"
	"sort"
	"strings"
	"unsafe"
)

type filterSignature struct {
	rowsPtr     uintptr
	rowsLen     int
	rowsCap     int
	firstID     string
	lastID      string
	query       string
	filterField string
	matchMode   MatchMode
	colKeysHash uint64
	sortHash    uint64
}

type itemsSignature struct {
	filterVer      uint64
	groupField     string
	collapsedHash  uint64
	collapsedCount int
}

type viewSignature struct {
	itemsVer    uint64
	colsHash    uint64
	markedHash  uint64
	markedCount int
}

type tableCache struct {
	filterValid bool
	filterSig   filterSignature
	filterVer   uint64
	filtered    []Row
	filteredIdx []int // nil means identity (filtered[i] == m.Rows[i])
	filterErr   string

	itemsValid bool
	itemsSig   itemsSignature
	itemsVer   uint64
	items      []Item
	itemRowIdx []int // nil means identity; -1 for group header

	viewValid bool
	viewSig   viewSignature
	cols      []any
	colKeys   []string
	colsHash  uint64
	rows      []any
	cellSlab  []any
}

func (m *Model) ensureCache() *tableCache {
	if m.cache == nil {
		m.cache = &tableCache{}
	}
	return m.cache
}

func sliceDataPtr(rows []Row) uintptr {
	if len(rows) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&rows[0]))
}

func hashColumnKeys(cols []Column) uint64 {
	h := newFNV64a()
	h.writeInt(len(cols))
	for _, c := range cols {
		h.writeString(c.Key)
	}
	return uint64(h)
}

func hashSortConfig(sortField string, sortDir SortDir, sortKeys []SortKey) uint64 {
	h := newFNV64a()
	h.writeString(sortField)
	h.writeInt(int(sortDir))
	h.writeInt(len(sortKeys))
	for _, k := range sortKeys {
		h.writeString(k.Field)
		h.writeInt(int(k.Dir))
	}
	return uint64(h)
}

func mixKey64(s string) uint64 {
	h := newFNV64a()
	h.writeString(s)
	x := uint64(h)
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// hashBoolMapTrue returns an order-independent, zero-allocation fingerprint of
// the keys mapped to true in m, along with their count.
func hashBoolMapTrue(m map[string]bool) (uint64, int) {
	if len(m) == 0 {
		return 0, 0
	}
	var sum, xor uint64
	count := 0
	for k, v := range m {
		if !v {
			continue
		}
		mk := mixKey64(k)
		sum += mk
		xor ^= mk
		count++
	}
	h := newFNV64a()
	h.writeInt(count)
	h.writeInt(int(sum))
	h.writeInt(int(xor))
	return uint64(h), count
}

func (m *Model) currentFilterSig() filterSignature {
	sig := filterSignature{
		rowsPtr:     sliceDataPtr(m.Rows),
		rowsLen:     len(m.Rows),
		rowsCap:     cap(m.Rows),
		query:       m.Query,
		filterField: m.FilterField,
		matchMode:   m.MatchMode,
		colKeysHash: hashColumnKeys(m.Columns),
		sortHash:    hashSortConfig(m.SortField, m.SortDir, m.SortKeys),
	}
	if n := len(m.Rows); n > 0 {
		sig.firstID = m.Rows[0].ID
		sig.lastID = m.Rows[n-1].ID
	}
	return sig
}

func (m *Model) cachedFilteredWithIdx() ([]Row, []int) {
	c := m.ensureCache()
	sig := m.currentFilterSig()
	if c.filterValid && c.filterSig == sig {
		m.FilterErr = c.filterErr
		if len(m.SortKeys) > 0 {
			m.syncPrimarySort()
		}
		return c.filtered, c.filteredIdx
	}
	rows, idx := m.computeFiltered()
	c.filterValid = true
	c.filterSig = sig
	c.filterVer++
	c.filtered = rows
	c.filteredIdx = idx
	c.filterErr = m.FilterErr
	c.itemsValid = false
	c.viewValid = false
	return rows, idx
}

func (m *Model) cachedFiltered() []Row {
	rows, _ := m.cachedFilteredWithIdx()
	return rows
}

func (m *Model) cachedItemsWithIdx() ([]Item, []int) {
	rows, rowIdx := m.cachedFilteredWithIdx()
	c := m.cache
	var colHash uint64
	var colCount int
	if m.GroupField != "" {
		colHash, colCount = hashBoolMapTrue(m.Collapsed)
	}
	sig := itemsSignature{
		filterVer:      c.filterVer,
		groupField:     m.GroupField,
		collapsedHash:  colHash,
		collapsedCount: colCount,
	}
	if c.itemsValid && c.itemsSig == sig {
		return c.items, c.itemRowIdx
	}
	items, itemIdx := m.buildItems(rows, rowIdx)
	c.itemsValid = true
	c.itemsSig = sig
	c.itemsVer++
	c.items = items
	c.itemRowIdx = itemIdx
	c.viewValid = false
	return items, itemIdx
}

func (m *Model) cachedItems() []Item {
	items, _ := m.cachedItemsWithIdx()
	return items
}

func (m *Model) buildItems(rows []Row, rowIdx []int) ([]Item, []int) {
	if m.GroupField == "" {
		out := make([]Item, len(rows))
		for i := range rows {
			out[i] = Item{Row: rows[i]}
		}
		return out, rowIdx
	}
	type grpEntry struct {
		row Row
		idx int
	}
	var order []string
	groups := map[string][]grpEntry{}
	for i, r := range rows {
		orig := i
		if rowIdx != nil {
			orig = rowIdx[i]
		}
		g := Text(r.Cells[m.GroupField])
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], grpEntry{row: r, idx: orig})
	}
	out := make([]Item, 0, len(rows)+len(order))
	outIdx := make([]int, 0, len(rows)+len(order))
	for _, g := range order {
		col := m.Collapsed[g]
		entries := groups[g]
		out = append(out, Item{Header: true, Group: g, Count: len(entries), Collapsed: col})
		outIdx = append(outIdx, -1)
		if !col {
			for _, e := range entries {
				out = append(out, Item{Group: g, Row: e.row})
				outIdx = append(outIdx, e.idx)
			}
		}
	}
	return out, outIdx
}

func (m *Model) computeFiltered() ([]Row, []int) {
	var idx []int
	unfiltered := false
	if m.Query == "" {
		m.FilterErr = ""
		unfiltered = true
	} else if m.MatchMode == "" || m.MatchMode == MatchSubstring {
		m.FilterErr = ""
		idx = m.filterSubstringIndices()
	} else {
		idx = m.filterMatcherIndices()
	}
	if len(m.SortKeys) > 0 {
		m.syncPrimarySort()
	}
	keys := m.effectiveSortKeys()
	if len(keys) == 0 {
		if unfiltered {
			return m.Rows, nil
		}
		return materializeRows(m.Rows, idx), idx
	}
	if unfiltered {
		idx = make([]int, len(m.Rows))
		for i := range idx {
			idx[i] = i
		}
	}
	sortRowIndices(m.Rows, idx, keys)
	return materializeRows(m.Rows, idx), idx
}

func materializeRows(src []Row, idx []int) []Row {
	if len(idx) == 0 {
		return nil
	}
	out := make([]Row, len(idx))
	for i, ri := range idx {
		out[i] = src[ri]
	}
	return out
}

func sortRowIndices(src []Row, idx []int, keys []SortKey) {
	sort.SliceStable(idx, func(i, j int) bool {
		ri, rj := &src[idx[i]], &src[idx[j]]
		for _, k := range keys {
			c := compare(ri.Cells[k.Field], rj.Cells[k.Field])
			if c != 0 {
				if k.Dir == SortDesc {
					return c > 0
				}
				return c < 0
			}
		}
		return ri.ID < rj.ID
	})
}

func (m *Model) filterMatcherIndices() []int {
	match := m.matcher()
	var idx []int
	for i := range m.Rows {
		if m.rowMatches(m.Rows[i], match) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *Model) filterSubstringIndices() []int {
	clauses := strings.Fields(m.Query)
	if len(clauses) == 0 {
		return m.filterMatcherIndices()
	}
	compiled := m.compileClauses(clauses)
	var idx []int
	for i := range m.Rows {
		if m.rowMatchesCompiled(&m.Rows[i], compiled) {
			idx = append(idx, i)
		}
	}
	return idx
}

// --- Zero-alloc ASCII case-insensitive substring matching ---

type foldTerm struct {
	lower       string
	ascii       bool
	specialFold bool // true when lower contains 'i' or 'k' (the only ASCII targets of non-ASCII unicode.ToLower)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func makeFoldTerm(s string) foldTerm {
	l := strings.ToLower(s)
	asc := isASCII(l)
	sp := asc && (strings.IndexByte(l, 'i') >= 0 || strings.IndexByte(l, 'k') >= 0)
	return foldTerm{lower: l, ascii: asc, specialFold: sp}
}

func asciiLowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func matchASCIIAt(haystack, lowerNeedle string, off int) bool {
	for j := 0; j < len(lowerNeedle); j++ {
		if asciiLowerByte(haystack[off+j]) != lowerNeedle[j] {
			return false
		}
	}
	return true
}

func scanFoldASCII(haystack, lowerNeedle string) bool {
	first := lowerNeedle[0]
	firstUpper := first
	if first >= 'a' && first <= 'z' {
		firstUpper = first - ('a' - 'A')
	}
	limit := len(haystack) - len(lowerNeedle)
	for i := 0; i <= limit; i++ {
		b := haystack[i]
		if (b == first || b == firstUpper) && matchASCIIAt(haystack, lowerNeedle, i) {
			return true
		}
	}
	return false
}

// hasSpecialLowerFold reports whether s contains U+0130 ('İ' -> 'i') or
// U+212A ('K' -> 'k'), the only non-ASCII runes whose unicode.ToLower is ASCII.
func hasSpecialLowerFold(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == 0xC4 && i+1 < len(s) && s[i+1] == 0xB0 {
			return true
		}
		if b == 0xE2 && i+2 < len(s) && s[i+1] == 0x84 && s[i+2] == 0xAA {
			return true
		}
	}
	return false
}

func (t foldTerm) match(haystack string) bool {
	if !t.ascii {
		return strings.Contains(strings.ToLower(haystack), t.lower)
	}
	n := len(t.lower)
	if n == 0 {
		return true
	}
	if n > len(haystack) {
		return false
	}
	if scanFoldASCII(haystack, t.lower) {
		return true
	}
	if t.specialFold && hasSpecialLowerFold(haystack) {
		return strings.Contains(strings.ToLower(haystack), t.lower)
	}
	return false
}

// --- Pre-compiled multi-condition substring filter clauses ---

type compiledFieldExpr struct {
	field      string
	lowerField string
	colKey     string
	hasColKey  bool
	neg        bool
	alts       []foldTerm
}

type compiledAlt struct {
	term      foldTerm
	fieldExpr *compiledFieldExpr
}

type compiledClause struct {
	neg       bool
	fieldExpr *compiledFieldExpr
	alts      []compiledAlt
}

func (m *Model) resolveColumnKey(field, lowerField string) (string, bool) {
	if field == "" {
		return "", false
	}
	for _, c := range m.Columns {
		if c.Key == field {
			return c.Key, true
		}
	}
	for _, c := range m.Columns {
		if strings.ToLower(c.Key) == lowerField {
			return c.Key, true
		}
	}
	return "", false
}

func (fe *compiledFieldExpr) resolveKey(r *Row) (string, bool) {
	if fe.hasColKey {
		return fe.colKey, true
	}
	if _, ok := r.Cells[fe.field]; ok {
		return fe.field, true
	}
	for k := range r.Cells {
		if strings.ToLower(k) == fe.lowerField {
			return k, true
		}
	}
	return "", false
}

func (fe *compiledFieldExpr) eval(r *Row) (res bool, resolved bool) {
	key, ok := fe.resolveKey(r)
	if !ok {
		return false, false
	}
	cell := Text(r.Cells[key])
	matched := false
	for _, alt := range fe.alts {
		if alt.match(cell) {
			matched = true
			break
		}
	}
	if fe.neg {
		return !matched, true
	}
	return matched, true
}

func (m *Model) compileFieldExpr(expr string, outerNeg, splitAlts bool) *compiledFieldExpr {
	if m.FilterField != "" {
		return nil
	}
	if idx := strings.Index(expr, "!="); idx > 0 {
		return m.buildFieldExpr(expr[:idx], expr[idx+2:], !outerNeg, splitAlts)
	}
	if idx := strings.Index(expr, ":"); idx > 0 {
		rest := expr[idx+1:]
		neg := outerNeg
		if strings.HasPrefix(rest, "!") && len(rest) > 1 {
			neg, rest = !neg, rest[1:]
		}
		return m.buildFieldExpr(expr[:idx], rest, neg, splitAlts)
	}
	return nil
}

func (m *Model) buildFieldExpr(field, rest string, neg, splitAlts bool) *compiledFieldExpr {
	lf := strings.ToLower(field)
	colKey, hasCol := m.resolveColumnKey(field, lf)
	var terms []foldTerm
	if splitAlts {
		parts := strings.Split(rest, "|")
		terms = make([]foldTerm, len(parts))
		for i, p := range parts {
			terms[i] = makeFoldTerm(p)
		}
	} else {
		terms = []foldTerm{makeFoldTerm(rest)}
	}
	return &compiledFieldExpr{
		field:      field,
		lowerField: lf,
		colKey:     colKey,
		hasColKey:  hasCol,
		neg:        neg,
		alts:       terms,
	}
}

func (m *Model) compileClauses(tokens []string) []compiledClause {
	out := make([]compiledClause, len(tokens))
	for i, tok := range tokens {
		out[i] = m.compileClause(tok)
	}
	return out
}

func (m *Model) compileClause(tok string) compiledClause {
	neg, body := stripNegPrefix(tok)
	cc := compiledClause{
		neg:       neg,
		fieldExpr: m.compileFieldExpr(body, neg, true),
	}
	parts := strings.Split(body, "|")
	cc.alts = make([]compiledAlt, 0, len(parts))
	for _, a := range parts {
		if a == "" && len(parts) > 1 {
			continue
		}
		cc.alts = append(cc.alts, compiledAlt{
			term:      makeFoldTerm(a),
			fieldExpr: m.compileFieldExpr(a, false, false),
		})
	}
	return cc
}

func stripNegPrefix(tok string) (bool, string) {
	if strings.HasPrefix(tok, "!=") && len(tok) > 2 {
		return true, tok[2:]
	}
	if strings.HasPrefix(tok, "!") && len(tok) > 1 {
		return true, tok[1:]
	}
	return false, tok
}

func (m *Model) rowContainsTerm(r *Row, t foldTerm) bool {
	if m.FilterField != "" {
		return t.match(Text(r.Cells[m.FilterField]))
	}
	for _, c := range m.Columns {
		if t.match(Text(r.Cells[c.Key])) {
			return true
		}
	}
	return false
}

func (m *Model) evalCompiledClause(r *Row, cc *compiledClause) bool {
	if cc.fieldExpr != nil {
		if res, ok := cc.fieldExpr.eval(r); ok {
			return res
		}
	}
	matched := false
	for i := range cc.alts {
		alt := &cc.alts[i]
		if alt.fieldExpr != nil {
			if res, ok := alt.fieldExpr.eval(r); ok {
				if res {
					matched = true
					break
				}
				continue
			}
		}
		if m.rowContainsTerm(r, alt.term) {
			matched = true
			break
		}
	}
	if cc.neg {
		return !matched
	}
	return matched
}

func (m *Model) rowMatchesCompiled(r *Row, clauses []compiledClause) bool {
	for i := range clauses {
		if !m.evalCompiledClause(r, &clauses[i]) {
			return false
		}
	}
	return true
}

// cloneFilteredRows clones rows preserving nil vs empty semantics.
func cloneFilteredRows(rows []Row) []Row {
	return slices.Clone(rows)
}
