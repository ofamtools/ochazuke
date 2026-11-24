package traits

import (
	"maps"

	"ofam.dev/ochazuke/core"
)

// Canonical object membership kinds for MotionNode.Objects.
const (
	ObjectSection = "section"
	ObjectBlock   = "block"
	ObjectItem    = "item"
	ObjectRow     = "row"
	ObjectCol     = "col"
	ObjectParent  = "parent"
	ObjectCell    = "cell"
)

// Canonical command IDs handled by MotionStep.
const (
	CmdMotionObjectNext   = "motion.object.next"
	CmdMotionObjectPrev   = "motion.object.prev"
	CmdMotionSameNext     = "motion.same.next"
	CmdMotionSamePrev     = "motion.same.prev"
	CmdMotionObjectSelect = "motion.object.select"
)

// MotionNode describes one navigable element with its geometry span and
// pure key-value object memberships (e.g. {"section": "sec-1", "block": "list-1",
// "parent": "t-1", "col": "c2"}).
type MotionNode struct {
	ID      string            `json:"id"`
	Span    ItemSpan          `json:"span"`
	Objects map[string]string `json:"objects,omitempty"`
}

// Object returns the node's membership value for kind, or "".
func (n MotionNode) Object(kind string) string {
	if kind == "" || len(n.Objects) == 0 {
		return ""
	}
	return n.Objects[kind]
}

// ObjectRange describes the contiguous index range [Start, End] (inclusive)
// of nodes sharing the same non-empty Object(Kind) value ID.
type ObjectRange struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// MotionConfig configures pure object and relative motion defaults.
type MotionConfig struct {
	Wrap           bool   `json:"wrap,omitempty"`
	DefaultObject  string `json:"defaultObject,omitempty"`  // defaults to ObjectBlock ("block")
	SectionObject  string `json:"sectionObject,omitempty"`  // defaults to ObjectSection ("section")
	RelativeObject string `json:"relativeObject,omitempty"` // e.g. ObjectParent ("parent") or ObjectCol ("col")
}

func clampMotionIndex(cur, n int) int {
	if n <= 0 {
		return 0
	}
	return min(max(cur, 0), n-1)
}

func motionDefaultObject(cfg MotionConfig) string {
	if cfg.DefaultObject != "" {
		return cfg.DefaultObject
	}
	return ObjectBlock
}

func motionSectionObject(cfg MotionConfig) string {
	if cfg.SectionObject != "" {
		return cfg.SectionObject
	}
	return ObjectSection
}

// CurrentObject returns the contiguous index range [Start, End] (inclusive) of
// nodes around cur that share the same non-empty Object(kind) value.
func CurrentObject(nodes []MotionNode, cur int, kind string) (ObjectRange, bool) {
	if len(nodes) == 0 || cur < 0 || cur >= len(nodes) || kind == "" {
		return ObjectRange{}, false
	}
	curVal := nodes[cur].Object(kind)
	if curVal == "" {
		return ObjectRange{}, false
	}
	start := cur
	for start > 0 && nodes[start-1].Object(kind) == curVal {
		start--
	}
	end := cur
	for end+1 < len(nodes) && nodes[end+1].Object(kind) == curVal {
		end++
	}
	return ObjectRange{
		Kind:  kind,
		ID:    curVal,
		Start: start,
		End:   end,
	}, true
}

// NextObject jumps forward count times from cur to the start of the next
// distinct non-empty object of kind.
func NextObject(nodes []MotionNode, cur int, kind string, count int, wrap bool) (target int, ok bool) {
	if len(nodes) == 0 {
		return 0, false
	}
	startCur := clampMotionIndex(cur, len(nodes))
	if kind == "" {
		return startCur, false
	}
	count = max(1, count)
	idx := startCur
	for range count {
		curVal := nodes[idx].Object(kind)
		found := -1
		for i := idx + 1; i < len(nodes); i++ {
			if v := nodes[i].Object(kind); v != "" && v != curVal {
				found = i
				break
			}
		}
		if found < 0 && wrap {
			for i := 0; i < idx; i++ {
				if v := nodes[i].Object(kind); v != "" && v != curVal {
					found = i
					break
				}
			}
		}
		if found < 0 {
			break
		}
		idx = found
	}
	return idx, idx != startCur
}

// PrevObject jumps backward count times from cur. If strictly inside an object
// of kind, the first step lands on the start of the current object; otherwise
// it jumps to the start of the nearest preceding distinct non-empty object.
func PrevObject(nodes []MotionNode, cur int, kind string, count int, wrap bool) (target int, ok bool) {
	if len(nodes) == 0 {
		return 0, false
	}
	startCur := clampMotionIndex(cur, len(nodes))
	if kind == "" {
		return startCur, false
	}
	count = max(1, count)
	idx := startCur
	for range count {
		if rng, has := CurrentObject(nodes, idx, kind); has && idx > rng.Start {
			idx = rng.Start
			continue
		}
		curVal := nodes[idx].Object(kind)
		found := -1
		for i := idx - 1; i >= 0; i-- {
			if v := nodes[i].Object(kind); v != "" && v != curVal {
				found = i
				break
			}
		}
		if found < 0 && wrap {
			for i := len(nodes) - 1; i > idx; i-- {
				if v := nodes[i].Object(kind); v != "" && v != curVal {
					found = i
					break
				}
			}
		}
		if found < 0 {
			break
		}
		prevRng, _ := CurrentObject(nodes, found, kind)
		idx = prevRng.Start
	}
	return idx, idx != startCur
}

// NextSame jumps forward count times from cur to the next node sharing the
// exact same non-empty Object(kind) value.
func NextSame(nodes []MotionNode, cur int, kind string, count int, wrap bool) (target int, ok bool) {
	if len(nodes) == 0 {
		return 0, false
	}
	startCur := clampMotionIndex(cur, len(nodes))
	if kind == "" {
		return startCur, false
	}
	curVal := nodes[startCur].Object(kind)
	if curVal == "" {
		return startCur, false
	}
	count = max(1, count)
	idx := startCur
	for range count {
		found := -1
		for i := idx + 1; i < len(nodes); i++ {
			if nodes[i].Object(kind) == curVal {
				found = i
				break
			}
		}
		if found < 0 && wrap {
			for i := 0; i < idx; i++ {
				if nodes[i].Object(kind) == curVal {
					found = i
					break
				}
			}
		}
		if found < 0 {
			break
		}
		idx = found
	}
	return idx, idx != startCur
}

// PrevSame jumps backward count times from cur to the previous node sharing
// the exact same non-empty Object(kind) value.
func PrevSame(nodes []MotionNode, cur int, kind string, count int, wrap bool) (target int, ok bool) {
	if len(nodes) == 0 {
		return 0, false
	}
	startCur := clampMotionIndex(cur, len(nodes))
	if kind == "" {
		return startCur, false
	}
	curVal := nodes[startCur].Object(kind)
	if curVal == "" {
		return startCur, false
	}
	count = max(1, count)
	idx := startCur
	for range count {
		found := -1
		for i := idx - 1; i >= 0; i-- {
			if nodes[i].Object(kind) == curVal {
				found = i
				break
			}
		}
		if found < 0 && wrap {
			for i := len(nodes) - 1; i > idx; i-- {
				if nodes[i].Object(kind) == curVal {
					found = i
					break
				}
			}
		}
		if found < 0 {
			break
		}
		idx = found
	}
	return idx, idx != startCur
}

// MotionStep dispatches the five canonical object and relative motion commands
// against nodes from cur under cfg.
func MotionStep(nodes []MotionNode, cur int, cfg MotionConfig, cmd core.Command) (target int, selection *ObjectRange, moved bool, handled bool) {
	kind := cmd.Arg("object")
	switch cmd.ID {
	case CmdMotionObjectNext:
		if kind == "" {
			kind = motionDefaultObject(cfg)
		}
		next, ok := NextObject(nodes, cur, kind, cmd.Times(), cfg.Wrap)
		return next, nil, ok, true
	case CmdMotionObjectPrev:
		if kind == "" {
			kind = motionDefaultObject(cfg)
		}
		next, ok := PrevObject(nodes, cur, kind, cmd.Times(), cfg.Wrap)
		return next, nil, ok, true
	case CmdMotionSameNext:
		if kind == "" {
			kind = cfg.RelativeObject
		}
		next, ok := NextSame(nodes, cur, kind, cmd.Times(), cfg.Wrap)
		return next, nil, ok, true
	case CmdMotionSamePrev:
		if kind == "" {
			kind = cfg.RelativeObject
		}
		next, ok := PrevSame(nodes, cur, kind, cmd.Times(), cfg.Wrap)
		return next, nil, ok, true
	case CmdMotionObjectSelect:
		if kind == "" {
			kind = motionDefaultObject(cfg)
		}
		if len(nodes) == 0 {
			return 0, nil, false, true
		}
		idx := clampMotionIndex(cur, len(nodes))
		rng, ok := CurrentObject(nodes, idx, kind)
		if !ok {
			return idx, nil, false, true
		}
		return idx, &rng, true, true
	default:
		return cur, nil, false, false
	}
}

func hasDistinctObjectBefore(nodes []MotionNode, idx int, kind string) bool {
	if kind == "" || idx <= 0 || idx >= len(nodes) {
		return false
	}
	curVal := nodes[idx].Object(kind)
	for i := idx - 1; i >= 0; i-- {
		if v := nodes[i].Object(kind); v != "" && v != curVal {
			return true
		}
	}
	return false
}

func hasDistinctObjectAfter(nodes []MotionNode, idx int, kind string) bool {
	if kind == "" || idx < 0 || idx >= len(nodes)-1 {
		return false
	}
	curVal := nodes[idx].Object(kind)
	for i := idx + 1; i < len(nodes); i++ {
		if v := nodes[i].Object(kind); v != "" && v != curVal {
			return true
		}
	}
	return false
}

// MotionFacts derives truthful read-only object motion facts for cur in nodes
// under cfg.
func MotionFacts(nodes []MotionNode, cur int, cfg MotionConfig) map[string]any {
	total := len(nodes)
	if total == 0 {
		return map[string]any{
			"index":           0,
			"total":           0,
			"empty":           true,
			"currentBlock":    "",
			"currentSection":  "",
			"currentRelative": "",
			"objects":         map[string]string{},
			"atFirstBlock":    true,
			"atLastBlock":     true,
			"atFirstSection":  true,
			"atLastSection":   true,
		}
	}
	idx := clampMotionIndex(cur, total)
	blockKind := motionDefaultObject(cfg)
	sectionKind := motionSectionObject(cfg)
	node := nodes[idx]
	objs := make(map[string]string, len(node.Objects))
	maps.Copy(objs, node.Objects)
	return map[string]any{
		"index":           idx,
		"total":           total,
		"empty":           false,
		"currentBlock":    node.Object(blockKind),
		"currentSection":  node.Object(sectionKind),
		"currentRelative": node.Object(cfg.RelativeObject),
		"objects":         objs,
		"atFirstBlock":    !hasDistinctObjectBefore(nodes, idx, blockKind),
		"atLastBlock":     !hasDistinctObjectAfter(nodes, idx, blockKind),
		"atFirstSection":  !hasDistinctObjectBefore(nodes, idx, sectionKind),
		"atLastSection":   !hasDistinctObjectAfter(nodes, idx, sectionKind),
	}
}

// MotionCommandSpecs returns the CommandSpecs for the five pure object and
// relative motion commands so hosts can register them with a single call.
func MotionCommandSpecs() []core.CommandSpec {
	objParam := core.ParamSpec{
		Name:        "object",
		Type:        core.ParamString,
		Description: "Object membership kind (e.g. block, section, row, col, parent)",
	}
	return []core.CommandSpec{
		{
			ID:          CmdMotionObjectNext,
			Description: "Jump to the next distinct object",
			Params:      []core.ParamSpec{objParam},
			Tags:        []string{"motion", "repeatable"},
		},
		{
			ID:          CmdMotionObjectPrev,
			Description: "Jump to the start of the current or previous distinct object",
			Params:      []core.ParamSpec{objParam},
			Tags:        []string{"motion", "repeatable"},
		},
		{
			ID:          CmdMotionSameNext,
			Description: "Jump to the next node sharing the same object membership",
			Params:      []core.ParamSpec{objParam},
			Tags:        []string{"motion", "repeatable"},
		},
		{
			ID:          CmdMotionSamePrev,
			Description: "Jump to the previous node sharing the same object membership",
			Params:      []core.ParamSpec{objParam},
			Tags:        []string{"motion", "repeatable"},
		},
		{
			ID:          CmdMotionObjectSelect,
			Description: "Select the contiguous range of the current object",
			Params:      []core.ParamSpec{objParam},
			Tags:        []string{"motion"},
		},
	}
}
