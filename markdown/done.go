package markdown

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// DoneSetting controls whether checked tasks and their subtrees are drawn.
// A document's SetDone override takes precedence over this setting.
const DoneSetting = "markdown.done"

// DoneIndicatorSetting controls the dim count of concealed checked tasks.
const DoneIndicatorSetting = "markdown.done-indicator"

// Done reports this document's effective checked-task visibility: show or hide.
func (d *Doc) Done() string {
	if d.done != "" {
		return d.done
	}
	return d.r.Settings().String(DoneSetting)
}

// SetDone changes checked-task visibility for this Doc's session only, without
// writing settings or source. Toggle alternates show and hide; an empty value
// restores the current setting for this document.
func (d *Doc) SetDone(mode string) error {
	switch mode {
	case "toggle":
		mode = "hide"
		if d.Done() == "hide" {
			mode = "show"
		}
	case "", "show", "hide":
	default:
		return fmt.Errorf("unknown done visibility %q: use show, hide or toggle", mode)
	}
	if err := d.Leave(); err != nil {
		return err
	}
	d.done = mode
	e, off := d.cursor()
	return d.relayout(e, off)
}

// hiddenElements uses the same subtree boundary as editing: a checked parent
// conceals its notes, continuations and children regardless of their state.
func (d *Doc) hiddenElements() ([]bool, int) {
	if d.Done() != "hide" {
		return nil, 0
	}
	hidden := make([]bool, len(d.elems))
	count := 0
	for i, e := range d.elems {
		if e.Kind != KindTask || !e.Checked {
			continue
		}
		count++
		if hidden[i] {
			continue
		}
		end := subtreeEnd(d.elems, i)
		for j := i; j <= end; j++ {
			hidden[j] = true
		}
	}
	return hidden, count
}

func (d *Doc) doneIndicator(count int) string {
	if count == 0 || !d.r.Settings().Bool(DoneIndicatorSetting) {
		return ""
	}
	line := ansi.Truncate(fmt.Sprintf("%d done hidden", count), d.width, "…")
	return lipgloss.NewStyle().Faint(true).Render(line)
}
