package ochazuke

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPickerExplicitRootInsideIgnoredAncestor(t *testing.T) {
	home := t.TempDir()
	if data, err := exec.Command("git", "init", "--quiet", "--template=", home).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, data)
	}
	root := filepath.Join(home, "projects")
	for _, name := range []string{"visible", "hidden"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(home, ".gitignore"):             "projects/\n",
		filepath.Join(root, ".gitignore"):             "hidden/\n*.tmp\n",
		filepath.Join(root, "visible", "main.go"):     "package main\n",
		filepath.Join(root, "visible", "scratch.tmp"): "ignored\n",
		filepath.Join(root, "hidden", "secret.txt"):   "ignored\n",
	}
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, recursive := range []bool{false, true} {
		entries, err := indexPickerEntries(context.Background(), root, recursive)
		if err != nil {
			t.Fatal(err)
		}
		want := []PickerEntry{{Path: ".gitignore"}, {Path: "visible", Dir: true}}
		if recursive {
			want = append(want, PickerEntry{Path: "visible/main.go"})
		}
		if !reflect.DeepEqual(pickerEntryShapes(entries), want) {
			t.Fatalf("recursive=%v explicit ignored root entries = %#v, want %#v", recursive, entries, want)
		}
	}
}
