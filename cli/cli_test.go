package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/oden"
)

func sampleAppInfo(bs []core.Binding, tuiCalled *[]string) cli.AppInfo {
	modes := []core.Mode{
		{Name: "normal"},
		{Name: "insert", Passthrough: true},
	}
	specs := []core.CommandSpec{
		{ID: "task.new", Description: "Create a new task", Tags: []string{"task", "write"}},
		{ID: "task.done", Description: "Mark task done", Tags: []string{"task"}},
	}
	if bs == nil {
		bs = []core.Binding{
			{Mode: "normal", Scope: "app", Keys: []string{"n"}, Command: "task.new", Hint: "new task"},
			{Mode: "normal", Scope: "list", Keys: []string{"x"}, Command: "task.done", Hint: "done", Hidden: true},
		}
	}
	return cli.AppInfo{
		Name:    "demo",
		Version: "1.2.3",
		Summary: "Demo Ochazuke app",
		Modes:   func() []core.Mode { return modes },
		Bindings: func() []core.Binding {
			return bs
		},
		Specs: func() []core.CommandSpec { return specs },
		ExportConfig: func(jsonl bool) ([]byte, error) {
			if jsonl {
				return []byte("{\"k\":\"theme\",\"v\":\"dark\"}\n{\"k\":\"density\",\"v\":\"balanced\"}\n"), nil
			}
			return []byte("{\n  \"theme\": \"dark\"\n}\n"), nil
		},
		RunSmoke: func(out io.Writer, dump bool) error {
			if dump {
				_, err := fmt.Fprintln(out, "SMOKE DUMP FRAME")
				return err
			}
			_, err := fmt.Fprintln(out, "SMOKE OK")
			return err
		},
		RunTUI: func(_ context.Context, args []string) error {
			if tuiCalled != nil {
				*tuiCalled = append([]string{}, args...)
			}
			return nil
		},
		Manifest: &oden.ExtManifest{
			Name:    "demo",
			Version: "1.2.3",
			Schemas: []oden.ExtSchema{
				{Kind: "task", Title: "Task", Plural: "Tasks"},
			},
		},
		Capabilities: &oden.Capabilities{
			Capabilities: []string{"query", "catalog"},
		},
		Query: func(_ context.Context, kind string, args []string) (oden.ExtQueryResult, error) {
			return oden.ExtQueryResult{
				Items: []oden.ExtRecord{
					{Kind: kind, ID: "t-1", Fields: map[string]any{"title": strings.Join(args, " ")}},
				},
				Total: 1,
			}, nil
		},
	}
}

func runApp(info cli.AppInfo, args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	app := cli.NewApp(info, args,
		oden.WithPath(""),
		oden.WithStdio(strings.NewReader(""), &outBuf, &errBuf),
	)
	err = app.Run(context.Background())
	return outBuf.String(), errBuf.String(), err
}

func TestVersionDispatch(t *testing.T) {
	info := sampleAppInfo(nil, nil)
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		out, _, err := runApp(info, args...)
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
		if strings.TrimSpace(out) != "demo 1.2.3" {
			t.Errorf("%v: got %q, want %q", args, strings.TrimSpace(out), "demo 1.2.3")
		}
	}
}

func TestSmokeDispatch(t *testing.T) {
	info := sampleAppInfo(nil, nil)

	out, _, err := runApp(info, "smoke")
	if err != nil {
		t.Fatalf("smoke: %v", err)
	}
	if strings.TrimSpace(out) != "SMOKE OK" {
		t.Errorf("smoke output = %q, want SMOKE OK", out)
	}

	out, _, err = runApp(info, "--smoke", "--dump")
	if err != nil {
		t.Fatalf("--smoke --dump: %v", err)
	}
	if strings.TrimSpace(out) != "SMOKE DUMP FRAME" {
		t.Errorf("--smoke --dump output = %q, want SMOKE DUMP FRAME", out)
	}
}

func TestBindingListAndCheck(t *testing.T) {
	info := sampleAppInfo(nil, nil)

	// Table without --all omits hidden binding.
	out, _, err := runApp(info, "binding", "list")
	if err != nil {
		t.Fatalf("binding list: %v", err)
	}
	if !strings.Contains(out, "task.new") || strings.Contains(out, "task.done") {
		t.Errorf("binding list should show visible only, got:\n%s", out)
	}

	// JSON with --all includes hidden binding.
	out, _, err = runApp(info, "binding", "list", "--all", "--json")
	if err != nil {
		t.Fatalf("binding list --all --json: %v", err)
	}
	var got []core.Binding
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("binding list json unmarshal: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("binding list --all count = %d, want 2", len(got))
	}

	// Clean bindings pass check.
	out, _, err = runApp(info, "binding", "check")
	if err != nil {
		t.Fatalf("binding check (clean): %v", err)
	}
	if !strings.Contains(out, "0 errors") {
		t.Errorf("binding check clean output = %q", out)
	}

	// Conflicting bindings fail check with ExitError.
	bad := sampleAppInfo([]core.Binding{
		{Mode: "normal", Scope: "app", Keys: []string{"n"}, Command: "task.new"},
		{Mode: "normal", Scope: "app", Keys: []string{"n"}, Command: "task.done"},
	}, nil)
	out, _, err = runApp(bad, "binding", "check", "--json")
	if err == nil {
		t.Fatal("binding check with duplicate expected error, got nil")
	}
	var ee *oden.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("expected ExitError code 1, got %T: %v", err, err)
	}
	var rep cli.BindingCheckReport
	if uerr := json.Unmarshal([]byte(out), &rep); uerr != nil {
		t.Fatalf("unmarshal check report: %v\n%s", uerr, out)
	}
	if rep.OK || rep.Errors == 0 {
		t.Errorf("expected OK=false and Errors>0, got %+v", rep)
	}
}

func TestInspectGroup(t *testing.T) {
	info := sampleAppInfo(nil, nil)

	// actions table & JSON
	out, _, err := runApp(info, "inspect", "actions")
	if err != nil {
		t.Fatalf("inspect actions: %v", err)
	}
	if !strings.Contains(out, "task.new") || !strings.Contains(out, "Create a new task") {
		t.Errorf("inspect actions table = %q", out)
	}

	out, _, err = runApp(info, "inspect", "actions", "--json")
	if err != nil {
		t.Fatalf("inspect actions --json: %v", err)
	}
	var specs []core.CommandSpec
	if err := json.Unmarshal([]byte(out), &specs); err != nil || len(specs) != 2 {
		t.Fatalf("inspect actions json = %v (len %d)", err, len(specs))
	}

	// bindings table & JSON
	out, _, err = runApp(info, "inspect", "bindings", "--json")
	if err != nil {
		t.Fatalf("inspect bindings --json: %v", err)
	}
	var bs []core.Binding
	if err := json.Unmarshal([]byte(out), &bs); err != nil || len(bs) != 2 {
		t.Fatalf("inspect bindings json = %v (len %d)", err, len(bs))
	}

	// modes table & JSON
	out, _, err = runApp(info, "inspect", "modes")
	if err != nil {
		t.Fatalf("inspect modes: %v", err)
	}
	if !strings.Contains(out, "normal") || !strings.Contains(out, "insert") {
		t.Errorf("inspect modes table = %q", out)
	}

	out, _, err = runApp(info, "inspect", "modes", "--json")
	if err != nil {
		t.Fatalf("inspect modes --json: %v", err)
	}
	var modes []core.Mode
	if err := json.Unmarshal([]byte(out), &modes); err != nil || len(modes) != 2 {
		t.Fatalf("inspect modes json = %v (len %d)", err, len(modes))
	}
}

func TestConfigExport(t *testing.T) {
	info := sampleAppInfo(nil, nil)

	out, _, err := runApp(info, "config", "export")
	if err != nil {
		t.Fatalf("config export: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil || m["theme"] != "dark" {
		t.Fatalf("config export json = %q (err=%v)", out, err)
	}

	out, _, err = runApp(info, "config", "export", "--jsonl")
	if err != nil {
		t.Fatalf("config export --jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("config export --jsonl lines = %d, want 2:\n%s", len(lines), out)
	}
}

func TestExtensionProtocol(t *testing.T) {
	info := sampleAppInfo(nil, nil)

	// catalog
	out, _, err := runApp(info, "catalog")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var mf oden.ExtManifest
	if err := json.Unmarshal([]byte(out), &mf); err != nil {
		t.Fatalf("catalog json: %v\n%s", err, out)
	}
	if mf.Name != "demo" || len(mf.Schemas) != 1 || mf.Schemas[0].Kind != "task" {
		t.Errorf("unexpected manifest: %+v", mf)
	}

	// query
	out, _, err = runApp(info, "query", "--kind", "task", "open")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var qr oden.ExtQueryResult
	if err := json.Unmarshal([]byte(out), &qr); err != nil {
		t.Fatalf("query json: %v\n%s", err, out)
	}
	if qr.Total != 1 || len(qr.Items) != 1 || qr.Items[0].Kind != "task" || qr.Items[0].Fields["title"] != "open" {
		t.Errorf("unexpected query result: %+v", qr)
	}

	// capabilities --json
	out, _, err = runApp(info, "capabilities", "--json")
	if err != nil {
		t.Fatalf("capabilities --json: %v", err)
	}
	var caps oden.Capabilities
	if err := json.Unmarshal([]byte(out), &caps); err != nil {
		t.Fatalf("capabilities json: %v\n%s", err, out)
	}
	if caps.Name != "demo" || caps.Version != "1.2.3" || len(caps.Capabilities) != 2 {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
}

func TestUniquePrefixAndRootFallback(t *testing.T) {
	var tuiArgs []string
	info := sampleAppInfo(nil, &tuiArgs)

	// Unique prefix: bi ch -> binding check
	out, _, err := runApp(info, "bi", "ch")
	if err != nil {
		t.Fatalf("bi ch: %v", err)
	}
	if !strings.Contains(out, "0 errors") {
		t.Errorf("bi ch output = %q", out)
	}

	// Unique prefix: ins ac -> inspect actions (note: "info" is an oden builtin)
	out, _, err = runApp(info, "ins", "ac")
	if err != nil {
		t.Fatalf("ins ac: %v", err)
	}
	if !strings.Contains(out, "task.new") {
		t.Errorf("ins ac output = %q", out)
	}

	// Empty args -> RunTUI
	tuiArgs = nil
	if _, _, err := runApp(info); err != nil {
		t.Fatalf("empty args RunTUI: %v", err)
	}

	// Root TUI flag -> RunTUI
	tuiArgs = nil
	if _, _, err := runApp(info, "--theme", "light"); err != nil {
		t.Fatalf("root TUI flag: %v", err)
	}
	if len(tuiArgs) != 2 || tuiArgs[0] != "--theme" || tuiArgs[1] != "light" {
		t.Errorf("RunTUI args = %v, want [--theme light]", tuiArgs)
	}

	// Unknown word -> oden ErrNotFound with "did you mean"
	_, stderr, err := runApp(info, "versin")
	if err == nil {
		t.Fatal("unknown word expected error")
	}
	if !strings.Contains(stderr, "did you mean") || !strings.Contains(stderr, "version") {
		t.Errorf("expected did-you-mean for versin, got stderr:\n%s", stderr)
	}
}

func TestPickerModelHeadless(t *testing.T) {
	items := []oden.Candidate{
		{ID: "1234567890abcdef", Title: "Alpha Task", Subtitle: "backend", Badges: []string{"p1"}},
		{ID: "fedcba0987654321", Title: "Beta Task", Subtitle: "frontend", Badges: []string{"p2"}},
		{ID: "abcdef1234567890", Title: "Gamma Doc", Subtitle: "notes"},
	}

	// Filtering, Readline backspace, and enter selection.
	m := cli.NewPickerModel("Select task", "", items, ui.DefaultDarkTheme())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Type "betx"
	for _, r := range "betx" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.Query() != "betx" {
		t.Fatalf("query = %q, want betx", m.Query())
	}
	if len(m.Picker().Filtered()) != 0 {
		t.Fatalf("filtered count for betx = %d, want 0", len(m.Picker().Filtered()))
	}

	// Backspace removes 'x' -> "bet" matches Beta Task
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.Query() != "bet" {
		t.Fatalf("after backspace query = %q, want bet", m.Query())
	}
	if f := m.Picker().Filtered(); len(f) != 1 || f[0].ID != "fedcba0987654321" {
		t.Fatalf("filtered after backspace = %+v, want Beta Task", f)
	}

	// Verify View renders non-empty output
	if v := m.View(); v.Content == "" {
		t.Error("expected non-empty rendered picker view")
	}

	// Press enter to confirm
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.Done() || m.Cancelled() {
		t.Fatalf("expected Done=true, Cancelled=false; got Done=%v Cancelled=%v", m.Done(), m.Cancelled())
	}
	sel, ok := m.Selected()
	if !ok || sel.ID != "fedcba0987654321" || sel.Title != "Beta Task" {
		t.Fatalf("Selected() = (%+v, %v), want Beta Task", sel, ok)
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd on confirm")
	}

	// Cancel with esc
	m2 := cli.NewPickerModel("Select task", "", items, ui.DefaultDarkTheme())
	m2.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m2.Done() || !m2.Cancelled() {
		t.Fatalf("expected Done=true, Cancelled=true on esc; got Done=%v Cancelled=%v", m2.Done(), m2.Cancelled())
	}
	if _, ok := m2.Selected(); ok {
		t.Fatal("expected no selection after esc cancel")
	}
}
