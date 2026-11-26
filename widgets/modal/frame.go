package modal

import (
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

// Frame holds the shared chrome, identity, sizing, and metadata for any modal
// overlay. Built-in modal adapters and custom app overlays both populate a
// Frame and delegate to BuildOverlay, Frame.Overlay, or FitHeightOverlay.
type Frame struct {
	ID        string
	Scope     string
	Title     string
	Tone      string
	WidthPct  int
	HeightPct int
	Ref       *core.Ref
	Badges    []ui.Badge
	Facts     map[string]any
}

// WithDefaults returns a copy of f with zero fields filled from defaults.
func (f Frame) WithDefaults(defaults Frame) Frame {
	if f.ID == "" {
		f.ID = defaults.ID
	}
	if f.Scope == "" {
		f.Scope = defaults.Scope
	}
	if f.Title == "" {
		f.Title = defaults.Title
	}
	if f.Tone == "" {
		f.Tone = defaults.Tone
	}
	if f.WidthPct <= 0 {
		f.WidthPct = defaults.WidthPct
	}
	if f.HeightPct <= 0 {
		f.HeightPct = defaults.HeightPct
	}
	if f.Ref == nil {
		f.Ref = defaults.Ref
	}
	if len(f.Badges) == 0 && len(defaults.Badges) > 0 {
		f.Badges = defaults.Badges
	}
	if len(f.Facts) == 0 && len(defaults.Facts) > 0 {
		f.Facts = defaults.Facts
	}
	return f
}

// Overlay wraps children in a ui.KindOverlay using f's chrome and metadata.
func (f Frame) Overlay(children ...ui.ViewNode) ui.ViewNode {
	return Overlay(OverlaySpec{
		ID:        f.ID,
		Scope:     f.Scope,
		Title:     f.Title,
		Tone:      f.Tone,
		WidthPct:  f.WidthPct,
		HeightPct: f.HeightPct,
		Badges:    f.Badges,
		Ref:       f.Ref,
		Facts:     f.Facts,
		Children:  children,
	})
}

// BuildOverlay wraps body in a ui.KindOverlay using frame's chrome and metadata.
func BuildOverlay(frame Frame, body ui.ViewNode) ui.ViewNode {
	return frame.Overlay(body)
}

// FitHeightOverlay builds a content-fitted ui.KindOverlay whose height shrinks
// to body's natural height instead of stretching to a fixed HeightPct. When
// width and height are positive and natural content height would exceed the
// available viewport (leaving top/bottom chrome clear on terminals >= 6 rows),
// the overlay height is capped to that bound and body is placed in a
// ui.KindScroll viewport.
func FitHeightOverlay(frame Frame, body ui.ViewNode, width, height int) ui.ViewNode {
	frame.HeightPct = 0
	ov := BuildOverlay(frame, body)
	if width <= 0 || height <= 0 {
		return ov
	}
	maxH := height
	if height >= 6 {
		maxH = height - 2
	}
	solved := ui.Measure(ov, ui.Theme{}, "", width, max(height, maxH+1))
	if r, ok := solved.Rects[ov.ID]; ok && r.H > maxH {
		if body.Kind != ui.KindScroll {
			scrollID := ""
			if frame.ID != "" {
				scrollID = frame.ID + ".scroll"
			}
			ov.Children = []ui.ViewNode{{
				ID:       scrollID,
				Kind:     ui.KindScroll,
				Children: []ui.ViewNode{body},
			}}
		}
		ov.Box.FixedH = maxH
	}
	return ov
}

// resolveModalIdentity normalizes modal ID, Scope, and FocusID defaults once
// for built-in modal adapters. When defScope is empty, Scope defaults to id.
func resolveModalIdentity(id, scope, focusID, defID, defScope, focusSuffix string) (string, string, string) {
	if id == "" {
		id = defID
	}
	if scope == "" {
		if defScope != "" {
			scope = defScope
		} else {
			scope = id
		}
	}
	if focusID == "" {
		focusID = id + focusSuffix
	}
	return id, scope, focusID
}
