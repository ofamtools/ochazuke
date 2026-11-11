package ochazuke

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPaletteCoveringInputSatisfiesUnfocusedSubject(t *testing.T) {
	t.Parallel()
	for _, parameter := range []string{"path", "file"} {
		t.Run(parameter, func(t *testing.T) {
			r := NewRuntime()
			r.Config().Disable()
			NewModel(r)
			file := filepath.Join(t.TempDir(), "requested.json")
			if err := os.WriteFile(file, []byte("requested document"), 0600); err != nil {
				t.Fatal(err)
			}
			var opened string
			r.Define("document").Props(PropSpec{Name: "id", Key: true}).Store(Store{}).Verb("open", func(c Ctx) Result {
				path, _ := c.Args[parameter].(string)
				data, err := os.ReadFile(path)
				if err != nil {
					return Failed(err)
				}
				opened = string(data)
				return Done(nil)
			})
			op, _ := r.Operation("document.open")
			op.Parameters = map[string]Parameter{"id": {Type: "string", Noun: "document", Required: true, Focus: true}, "path": {Type: PropPath, Path: "file", Covers: true}, "file": {Type: PropPath, Path: "file", Covers: true, Named: true}}
			op.Positional = []string{"path"}
			r.Operations.Set(op)
			input := file
			if parameter == "file" {
				input = "file=" + file
			}
			paletteRequest(t, r, "open", "palette", nil)
			paletteLine(t, r, "document open "+input)
			if opened != "requested document" || r.TopModal() != "" {
				t.Fatalf("covering input asked for unrelated id: opened %q modal %q", opened, r.TopModal())
			}
			if missing := r.missingTargets(op, Arguments{parameter: file}, "app"); len(missing) != 0 {
				t.Fatalf("args form still requires covered targets: %v", missing)
			}
			opened = ""
			paletteRequest(t, r, "open", "palette", nil)
			paletteLine(t, r, "document open")
			if opened != "" || r.TopModal() == "" {
				t.Fatalf("absent covering input bypassed required subject: opened %q modal %q", opened, r.TopModal())
			}
		})
	}
}
