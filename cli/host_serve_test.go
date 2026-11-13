package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	serveToolOnce sync.Once
	serveToolDir  string
	serveToolBin  string
	serveToolErr  error
)

// serveTool builds testdata/servetool once per test binary (TestMain
// removes it).
func serveTool(t testing.TB) string {
	t.Helper()
	serveToolOnce.Do(func() {
		serveToolDir, serveToolErr = os.MkdirTemp("", "ochazuke-servetool-")
		if serveToolErr != nil {
			return
		}
		serveToolBin = filepath.Join(serveToolDir, "servetool")
		out, err := exec.Command("go", "build", "-o", serveToolBin, "./testdata/servetool").CombinedOutput()
		if err != nil {
			serveToolErr = errors.New(err.Error() + "\n" + string(out))
		}
	})
	if serveToolErr != nil {
		t.Fatalf("build servetool: %v", serveToolErr)
	}
	return serveToolBin
}

// serveFixture is a data directory and the complete environment of a call
// that touches nothing outside it.
func serveFixture(t testing.TB) (data string, env func(extra ...string) []string) {
	dir := t.TempDir()
	data = filepath.Join(dir, "data")
	return data, func(extra ...string) []string {
		return append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + filepath.Join(dir, "config"), "XDG_STATE_HOME=" + filepath.Join(dir, "state"), "XDG_DATA_HOME=" + filepath.Join(dir, "share"), "XDG_CACHE_HOME=" + filepath.Join(dir, "cache")}, extra...)
	}
}

func servedCount(p *ServePool) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.served
}

// A served call prints the bytes an exec of the same words prints, on
// stdout and stderr, with the same exit status: results, a table, a
// failure, the catalog (whose commands are the program's own app's), and
// an application command outside the nouns (answered Exec, run by exec).
func TestHostServeSameBytesAsExec(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}}
	defer pool.Close()
	ctx := context.Background()
	cases := [][]string{{"item", "add", "--name", "a"}, {"item", "list"}, {"item", "list", "--format", "table"}, {"item", "add"}, {"item", "nope"}, {"catalog"}, {"extra"}, {"item", "list", "--help"}, {"ite", "lis"}, {"list"}, {"ochazuke.setting", "list"}}
	for _, words := range cases {
		call := ServeCall{Argv: append([]string{"--data", data}, words...), Env: env("OCHAZUKE_ACTOR=owner")}
		before := servedCount(pool)
		served, err := pool.Run(ctx, call, false)
		if err != nil {
			t.Fatalf("%v served: %v", words, err)
		}
		if got := servedCount(pool) - before; got != 1 && words[0] != "extra" || got != 0 && words[0] == "extra" {
			t.Fatalf("%v: served %d calls", words, got)
		}
		execd, err := pool.exec(ctx, call, nil)
		if err != nil {
			t.Fatalf("%v exec: %v", words, err)
		}
		if !bytes.Equal(served.Stdout, execd.Stdout) || !bytes.Equal(served.Stderr, execd.Stderr) || served.Exit != execd.Exit {
			t.Fatalf("%v differs:\nserved exit %d stdout %q stderr %q\nexec   exit %d stdout %q stderr %q", words, served.Exit, served.Stdout, served.Stderr, execd.Exit, execd.Stdout, execd.Stderr)
		}
	}
}

// $/cancel ends a running call (its job's context), and the process
// answers the next call.
func TestHostServeCancel(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}, Size: 1}
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := pool.Run(ctx, ServeCall{Argv: []string{"--data", data, "item", "slow"}, Env: env()}, false)
	if !errors.Is(err, context.Canceled) || time.Since(start) > 10*time.Second {
		t.Fatalf("slow call: %v after %s", err, time.Since(start))
	}
	pids := pool.Connections()
	answer, err := pool.Run(context.Background(), ServeCall{Argv: []string{"--data", data, "item", "list"}, Env: env()}, true)
	if err != nil || answer.Exit != 0 || servedCount(pool) != 1 {
		t.Fatalf("after cancel: %v %+v served %d", err, answer, servedCount(pool))
	}
	if after := pool.Connections(); len(pids) != 1 || len(after) != 1 || after[0].PID != pids[0].PID {
		t.Fatalf("the cancelled call's process was replaced: %+v then %+v", pids, after)
	}
}

// Each call runs with its own environment (what the tool reads, and the
// actor), on one process.
func TestHostServePerCallEnvironment(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}, Size: 1}
	defer pool.Close()
	for _, c := range []struct{ value, actor string }{{"one", "owner"}, {"two", "agent"}, {"three", "owner"}} {
		answer, err := pool.Run(context.Background(), ServeCall{Argv: []string{"--data", data, "item", "list"}, Env: env("FIXTURE_VALUE="+c.value, "OCHAZUKE_ACTOR="+c.actor)}, true)
		out := string(answer.Stdout)
		if err != nil || !strings.Contains(out, `"value": "`+c.value+`"`) || !strings.Contains(out, `"actor": "`+c.actor+`"`) {
			t.Fatalf("%s/%s: %v %s %s", c.value, c.actor, err, out, answer.Stderr)
		}
	}
	if servedCount(pool) != 3 || len(pool.Connections()) != 1 {
		t.Fatalf("served %d on %d processes", servedCount(pool), len(pool.Connections()))
	}
}

// A print to stdout outside the result goes to stderr, never into the
// protocol: the call still answers its result, and the process goes on to
// stream a watch call's lines.
func TestHostServeStrayStdout(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}}
	defer pool.Close()
	answer, err := pool.Run(context.Background(), ServeCall{Argv: []string{"--data", data, "item", "stray"}, Env: env()}, false)
	if err != nil || string(answer.Stdout) != "\"ok\"\n" || servedCount(pool) != 1 {
		t.Fatalf("stray: %v %+v", err, answer)
	}
	// A watch call sends its stdout as lines while it runs.
	var lines []string
	answer, err = pool.Watch(context.Background(), ServeCall{Argv: []string{"--data", data, "item", "list"}, Env: env("FIXTURE_VALUE=w")}, func(line []byte) { lines = append(lines, string(line)) })
	if err != nil || answer.Exit != 0 || len(answer.Stdout) != 0 || servedCount(pool) != 2 || !slices.Contains(lines, `  "value": "w"`) {
		t.Fatalf("after stray, watch: %v %+v %q", err, answer, lines)
	}
}

// A served tool's Host.Call goes over the pipe to the pool's OnHostRun: the
// host command named in OCHAZUKE_HOST does not exist, so an exec would fail.
func TestServedHostCallUsesPipe(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	var mu sync.Mutex
	var got []string
	pool := &ServePool{Bin: bin, Args: []string{"--data", data}, OnHostRun: func(_ context.Context, call ServeCall) ServeAnswer {
		mu.Lock()
		got = call.Argv
		mu.Unlock()
		return ServeAnswer{Stdout: []byte(`{"peer":"piped"}`)}
	}}
	defer pool.Close()
	answer, err := pool.Run(context.Background(), ServeCall{Argv: []string{"--data", data, "item", "host"}, Env: env(`OCHAZUKE_HOST=["/nonexistent/host","--data","h"]`, "OCHAZUKE_EXTENSION=lane")}, false)
	mu.Lock()
	defer mu.Unlock()
	if err != nil || !strings.Contains(string(answer.Stdout), "piped") || servedCount(pool) != 1 {
		t.Fatalf("host call: %v %s %s", err, answer.Stdout, answer.Stderr)
	}
	if len(got) < 4 || got[0] != "/nonexistent/host" || !slices.Contains(got, "peer") || !slices.Contains(got, "inspect") {
		t.Fatalf("host.run argv %q", got)
	}
}

// Standalone, the same Host.Call and HostCommand start the host.
func TestStandaloneHostCallExecs(t *testing.T) {
	bin := serveTool(t)
	data, env := serveFixture(t)
	pool := &ServePool{Bin: bin}
	answer, err := pool.exec(context.Background(), ServeCall{Argv: []string{"--data", data, "item", "host"}, Env: env(`OCHAZUKE_HOST=["`+bin+`","--data","`+data+`"]`, "OCHAZUKE_EXTENSION=lane")}, nil)
	if err != nil || answer.Exit != 0 || !strings.Contains(string(answer.Stdout), "inspected") {
		t.Fatalf("standalone host call: %v %s %s", err, answer.Stdout, answer.Stderr)
	}
	out, err := HostCommand(context.Background(), []string{bin, "--data", data, "peer", "inspect"})
	if err != nil || !strings.Contains(string(out), "inspected") {
		t.Fatalf("HostCommand: %v %s", err, out)
	}
}
