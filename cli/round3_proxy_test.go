//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

func TestRound3MountedLimitKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "pid")
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	var got error
	r.Define("remote").Verb("work", func(ochazuke.Ctx) ochazuke.Result {
		return RunMounted(CatalogVerb{Name: "work", Timeout: "100ms"}, ProxyCommand{Bin: "/bin/sh", Args: []string{"-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidfile}, Timeout: time.Hour, WaitDelay: 2 * time.Second}).Then(func(_ ochazuke.Ctx, _ any, err error) { got = err })
	})
	r.Execute(ochazuke.Request{Noun: "remote", Verb: "work"})
	_ = Settle(r, nil)
	if got == nil || got.Error() != "work: stopped after 100ms (limit)" {
		t.Fatal(got)
	}
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("descendant %d survived", pid)
	}
}

func TestRound3MountedErrorWording(t *testing.T) {
	for _, tc := range []struct{ script, want string }{{`echo 'child reason' >&2; exit 3`, "read: child reason"}, {`exit 4`, "read: exit status 4"}} {
		r := ochazuke.NewRuntime()
		r.Config().Disable()
		var got error
		r.Define("remote").Verb("read", func(ochazuke.Ctx) ochazuke.Result {
			return RunMounted(CatalogVerb{Name: "read"}, ProxyCommand{Bin: "/bin/sh", Args: []string{"-c", tc.script}}).Then(func(_ ochazuke.Ctx, _ any, err error) { got = err })
		})
		r.Execute(ochazuke.Request{Noun: "remote", Verb: "read"})
		_ = Settle(r, nil)
		if got == nil || got.Error() != tc.want {
			t.Fatal(got)
		}
	}
}

func TestRound3ZeroTimeoutUsesHostLimit(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("remote").Verb("read", func(ochazuke.Ctx) ochazuke.Result {
		return RunMounted(CatalogVerb{Name: "read", Timeout: "0s"}, ProxyCommand{
			Bin: "/bin/sh", Args: []string{"-c", "sleep 1"}, Timeout: 50 * time.Millisecond,
		})
	})
	if _, err := r.Execute(ochazuke.Request{Noun: "remote", Verb: "read"}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(r, nil); err == nil || err.Error() != "read: stopped after 50ms (limit)" {
		t.Fatal(err)
	}
}
