package ochazuke_test

import (
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/markdown"
)

// The framework's drawn parts declare their kind, so an app's Nouns page
// files them under parts, not commands: the markdown viewer, the message
// log, a terminal and a timeline card's body.
func TestFrameworkPartsAreParts(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	markdown.Register(r)
	r.EnsureTerminalLeader()
	for _, noun := range []string{markdown.ViewNoun, "ochazuke.message", "ochazuke.terminal", ochazuke.TimelineBodyNoun} {
		if got := r.NounKind(noun); got != ochazuke.NounKindPart {
			t.Errorf("%s is a %q noun, want part", noun, got)
		}
	}
	for name := range r.Nouns {
		if r.NounKind(name) == ochazuke.NounKindCommand && len(name) > 9 && name[:9] == "ochazuke." {
			t.Errorf("framework noun %s derives as a command: declare its kind", name)
		}
	}
}
