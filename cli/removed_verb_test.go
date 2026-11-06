package cli

import (
	"strings"
	"testing"
)

// The CLI names the replacement of a removed verb, in every spelling of a
// merged plural noun (bin settings list, bin ochazuke settings list, bin
// jobs list) and for a renamed command verb (bin focus set), before the
// words resolve against the catalog.
func TestCLIRemovedVerbNamesItsReplacement(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"settings", "list"}, "use ochazuke.setting.list"},
		{[]string{"ochazuke", "settings", "list"}, "use ochazuke.setting.list"},
		{[]string{"ochazuke.settings", "edit"}, "use ochazuke.setting.open"},
		{[]string{"jobs", "list"}, "use ochazuke.job.list"},
		{[]string{"focus", "set", "--json", `{"id":"x"}`}, "use ochazuke.focus.to {id}"},
	} {
		_, _, err := f.run("", tc.args...)
		if err == nil || !strings.Contains(err.Error(), "was removed") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err %v, want %q", tc.args, err, tc.want)
		}
	}
	if out, _, err := f.run("", "setting", "names"); err != nil || !strings.Contains(out, "[") {
		t.Fatalf("setting names: %q %v", out, err)
	}
}
