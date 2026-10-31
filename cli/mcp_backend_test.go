package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli/mcp"
)

func mcpCall(t *testing.T, s *mcp.Server, tool string, args map[string]any) map[string]any {
	t.Helper()
	value, err := s.Call(context.Background(), tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return value.(map[string]any)
}
func mcpFixture(t *testing.T) (*fixture, *Shell, *mcp.Server) {
	t.Helper()
	f := newFixture(t)
	f.r.Config().Disable()
	sh := New(f.r, "tool", strings.NewReader(""), io.Discard, io.Discard, About("Test widgets"))
	a := ochazuke.Actor{Kind: ochazuke.ActorAgent, Name: "lane", Pane: "p1", Session: "s1"}
	sh.Actor = &a
	server, err := sh.NewMCPServer()
	if err != nil {
		t.Fatal(err)
	}
	return f, sh, server
}
func TestMCPBackendDiscoveryAndRun(t *testing.T) {
	f, _, server := mcpFixture(t)
	catalog := mcpCall(t, server, "catalog", map[string]any{"search": "rename"})
	if catalog["live"] != false || len(catalog["nouns"].([]map[string]any)) != 1 {
		t.Fatal(catalog)
	}
	first := mcpCall(t, server, "catalog", map[string]any{"limit": 1})
	if first["more"].(int) == 0 || first["next_offset"] != 1 {
		t.Fatal(first)
	}
	next := mcpCall(t, server, "catalog", map[string]any{"limit": 1, "offset": 1})
	if first["hash"] != next["hash"] {
		t.Fatal("unstable catalog hash")
	}
	desc := mcpCall(t, server, "describe", map[string]any{"verb": "widget.ren"})
	if desc["verb"] != "widget.rename" || desc["right"] != "allow" || desc["inputSchema"] == nil {
		t.Fatal(desc)
	}
	result := mcpCall(t, server, "run", map[string]any{"verb": "widget.rename", "target": "w1", "args": map[string]any{"name": "changed"}})
	if result["status"] != "done" || len(f.calls) != 1 || f.calls[0].Target != "w1" || result["entry"] == nil {
		t.Fatal(result, f.calls)
	}
	entry := f.r.History(0)[len(f.r.History(0))-1]
	if entry.Context.Source != "mcp" || !entry.Context.Actor.IsAgent() || entry.Context.Actor.Pane != "p1" {
		t.Fatal(entry)
	}
	state := mcpCall(t, server, "state", map[string]any{"include": []any{"history"}, "limit": 1})
	if len(state["recent"].([]ochazuke.HistoryEntry)) != 1 || state["live"] != false {
		t.Fatal(state)
	}
}
func TestMCPBackendRightsDryRunAndIdentity(t *testing.T) {
	f, _, server := mcpFixture(t)
	for _, name := range []string{"widget.remove", "settings.get"} {
		args := map[string]any{"verb": name, "target": "w1"}
		if name == "settings.get" {
			args["args"] = map[string]any{"name": "agents.rights-gate"}
		}
		result := mcpCall(t, server, "run", args)
		if result["status"] != "refused" {
			t.Fatal(result)
		}
		if name == "widget.remove" && !strings.Contains(result["reason"].(string), "needs the owner's confirmation") {
			t.Fatal(result)
		}
	}
	before := len(f.r.HistoryOf(0, ochazuke.HistoryAction, ochazuke.HistoryChange, ochazuke.HistoryFetch))
	result := mcpCall(t, server, "run", map[string]any{"verb": "widget.rename", "target": "w1", "args": map[string]any{"name": "dry"}, "dry_run": true})
	if result["status"] != "done" || len(f.calls) != 0 || result["entry"] != nil {
		t.Fatal(result, f.calls)
	}
	if len(f.r.HistoryOf(0, ochazuke.HistoryAction, ochazuke.HistoryChange, ochazuke.HistoryFetch)) != before {
		t.Fatal("dry run recorded history")
	}
	result = mcpCall(t, server, "run", map[string]any{"verb": "widget.rename", "target": "missing", "args": map[string]any{"name": "dry"}, "dry_run": true})
	if result["status"] != "failed" {
		t.Fatal("invalid target accepted", result)
	}
	if _, err := server.Call(context.Background(), "run", map[string]any{"verb": "widget.rename", "actor": map[string]any{"kind": "owner"}}); err == nil {
		t.Fatal("actor argument accepted")
	}
	t.Setenv("OCHAZUKE_ACTOR", "owner")
	result = mcpCall(t, server, "run", map[string]any{"verb": "settings.list"})
	if result["status"] != "refused" {
		t.Fatal("identity changed after startup", result)
	}
}
func TestMCPBackendPendingModalMessagesAndAliases(t *testing.T) {
	f, sh, server := mcpFixture(t)
	result := mcpCall(t, server, "run", map[string]any{"verb": "widget.refresh", "target": "w1"})
	if result["status"] != "done" || result["result"] != "fresh" {
		t.Fatal(result)
	}
	f.r.Define("widget").Verb("tell", func(c ochazuke.Ctx) ochazuke.Result { c.Info("hello"); return ochazuke.Done("ok") }).Fetch()
	result = mcpCall(t, server, "run", map[string]any{"verb": "widget.tell"})
	messages := result["messages"].([]ochazuke.UIMessage)
	if len(messages) != 1 || messages[0].Text != "hello" {
		t.Fatal(result)
	}
	result = mcpCall(t, server, "run", map[string]any{"verb": "widget.ask"})
	if result["status"] != "failed" || f.r.TopModal() != "" {
		t.Fatal(result)
	}
	result = mcpCall(t, server, "run", map[string]any{"verb": "widget.list", "acts": "marks"})
	if result["status"] != "refused" {
		t.Fatal(result)
	}
	if err := f.r.AddActionAlias(ochazuke.ActionAlias{Alias: "widget.retitle", Action: "widget.rename", Args: ochazuke.Arguments{"name": "preset"}}); err != nil {
		t.Fatal(err)
	}
	sh.MCPAliases = []mcp.Alias{{Tool: mcp.Tool{Name: "widget_retitle"}, Verb: "widget.retitle"}}
	server, err := sh.NewMCPServer()
	if err != nil {
		t.Fatal(err)
	}
	result = mcpCall(t, server, "run", map[string]any{"verb": "widget.retitle", "target": "w1"})
	if result["status"] != "done" || f.calls[len(f.calls)-1].Args["name"] != "preset" {
		t.Fatal(result, f.calls)
	}
}
func TestMCPShellCommandsAndMirrors(t *testing.T) {
	f := newFixture(t)
	f.r.Config().Disable()
	for _, cmd := range []string{"tools", "instructions", "config"} {
		out, _, err := f.run("", "mcp", cmd)
		if err != nil || out == "" {
			t.Fatal(cmd, out, err)
		}
		if cmd == "tools" {
			var tools []mcp.Tool
			if err := json.Unmarshal([]byte(out), &tools); err != nil || len(tools) != 4 {
				t.Fatal(out, err)
			}
		}
	}
	out, _, err := f.mounted(t, "mcp", "tools")
	if err != nil || !strings.Contains(out, `"catalog"`) {
		t.Fatal(out, err)
	}
	var output bytes.Buffer
	sh := New(f.r, "tool", strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-11-25\"}}\n"), &output, io.Discard)
	if err := sh.Run(context.Background(), []string{"mcp", "serve"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"protocolVersion":"2025-11-25"`) {
		t.Fatal(output.String())
	}
}

func TestMCPBackendDryRunGuardAndPolicyHash(t *testing.T) {
	f, _, server := mcpFixture(t)
	before := mcpCall(t, server, "catalog", map[string]any{})["hash"]
	if err := f.r.Settings().Set(ochazuke.AgentRightsSetting, `[{"match":"widget.rename","right":"never"}]`); err != nil {
		t.Fatal(err)
	}
	after := mcpCall(t, server, "catalog", map[string]any{})["hash"]
	if before == after {
		t.Fatal("effective policy did not change hash")
	}
	if err := f.r.Settings().Set(ochazuke.AgentRightsGateSetting, false); err != nil {
		t.Fatal(err)
	}
	desc := mcpCall(t, server, "describe", map[string]any{"verb": "widget.rename"})
	if desc["right"] != "allow" {
		t.Fatal(desc)
	}
	f.r.DefineGuard("mcp-ready", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return false })
	if err := f.r.AddVerbAvailability("widget.rename", "mcp-ready"); err != nil {
		t.Fatal(err)
	}
	result := mcpCall(t, server, "run", map[string]any{"verb": "widget.rename", "target": "w1", "args": map[string]any{"name": "dry"}, "dry_run": true})
	if result["status"] != "failed" || !strings.Contains(result["reason"].(string), "not applicable") {
		t.Fatal(result)
	}
	if len(f.calls) != 0 {
		t.Fatal("guarded dry run called handler")
	}
}

func TestMCPBackendNoStaleEntryOnPrepareFailure(t *testing.T) {
	_, sh, server := mcpFixture(t)
	result := mcpCall(t, server, "run", map[string]any{"verb": "widget.list"})
	if result["entry"] == nil {
		t.Fatal(result)
	}
	sh.Prepare = func(context.Context) error { return io.ErrClosedPipe }
	result = mcpCall(t, server, "run", map[string]any{"verb": "widget.list"})
	if result["status"] != "failed" || result["entry"] != nil || result["undo"] != nil {
		t.Fatal("stale entry", result)
	}
}

func TestMCPCommandKeepsGlobalArguments(t *testing.T) {
	_, sh, server := mcpFixture(t)
	sh.MCPCommand = []string{"/tmp/example tool", "--data", "/tmp/fixture's data"}
	result := mcpCall(t, server, "run", map[string]any{"verb": "widget.remove", "target": "w1"})
	command, _ := result["owner_command"].(string)
	if !strings.Contains(command, "'--data' '/tmp/fixture'\\''s data'") {
		t.Fatal(command)
	}
	var out bytes.Buffer
	sh.Stdout = &out
	if err := sh.Run(context.Background(), []string{"mcp", "config"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"--data"`) || !strings.Contains(out.String(), `"/tmp/example tool"`) {
		t.Fatal(out.String())
	}
}

func TestMCPShellKeepsAppNoun(t *testing.T) {
	f := newFixture(t)
	f.r.DefineOwn("mcp").Verb("show", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("app-owned") })
	for _, mounted := range []bool{false, true} {
		var out string
		var err error
		if mounted {
			out, _, err = f.mounted(t, "mcp", "show")
		} else {
			out, _, err = f.run("", "mcp", "show")
		}
		if err != nil || !strings.Contains(out, "app-owned") {
			t.Fatal(out, err)
		}
	}
}

func TestMCPBackendExtensionsOptIn(t *testing.T) {
	f, _, server := mcpFixture(t)
	mounted := MountNouns(f.r, []CatalogNoun{{Name: "external", Verbs: []CatalogVerb{{Name: "list", History: ochazuke.HistoryFetch}}}}, HostMount{Local: func(n string) string { return "ext." + n }, Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } }})
	if len(mounted.Warnings) > 0 {
		t.Fatal(mounted.Warnings)
	}
	hidden := mcpCall(t, server, "catalog", map[string]any{"noun": "ext.external"})
	if len(hidden["nouns"].([]map[string]any)) != 0 {
		t.Fatal("extension listed by default", hidden)
	}
	if err := f.r.Settings().Set("mcp.include-extensions", true); err != nil {
		t.Fatal(err)
	}
	shown := mcpCall(t, server, "catalog", map[string]any{"noun": "ext.external"})
	if len(shown["nouns"].([]map[string]any)) != 1 || shown["hash"] == hidden["hash"] {
		t.Fatal(shown, hidden)
	}
}

func TestMCPExplicitIdentityOverridesEnvironment(t *testing.T) {
	t.Setenv("OCHAZUKE_ACTOR", "invalid")
	_, _, server := mcpFixture(t)
	result := mcpCall(t, server, "run", map[string]any{"verb": "settings.list"})
	if result["status"] != "refused" {
		t.Fatal(result)
	}
}
