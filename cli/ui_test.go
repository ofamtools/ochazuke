package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// What a handler tells the user prints on stderr while the command runs:
// setup's messages first, then the verb's, then a job's Then; the sink is
// detached again when Run returns. The status line prints by its tone:
// ctx.Info as status:, ctx.Error and ctx.Status (its older spelling) as
// error:.
func TestShellPrintsNoticesAndStatusOnStderr(t *testing.T) {
	f := newFixture(t)
	f.r.UI().Notify("daemon started")
	f.r.Define("widget").
		Verb("tell", func(c ochazuke.Ctx) ochazuke.Result {
			id := c.Notify("telling " + c.Instance.ID)
			c.ClearNotice(id)
			c.Info("told")
			c.Error("could not tell w2")
			c.Status("could not tell w3")
			c.Status("")
			return ochazuke.Pending(func(context.Context) (any, error) { return "later", nil }).Then(func(c ochazuke.Ctx, _ any, _ error) {
				c.Notify("job finished")
			})
		}, "Tell the user")
	out, errOut, err := f.run("", "widget", "tell", "--target", "w1")
	if err != nil || !strings.Contains(out, "later") {
		t.Fatalf("out %s err %v", out, err)
	}
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(errOut), "\n") {
		if !strings.HasPrefix(line, "job #") {
			lines = append(lines, line)
		}
	}
	want := []string{"notice: daemon started", "notice: telling w1", "status: told", "error: could not tell w2", "error: could not tell w3", "notice: job finished"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stderr\n%s\nwant\n%s", errOut, strings.Join(want, "\n"))
	}
	if f.r.UI().Sink() != nil {
		t.Fatalf("the shell's sink stayed attached: %v", f.r.UI().Sink())
	}
}
