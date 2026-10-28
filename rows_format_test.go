package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFormatRowsKeepsStructOrder(t *testing.T) {
	type row struct {
		Keys   string `json:"keys"`
		Action string `json:"action"`
		Note   string `json:"note,omitempty"`
	}
	got, err := FormatRows([]row{{Keys: "a", Action: "x"}, {Keys: "<", Action: "y & z", Note: `say "hi", ok: yes`}})
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n" +
		`{"keys": "a", "action": "x"},` + "\n" +
		`{"keys": "<", "action": "y & z", "note": "say \"hi\", ok: yes"}` + "\n" +
		"]\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	var back []row
	if err := json.Unmarshal(got, &back); err != nil || !reflect.DeepEqual(back[1].Note, `say "hi", ok: yes`) {
		t.Fatalf("read back %v %v", back, err)
	}
	if _, err := FormatRowsJSON([]byte(`{"not": "rows"}`)); err == nil {
		t.Fatal("an object is not rows")
	}
}

// config export writes its row files one row per line: the settings
// reference and the actions example.
func TestConfigExportWritesRowFiles(t *testing.T) {
	r, _ := exportFixture(t)
	out := t.TempDir()
	if _, err := r.ExportConfig(out, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{SettingsReferenceFile, ActionsExampleFile} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if lines[0] != "[" || lines[len(lines)-1] != "]" || len(lines)-2 != len(items) {
			t.Fatalf("%s is not one row per line:\n%s", name, data)
		}
		for _, line := range lines[1 : len(lines)-1] {
			if !strings.HasPrefix(line, "{") {
				t.Fatalf("%s: row line %q", name, line)
			}
		}
	}
}
