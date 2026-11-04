package ochazuke

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"os/exec"
	"testing"
)

func TestForegroundRetainsLastStderrReason(t *testing.T) {
	r := NewRuntime()
	var out bytes.Buffer
	r.Define("app").Verb("work", func(Ctx) Result {
		cmd := exec.Command("sh", "-c", `printf '%05000d\nsh: last useful reason\n' 0 >&2; exit 7`)
		cmd.Stderr = &out
		return Foreground(cmd)
	})
	r.Execute(Request{Noun: "app", Verb: "work"})
	var got error
	drain(t, r, func(msg tea.Msg) {
		_, err := r.Deliver(msg)
		if err != nil {
			got = err
		}
	})
	if got == nil || got.Error() != "sh: last useful reason (exit 7)" {
		t.Fatal(got)
	}
}
