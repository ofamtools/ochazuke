package grid

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// The grid noun's embedded keys pass the rule check.
func TestGridBindingsPassTheRuleCheck(t *testing.T) {
	r := ochazuke.NewRuntime()
	Register(r)
	if problems := r.CheckDefaultBindings("grid/bindings.json", defaultBindings); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}
