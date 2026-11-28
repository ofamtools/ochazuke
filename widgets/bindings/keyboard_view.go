package bindings

import (
	"fmt"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/ui"
)

// ViewNode renders the spec and details for a selected physical key. An
// empty selection uses the first declared stroke on the current layer.
func (spec KeyboardSpec) ViewNode(id, selectedKey string) ui.ViewNode {
	shift := "Shift off"
	if spec.Shifted {
		shift = "Shift on"
	}
	header := keyboardText("Layer · "+spec.ActivePrefix.Label+"   "+shift, "accent")
	header.ID, header.Focusable = id+".keyboard", true
	board := spec.keyboardRows(id)
	panel := spec.prefixPanel(id)
	body := ui.ViewNode{Kind: ui.KindHStack, Gap: 1, Children: []ui.ViewNode{board, panel}}
	body.Box.Flex = 1
	return ui.ViewNode{
		ID: id, Kind: ui.KindVStack, Scope: "bindings",
		Facts: map[string]any{"viewMode": ViewKeyboard, "activePrefix": spec.ActivePrefix.Prefix,
			"prefixCount": len(spec.Prefixes), "shifted": spec.Shifted, "empty": spec.Empty},
		Children: []ui.ViewNode{header, body, spec.keyDetails(selectedKey)},
	}
}

func keyboardText(text, tone string) ui.ViewNode {
	return ui.ViewNode{Kind: ui.KindText, Tone: tone, Props: map[string]any{"text": text}}
}

func (spec KeyboardSpec) keyboardRows(id string) ui.ViewNode {
	board := ui.ViewNode{Kind: ui.KindVStack}
	board.Box.Flex = 1
	for i, row := range spec.Rows {
		line := ui.ViewNode{Kind: ui.KindHStack}
		line.Box.FixedH = 4
		if i > 0 && i < 4 {
			line.Padding[3] = i
		}
		for _, cap := range row {
			line.Children = append(line.Children, cap.viewNode(id, true))
		}
		if len(row) < len(spec.Rows[0]) && i < 4 {
			spacer := keyboardText("", "")
			spacer.Box.Flex = len(spec.Rows[0]) - len(row)
			line.Children = append(line.Children, spacer)
		}
		board.Children = append(board.Children, line)
	}
	if len(spec.OtherKeys) > 0 {
		keys := ui.ViewNode{Kind: ui.KindGrid, MinColW: 15, Gap: 1}
		for _, cap := range spec.OtherKeys {
			keys.Children = append(keys.Children, cap.viewNode(id, false))
		}
		viewport := ui.ViewNode{ID: id + ".other", Kind: ui.KindScroll, Children: []ui.ViewNode{keys}}
		board.Children = append(board.Children, viewport)
	}
	return board
}

func (cap Keycap) viewNode(id string, boxed bool) ui.ViewNode {
	legend := cap.Legend
	if cap.HasShift {
		legend += "⇧"
	}
	label := cap.shortLabel()
	text := strings.TrimSpace(legend + "  " + label)
	if boxed {
		text = legend + "\n" + label
	}
	n := keyboardText(text, cap.Tone)
	n.ID, n.Scope, n.Border, n.Focusable = id+".key."+cap.PhysicalKey, "bindings.key", boxed, true
	n.Facts = map[string]any{"key": cap.PhysicalKey, "stroke": cap.Key, "hasPrefix": cap.ChildCount > 0, "prefix": cap.NextPrefix}
	if cap.ChildCount > 0 {
		n.Actions = []ui.ActionHint{prefixAction(cap.NextPrefix, "Open "+cap.NextPrefix)}
	} else {
		n.Actions = []ui.ActionHint{{Label: "Inspect " + cap.Key, Command: CmdKeySelect, Args: map[string]any{"key": cap.PhysicalKey}}}
	}
	return n
}

func (cap Keycap) shortLabel() string {
	var parts []string
	if cap.ChildCount > 0 {
		parts = append(parts, "▸"+strconv.Itoa(cap.ChildCount))
	}
	if len(cap.Actions) > 0 {
		action := cap.Actions[0]
		label := action.label()
		if label == action.Command {
			label = label[strings.LastIndex(label, ".")+1:]
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " ")
}

func prefixAction(prefix, label string) ui.ActionHint {
	return ui.ActionHint{Label: label, Command: CmdPrefixSelect, Args: map[string]any{"prefix": prefix}}
}

func (spec KeyboardSpec) prefixPanel(id string) ui.ViewNode {
	list := ui.ViewNode{ID: id + ".prefixes", Kind: ui.KindScroll}
	for i, layer := range spec.Prefixes {
		active := layer.Prefix == spec.ActivePrefix.Prefix
		label, tone := "  "+layer.Label, "text"
		if active {
			label, tone = "▸ "+layer.Label, "accent"
			list.Props = map[string]any{"offset": max(0, i-2)}
		}
		n := keyboardText(label, tone)
		n.ID, n.Scope, n.Focusable = id+".prefix."+layer.Prefix, "bindings.prefix", true
		n.Box.FixedH = 1
		n.Facts = map[string]any{"prefix": layer.Prefix, "active": active}
		n.Actions = []ui.ActionHint{prefixAction(layer.Prefix, layer.Label)}
		list.Children = append(list.Children, n)
	}
	panel := ui.ViewNode{Kind: ui.KindPanel, Title: "Prefixes", Border: true, Children: []ui.ViewNode{list}}
	panel.Box.FixedW = 18
	return panel
}

func (spec KeyboardSpec) keyDetails(selected string) ui.ViewNode {
	cap, ok := spec.keycap(selected)
	if !ok {
		cap, ok = spec.firstDeclared()
	}
	if !ok {
		return keyboardText("No exact bindings on this layer", "muted")
	}
	var details []string
	for _, a := range cap.Actions {
		details = append(details, actionDetail(a))
	}
	if cap.ChildCount > 0 {
		details = append(details, fmt.Sprintf("▸ %s  (%d next)", cap.NextPrefix, cap.ChildCount))
	}
	if len(details) == 0 {
		details = append(details, "unbound")
	}
	line := keyboardText(cap.Key+"  ·  "+strings.Join(details, "  ·  "), cap.Tone)
	line.Box.MaxH = 3
	return line
}

func actionDetail(action KeyAction) string {
	label := action.label()
	if !action.Unbind && label != action.Command {
		label = action.Command + "  ·  " + label
	}
	if action.Scope != "" || action.Mode != "" {
		label += " [" + strings.Trim(strings.Join([]string{action.Scope, action.Mode}, "/"), "/") + "]"
	}
	if action.When != "" {
		label += " when " + action.When
	}
	return label
}

func (spec KeyboardSpec) firstDeclared() (Keycap, bool) {
	for _, row := range spec.Rows {
		for _, cap := range row {
			if cap.declared() {
				return cap, true
			}
		}
	}
	if len(spec.OtherKeys) > 0 {
		return spec.OtherKeys[0], true
	}
	return Keycap{}, false
}
