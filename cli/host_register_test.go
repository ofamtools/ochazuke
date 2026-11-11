package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// The test binary acts as both extension and callback host, with all state
// rooted in the supplied temporary directory; no installed app is invoked.
func TestHostRegisterProcess(t *testing.T) {
	if os.Getenv("OCHAZUKE_REGISTER_FIXTURE") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 || len(os.Args) < separator+3 {
		fmt.Fprintln(os.Stderr, "register fixture needs mode and temporary directory")
		os.Exit(1)
	}
	mode, dir := os.Args[separator+1], os.Args[separator+2]
	var err error
	if mode == "extension" {
		host, ok, readErr := HostFromEnv()
		if readErr != nil || !ok {
			fmt.Fprintln(os.Stderr, "register fixture host:", ok, readErr)
			os.Exit(1)
		}
		var raw []byte
		raw, err = host.Run(context.Background(), "ochazuke.register.set", ochazuke.Arguments{"name": "q", "props": map[string]any{"text": "captured output\n", "shape": "char"}})
		if err == nil {
			_, err = os.Stdout.Write(raw)
		}
	} else {
		r := ochazuke.NewRuntime()
		r.Config().Disable()
		r.Config().Dir = filepath.Join(dir, "config")
		r.Config().StateDir = filepath.Join(dir, "state")
		sh := New(r, "host-fixture", strings.NewReader(""), os.Stdout, os.Stderr)
		owner := ochazuke.Owner()
		sh.Actor = &owner
		err = sh.Run(context.Background(), os.Args[separator+3:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestMountedRegisterCallbackRetainsOutputInHost(t *testing.T) {
	dir := t.TempDir()
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	host.Config().StateDir = filepath.Join(dir, "state")
	if err := host.StoreYank(ochazuke.Yank{Text: "previous", Shape: "char"}, "q", false); err != nil {
		t.Fatal(err)
	}
	command, _ := json.Marshal([]string{os.Args[0], "-test.run=^TestHostRegisterProcess$", "--", "host", dir})
	env := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "OCHAZUKE_HOST", "OCHAZUKE_EXTENSION", "OCHAZUKE_CALLER", "OCHAZUKE_HOST_DEPTH", "OCHAZUKE_HOST_CHAIN", "OCHAZUKE_ACTOR", "OCHAZUKE_REGISTER_FIXTURE"} {
			if strings.HasPrefix(entry, key+"=") {
				return true
			}
		}
		return false
	})
	env = append(env, "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_STATE_HOME="+filepath.Join(dir, "state"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "OCHAZUKE_REGISTER_FIXTURE=1", "OCHAZUKE_HOST="+string(command), "OCHAZUKE_EXTENSION=capture")
	mounted := MountNouns(host, []CatalogNoun{{Name: "capture", Verbs: []CatalogVerb{{Name: "write"}}}}, HostMount{
		Local: func(noun string) string { return "tool." + noun },
		Proxy: func(_ string, v CatalogVerb) any {
			return func(ochazuke.Ctx) ochazuke.Result {
				return RunMounted(v, ProxyCommand{Bin: os.Args[0], Args: []string{"-test.run=^TestHostRegisterProcess$", "--", "extension", dir}, Env: env})
			}
		},
	})
	if len(mounted.Warnings) != 0 {
		t.Fatal(mounted.Warnings)
	}
	var stdout, stderr bytes.Buffer
	sh := New(host, "host", strings.NewReader(""), &stdout, &stderr)
	if err := sh.Run(context.Background(), []string{"tool.capture", "write"}); err != nil {
		t.Fatalf("mounted callback: %v\n%s", err, stderr.String())
	}
	value, err := host.ReadYank("q")
	if err != nil || value.Text != "captured output\n" {
		t.Fatalf("live host retained %+v %v; callback stdout %s", value, err, stdout.String())
	}
	restarted := ochazuke.NewRuntime()
	restarted.Config().Disable()
	restarted.Config().StateDir = filepath.Join(dir, "state")
	value, err = restarted.ReadYank("q")
	if err != nil || value.Text != "captured output\n" {
		t.Fatalf("restarted host retained %+v %v", value, err)
	}
}
