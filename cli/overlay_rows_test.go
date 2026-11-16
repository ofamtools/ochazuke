package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// overlayLocalRows are the 44 local verbs mtm's catalog listed unavailable
// on 2026-11-16 (overlay-every-store, groups N to S), each with the
// dry-run outcome it answers now. The overlay fills the stores that cannot
// write their things and the system facts; still nothing here: the picker
// and setting-item (NoOverlay: the owner's open Q6, picker-edit and
// rows keyed by place as binding-row-edit), the outline (NoOverlay: its
// nodes are the application's tree), the page (an instances store, left
// to its routes), and appearance, binding-file and register (stores that
// write their own props: their authors decide create, delete and
// archive).
var overlayLocalRows = map[string]string{
	"ochazuke.agent.archive": "done", "ochazuke.agent.create": "done", "ochazuke.agent.delete": "done", "ochazuke.agent.edit": "done", "ochazuke.agent.set": "done",
	"ochazuke.appearance.archive": "nothing", "ochazuke.appearance.create": "nothing", "ochazuke.appearance.delete": "nothing",
	"ochazuke.binding-file.archive": "nothing", "ochazuke.binding-file.create": "nothing", "ochazuke.binding-file.delete": "nothing",
	"ochazuke.config.archive": "done", "ochazuke.config.create": "done", "ochazuke.config.delete": "done", "ochazuke.config.edit": "done", "ochazuke.config.set": "done",
	"ochazuke.mcp.archive": "done", "ochazuke.mcp.create": "done", "ochazuke.mcp.delete": "done", "ochazuke.mcp.edit": "done", "ochazuke.mcp.set": "done",
	"ochazuke.snapshot.archive": "done", "ochazuke.snapshot.create": "done", "ochazuke.snapshot.delete": "done",
	"ochazuke.outline.archive": "nothing", "ochazuke.outline.create": "nothing", "ochazuke.outline.set": "nothing",
	"ochazuke.register.archive": "nothing", "ochazuke.register.create": "nothing", "ochazuke.register.delete": "nothing",
	"ochazuke.setting.archive": "done", "ochazuke.setting.create": "done", "ochazuke.setting.delete": "done",
	"ochazuke.setting-item.archive": "nothing", "ochazuke.setting-item.create": "nothing", "ochazuke.setting-item.set": "nothing",
	"ochazuke.page.archive": "nothing", "ochazuke.page.create": "nothing", "ochazuke.page.delete": "nothing",
	"ochazuke.picker.archive": "nothing", "ochazuke.picker.create": "nothing", "ochazuke.picker.delete": "nothing", "ochazuke.picker.edit": "nothing", "ochazuke.picker.set": "nothing",
}

// The 44 local rows dry-run (Runtime.CheckRequest, as the outcomes rule
// does) to the outcome the table says: a filled verb answers done or asks
// for its thing, never nothing / not-here.
func TestOverlayLocalRowsAnswer(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	New(r, "tool", strings.NewReader(""), io.Discard, io.Discard)
	got := map[string]string{}
	for key := range overlayLocalRows {
		noun, verb, _ := strings.Cut(key[len("ochazuke."):], ".")
		o := ochazuke.OutcomeOf(nil, r.CheckRequest(ochazuke.Request{Noun: "ochazuke." + noun, Verb: verb}))
		got[key] = string(o.Kind)
		if o.Kind == ochazuke.OutcomeNothing {
			got[key] += ": " + o.Reason
		}
	}
	var wrong []string
	keys := make([]string, 0, len(got))
	for key := range got {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if kind, _, _ := strings.Cut(got[key], ":"); kind != overlayLocalRows[key] {
			wrong = append(wrong, fmt.Sprintf("%s: %s, want %s", key, got[key], overlayLocalRows[key]))
		}
	}
	if len(wrong) > 0 {
		t.Fatalf("%d of %d rows answer otherwise:\n%s", len(wrong), len(got), strings.Join(wrong, "\n"))
	}
}
