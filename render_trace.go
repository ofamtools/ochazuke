//go:build ochazuke_render_trace

package ochazuke

import "maps"

// RenderTrace is a diagnostic snapshot available only in instrumented test
// builds. Frames counts render queries; Compositions counts expensive shell
// compositions after exact visible-input comparison.
type RenderTrace struct {
	Messages                                       map[string]int
	Views, Frames, Compositions, ModalCompositions int
}

// TraceRender enables test counters and returns a function taking snapshots.
// Normal builds have neither this API nor any active counters.
func (m *Model) TraceRender() func() RenderTrace {
	t := &renderTrace{messages: map[string]int{}}
	m.redraw.trace = t
	return func() RenderTrace {
		return RenderTrace{maps.Clone(t.messages), t.views, t.frames, t.compositions, t.modalCompositions}
	}
}
