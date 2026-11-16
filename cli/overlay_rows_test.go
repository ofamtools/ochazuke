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
// dry-run outcome it answers now: the overlay fills the rest, so only the
// picker (NoOverlay: the owner's open Q6 picker-edit) and the page (an
// instances store, which the overlay leaves to its routes) still do
// nothing here.
var overlayLocalRows = map[string]string{
	"ochazuke.agent.archive": "done", "ochazuke.agent.create": "done", "ochazuke.agent.delete": "done", "ochazuke.agent.edit": "done", "ochazuke.agent.set": "done",
	"ochazuke.appearance.archive": "done", "ochazuke.appearance.create": "done", "ochazuke.appearance.delete": "done",
	"ochazuke.binding-file.archive": "done", "ochazuke.binding-file.create": "done", "ochazuke.binding-file.delete": "done",
	"ochazuke.config.archive": "done", "ochazuke.config.create": "done", "ochazuke.config.delete": "done", "ochazuke.config.edit": "done", "ochazuke.config.set": "done",
	"ochazuke.mcp.archive": "done", "ochazuke.mcp.create": "done", "ochazuke.mcp.delete": "done", "ochazuke.mcp.edit": "done", "ochazuke.mcp.set": "done",
	"ochazuke.snapshot.archive": "done", "ochazuke.snapshot.create": "done", "ochazuke.snapshot.delete": "done",
	"ochazuke.outline.archive": "needs", "ochazuke.outline.create": "done", "ochazuke.outline.set": "needs",
	"ochazuke.register.archive": "needs", "ochazuke.register.create": "done", "ochazuke.register.delete": "needs",
	"ochazuke.setting.archive": "needs", "ochazuke.setting.create": "done", "ochazuke.setting.delete": "needs",
	"ochazuke.setting-item.archive": "needs", "ochazuke.setting-item.create": "done", "ochazuke.setting-item.set": "needs",
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
