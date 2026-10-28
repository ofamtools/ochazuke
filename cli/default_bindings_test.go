package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// The command noun's embedded keys pass the rule check once the Commands
// page registered its verbs.
func TestCommandBindingsPassTheRuleCheck(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	m := ochazuke.NewModel(r)
	if _, err := AddCommands(m, &fakeCommands{}, CommandsConfig{}); err != nil {
		t.Fatal(err)
	}
	if problems := r.CheckDefaultBindings("cli/bindings.json", defaultBindings); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

// BIN bindings defaults prints the bindings file as text, unquoted, with
// the framework's rows tagged by their embedded file.
func TestBindingsDefaultsPrintsAFile(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	var out, errs strings.Builder
	s := New(r, "demo", nil, &out, &errs)
	if err := s.Run(context.Background(), []string{"bindings", "defaults"}); err != nil {
		t.Fatalf("%v: %s", err, errs.String())
	}
	text := out.String()
	if !strings.HasPrefix(text, ochazuke.BindingsHeader+"\n{\n  \"modes\": {\n") || !strings.Contains(text, `{"context": "ochazuke.list/normal", "source": "ochazuke/bindings/list.json", "bindings": {`) {
		t.Fatalf("bindings defaults:\n%s", text)
	}
}
