package cli

import (
	"os"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

// A mounted verb's captured answer is presented by the kind its catalog row
// declares, on a key: text and Markdown open their viewer, rows open the
// results table, a plain status (any other kind) opens nothing and leaves
// the host's status line. keys.text-results and keys.table-results turn the
// first two off. No tool, noun or verb name is consulted.
func TestMountedAnswerPresentedByDeclaredKind(t *testing.T) {
	for _, tc := range []struct {
		name, result, script, off, modal string
	}{
		{"text", "text", `printf '"line one\\nline two"'`, "", ochazuke.TextViewID},
		{"markdown", "markdown", `printf '"# Heading\\n\\nbody"'`, "", "markdown-view"},
		{"rows", "rows", `printf '[{"name":"a","size":1},{"name":"b","size":2}]'`, "", "palette-results"},
		{"status", "JSON value", `printf '{"ok":true}'`, "", ""},
		{"text off", "text", `printf '"line one\\nline two"'`, ochazuke.KeyTextResultsSetting, ""},
		{"rows off", "rows", `printf '[{"name":"a"}]'`, ochazuke.KeyTableResultsSetting, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ochazuke.NewRuntime()
			r.Config().Disable()
			if tc.off != "" {
				if err := r.Settings().Set(tc.off, false); err != nil {
					t.Fatal(err)
				}
			}
			MountNouns(r, []CatalogNoun{{Name: "item", Verbs: []CatalogVerb{{Name: "read", Result: tc.result}}}}, HostMount{
				Local: func(n string) string { return "ext." + n },
				Proxy: func(_ string, v CatalogVerb) any {
					return func(ochazuke.Ctx) ochazuke.Result {
						return RunMounted(v, ProxyCommand{Bin: "/bin/sh", Args: []string{"-c", tc.script}, Env: os.Environ(), WaitDelay: 5 * time.Second})
					}
				},
			})
			r.Instance("row", ochazuke.Kind("ext.item"))
			if err := r.SetFocus("row"); err != nil {
				t.Fatal(err)
			}
			row := ochazuke.Do("ext.item.read")
			row.Noun, row.Mode, row.Keys = "ext.item", "normal", "v"
			if err := r.Bind(row); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Dispatch(ochazuke.MacroKey{Key: "v"}); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil {
				t.Fatal(err)
			}
			if got := r.TopModal(); got != tc.modal {
				t.Fatalf("top modal %q, want %q", got, tc.modal)
			}
		})
	}
}
