package markdown

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// The markdown nouns's embedded keys pass the rule check.
func TestMarkdownBindingsPassTheRuleCheck(t *testing.T) {
	r := ochazuke.NewRuntime()
	Register(r)
	if problems := r.CheckDefaultBindings("markdown/bindings.json", defaultBindings); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}
