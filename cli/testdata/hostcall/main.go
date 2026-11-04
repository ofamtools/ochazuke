// This fixture only reads and writes its supplied temporary state directory.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) > 1 && args[0] == "--tool" {
		host, ok, err := cli.HostFromEnv()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("tool has no host")
		}
		if args[1] == "peers" {
			peers, err := host.Peers(context.Background())
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(peers)
		}
		raw, err := host.Run(context.Background(), args[1], nil)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(raw)
		return err
	}
	if len(args) < 2 || args[0] != "--data" {
		return fmt.Errorf("fixture needs --data DIR")
	}
	dir := args[1]
	args = args[2:]
	keyRun := len(args) == 2 && args[0] == "--owner-key"
	r := ochazuke.NewRuntime()
	r.Config().Dir = filepath.Join(dir, "config")
	r.Config().StateDir = filepath.Join(dir, "state")
	r.DeclareSetting(ochazuke.Setting{Name: "extensions.host-call-depth", Type: ochazuke.SettingInteger, Default: 4})
	r.DeclareSetting(ochazuke.Setting{Name: "extensions.host-call-rights", Type: ochazuke.SettingString, Default: "agent", Protected: true, Enum: []string{"agent", "caller"}})
	// Deliberately harmless body: reaching this is still a rights failure.
	r.Define("extension").Verb("register", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("NEVER BODY REACHED") }).Agent(ochazuke.AgentNever)
	r.Define("peer").Verb("inspect", func(c ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done(map[string]any{"actor": c.Actor(), "data": dir})
	}).Agent(ochazuke.AgentAllow)
	r.Define("peer").Verb("ask", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("CONFIRM BODY REACHED") }).Agent(ochazuke.AgentConfirm)
	r.Define("extension").Verb("peers", func(ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done([]cli.HostPeer{{Name: "o-md", Namespace: "md", Version: "0.1.0", Nouns: []cli.HostPeerNoun{{Noun: "md.doc", Local: "doc", Verbs: []cli.HostPeerVerb{{Verb: "show", Agent: "allow"}}}}}})
	}).Agent(ochazuke.AgentAllow)
	r.Define("peer").Verb("route", func(c ochazuke.Ctx) ochazuke.Result {
		executable, err := os.Executable()
		if err != nil {
			return ochazuke.Failed(err)
		}
		command, _ := json.Marshal([]string{executable, "--data", dir})
		actor, _ := json.Marshal(c.Actor())
		env := append(os.Environ(), "OCHAZUKE_HOST="+string(command), "OCHAZUKE_EXTENSION=lane", "OCHAZUKE_ACTOR="+string(actor))
		return cli.RunMounted(cli.CatalogVerb{Name: "route"}, cli.ProxyCommand{Bin: executable, Args: []string{"--tool", ochazuke.StringArg(c.Args, "action", "")}, Env: env}).Then(func(_ ochazuke.Ctx, value any, err error) {
			if keyRun && err == nil {
				json.NewEncoder(os.Stdout).Encode(value)
			}
		})
	}).Agent(ochazuke.AgentAllow)
	op := r.Operations["peer.route"]
	op.Parameters = map[string]ochazuke.Parameter{"action": {Type: "string", Required: true}}
	r.Operations[op.Key()] = op
	if len(args) == 2 && args[0] == "--owner-key" {
		r.EnsureHistory()
		defer r.Config().Flush()
		r.Define("screen").Mode("normal").Bind("v", ochazuke.Do("peer.route", ochazuke.Arguments{"action": args[1]}))
		r.Instance("screen1", ochazuke.Kind("screen"))
		if err := r.SetFocus("screen1"); err != nil {
			return err
		}
		if _, err := r.Dispatch(ochazuke.MacroKey{Key: "v"}); err != nil {
			return err
		}
		return cli.Settle(r, nil)
	}
	sh := cli.New(r, "host-fixture", strings.NewReader(""), os.Stdout, os.Stderr)
	// An explicitly supplied actor tests that Shell cannot bypass the caller
	// narrowing even if a host has already bound an owner transport identity.
	if os.Getenv("FIXTURE_EXPLICIT_OWNER") == "1" {
		actor := ochazuke.Owner()
		sh.Actor = &actor
	}
	return sh.Run(context.Background(), args)
}
