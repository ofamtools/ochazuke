package ochazuke

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// FocusDisplaySetting is the framework setting that shows where the focus
// is left of the mode badge in the header: off (the default) leaves the chrome as
// it was, noun shows the focused instance's noun (pane  NORMAL), stack
// the focus path from the outermost level to the innermost, open modals
// included (panes › pane-list › pane  NORMAL). A row of
// settings/ochazuke.json, declared when the runtime is made; read at
// every frame, so a change (settings set, the Settings page, the
// focus.display verb, another process's write) shows on the next one.
const FocusDisplaySetting = "focus-display"

// The focus-display values, in the order focus.display cycles them.
const (
	FocusDisplayOff   = "off"
	FocusDisplayNoun  = "noun"
	FocusDisplayStack = "stack"
)

// FocusDisplayModes are focus-display's values, in cycle order.
var FocusDisplayModes = []string{FocusDisplayOff, FocusDisplayNoun, FocusDisplayStack}

// FocusLabelMax is the widest label (in cells) the stack shows for a
// level; a level with a longer label, or none, shows its noun.
const FocusLabelMax = 16

// focusStackSep joins the levels of the stack.
const focusStackSep = " › "

// FocusDisplay is the focus-display value, off when it is undeclared or
// holds anything else.
func (r *Runtime) FocusDisplay() string {
	if v := r.Settings().String(FocusDisplaySetting); slices.Contains(FocusDisplayModes, v) {
		return v
	}
	return FocusDisplayOff
}

// FocusPath is the focus as the stack shows it, outermost level first and
// the focused instance last, app left out: each level's label when it has
// one no wider than FocusLabelMax (a page's is its title), else its noun
// as DisplayNoun. Open modals are levels in the chain: a modal's levels
// follow the focus it opened over.
func (r *Runtime) FocusPath() []string {
	ids := r.focusPathIDs()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.focusLevelName(id))
	}
	return out
}

// focusPathIDs is the instances of the focus path, outermost first: the
// path to the focus beneath the modals, then for each open modal the path
// from the modal instance to the focus inside it (a modal parented away
// from the focus it opened over still follows it).
func (r *Runtime) focusPathIDs() []string {
	var out []string
	add := func(ids []string) {
		for _, id := range ids {
			if id != AppNoun && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	add(r.InstancePath(r.BaseFocus()))
	for _, modal := range r.modals {
		path := r.InstancePath(r.ModalFocus(modal))
		if i := slices.Index(path, modal); i >= 0 {
			path = path[i:]
		} else {
			path = []string{modal}
		}
		add(path)
	}
	return out
}

// focusLevelName is one level's name in the stack.
func (r *Runtime) focusLevelName(id string) string {
	inst := r.Instances[id]
	if inst == nil {
		return id
	}
	label := inst.Label
	if page := r.PageOf(id); page != nil && label == "" {
		label = page.Title
	}
	label = strings.TrimSpace(modalLabel(label))
	if label != "" && ansi.StringWidth(label) <= FocusLabelMax {
		return label
	}
	return r.DisplayNoun(inst.Noun)
}

// FocusNoun is the focused instance's noun as DisplayNoun, "" without a
// focus.
func (r *Runtime) FocusNoun() string {
	if inst := r.Instances[r.Focus]; inst != nil {
		return r.DisplayNoun(inst.Noun)
	}
	return ""
}

// focusDisplayLevels is what the header shows beside the mode for the
// focus-display value: nothing when off. A zoomed tree with
// tree.breadcrumb title adds its breadcrumb: after the noun, or in the
// stack before the focused row.
func (r *Runtime) focusDisplayLevels() []string {
	crumb := r.titleCrumb()
	switch r.FocusDisplay() {
	case FocusDisplayNoun:
		if noun := r.FocusNoun(); noun != "" {
			return append([]string{noun}, crumb...)
		}
	case FocusDisplayStack:
		path := r.FocusPath()
		if len(crumb) > 0 && len(path) > 0 && r.rows[r.BaseFocus()] != "" && len(r.modals) == 0 {
			return slices.Concat(path[:len(path)-1], crumb, path[len(path)-1:])
		}
		return append(path, crumb...)
	}
	return crumb
}

// FitFocusPath joins levels with " › " in at most width cells. When they
// do not fit, it cuts from the left a whole level at a time behind "… › ",
// keeping the innermost level; with less room the innermost level alone
// follows "…", cut from its left when it is wider still. A cut path always
// starts with "…". Width under 1 is "".
func FitFocusPath(levels []string, width int) string {
	if width < 1 || len(levels) == 0 {
		return ""
	}
	clean := make([]string, len(levels))
	for i, level := range levels {
		clean[i] = modalLabel(level)
	}
	if s := strings.Join(clean, focusStackSep); ansi.StringWidth(s) <= width {
		return s
	}
	lead := "…" + focusStackSep
	for i := 1; i < len(clean); i++ {
		if s := lead + strings.Join(clean[i:], focusStackSep); ansi.StringWidth(s) <= width {
			return s
		}
	}
	inner := clean[len(clean)-1]
	if len(clean) > 1 {
		inner = "…" + inner
	}
	return cutLeft(inner, width)
}

// cutLeft keeps the right end of s in width cells behind "…".
func cutLeft(s string, width int) string {
	w := ansi.StringWidth(s)
	if w <= width {
		return s
	}
	if width < 1 {
		return ""
	}
	if width == 1 {
		return "…"
	}
	return "…" + ansi.TruncateLeft(s, w-(width-1), "")
}

func registerFocusDisplay(r *Runtime) {
	r.Register(Operation{Verb: "display", Noun: "ochazuke.focus", Description: "Show where the focus is beside the mode (the focus-display setting): off, noun or stack; with no mode, the next in that order", Parameters: map[string]Parameter{"mode": {Type: "string", Enum: slices.Clone(FocusDisplayModes), Description: "off, noun or stack (default: the next after the current)"}}, Result: "the focus-display value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		mode := StringArg(a, "mode", "")
		if mode == "" {
			i := slices.Index(FocusDisplayModes, r.FocusDisplay())
			mode = FocusDisplayModes[(i+1)%len(FocusDisplayModes)]
		}
		if err := r.Settings().Set(FocusDisplaySetting, mode); err != nil {
			return nil, err
		}
		return mode, nil
	}})
}
