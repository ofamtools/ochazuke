package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestHostFromEnvDeclaration(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "[]", "[1]", "bad", `["/fixture/host","--data","/fixture/a b"]`} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"OCHAZUKE_HOST": raw, "OCHAZUKE_EXTENSION": "lane"}
			h, ok, err := hostFromEnv(func(k string) string { return env[k] }, "o-lane")
			if raw == "" {
				if ok || err != nil {
					t.Fatalf("standalone: %v %v", ok, err)
				}
				peers, err := h.Peers(context.Background())
				if err != nil || len(peers) != 0 {
					t.Fatalf("standalone peers: %v %v", peers, err)
				}
				return
			}
			if strings.Contains(raw, "/fixture/host") {
				if !ok || err != nil || h.Command[2] != "/fixture/a b" || h.Caller != "lane" {
					t.Fatalf("host: %+v %v %v", h, ok, err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s", raw)
			}
		})
	}
}

func TestHostCallOwnerRouteRightsHistoryAndDepth(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "host-fixture")
	build := exec.Command("go", "build", "-o", bin, "./testdata/hostcall")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake host/tool: %v\n%s", err, out)
	}
	envFor := func(dir string) []string {
		return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + filepath.Join(dir, "config"), "XDG_STATE_HOME=" + filepath.Join(dir, "state"), "XDG_DATA_HOME=" + filepath.Join(dir, "data"), "OCHAZUKE_ACTOR=owner"}
	}
	for _, action := range []string{"extension.register", "peer.ask", "peer.inspect"} {
		t.Run("owner-key/"+action, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd := exec.Command(bin, "--data", dir, "--owner-key", action)
			cmd.Env = envFor(dir)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if action != "peer.inspect" {
				want := "agents may never run this verb"
				if action == "peer.ask" {
					want = "confirm"
				}
				if err == nil || !strings.Contains(stderr.String(), want) || strings.Contains(stdout.String(), "BODY REACHED") {
					t.Fatalf("route: err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("route: %v %s", err, stderr.String())
			}
			var answer struct {
				Actor ochazuke.Actor `json:"actor"`
				Data  string         `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &answer); err != nil {
				t.Fatalf("answer: %v %s", err, stdout.String())
			}
			if answer.Data != dir || answer.Actor.Kind != "agent" || answer.Actor.Name != "lane" || answer.Actor.Harness != "extension" || answer.Actor.Via == nil || answer.Actor.Via.Kind != "owner" {
				t.Fatalf("answer: %+v", answer)
			}
			history, err := os.ReadFile(filepath.Join(dir, "state", ochazuke.HistoryFile))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range bytes.Split(bytes.TrimSpace(history), []byte("\n")) {
				var e ochazuke.HistoryEntry
				if err := json.Unmarshal(line, &e); err != nil {
					t.Fatal(err)
				}
				if e.Request.Key() == "peer.inspect" {
					found = true
					if e.Context.Actor.Name != "lane" || e.Context.Actor.Via == nil || e.Context.Actor.Via.Kind != "owner" {
						t.Fatalf("history actor: %+v", e.Context.Actor)
					}
				}
			}
			if !found {
				t.Fatalf("host-call action missing from history: %s", history)
			}
		})
	}
	t.Run("explicit-owner-still-narrows", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		h := Host{Command: []string{bin, "--data", dir}, Caller: "lane", Env: append(envFor(dir), "FIXTURE_EXPLICIT_OWNER=1")}
		out, err := h.Run(context.Background(), "extension.register", nil)
		if err == nil || !strings.Contains(err.Error(), "agents may never run this verb") || bytes.Contains(out, []byte("BODY REACHED")) {
			t.Fatalf("explicit owner: %s %v", out, err)
		}
	})
	t.Run("caller-policy-cannot-demote-detected-agent", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		config := filepath.Join(dir, "config")
		if err := os.MkdirAll(config, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config, ochazuke.SettingsFile), []byte(`{"extensions.host-call-rights":"caller"}`), 0600); err != nil {
			t.Fatal(err)
		}
		h := Host{Command: []string{bin, "--data", dir}, Caller: "lane", Env: append(envFor(dir), "CODEX_CI=1")}
		_, err := h.Run(context.Background(), "extension.register", nil)
		if err == nil || !strings.Contains(err.Error(), "agents may never run this verb") {
			t.Fatalf("detected caller: %v", err)
		}
	})
	t.Run("peers", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		h := Host{Command: []string{bin, "--data", dir}, Caller: "lane", Env: envFor(dir)}
		peers, err := h.Peers(context.Background())
		if err != nil || len(peers) != 1 || peers[0].Namespace != "md" || peers[0].Nouns[0].Noun != "md.doc" || peers[0].Nouns[0].Verbs[0].Verb != "show" {
			t.Fatalf("peers: %+v %v", peers, err)
		}
	})
	t.Run("depth-five", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		h := Host{Command: []string{bin, "--data", dir}, Caller: "lane", Env: append(envFor(dir), "OCHAZUKE_HOST_DEPTH=4", `OCHAZUKE_HOST_CHAIN=["lane","md","lane","md"]`)}
		_, err := h.Run(context.Background(), "peer.inspect", nil)
		if err == nil || !strings.Contains(err.Error(), "host call depth 5 > 4: lane -> md -> lane -> md -> lane") {
			t.Fatalf("depth: %v", err)
		}
	})
}
