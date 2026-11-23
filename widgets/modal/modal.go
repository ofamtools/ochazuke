// Package modal provides a pure modal overlay stack synchronized with
// core.State.Modals and reusable Modal adapters for Ochazuke's built-in
// widgets (picker, form, confirm, markdown, palette, pathpicker, commandline).
//
// It performs no I/O, starts no goroutines, and never imports Bubble Tea or
// Lip Gloss.
package modal

import (
	"maps"
	"time"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/toast"
)

// Shared modal command IDs.
const (
	CmdCancel          = "modal.cancel"
	CmdConfirmAccept   = "confirm.accept"
	CmdConfirmCancel   = "confirm.cancel"
	CmdPickerInsert    = "picker.insert"
	CmdPickerBackspace = "picker.backspace"
	CmdFormActivate    = "form.activate"
)

// Standard modal mode names.
const (
	ModePicker      = "picker"
	ModeForm        = "form"
	ModeFormEdit    = "form-edit"
	ModeConfirm     = "confirm"
	ModeReader      = "reader"
	ModeCommandLine = "commandline"
)

// CloseReason explains why a modal was dismissed.
type CloseReason uint8

// Modal close reasons.
const (
	CloseCancel CloseReason = iota
	CloseSubmit
	CloseDropAll
)

// String returns the human-readable name of r.
func (r CloseReason) String() string {
	switch r {
	case CloseCancel:
		return "cancel"
	case CloseSubmit:
		return "submit"
	case CloseDropAll:
		return "drop-all"
	default:
		return "unknown"
	}
}

// Modal is one active modal overlay managed by a Stack.
type Modal interface {
	ID() string
	Mode() string
	Scope() string
	FocusID() string
	View(w, h int) ui.ViewNode
	Handle(cmd core.Command) Result
	OnClose(reason CloseReason)
}

// ToastSpec describes a transient toast notification emitted by a modal result.
type ToastSpec struct {
	Text  string
	Level toast.Level
	TTL   time.Duration
}

// Result is the structured outcome returned by Modal.Handle.
type Result struct {
	Handled     bool
	Close       bool
	CloseReason CloseReason
	Push        Modal
	Mode        string
	FocusID     string
	Status      string
	SetStatus   bool
	Toast       *ToastSpec
	Emit        []core.Command
	Effects     []core.Effect
}

// OverlaySpec describes a ui.KindOverlay wrapper built by Overlay.
type OverlaySpec struct {
	ID        string
	Scope     string
	Title     string
	Tone      string
	WidthPct  int
	HeightPct int
	Badges    []ui.Badge
	Ref       *core.Ref
	Facts     map[string]any
	Children  []ui.ViewNode
}

// Overlay wraps spec.Children in a ui.KindOverlay node. Facts from descendant
// children are mirrored underneath spec.Facts so core.Keymap bindings targeted
// at the overlay's scope can inspect inner widget facts.
func Overlay(spec OverlaySpec) ui.ViewNode {
	scope := spec.Scope
	if scope == "" {
		scope = spec.ID
	}
	if scope == "" {
		scope = "modal"
	}

	var props map[string]any
	if spec.WidthPct > 0 || spec.HeightPct > 0 {
		props = map[string]any{}
		if spec.WidthPct > 0 {
			props["widthPct"] = spec.WidthPct
		}
		if spec.HeightPct > 0 {
			props["heightPct"] = spec.HeightPct
		}
	}

	facts := collectChildFacts(nil, spec.Children)
	if len(spec.Facts) > 0 {
		if facts == nil {
			facts = make(map[string]any, len(spec.Facts))
		}
		maps.Copy(facts, spec.Facts)
	}

	var ref *core.Ref
	if spec.Ref != nil {
		r := *spec.Ref
		ref = &r
	}

	return ui.ViewNode{
		ID:       "modal." + spec.ID,
		Kind:     ui.KindOverlay,
		Scope:    scope,
		Ref:      ref,
		Facts:    facts,
		Title:    spec.Title,
		Tone:     spec.Tone,
		Badges:   spec.Badges,
		Props:    props,
		Children: spec.Children,
	}
}

func collectChildFacts(dst map[string]any, nodes []ui.ViewNode) map[string]any {
	for i := range nodes {
		n := &nodes[i]
		if len(n.Children) > 0 {
			dst = collectChildFacts(dst, n.Children)
		}
		if len(n.Facts) > 0 {
			if dst == nil {
				dst = make(map[string]any, len(n.Facts))
			}
			maps.Copy(dst, n.Facts)
		}
	}
	return dst
}
