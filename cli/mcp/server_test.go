package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type backendFunc func(context.Context, string, map[string]any) (any, error)

func (f backendFunc) Call(c context.Context, n string, a map[string]any) (any, error) {
	return f(c, n, a)
}
func testServer(t *testing.T, b Backend, aliases ...Alias) *Server {
	t.Helper()
	s, err := New(b, Options{Name: "example", Version: "v1", Instructions: "Use catalog then describe then run.", Aliases: aliases})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func play(t *testing.T, s *Server, lines string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(lines), &out); err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(&out)
	replies := []map[string]any{}
	for d.More() {
		var reply map[string]any
		if err := d.Decode(&reply); err != nil {
			t.Fatal(err)
		}
		replies = append(replies, reply)
	}
	return replies
}
func initialized(version string) string {
	return fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":%q}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n", version)
}
func TestMCPProtocolNegotiation(t *testing.T) {
	for _, version := range append([]string{"future"}, protocolVersions...) {
		t.Run(version, func(t *testing.T) {
			s := testServer(t, backendFunc(func(context.Context, string, map[string]any) (any, error) { return map[string]any{"ok": true}, nil }))
			replies := play(t, s, initialized(version)+`{"jsonrpc":"2.0","id":"tool","method":"tools/call","params":{"name":"state","arguments":{}}}`+"\n")
			first := replies[0]["result"].(map[string]any)
			want := version
			if version == "future" {
				want = protocolVersions[0]
			}
			if first["protocolVersion"] != want || first["instructions"] == "" {
				t.Fatalf("initialize: %+v", first)
			}
			if len(replies) != 2 || replies[1]["id"] != "tool" {
				t.Fatalf("notifications answered or id lost: %+v", replies)
			}
			result := replies[1]["result"].(map[string]any)
			_, structured := result["structuredContent"]
			if structured != (want >= "2025-06-18") {
				t.Fatalf("structured for %s: %+v", want, result)
			}
			if !strings.Contains(fmt.Sprint(result["content"]), DataNote) {
				t.Fatal("missing data note")
			}
		})
	}
}
func TestMCPProtocolValidation(t *testing.T) {
	calls := 0
	s := testServer(t, backendFunc(func(context.Context, string, map[string]any) (any, error) { calls++; return nil, nil }))
	for _, args := range []string{
		`{"name":"missing"}`, `{"name":"describe","arguments":{}}`, `{"name":"run","arguments":{"verb":"task.set","actor":{"kind":"owner"}}}`, `{"name":"run","arguments":{"verb":""}}`, `{"name":"run","arguments":{"verb":"task.set","args":[]}}`, `{"name":"run","arguments":{"verb":"task.set","dry_run":"yes"}}`, `{"name":"catalog","arguments":{"limit":1.5}}`, `{"name":"catalog","arguments":{"limit":0}}`, `{"name":"catalog","arguments":{"include":""}}`, `{"name":"state","arguments":{"include":["passwords"]}}`, `{"name":"describe","arguments":{"verb":null}}`, `{"name":"state","arguments":{},"actor":"owner"}`,
	} {
		replies := play(t, s, initialized("2025-11-25")+`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+args+"}\n")
		if replies[1]["error"].(map[string]any)["code"] != float64(-32602) {
			t.Fatalf("accepted %s: %+v", args, replies)
		}
	}
	if calls != 0 {
		t.Fatalf("backend called %d times for bad args", calls)
	}
	replies := play(t, s, "not json\n"+`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`+"\n")
	if len(replies) != 2 || replies[0]["error"].(map[string]any)["code"] != float64(-32700) || replies[1]["error"].(map[string]any)["code"] != float64(-32000) {
		t.Fatal(replies)
	}
}
func TestMCPMirrorsAndErrors(t *testing.T) {
	s := testServer(t, backendFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		if name == "catalog" {
			return map[string]any{"nouns": []any{}}, nil
		}
		return nil, errors.New("store unavailable")
	}))
	var lines strings.Builder
	lines.WriteString(initialized("2025-11-25"))
	requests := []string{`"method":"tools/list"`, `"method":"resources/list"`, `"method":"resources/read","params":{"uri":"ochazuke://catalog"}`, `"method":"resources/read","params":{"uri":"ochazuke://instructions"}`, `"method":"prompts/list"`, `"method":"prompts/get","params":{"name":"guide"}`, `"method":"tools/call","params":{"name":"state"}`, `"method":"nope"`}
	for i, req := range requests {
		fmt.Fprintf(&lines, "{\"jsonrpc\":\"2.0\",\"id\":%d,%s}\n", i+2, req)
	}
	replies := play(t, s, lines.String())
	tools := replies[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Fatal(tools)
	}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		ann := tool["annotations"].(map[string]any)
		if ann["readOnlyHint"] != (tool["name"] != "run") {
			t.Fatal(tool)
		}
	}
	if !strings.Contains(fmt.Sprint(replies[4]), s.Instructions()) || !strings.Contains(fmt.Sprint(replies[6]), s.Instructions()) {
		t.Fatal("instructions mirrors differ")
	}
	if replies[7]["result"].(map[string]any)["isError"] != true || replies[8]["error"].(map[string]any)["code"] != float64(-32601) {
		t.Fatal(replies)
	}
}
func TestMCPAliases(t *testing.T) {
	calls := 0
	s := testServer(t, backendFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		calls++
		if name != "run" || args["verb"] != "task.list" || args["args"].(map[string]any)["status"] != "open" {
			t.Fatalf("alias dispatch: %s %+v", name, args)
		}
		return nil, nil
	}), Alias{Tool: Tool{Name: "task_list", InputSchema: object(map[string]any{"state": text()})}, Verb: "task.list", Args: map[string]string{"state": "status"}, Hidden: true})
	if len(s.Tools()) != 4 {
		t.Fatal("hidden alias listed")
	}
	if _, err := s.Call(context.Background(), "task_list", map[string]any{"state": "open"}); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if _, err := s.Call(context.Background(), "task_list", map[string]any{"unknown": true}); err == nil {
		t.Fatal("unknown alias arg")
	}
}

func TestMCPProtocolInvalidEnvelope(t *testing.T) {
	s := testServer(t, backendFunc(func(context.Context, string, map[string]any) (any, error) { return nil, nil }))
	for _, line := range []string{`[]`, `{"jsonrpc":"1.0","id":3,"method":"ping"}`, `{"jsonrpc":"2.0","id":false,"method":"ping"}`} {
		replies := play(t, s, line+"\n")
		if len(replies) != 1 || replies[0]["error"].(map[string]any)["code"] != float64(-32600) {
			t.Fatal(line, replies)
		}
	}
	replies := play(t, s, initialized("2025-11-25")+`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"state","arguments":null}}`+"\n")
	if replies[1]["error"].(map[string]any)["code"] != float64(-32602) {
		t.Fatal(replies)
	}
}
