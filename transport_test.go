package ochazuke

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// socketDir is a private temporary directory for a test socket (short,
// under the system temp dir, so the path fits a Unix socket's limit).
func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "oz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// A socket file left by an application that was killed (nothing listens)
// is removed and listened on again; a live socket is never removed.
func TestListenReplacesOnlyADeadSocket(t *testing.T) {
	path := filepath.Join(socketDir(t), "s")
	dead, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	dead.(*net.UnixListener).SetUnlinkOnClose(false)
	dead.Close() // the file stays, as after a kill
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("the dead socket's file is gone: %v", err)
	}
	r := NewRuntime()
	server, err := Listen(path, r.HandleCall)
	if err != nil {
		t.Fatalf("listen over a dead socket: %v", err)
	}
	defer server.Close()
	if _, err := Listen(path, r.HandleCall); err == nil || !strings.Contains(err.Error(), "another application is listening") {
		t.Fatalf("listen over a live socket: %v", err)
	}
	if _, err := Call(context.Background(), path, Request{Noun: "app", Verb: "nothing"}); err != nil && strings.Contains(err.Error(), "connect") {
		t.Fatalf("the live socket stopped answering: %v", err)
	}
}

// A file that is not a socket is never removed.
func TestListenKeepsAFileThatIsNotASocket(t *testing.T) {
	path := filepath.Join(socketDir(t), "s")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path, NewRuntime().HandleCall); err == nil {
		t.Fatal("listened over a plain file")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatalf("the file was touched: %q %v", data, err)
	}
}

// The refusal of a socket directory others can read says what to do.
func TestListenRefusalSaysWhatToDo(t *testing.T) {
	dir := socketDir(t)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := Listen(filepath.Join(dir, "s"), NewRuntime().HandleCall)
	if err == nil || !strings.Contains(err.Error(), "chmod 700 "+dir) {
		t.Fatalf("refusal %v, want it to name chmod 700 %s", err, dir)
	}
}
