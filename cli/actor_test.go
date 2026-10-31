package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

func TestShellActorEnvironment(t *testing.T) {
	t.Setenv("OCHAZUKE_ACTOR", "agent")
	t.Setenv("OCHAZUKE_HARNESS", "test")
	t.Setenv("TUIOS_PANE_ID", "p")
	t.Setenv("TUIOS_SESSION", "s")
	f := newFixture(t)
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
