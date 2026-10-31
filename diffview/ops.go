package diffview

import (
	_ "embed"
	"errors"

	"ofam.dev/ochazuke"
)

//go:embed bindings.json
var defaultBindings []byte

// Register defines verbs, file navigation events and binding rows once per
// runtime. New calls it; a host can call it before loading user bindings.
func Register(r *ochazuke.Runtime) {
	if _, ok := r.Operations[Noun+".hunk"]; ok {
		return
	}
	r.Builtin(func() {
		r.Define(Noun).Focusable()
		_ = r.DeclareEvent(ochazuke.EventSpec{Name: EventFile, Source: ochazuke.FrameworkNamespace, Description: "Diff requested another file; the host loads it", Fields: []ochazuke.EventField{{Name: "id", Type: "string"}, {Name: "delta", Type: "integer"}}})
		op := func(verb, desc string, params map[string]ochazuke.Parameter, fn func(*View, ochazuke.Arguments)) {
			r.Register(ochazuke.Operation{Noun: Noun, Verb: verb, Description: desc, Motion: true, Parameters: params, Result: "diff state", Run: func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
				var v *View
				for _, id := range c.Path {
					if v = Of(r, id); v != nil {
						break
					}
				}
				if v == nil {
					return nil, errors.New("no focused diff")
				}
				fn(v, a)
				return v.Snapshot(), nil
			}})
		}
		params := map[string]ochazuke.Parameter{"delta": {Type: "integer", Description: "Direction or lines, default 1"}, "count": {Type: "integer", Description: "Repeat count, default 1"}}
		delta := func(a ochazuke.Arguments) int {
			return ochazuke.IntArg(a, "delta", 1) * max(1, ochazuke.IntArg(a, "count", 1))
		}
		op("hunk", "Jump to the next or previous hunk start, without wrapping", params, func(v *View, a ochazuke.Arguments) { v.Hunk(delta(a)) })
		op("file", "Ask the host for the next or previous file", params, func(v *View, a ochazuke.Arguments) {
			d := delta(a)
			r.Emit(EventFile, map[string]any{"id": v.id, "delta": d})
			if v.OnFileChange != nil {
				v.OnFileChange(d)
			}
		})
		op("move", "Move by display rows, following the half-page rule", params, func(v *View, a ochazuke.Arguments) { v.Move(delta(a)) })
		scrollParams := map[string]ochazuke.Parameter{"delta": {Type: "integer"}, "count": {Type: "integer"}, "half": {Type: "boolean", Description: "Scroll by half the window"}}
		op("scroll", "Scroll the diff window by lines or half pages", scrollParams, func(v *View, a ochazuke.Arguments) {
			d := delta(a)
			if a["half"] == true {
				d *= max(1, v.height/2)
			}
			v.ScrollLines(d)
		})
		op("scroll-columns", "Scroll source text horizontally, preserving line numbers", params, func(v *View, a ochazuke.Arguments) { v.ScrollColumns(delta(a)) })
		op("show", "Describe the diff viewport", nil, func(*View, ochazuke.Arguments) {})
		r.MustLoadDefaultBindings(ochazuke.FrameworkBindingsSource+"diffview/bindings.json", defaultBindings)
	})
}
