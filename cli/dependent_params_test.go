package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

func dependentCLI(t *testing.T) (*ochazuke.Runtime, *Shell, *bytes.Buffer) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Register(ochazuke.Operation{Noun: "choice", Verb: "list", Parameters: map[string]ochazuke.Parameter{"kind": {Type: "string"}}, App: func(c ochazuke.Ctx) ochazuke.Result {
		kind := ochazuke.StringArg(c.Args, "kind", "default")
		return ochazuke.Pending(func(context.Context) (any, error) { return []string{kind + "-a", kind + "-b"}, nil })
	}})
	r.Register(ochazuke.Operation{Noun: "thing", Verb: "create", Parameters: map[string]ochazuke.Parameter{"a": {Type: "string"}, "b": {Type: "string", Source: "choice.list", SourceArgs: map[string]string{"kind": "a"}}}, Run: func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return true, nil }})
	out := &bytes.Buffer{}
	return r, New(r, "test-bin", nil, out, io.Discard), out
}

func TestCLICompletionDependentValues(t *testing.T) {
	_, sh, out := dependentCLI(t)
	for _, words := range [][]string{{"thing", "create", "--a", "two", "--b", ""}, {"thing", "create", "--a=two", "--b=two-"}, {"thing", "create", "--json", `{"a":"two"}`, "--b", ""}} {
		choices, err := sh.Complete(words)
		if err != nil || len(choices) != 2 || strings.TrimPrefix(choices[0].ID, "--b=") != "two-a" {
			t.Fatalf("%v: %v %v", words, choices, err)
		}
	}
	app := oden.New("test-bin", []string{"complete", "--", "thing", "create", "--a", "two", "--b", ""})
	Mount(app, sh)
	if err := app.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out.String() != "two-a\ntwo-b\n" {
		t.Fatal(out.String())
	}
	if err := sh.Run(context.Background(), []string{"thing", "create", "--a", "two", "--b", "two-a"}); err != nil {
		t.Fatal(err)
	}
	if err := sh.Run(context.Background(), []string{"thing", "create", "--a", "two", "--b", "default-a"}); err == nil {
		t.Fatal("accepted stale choice")
	}
}

func TestDependentCatalogAndDescribe(t *testing.T) {
	r, sh, _ := dependentCLI(t)
	raw, err := Catalog(r, "test-bin", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compact.String(), `"sourceArgs":{"kind":"a"}`) {
		t.Fatal("catalog lost mapping")
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	mounted := MountNouns(host, doc.AppNouns(), HostMount{Local: func(n string) string { return "ext-" + n }, Proxy: func(string, CatalogVerb) any {
		return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	}})
	if len(mounted.Warnings) != 0 {
		t.Fatal(mounted.Warnings)
	}
	if got := host.Operations["ext-thing.create"].Parameters["b"]; got.Source != "ext-choice.list" || got.SourceArgs["kind"] != "a" {
		t.Fatal(got)
	}
	// describe returns the original parameter metadata, including dependencies.
	value, err := (&mcpBackend{shell: sh}).describe("thing.create")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"sourceArgs":{"kind":"a"}`) {
		t.Fatal("describe lost mapping")
	}
}

func TestCLICompletionScripts(t *testing.T) {
	_, sh, out := dependentCLI(t)
	for _, shell := range []string{"bash", "zsh"} {
		out.Reset()
		if err := sh.completionCommand(context.Background(), []string{shell}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "complete --") {
			t.Fatal(out.String())
		}
	}
}

func TestCLICompletionShellIntegration(t *testing.T) {
	_, sh, out := dependentCLI(t)
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skip(err)
			}
			out.Reset()
			if err := sh.completionCommand(context.Background(), []string{shell}); err != nil {
				t.Fatal(err)
			}
			script := out.String()
			setup := `test-bin() { printf '%s\n' 'two-a' 'two-b'; }
`
			tail := `COMP_WORDS=(test-bin thing create --a two --b "")
COMP_CWORD=6
_ochazuke_complete
printf '%s\n' "${COMPREPLY[@]}"
`
			if shell == "zsh" {
				setup += `compdef() { :; }
compadd() { shift; printf '%s\n' "$@"; }
`
				tail = `words=(test-bin thing create --a two --b "")
CURRENT=7
_ochazuke_complete
`
			}
			cmd := exec.Command(path, "-f", "-s")
			cmd.Stdin = strings.NewReader(setup + script + tail)
			got, err := cmd.CombinedOutput()
			if err != nil || string(got) != "two-a\ntwo-b\n" {
				t.Fatalf("%s: %s %v", shell, got, err)
			}
		})
	}
}
