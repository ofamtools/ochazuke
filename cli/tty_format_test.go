package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// TestTTYFormat: with no --format, a terminal prints as cli.tty-format
// says (one case per value), a pipe prints JSON, and --format or a bare
// --json wins.
func TestTTYFormat(t *testing.T) {
	const table, json = "table", "json"
	cases := []struct {
		setting  string
		terminal bool
		args     []string
		want     string
	}{
		{"auto", true, []string{"pane", "list"}, table},  // the verb's human format
		{"auto", true, []string{"item", "list"}, json},   // a verb with none
		{"json", true, []string{"pane", "list"}, json},   // always JSON
		{"table", true, []string{"item", "list"}, table}, // any rows answer
		{"table", true, []string{"pane", "show"}, json},  // not rows: JSON, no note
		{"auto", false, []string{"pane", "list"}, json},  // a pipe
		{"auto", true, []string{"pane", "list", "--format", "json"}, json},
		{"auto", true, []string{"pane", "list", "--json"}, json},
		{"json", true, []string{"item", "list", "--format", "table"}, table},
	}
	for _, c := range cases {
		r := formatRuntime()
		op, _ := r.Operations.Get(ochazuke.KeyOf("pane", "list"))
		op.HumanFormat = ochazuke.HumanFormatTable
		if err := r.Settings().Set(ochazuke.CLITTYFormatSetting, c.setting); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		sh := New(r, "tool", strings.NewReader(""), &out, &errs)
		terminal := c.terminal
		sh.Terminal = func() bool { return terminal }
		if err := sh.Run(context.Background(), c.args); err != nil {
			t.Fatalf("%s %v: %v", c.setting, c.args, err)
		}
		got := json
		if s := strings.TrimSpace(out.String()); !strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "{") {
			got = table
		}
		if got != c.want || errs.Len() > 0 {
			t.Errorf("cli.tty-format %s, terminal %v, %v: printed %s (stderr %q), want %s:\n%s", c.setting, c.terminal, c.args, got, errs.String(), c.want, out.String())
		}
	}
}
