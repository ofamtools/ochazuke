package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite MCP instructions golden")

func TestMCPInstructionsGolden(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, dir)
	}
	t.Setenv("TUIOS_PANE_ID", "test-pane")
	t.Setenv("OCHAZUKE_HARNESS", "test")
	t.Setenv("OCHAZUKE_ACTOR", "agent")
	t.Setenv("OCHAZUKE_MODEL", "")
	t.Setenv("TUIOS_SESSION", "")
	out, err := os.Create(filepath.Join(dir, "instructions.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	previous := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = previous }()
	if err := run([]string{"--data", filepath.Join(dir, "data"), "mcp", "instructions"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	path := "testdata/mcp-instructions.golden"
	if *update {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("instructions golden differs: %v\n%s", err, got)
	}
}
