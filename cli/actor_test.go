package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

func TestShellActorEnvironment(t *testing.T) {
	f := newFixture(t)
	t.Setenv("OCHAZUKE_ACTOR", "agent")
	t.Setenv("OCHAZUKE_HARNESS", "test")
	t.Setenv("TUIOS_PANE_ID", "p")
	t.Setenv("TUIOS_SESSION", "s")
	if _, _, err := f.run("", "widget", "rename", "--name", "new", "--target", "w1"); err != nil {
		t.Fatal(err)
	}
	entries := f.r.History(0)
	e := entries[len(entries)-1]
	want := ochazuke.Actor{Kind: ochazuke.ActorAgent, Harness: "test", Pane: "p", Session: "s"}
	if e.Context.Actor != want || e.Context.Source != "cli" {
		t.Fatalf("entry: %+v", e)
	}
	if f.r.Actor() != ochazuke.Owner() {
		t.Fatal("actor leaked")
	}
	out, _, err := f.run("", "history", "list", "--actor", "self", "--limit", "1")
	if err != nil {
		t.Fatal(err)
	}
	var got []ochazuke.HistoryEntry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Context.Actor != want {
		t.Fatalf("history output: %s", out)
	}
	t.Setenv("OCHAZUKE_HARNESS", "")
	t.Setenv("OCHAZUKE_ACTOR", "owner")
	if _, _, err := f.run("", "widget", "show", "--target", "w1"); err != nil {
		t.Fatal(err)
	}
	entries = f.r.History(0)
	if entries[len(entries)-1].Context.Actor != ochazuke.Owner() {
		t.Fatal("owner override")
	}
	t.Setenv("OCHAZUKE_ACTOR", "invalid")
	if _, _, err := f.run("", "widget", "show", "--target", "w1"); err == nil {
		t.Fatal("invalid identity accepted")
	}
	var stdout, stderr bytes.Buffer
	sh := New(f.r, "tool", strings.NewReader(""), &stdout, &stderr)
	sh.Actor = &want
	sh.Source = "mcp"
	if err := sh.Run(context.Background(), []string{"widget", "show", "--target", "w1"}); err != nil {
		t.Fatal(err)
	}
	entries = f.r.History(0)
	e = entries[len(entries)-1]
	if e.Context.Actor != want || e.Context.Source != "mcp" {
		t.Fatalf("explicit identity: %+v", e)
	}
}

func TestMountedBindingsAgentRightsOpen(t *testing.T) {
	for _, identity := range []string{"explicit", "detected", "detected-owner-override"} {
		for _, noun := range []string{"bindings", "ochazuke.bindings", "ochazuke bindings"} {
			for _, verb := range []string{"list", "check", "defaults", "export", "path", "stops"} {
				t.Run(identity+"/"+noun+"/"+verb, func(t *testing.T) {
					dir := t.TempDir()
					r := ochazuke.NewRuntime()
					r.Config().Dir = filepath.Join(dir, "config")
					r.Config().StateDir = filepath.Join(dir, "state")
					var stdout, stderr bytes.Buffer
					sh := New(r, "tool", strings.NewReader(""), &stdout, &stderr)
					actor := ochazuke.Actor{Kind: ochazuke.ActorAgent, Harness: "test"}
					if identity != "explicit" {
						env := map[string]string{"CODEX_CI": "1"}
						if identity == "detected-owner-override" {
							env["OCHAZUKE_ACTOR"] = "owner"
						}
						var err error
						actor, err = ochazuke.ActorFromEnv(func(k string) string { return env[k] })
						if err != nil {
							t.Fatal(err)
						}
					}
					sh.Actor = &actor
					args := append(strings.Fields(noun), verb)
					target := filepath.Join(dir, "export.json")
					if verb == "export" {
						args = append(args, "--path", target)
					}
					if verb == "list" {
						args = append(args, "--all")
					}
					app := oden.New("tool", args, oden.WithStdio(strings.NewReader(""), &stdout, &stderr))
					Mount(app, sh)
					err := app.Run(context.Background())
					// Agents may read and write config and bindings (not protected
					// settings); the destructive default is pinned by
					// TestMountedDestructiveBeatsDeclaredAllow.
					var refused *ochazuke.RefusedError
					if errors.As(err, &refused) {
						t.Fatalf("actor %+v: bindings verb refused: %v", actor, err)
					}
				})
			}
		}
	}
}
