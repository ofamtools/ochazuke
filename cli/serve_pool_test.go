package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// RunMounted with a pool answers what it answers without one; the pool
// starts serving on its After-th call.
func TestServePoolRunMountedEqualsExec(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}, After: 2}
	defer pool.Close()
	verbs := []struct {
		v     CatalogVerb
		words []string
	}{
		{CatalogVerb{Name: "item add"}, []string{"item", "add", "--name", "x"}},
		{CatalogVerb{Name: "item list", History: "fetch"}, []string{"item", "list"}},
		{CatalogVerb{Name: "item add"}, []string{"item", "add"}},
		{CatalogVerb{Name: "item list", History: "fetch"}, []string{"item", "list", "--format", "table"}},
	}
	for i, c := range verbs {
		run := func(p *ServePool) (any, error) {
			call := ProxyCommand{Bin: bin, Args: append([]string{"--data", data}, c.words...), Env: env("OCHAZUKE_ACTOR=owner"), Timeout: time.Minute, Pool: p}
			return RunMounted(c.v, call).Await(context.Background())
		}
		pooled, perr := run(pool)
		plain, err := run(nil)
		if (perr == nil) != (err == nil) || perr != nil && perr.Error() != err.Error() || !equalJSON(pooled, plain) {
			t.Fatalf("%v: pool %v %v, exec %v %v", c.words, pooled, perr, plain, err)
		}
		if want := i; servedCount(pool) != want {
			t.Fatalf("after call %d served %d, want %d (the first execs)", i+1, servedCount(pool), want)
		}
	}
}

func equalJSON(a, b any) bool { return reflect.DeepEqual(a, b) }

// A rebuilt binary (a new stamp) drains the pool and tells the host; the
// next call runs on a new process of the new binary.
func TestServePoolRebuiltBinary(t *testing.T) {
	src := serveTool(t)
	bin := filepath.Join(t.TempDir(), "servetool")
	copyFile(t, src, bin)
	data, env := serveFixture(t)
	var stale []string
	var mu sync.Mutex
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}, OnStale: func(reason string) {
		mu.Lock()
		stale = append(stale, reason)
		mu.Unlock()
	}}
	defer pool.Close()
	call := ServeCall{Argv: []string{"--data", data, "item", "list"}, Env: env()}
	if _, err := pool.Run(context.Background(), call, true); err != nil {
		t.Fatal(err)
	}
	first := pool.Connections()
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(bin, later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Run(context.Background(), call, true); err != nil {
		t.Fatal(err)
	}
	second := pool.Connections()
	mu.Lock()
	defer mu.Unlock()
	if len(first) != 1 || len(second) != 1 || first[0].PID == second[0].PID || first[0].Stamp == second[0].Stamp || len(stale) != 1 || stale[0] != "binary changed" {
		t.Fatalf("rebuilt: %+v then %+v, stale %q", first, second, stale)
	}
}

func copyFile(t *testing.T, from, to string) {
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// With every process busy and the pool full a call execs rather than
// waits; an idle process exits after IdleTimeout.
func TestServePoolOverflowExecsAndIdleExits(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}, Size: 1, IdleTimeout: 300 * time.Millisecond}
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = pool.Run(ctx, ServeCall{Argv: []string{"--data", data, "item", "slow"}, Env: env()}, true)
	}()
	waitFor(t, func() bool { c := pool.Connections(); return len(c) == 1 && c[0].State == "busy" })
	answer, err := pool.Run(context.Background(), ServeCall{Argv: []string{"--data", data, "item", "list"}, Env: env()}, true)
	if err != nil || answer.Exit != 0 || servedCount(pool) != 0 {
		t.Fatalf("overflow: %v %+v served %d", err, answer, servedCount(pool))
	}
	cancel()
	<-done
	waitFor(t, func() bool { return len(pool.Connections()) == 0 })
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A process that dies under a read is retried once by exec; under a write
// the call fails (it may have applied) with the cause.
func TestServePoolCrashRetriesReadsOnly(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}}
	defer pool.Close()
	call := ServeCall{Argv: []string{"--data", data, "item", "die"}, Env: env()}
	answer, err := pool.Run(context.Background(), call, true)
	if err != nil || string(answer.Stdout) != "\"alive\"\n" {
		t.Fatalf("read: %v %+v", err, answer)
	}
	if _, err := pool.Run(context.Background(), call, false); err == nil {
		t.Fatal("a write on a dying process should fail, not run again")
	}
	if len(pool.Connections()) != 0 {
		t.Fatalf("dead processes kept: %+v", pool.Connections())
	}
}

// BenchmarkServeVsExec compares one list call by exec with the same call
// on a served process (go test -run '^$' -bench ServeVsExec ./cli).
func BenchmarkServeVsExec(b *testing.B) {
	bin := serveTool(b)
	data, env := serveFixture(b)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}, Size: 1}
	defer pool.Close()
	call := ServeCall{Argv: []string{"--data", data, "item", "list"}, Env: env()}
	b.Run("exec", func(b *testing.B) {
		for b.Loop() {
			if _, err := pool.exec(context.Background(), call, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("served", func(b *testing.B) {
		if _, err := pool.Run(context.Background(), call, true); err != nil {
			b.Fatal(err)
		}
		for b.Loop() {
			if _, err := pool.Run(context.Background(), call, true); err != nil {
				b.Fatal(err)
			}
		}
	})
}
