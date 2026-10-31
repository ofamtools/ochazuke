package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

var testContext = context.Background()

var realTestHome string

// TestMain isolates even a test that forgets Options.Home. os/user supplies
// the login home independently of the test runner's already-isolated HOME.
func TestMain(m *testing.M) {
	if u, err := user.Current(); err == nil {
		realTestHome = u.HomeDir
	}
	if realTestHome == "" {
		panic("cannot identify login home for setup test guard")
	}
	if home, err := filepath.EvalSymlinks(realTestHome); err == nil {
		realTestHome = home
	}
	home, err := os.MkdirTemp("", "ochazuke-setup-test-home-")
	if err != nil {
		panic(err)
	}
	for key, dir := range map[string]string{"HOME": "", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data", "CODEX_HOME": "codex", "CLAUDE_CONFIG_DIR": "claude"} {
		path := filepath.Join(home, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			panic(err)
		}
		if err := os.Setenv(key, path); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

func guardTestPaths(t *testing.T, s *Setup) {
	t.Helper()
	paths := []string{s.options.Home, s.options.Bin}
	for _, i := range s.items {
		r, err := s.render(i)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, r.Path)
		if r.Probe != nil {
			paths = append(paths, r.Probe.Path)
		}
		for _, req := range i.Requires {
			if strings.HasPrefix(req, "dir:") {
				path, err := s.expand(strings.TrimPrefix(req, "dir:"))
				if err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
		}
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		rel, err := filepath.Rel(realTestHome, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("setup test resolved a path in the owner's login home: %s", path)
		}
	}
}

func put(t *testing.T, path, stringData string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(stringData), 0640); err != nil {
		t.Fatal(err)
	}
}
func get(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func newSetup(t *testing.T, items []Item, opts Options) *Setup {
	t.Helper()
	if opts.Tool == "" {
		opts.Tool = "tool"
	}
	if opts.Home == "" {
		opts.Home = t.TempDir()
	}
	createBin := opts.Bin == ""
	if createBin {
		opts.Bin = filepath.Join(opts.Home, "bin", "tool")
	}
	s, err := New(items, opts)
	if err != nil {
		t.Fatal(err)
	}
	guardTestPaths(t, s)
	if createBin {
		put(t, s.options.Bin, "binary")
	}
	return s
}

func TestSetupDefaultHomeIsIsolated(t *testing.T) {
	s, err := New(kindsFixture()[:1], Options{Tool: "tool"})
	if err != nil {
		t.Fatal(err)
	}
	guardTestPaths(t, s)
	want, _ := os.UserHomeDir()
	want, _ = filepath.EvalSymlinks(want)
	if s.options.Home != want {
		t.Fatalf("default home %s != isolated HOME %s", s.options.Home, want)
	}
}

// kindsFixture is a small app declaration: one item of every kind, loaded from
// the very same JSON representation an app embeds or a user puts in setup.json.
func kindsFixture() []Item {
	return []Item{
		{ID: "file", Name: "Extension", Kind: File, Path: "~/extension", Comment: "//", Content: "extension v1\n"},
		{ID: "block", Name: "Rules", Kind: Block, Path: "~/AGENTS.md", Content: "Use this tool.\n"},
		{ID: "json", Name: "JSON server", Kind: JSON, Path: "~/servers.json", Pointer: "/servers/tool", Content: `{"name":"tool","version":1}`, Match: Match{Fields: []string{"/name"}}},
		{ID: "array", Name: "Hooks", Kind: JSONArray, Path: "~/hooks.json", Pointer: "/hooks", Content: `[{"command":"tool-owned v1"}]`, Match: Match{Field: "/command", Contains: "tool-owned"}},
		{ID: "command", Name: "Harness", Kind: Command, Probe: &Probe{Path: "~/command.json", Pointer: "/servers/tool"}, Content: `{"name":"tool","version":1}`, Match: Match{Fields: []string{"/name"}}, Install: []string{"harness", "add", "{{.Bin}}"}, Uninstall: []string{"harness", "remove"}},
		{ID: "check", Name: "PATH", Kind: Check, Check: []string{"harness", "check"}, Advice: "export PATH=~/bin:$PATH"},
	}
}

func TestSetupKindsStates(t *testing.T) {
	for _, item := range kindsFixture() {
		for _, state := range []State{Missing, Installed, Outdated, Conflict, External, NotApplicable, Skipped} {
			if item.Kind == Check && (state == Missing || state == Outdated || state == Conflict || state == External) {
				continue
			}
			t.Run(item.ID+"/"+string(state), func(t *testing.T) {
				home := t.TempDir()
				bin := filepath.Join(home, "bin", "harness")
				put(t, bin, "#!/bin/sh\nexit 0\n")
				if err := os.Chmod(bin, 0700); err != nil {
					t.Fatal(err)
				}
				opts := Options{Tool: "tool", Home: home, Env: map[string]string{"PATH": filepath.Dir(bin)}}
				if state == Skipped {
					opts.Skip = []string{item.ID}
				}
				if state == NotApplicable {
					item.Requires = []string{"bin:unavailable-harness"}
				}
				s := newSetup(t, []Item{item}, opts)
				rendered, err := s.render(item)
				if err != nil {
					t.Fatal(err)
				}
				path := rendered.Path
				if item.Kind == Command {
					path = rendered.Probe.Path
				}
				switch state {
				case Installed, Outdated:
					if item.Kind == Check {
						break
					}
					if item.Kind == Command {
						v := rendered.Content
						if state == Outdated {
							v = strings.ReplaceAll(v, `"version":1`, `"version":0`)
						}
						put(t, path, `{"servers":{"tool":`+v+`}}`)
					} else {
						out, _, e := s.fileEdit(rendered, snapshot{}, false)
						if e != nil {
							t.Fatal(e)
						}
						data := string(out.data)
						if state == Outdated {
							data = strings.ReplaceAll(data, "v1", "v0")
							data = strings.ReplaceAll(data, `"version": 1`, `"version": 0`)
							data = strings.ReplaceAll(data, "Use this tool.", "Old rules.")
						}
						put(t, path, data)
					}
				case Conflict:
					data := "someone else's file"
					switch item.Kind {
					case Block:
						b, _ := markers(rendered, "tool")
						data = b + "\nunterminated"
					case JSON, Command:
						data = `{"servers":{"tool":{"name":"different"}}}`
					case JSONArray:
						data = `{"hooks":12}`
					}
					put(t, path, data)
				case External:
					target := filepath.Join(home, "outside")
					put(t, target, "untouched")
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				}
				rows, err := s.List(testContext)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 || rows[0].State != state {
					t.Fatalf("want %s, got %+v", state, rows)
				}
				if state == Conflict || state == External {
					_, err = s.Install(testContext, Selection{})
					if err == nil {
						t.Fatal("unsafe install was accepted")
					}
				}
			})
		}
	}
}

func TestSetupRoundTripBackupsAndMode(t *testing.T) {
	for _, item := range kindsFixture()[:4] {
		t.Run(item.ID, func(t *testing.T) {
			s := newSetup(t, []Item{item}, Options{})
			rendered, _ := s.render(item)
			original := "untouched text without final newline"
			switch item.Kind {
			case File:
				original = "// tool setup: file\nold version\n"
			case JSON, JSONArray:
				original = "{ \"z\": 1, \"a\": {\"preserve\":true} }\n"
			}
			put(t, rendered.Path, original)
			if _, err := s.Install(testContext, Selection{}); err != nil {
				t.Fatal(err)
			}
			backup := rendered.Path + ".tool.bak"
			if get(t, backup) != original {
				t.Fatal("backup differs")
			}
			if info, err := os.Stat(rendered.Path); err != nil || info.Mode().Perm() != 0640 {
				t.Fatal("file mode changed", err)
			}
			if _, err := s.Uninstall(testContext, Selection{}); err != nil {
				t.Fatal(err)
			}
			if get(t, rendered.Path) != original {
				t.Fatal("uninstall did not restore original bytes")
			}
			if get(t, backup) != original {
				t.Fatal("first backup changed")
			}
		})
	}
}

func TestSetupNewFileRoundTrip(t *testing.T) {
	for _, item := range kindsFixture()[:4] {
		t.Run(item.ID, func(t *testing.T) {
			s := newSetup(t, []Item{item}, Options{})
			if _, err := s.Install(testContext, Selection{}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Uninstall(testContext, Selection{}); err != nil {
				t.Fatal(err)
			}
			i, _ := s.render(item)
			if _, err := os.Stat(i.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("new file left behind: %v", err)
			}
		})
	}
}

func TestSetupOptInAndSkip(t *testing.T) {
	i := kindsFixture()[1]
	i.OptIn = true
	i.Group = "shell"
	i.Path = "~/.zshrc"
	i.Content = `eval "$(tool shell-init zsh)"`
	s := newSetup(t, []Item{i}, Options{})
	p, err := s.Plan(testContext, Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Writes) != 0 || p.Items[0].State != Manual || p.Items[0].Next != i.Content {
		t.Fatalf("opt-in: %+v", p)
	}
	if _, err = s.Install(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Install(testContext, Selection{IDs: []string{i.ID}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.List(testContext)
	if rows[0].State != Installed {
		t.Fatal(rows)
	}
	// Even an owned but outdated opt-in block is excluded from bare install.
	rendered, _ := s.render(i)
	put(t, rendered.Path, strings.ReplaceAll(get(t, rendered.Path), "shell-init zsh", "shell-init bash"))
	p, err = s.Plan(testContext, Selection{Group: "shell"})
	if err != nil || len(p.Writes) != 0 || p.Items[0].State != Manual {
		t.Fatalf("outdated opt-in was included: %+v %v", p, err)
	}
	s.options.Skip = []string{i.ID}
	p, _ = s.Plan(testContext, Selection{IDs: []string{i.ID}})
	if p.Items[0].State != Skipped || len(p.Writes) != 0 {
		t.Fatal("skip did not win")
	}
}

func TestSetupOptInExternalDoesNotBlockDefaults(t *testing.T) {
	items := kindsFixture()[:2]
	items[1].OptIn = true
	s := newSetup(t, items, Options{})
	i, _ := s.render(items[1])
	outside := filepath.Join(t.TempDir(), "rules")
	put(t, outside, "external")
	if err := os.Symlink(outside, i.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Install(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	if get(t, outside) != "external" {
		t.Fatal("external opt-in changed")
	}
}

func TestSetupMatchingCommands(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		args          []string
		state         State
	}{{"bare", "tool", []string{"mcp"}, Installed}, {"other", "other", []string{"mcp"}, Conflict}, {"wrong-args", "tool", []string{"serve"}, Conflict}, {"old-path", "/gone/tool", []string{"mcp"}, Outdated}, {"current", "CURRENT", []string{"mcp"}, Installed}, {"current-gone", "CURRENT-GONE", []string{"mcp"}, Outdated}} {
		t.Run(tc.name, func(t *testing.T) {
			i := kindsFixture()[2]
			i.Content = `{"command":"{{.Bin}}","args":["mcp"]}`
			i.Match = Match{Command: "/command", Args: "/args"}
			s := newSetup(t, []Item{i}, Options{})
			command := tc.command
			if strings.HasPrefix(command, "CURRENT") {
				command = s.options.Bin
			}
			if tc.command == "CURRENT-GONE" {
				if err := os.Remove(command); err != nil {
					t.Fatal(err)
				}
			}
			v := plain(map[string]any{"command": command, "args": tc.args})
			rendered, _ := s.render(i)
			put(t, rendered.Path, `{"servers":{"tool":`+string(v)+`}}`)
			rows, err := s.List(testContext)
			if err != nil || rows[0].State != tc.state {
				t.Fatalf("%+v %v", rows, err)
			}
			p, _ := s.Plan(testContext, Selection{})
			if tc.state == Installed && len(p.Writes) != 0 {
				t.Fatal("adopted command would be rewritten")
			}
		})
	}
}

func TestSetupUninstallPreservesLaterChanges(t *testing.T) {
	i := kindsFixture()[2]
	s := newSetup(t, []Item{i}, Options{})
	r, _ := s.render(i)
	put(t, r.Path, `{"original":1}`)
	if _, err := s.Install(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	current := get(t, r.Path)
	put(t, r.Path, strings.Replace(current, `"original": 1`, `"original": 2`, 1))
	if _, err := s.Uninstall(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	if !sameJSON([]byte(get(t, r.Path)), []byte(`{"original":2}`)) {
		t.Fatalf("later change lost: %s", get(t, r.Path))
	}
}

func TestSetupJSONKeyOrderAndEscapes(t *testing.T) {
	i := kindsFixture()[2]
	i.Pointer = "/a~1b/~0item"
	s := newSetup(t, []Item{i}, Options{})
	r, _ := s.render(i)
	put(t, r.Path, `{"z":0,"a/b":{"before":1},"a":2}`)
	if _, err := s.Install(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	got := get(t, r.Path)
	if strings.Index(got, `"z"`) > strings.Index(got, `"a/b"`) || strings.Index(got, `"a/b"`) > strings.LastIndex(got, `"a"`) {
		t.Fatal("key order changed", got)
	}
	if !strings.Contains(got, `"~item"`) {
		t.Fatal(got)
	}
}

func TestSetupPlanSharedTargetAndPreflight(t *testing.T) {
	a, b := kindsFixture()[1], kindsFixture()[1]
	a.ID = "first"
	b.ID = "second"
	s := newSetup(t, []Item{a, b}, Options{})
	p, err := s.Plan(testContext, Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Writes) != 1 || !strings.Contains(p.Writes[0].Content, "tool: first") || !strings.Contains(p.Writes[0].Content, "tool: second") {
		t.Fatalf("not composed: %+v", p)
	}
	if err = s.Apply(testContext, p); err != nil {
		t.Fatal(err)
	}
	a, b = kindsFixture()[0], kindsFixture()[1]
	a.Path = "~/first"
	b.Path = "~/second"
	s = newSetup(t, []Item{a, b}, Options{})
	p, err = s.Plan(testContext, Selection{})
	if err != nil {
		t.Fatal(err)
	}
	put(t, p.Writes[1].Path, "a concurrent edit")
	if err = s.Apply(testContext, p); err == nil {
		t.Fatal("stale plan applied")
	}
	if _, err = os.Stat(p.Writes[0].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("earlier target changed")
	}
}

func TestSetupStageFailureLeavesFilesUnchanged(t *testing.T) {
	a, b := kindsFixture()[0], kindsFixture()[1]
	a.Path = "~/first"
	b.Path = "~/second"
	s := newSetup(t, []Item{a, b}, Options{})
	p, err := s.Plan(testContext, Selection{})
	if err != nil {
		t.Fatal(err)
	}
	put(t, p.Writes[1].Path+".tool.bak", "backup")
	if err = os.Chmod(s.options.Home, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(s.options.Home, 0700)
	if err = s.Apply(testContext, p); err == nil {
		t.Skip("filesystem permits writes despite directory mode")
	}
	if _, err = os.Stat(p.Writes[0].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial target write")
	}
}

func TestSetupSymlinkParentAndBackup(t *testing.T) {
	for _, what := range []string{"parent", "backup"} {
		t.Run(what, func(t *testing.T) {
			i := kindsFixture()[1]
			s := newSetup(t, []Item{i}, Options{})
			r, _ := s.render(i)
			outside := t.TempDir()
			target := filepath.Join(outside, "file")
			put(t, target, "outside")
			if what == "parent" {
				i.Path = "~/link/file"
				if err := os.Symlink(outside, filepath.Join(s.options.Home, "link")); err != nil {
					t.Fatal(err)
				}
				s.items = []Item{i}
				rows, err := s.List(testContext)
				if err != nil || rows[0].State != External {
					t.Fatalf("%+v %v", rows, err)
				}
			} else {
				put(t, r.Path, "original")
				if err := os.Symlink(target, r.Path+".tool.bak"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Install(testContext, Selection{}); err == nil {
				t.Fatal("symlink write accepted")
			}
			if get(t, target) != "outside" {
				t.Fatal("wrote through a symlink")
			}
		})
	}
}

func TestSetupProbeTOMLAndJSON(t *testing.T) {
	for _, format := range []string{"toml", "json"} {
		t.Run(format, func(t *testing.T) {
			i := kindsFixture()[4]
			i.Probe = &Probe{Path: "~/probe", Format: format}
			if format == "toml" {
				i.Probe.Table = []string{"servers", "tool"}
			} else {
				i.Probe.Pointer = "/servers/tool"
			}
			s := newSetup(t, []Item{i}, Options{})
			r, _ := s.render(i)
			data := `{"servers":{"tool":{"name":"tool","version":1}}}`
			if format == "toml" {
				data = "[servers.tool]\nname = 'tool'\nversion = 1\n"
			}
			put(t, r.Probe.Path, data)
			rows, err := s.List(testContext)
			if err != nil || rows[0].State != Installed {
				t.Fatalf("%+v %v", rows, err)
			}
			put(t, r.Probe.Path, "not valid "+format)
			rows, err = s.List(testContext)
			if err != nil || rows[0].State != Conflict {
				t.Fatalf("%+v %v", rows, err)
			}
		})
	}
}

func TestSetupCommandRunnerAndHome(t *testing.T) {
	home := t.TempDir()
	home, _ = filepath.EvalSymlinks(home)
	harness := filepath.Join(home, "bin", "harness")
	put(t, harness, "#!/bin/sh\nprintf '%s' \"$HOME\"\n")
	if err := os.Chmod(harness, 0700); err != nil {
		t.Fatal(err)
	}
	i := kindsFixture()[4]
	var calls [][]string
	opts := Options{Tool: "tool", Home: home, Env: map[string]string{"PATH": filepath.Dir(harness)}, Runner: func(ctx context.Context, argv, env []string) ([]byte, error) {
		calls = append(calls, append([]string(nil), argv...))
		var homeSet bool
		for _, e := range env {
			if e == "HOME="+home {
				homeSet = true
			}
			if strings.HasPrefix(e, "XDG_CONFIG_HOME=") && !strings.HasPrefix(e, "XDG_CONFIG_HOME="+home+"/") {
				t.Fatal("host XDG leaked", e)
			}
		}
		if !homeSet {
			t.Fatal("HOME not isolated")
		}
		path := filepath.Join(home, "command.json")
		switch argv[1] {
		case "add":
			put(t, path, `{"servers":{"tool":{"name":"tool","version":1}}}`)
		case "remove":
			return nil, os.Remove(path)
		default:
			t.Fatal(argv)
		}
		return nil, nil
	}}
	s := newSetup(t, []Item{i}, opts)
	if _, err := s.Install(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Uninstall(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0][2] != s.options.Bin {
		t.Fatal(calls)
	}
	// Exercise a real fake harness CLI on PATH as a read-only check.
	i = kindsFixture()[5]
	s = newSetup(t, []Item{i}, Options{Home: home, Env: opts.Env})
	rows, err := s.List(testContext)
	if err != nil || rows[0].State != Installed {
		t.Fatalf("%+v %v", rows, err)
	}
	s.options.Runner = func(context.Context, []string, []string) ([]byte, error) { return nil, errors.New("probe failed") }
	rows, err = s.List(testContext)
	if err != nil || rows[0].State != Manual || rows[0].Next != i.Advice {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestSetupCommandOutputProbe(t *testing.T) {
	i := kindsFixture()[4]
	i.Check = []string{"harness", "list"}
	i.Probe = &Probe{Where: map[string]string{"/name": "tool"}, Pointer: "/data"}
	home := t.TempDir()
	h := filepath.Join(home, "harness")
	put(t, h, "#!/bin/sh\nprintf '%s' '[{\"name\":\"tool\",\"data\":{\"name\":\"tool\",\"version\":1}}]'\n")
	if err := os.Chmod(h, 0700); err != nil {
		t.Fatal(err)
	}
	s := newSetup(t, []Item{i}, Options{Home: home, Env: map[string]string{"PATH": home}})
	rows, err := s.List(testContext)
	if err != nil || rows[0].State != Installed {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestSetupCommandPlanRefusesNewConflict(t *testing.T) {
	i := kindsFixture()[4]
	home := t.TempDir()
	harness := filepath.Join(home, "harness")
	put(t, harness, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(harness, 0700); err != nil {
		t.Fatal(err)
	}
	ran := false
	s := newSetup(t, []Item{i}, Options{Home: home, Env: map[string]string{"PATH": home}, Runner: func(context.Context, []string, []string) ([]byte, error) { ran = true; return nil, nil }})
	p, err := s.Plan(testContext, Selection{})
	if err != nil {
		t.Fatal(err)
	}
	rendered, _ := s.render(i)
	put(t, rendered.Probe.Path, `{"servers":{"tool":{"name":"someone-else"}}}`)
	if err := s.Apply(testContext, p); err == nil || ran {
		t.Fatalf("stale command plan executed: %v, ran %v", err, ran)
	}
}

func TestSetupExpansionAndLoading(t *testing.T) {
	t.Setenv("CODEX_HOME", "/must-not-use-host-home")
	i := kindsFixture()[1]
	i.Path = "${CODEX_HOME:-${CONFIG_ROOT:-~/.codex}}/AGENTS.md"
	s := newSetup(t, []Item{i}, Options{})
	r, err := s.render(i)
	if err != nil || r.Path != filepath.Join(s.options.Home, ".codex", "AGENTS.md") {
		t.Fatalf("%s %v", r.Path, err)
	}
	b, _ := json.Marshal(kindsFixture())
	items, err := Load(fstest.MapFS{"setup.json": {Data: b}}, "setup.json")
	if err != nil || !reflect.DeepEqual(items, kindsFixture()) {
		t.Fatal("embedded fixture differs", err)
	}
	if _, err = Decode(strings.NewReader(`[{"typo":1}]`)); err == nil {
		t.Fatal("unknown field allowed")
	}
	if _, err = Decode(bytes.NewReader(append(b, []byte(" []")...))); err == nil {
		t.Fatal("trailing document allowed")
	}
}

func TestSetupInvalidItems(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Item)
	}{{"id", func(i *Item) { i.ID = "../bad" }}, {"kind", func(i *Item) { i.Kind = "special-tool" }}, {"path", func(i *Item) { i.Path = "" }}, {"template", func(i *Item) { i.Content = "{{.Unknown}}" }}, {"markers", func(i *Item) { i.Begin = "only one" }}, {"requires", func(i *Item) { i.Requires = []string{"magic:tool"} }}} {
		t.Run(tc.name, func(t *testing.T) {
			i := kindsFixture()[1]
			tc.change(&i)
			if _, err := New([]Item{i}, Options{Tool: "tool", Home: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "setup item") {
				t.Fatalf("bad item reason: %v", err)
			}
		})
	}
}

func TestSetupPackageHasNoTUIImports(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", ".")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"bubbletea", "lipgloss", "/ochazuke/cli"} {
		if bytes.Contains(out, []byte(forbidden)) {
			t.Fatal("TUI dependency", string(out))
		}
	}
}

// Follow manages the file symlinked paths point to: a pair of links to one
// file (a dotfiles repo's instructions linked as two tools' files) holds one
// block, install is idempotent, uninstall restores the original, and the
// links stay links.
func TestSetupFollowSymlinkedPair(t *testing.T) {
	block := func(id, path string) Item {
		return Item{ID: id, Name: id, Kind: Block, Path: path, Begin: "<!-- tool:begin -->", End: "<!-- tool:end -->", Content: "## Note\n\nshared", Follow: true}
	}
	s := newSetup(t, []Item{block("one", "~/.a/AGENTS.md"), block("two", "~/.b/CLAUDE.md")}, Options{})
	home := s.options.Home
	source := filepath.Join(home, "repo", "AGENTS.md")
	put(t, source, "# Rules\n")
	for _, link := range []string{filepath.Join(home, ".a", "AGENTS.md"), filepath.Join(home, ".b", "CLAUDE.md")} {
		if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../repo/AGENTS.md", link); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.Plan(testContext, Selection{})
	if err != nil || len(p.Writes) != 1 || p.Writes[0].Path != source {
		t.Fatalf("one write to the link target: %+v %v", p, err)
	}
	if p.Items[0].State != Missing || p.Items[1].State != Installed {
		t.Fatalf("the second item sees the first's block: %+v", p.Items)
	}
	for range 2 {
		if _, err = s.Install(testContext, Selection{}); err != nil {
			t.Fatal(err)
		}
	}
	want := "# Rules\n<!-- tool:begin -->\n## Note\n\nshared\n<!-- tool:end -->\n"
	if got := get(t, source); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	rows, err := s.List(testContext)
	if err != nil || rows[0].State != Installed || rows[1].State != Installed {
		t.Fatalf("%+v %v", rows, err)
	}
	if info, err := os.Lstat(filepath.Join(home, ".a", "AGENTS.md")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced", err)
	}
	if _, err = s.Uninstall(testContext, Selection{}); err != nil {
		t.Fatal(err)
	}
	if got := get(t, source); got != "# Rules\n" {
		t.Fatalf("uninstall left %q", got)
	}
	// A dangling link creates its target; without follow the link is external.
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Install(testContext, Selection{IDs: []string{"one"}}); err != nil {
		t.Fatal(err)
	}
	if got := get(t, source); got != "<!-- tool:begin -->\n## Note\n\nshared\n<!-- tool:end -->\n" {
		t.Fatalf("dangling link: %q", got)
	}
	plain := block("plain", "~/.a/AGENTS.md")
	plain.Follow = false
	s = newSetup(t, []Item{plain}, Options{Home: home})
	if rows, err = s.List(testContext); err != nil || rows[0].State != External {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestSetupFollowRefusedOnCommands(t *testing.T) {
	_, err := New([]Item{{ID: "c", Name: "c", Kind: Check, Check: []string{"true"}, Advice: "x", Follow: true}}, Options{Tool: "tool"})
	if err == nil || !strings.Contains(err.Error(), "follow") {
		t.Fatal(err)
	}
}
