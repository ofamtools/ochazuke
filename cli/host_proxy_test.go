package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

func TestRunMountedCapturedAndForeground(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		foreground   bool
		timeout      time.Duration
		want         string
		fail         bool
	}{
		{name: "text", script: `printf '"one\\ntwo"'`, want: "one\ntwo"},
		{name: "failure", script: `printf 'remote refusal' >&2; exit 3`, fail: true, want: "remote refusal"},
		{name: "timeout", script: `sleep 1`, timeout: time.Millisecond, fail: true, want: "limit"},
		{name: "foreground", foreground: true, timeout: time.Nanosecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ochazuke.NewRuntime()
			r.Config().Disable()
			marker := filepath.Join(t.TempDir(), "finished")
			if tc.foreground {
				tc.script = `sleep 0.02; printf handed-over > "$MARKER"`
			}
			var value any
			var workErr error
			r.Define("remote").Verb("read", func(ochazuke.Ctx) ochazuke.Result {
				return RunMounted(CatalogVerb{Foreground: tc.foreground}, ProxyCommand{Bin: "/bin/sh", Args: []string{"-c", tc.script}, Env: append(os.Environ(), "MARKER="+marker), Timeout: tc.timeout, WaitDelay: 5 * time.Second}).Then(func(_ ochazuke.Ctx, v any, err error) { value, workErr = v, err })
			})
			if _, err := r.Execute(ochazuke.Request{Noun: "remote", Verb: "read"}); err != nil {
				t.Fatal(err)
			}
			if err := Settle(r, nil); err != nil && !tc.fail {
				t.Fatal(err)
			}
			if tc.fail {
				if workErr == nil || !strings.Contains(workErr.Error(), tc.want) {
					t.Fatalf("error=%v", workErr)
				}
				return
			}
			if tc.foreground {
				data, err := os.ReadFile(marker)
				if err != nil || string(data) != "handed-over" {
					t.Fatalf("terminal process=%q %v", data, err)
				}
			} else if value != tc.want {
				t.Fatalf("answer=%#v", value)
			}
		})
	}
}

func TestMountedRowGuardsDeferToExtension(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	calls := 0
	MountNouns(r, []CatalogNoun{{Name: "item", Verbs: []CatalogVerb{{Name: "read", Foreground: true}}}}, HostMount{Local: func(n string) string { return "ext." + n }, Proxy: func(string, CatalogVerb) any {
		return func(ochazuke.Ctx) ochazuke.Result { calls++; return ochazuke.Failed(errors.New("extension refused")) }
	}})
	r.Instance("row", ochazuke.Kind("ext.item"))
	if err := r.SetFocus("row"); err != nil {
		t.Fatal(err)
	}
	row := ochazuke.Do("ext.item.read").Guard("remote-only")
	row.Noun, row.Mode, row.Keys = "ext.item", "normal", "v"
	if err := r.Bind(row); err == nil {
		t.Fatal("a host row lost its unknown guard")
	}
	row.When = "" // Only the catalog-load path strips producing-runtime guards.
	if err := r.Bind(row); err != nil {
		t.Fatal(err)
	}
	host := row
	host.Keys, host.When = "Z", "input"
	if err := r.Bind(host); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(ochazuke.MacroKey{Key: "Z"}); err != nil || calls != 0 {
		t.Fatal("host guard was not retained")
	}
	if _, err := r.Dispatch(ochazuke.MacroKey{Key: "v"}); err == nil || err.Error() != "extension refused" || calls != 1 {
		t.Fatalf("remote guard: calls=%d err=%v", calls, err)
	}
	data, err := Catalog(r, "host", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, n := range doc.Nouns {
		if n.Name == "ext.item" {
			if len(n.Verbs) != 1 || !n.Verbs[0].Foreground {
				t.Fatal("foreground lost in catalog")
			}
			return
		}
	}
	t.Fatal("mounted noun missing from catalog")
}
