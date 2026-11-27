package tree

import (
	"encoding/binary"
	"hash/maphash"

	"ofam.dev/ochazuke/ui"
)

var treeHashSeed = maphash.MakeSeed()

type treeHasher struct {
	h   maphash.Hash
	buf [8]byte
}

func newTreeHasher() treeHasher {
	var th treeHasher
	th.h.SetSeed(treeHashSeed)
	return th
}

func (h *treeHasher) writeByte(b byte) {
	_ = h.h.WriteByte(b)
}

func (h *treeHasher) writeBool(b bool) {
	if b {
		_ = h.h.WriteByte(1)
		return
	}
	_ = h.h.WriteByte(0)
}

func (h *treeHasher) writeInt(n int) {
	binary.LittleEndian.PutUint64(h.buf[:], uint64(n))
	_, _ = h.h.Write(h.buf[:])
}

func (h *treeHasher) writeUint64(u uint64) {
	binary.LittleEndian.PutUint64(h.buf[:], u)
	_, _ = h.h.Write(h.buf[:])
}

func (h *treeHasher) writeString(s string) {
	h.writeInt(len(s))
	_, _ = h.h.WriteString(s)
}

func (h *treeHasher) sum64() uint64 {
	return h.h.Sum64()
}

func (h *treeHasher) writeBadges(bs []ui.Badge) {
	h.writeInt(len(bs))
	for _, b := range bs {
		h.writeString(b.Text)
		h.writeString(b.Tone)
	}
}

func (h *treeHasher) writeTreeMeta(ms []ui.TreeMeta) {
	h.writeInt(len(ms))
	for _, mt := range ms {
		h.writeString(mt.Text)
		h.writeString(mt.Tone)
	}
}

func (h *treeHasher) writeToneSpans(sps []ui.ToneSpan) {
	h.writeInt(len(sps))
	for _, sp := range sps {
		h.writeInt(sp.From)
		h.writeInt(sp.To)
		h.writeString(sp.Tone)
		h.writeString(sp.Bg)
	}
}

func (h *treeHasher) writeInlineRuns(runs []ui.InlineRun) {
	h.writeInt(len(runs))
	for i := range runs {
		r := &runs[i]
		h.writeString(r.Kind)
		h.writeString(r.Text)
		h.writeString(r.Label)
		h.writeString(r.Tone)
		h.writeString(r.Bg)
		h.writeString(r.Href)
		h.writeString(r.Command)
		if r.Ref != nil {
			h.writeByte(1)
			h.writeString(r.Ref.Kind)
			h.writeString(r.Ref.ID)
		} else {
			h.writeByte(0)
		}
	}
}

func (h *treeHasher) writeEditState(e *EditState) {
	if e == nil {
		h.writeByte(0)
		return
	}
	h.writeByte(1)
	h.writeString(string(e.Kind))
	h.writeString(e.TargetID)
	h.writeString(e.ParentID)
	h.writeString(e.AfterID)
	h.writeString(e.BeforeID)
	h.writeString(e.Prefix)
	h.writeInt(len(e.Line.Text))
	for _, r := range e.Line.Text {
		h.writeInt(int(r))
	}
	h.writeInt(e.Line.Cursor)
}

func mixKey64(s string) uint64 {
	x := maphash.String(treeHashSeed, s)
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func (h *treeHasher) writeBoolMapTrue(m map[string]bool) {
	if len(m) == 0 {
		h.writeInt(0)
		return
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
	h.writeInt(count)
	h.writeUint64(sum)
	h.writeUint64(xor)
}

func hashModelNodes(h *treeHasher, ns []Node, folded, marked map[string]bool) {
	h.writeInt(len(ns))
	for i := range ns {
		nd := &ns[i]
		h.writeString(nd.ID)
		h.writeString(nd.Label)
		h.writeString(nd.Scope)
		if nd.Ref != nil {
			h.writeByte(1)
			h.writeString(nd.Ref.Kind)
			h.writeString(nd.Ref.ID)
		} else {
			h.writeByte(0)
		}
		h.writeString(nd.Tone)
		h.writeString(nd.Gutter)
		h.writeString(nd.GutterTone)
		h.writeString(nd.Icon)
		h.writeString(nd.IconTone)
		h.writeString(nd.Badge)
		h.writeBool(nd.Unread)
		h.writeBool(folded[nd.ID])
		h.writeBool(marked[nd.ID])
		h.writeBadges(nd.Badges)
		h.writeTreeMeta(nd.Meta)
		h.writeToneSpans(nd.LabelSpans)
		h.writeInlineRuns(nd.LabelRuns)
		if len(nd.Children) > 0 {
			hashModelNodes(h, nd.Children, folded, marked)
		}
	}
}

func hashUINodes(h *treeHasher, ns []ui.TreeNode) {
	h.writeInt(len(ns))
	for i := range ns {
		nd := &ns[i]
		exp := true
		if nd.Expanded != nil {
			exp = *nd.Expanded
		}
		h.writeString(nd.Label)
		h.writeBool(exp)
		h.writeString(nd.Tone)
		h.writeString(nd.Gutter)
		h.writeString(nd.GutterTone)
		h.writeString(nd.Icon)
		h.writeString(nd.IconTone)
		h.writeString(nd.Badge)
		h.writeBool(nd.Unread)
		h.writeBool(nd.Editing)
		h.writeString(nd.EditPrefix)
		h.writeString(nd.EditValue)
		h.writeInt(nd.EditCursor)
		h.writeBadges(nd.Badges)
		h.writeTreeMeta(nd.Meta)
		h.writeToneSpans(nd.LabelSpans)
		h.writeInlineRuns(nd.LabelRuns)
		if len(nd.Children) > 0 {
			hashUINodes(h, nd.Children)
		}
	}
}

type treeViewCache struct {
	contentSig  uint64
	contentHash uint64
	rows        []VisibleRow
	uiNodes     []ui.TreeNode
	uiValid     bool
}

func (m *Model) contentSignature(roots []Node, filterErr string) uint64 {
	h := newTreeHasher()
	h.writeBool(m.Guides)
	h.writeBool(m.GapTopLevel)
	h.writeBool(m.ShowFoldCount)
	h.writeBool(m.ShowGutter)
	h.writeBool(m.Wrap)
	h.writeString(m.HoistID())
	h.writeString(m.Query)
	h.writeString(string(m.MatchMode))
	h.writeString(filterErr)
	h.writeBool(m.IsFiltered())
	h.writeBoolMapTrue(m.Marked)
	h.writeEditState(m.edit)
	hashModelNodes(&h, roots, m.Folded, m.Marked)
	return h.sum64()
}

func (m *Model) hashStaticContent(filterErr string, uiNodes []ui.TreeNode) uint64 {
	h := newTreeHasher()
	h.writeBool(m.Guides)
	h.writeBool(m.GapTopLevel)
	h.writeBool(m.ShowFoldCount)
	h.writeBool(m.ShowGutter)
	h.writeBool(m.Wrap)
	h.writeString(m.HoistID())
	h.writeBoolMapTrue(m.Marked)
	h.writeString(m.Query)
	h.writeString(string(m.MatchMode))
	h.writeString(filterErr)
	hashUINodes(&h, uiNodes)
	return h.sum64()
}

func (m *Model) cachedRows(roots []Node, filterErr string) ([]VisibleRow, uint64) {
	sig := m.contentSignature(roots, filterErr)
	if m.viewCache != nil && m.viewCache.contentSig == sig {
		return m.viewCache.rows, sig
	}
	rows := m.flattenRoots(roots)
	m.viewCache = &treeViewCache{
		contentSig: sig,
		rows:       rows,
	}
	return rows, sig
}

func (m *Model) cachedViewData(roots []Node, filterErr string) ([]VisibleRow, []ui.TreeNode, uint64) {
	rows, _ := m.cachedRows(roots, filterErr)
	if m.viewCache.uiValid {
		return rows, m.viewCache.uiNodes, m.viewCache.contentHash
	}
	uiNodes := m.buildUINodes(roots, "")
	contentHash := m.hashStaticContent(filterErr, uiNodes)
	m.viewCache.uiNodes = uiNodes
	m.viewCache.contentHash = contentHash
	m.viewCache.uiValid = true
	return rows, uiNodes, contentHash
}

func combineViewCacheKey(contentHash uint64, selIdx, offset int, vOK bool, vFrom, vTo int, visActive bool, visAnchor int) uint64 {
	h := newTreeHasher()
	h.writeUint64(contentHash)
	h.writeInt(selIdx)
	h.writeInt(offset)
	h.writeBool(vOK)
	h.writeInt(vFrom)
	h.writeInt(vTo)
	h.writeBool(visActive)
	h.writeInt(visAnchor)
	return h.sum64() | 1
}
