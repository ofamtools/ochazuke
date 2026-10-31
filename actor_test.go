package ochazuke

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testAgent(pane string) Actor {
	return Actor{Kind: ActorAgent, Harness: "test", Pane: pane, Session: "test-session"}
}

func TestActorParseAndEnvironment(t *testing.T) {
	for _, raw := range []string{"owner", "agent", "unknown", `{"kind":"agent","name":"lane","pane":"p","session":"s","harness":"test","model":"m"}`} {
		a, err := ParseActor(raw)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ParseActor(string(data))
		if err != nil || a != b {
			t.Fatalf("roundtrip %s: %+v %v", raw, b, err)
		}
	}
	for _, raw := range []string{"", "{}", "null", "root", `{"kind":"root"}`} {
		if _, err := ParseActor(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	env := map[string]string{"TUIOS_PANE_ID": "p", "TUIOS_SESSION": "s", "OCHAZUKE_HARNESS": "test", "OCHAZUKE_MODEL": "m"}
	get := func(k string) string { return env[k] }
	if a, err := ActorFromEnv(get); err != nil || a != Owner() {
		t.Fatalf("pane without explicit actor: %+v %v", a, err)
	}
	env["OCHAZUKE_ACTOR"] = "agent"
	a, err := ActorFromEnv(get)
	if err != nil || a.Pane != "p" || a.Session != "s" || a.Harness != "test" || a.Model != "m" {
		t.Fatalf("env: %+v %v", a, err)
	}
	env["OCHAZUKE_ACTOR"] = `{"kind":"agent","pane":"explicit","session":"elsewhere"}`
	a, err = ActorFromEnv(get)
	if err != nil || a.Pane != "explicit" || a.Session != "elsewhere" {
		t.Fatalf("override: %+v %v", a, err)
	}
	env["OCHAZUKE_ACTOR"] = "bad"
	if _, err := ActorFromEnv(get); err == nil {
		t.Fatal("invalid env accepted")
	}
	if (Actor{}).String() != "owner" || testAgent("p").String() != "agent test (pane p)" {
		t.Fatal("actor display")
	}
	for _, b := range []Actor{Owner(), {Kind: ActorAgent}, testAgent("other"), {Kind: ActorAgent, Harness: "test", Pane: "p", Session: "other"}} {
		if testAgent("p").SameAgent(b) {
			t.Fatalf("claimed another identity: %+v", b)
		}
	}
	if !testAgent("p").SameAgent(testAgent("p")) || (Actor{Kind: ActorAgent}).SameAgent(Actor{Kind: ActorAgent}) {
		t.Fatal("identity matching")
	}
}

func TestActorRequestsConfirmAndPending(t *testing.T) {
	r := NewRuntime()
	a := testAgent("p")
	var handler, callback Actor
	r.Define("work").Verb("nested", func(c Ctx) Result { handler = c.Actor(); return Done(nil) }).
		Verb("start", func(c Ctx) Result {
			handler = c.Actor()
			if _, err := c.Runtime.Execute(Request{Noun: "work", Verb: "nested"}); err != nil {
				return Failed(err)
			}
			return Pending(func(context.Context) (any, error) { return "ok", nil }).Then(func(c Ctx, _ any, _ error) {
				callback = c.Actor()
				if c.Runtime.Actor() != a {
					t.Errorf("callback runtime actor: %+v", c.Runtime.Actor())
				}
				if _, err := c.Runtime.Execute(Request{Noun: "work", Verb: "nested"}); err != nil {
					t.Error(err)
				}
			})
		}).Confirm("Start?")
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Request: Request{Noun: "work", Verb: "start"}, Actor: a, Source: "mcp", Reply: reply})
	if res := <-reply; res.Error != "" {
		t.Fatal(res.Error)
	}
	if r.TopModal() == "" {
		t.Fatal("no confirmation")
	}
	if r.Actor() != Owner() {
		t.Fatal("actor leaked after call")
	}
	mustRun(t, r, Request{Noun: "ochazuke.confirm", Verb: "accept"})
	if handler != a {
		t.Fatalf("handler: %+v", handler)
	}
	for _, msg := range runCmd(r.PendingCommands()) {
		if _, err := r.Deliver(msg); err != nil {
			t.Fatal(err)
		}
	}
	if callback != a || r.Actor() != Owner() {
		t.Fatalf("callback %+v, current %+v", callback, r.Actor())
	}
	for _, e := range r.History(0) {
		if e.Request.Noun == "work" && (e.Context.Actor != a || e.Context.Source != "mcp" || e.Request.Actor != nil) {
			t.Fatalf("entry: %+v", e)
		}
	}
	mustRun(t, r, Request{Noun: "work", Verb: "nested"})
	if handler != Owner() {
		t.Fatal("owner request changed")
	}
	if _, err := r.Execute(Request{Noun: "work", Verb: "nested", Actor: &Actor{Kind: "bad"}}); err == nil {
		t.Fatal("invalid actor accepted")
	}
	raw := `{"noun":"work","verb":"nested","actor":{"kind":"agent","pane":"api"}}`
	var q Request
	if err := decodeRequest([]byte(raw), &q); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, q)
	if handler.Pane != "api" {
		t.Fatal(handler)
	}
}

func TestActorHistoryFilterAndPersistence(t *testing.T) {
	config, state := t.TempDir(), t.TempDir()
	m := historyModel(t, config, state)
	r := m.Runtime
	a := testAgent("p")
	b := testAgent("q")
	r.Define("work").Verb("touch", func(Ctx) Result { return Done(nil) })
	for _, actor := range []Actor{a, Owner(), b, a, Owner()} {
		mustRun(t, r, Request{Noun: "work", Verb: "touch", Actor: &actor})
	}
	if err := r.Config().Flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(state, HistoryFile))
	if err != nil || !strings.Contains(string(data), `"actor":{"kind":"agent"`) {
		t.Fatalf("persist: %s %v", data, err)
	}
	next := historyModel(t, config, state)
	seen := 0
	for _, e := range next.Runtime.History(0) {
		if e.Context.Actor == a {
			seen++
		}
	}
	if seen != 2 {
		t.Fatalf("restored actor count %d", seen)
	}
	for _, tc := range []struct {
		filter string
		n      int
	}{{"owner", 2}, {"agent", 3}, {"self", 2}, {`{"kind":"agent","harness":"test","pane":"q","session":"test-session"}`, 1}, {"unknown", 0}} {
		restore := r.WithActor(a)
		entries, err := r.historyByActor(0, nil, tc.filter)
		restore()
		if err != nil || len(entries) != tc.n {
			t.Fatalf("filter %s: %d %v", tc.filter, len(entries), err)
		}
	}
	result, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "list", Actor: &a, Args: Arguments{"actor": "self", "limit": 1, "kind": "all"}})
	if err != nil {
		t.Fatal(err)
	}
	entries := result.([]HistoryEntry)
	if len(entries) != 1 || entries[0].Context.Actor != a || entries[0].Seq != 4 {
		t.Fatalf("filter before limit: %+v", entries)
	}
	if _, err := r.historyByActor(0, nil, "bad"); err == nil {
		t.Fatal("bad actor filter accepted")
	}
	// Legacy lines have the same owner default as legacy API requests.
	oldState := t.TempDir()
	old := `{"seq":1,"request":{"noun":"app","verb":"quit"},"context":{"focus":"app","mode":"normal","source":"cli"},"result":{"ok":true}}`
	if err := os.WriteFile(filepath.Join(oldState, HistoryFile), []byte(old+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	legacy := historyModel(t, t.TempDir(), oldState)
	if got := legacy.Runtime.History(0)[0].Context.Actor; got != Owner() {
		t.Fatalf("legacy actor: %+v", got)
	}
}

func TestActorSocketRequest(t *testing.T) {
	r := NewRuntime()
	r.Define("work").Verb("who", func(c Ctx) Result { return Done(c.Actor()) })
	path := filepath.Join(socketDir(t), "s")
	server, err := Listen(path, r.HandleCall)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	a := testAgent("wire")
	value, err := Call(context.Background(), path, Request{Noun: "work", Verb: "who", Actor: &a})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got Actor
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got != a {
		t.Fatalf("socket actor: %+v", got)
	}
}
