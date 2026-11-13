// This fixture is a small tool for the host serve tests: it only reads and
// writes the --data directory it is given.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
	"ofam.dev/oden"
)

func main() {
	os.Exit(oden.Report(os.Stderr, "servetool", run(os.Args[1:])))
}

func run(args []string) error {
	if len(args) < 2 || args[0] != "--data" {
		return errors.New("servetool needs --data DIR")
	}
	dir := args[1]
	args = args[2:]
	r := ochazuke.NewRuntime()
	r.Config().Dir = filepath.Join(dir, "config")
	r.Config().StateDir = filepath.Join(dir, "state")
	file := filepath.Join(dir, "items")
	read := func() []string {
		raw, _ := os.ReadFile(file)
		return strings.Fields(string(raw))
	}
	allow := func(b *ochazuke.NounBuilder) { b.Agent(ochazuke.AgentAllow) }
	// list reads the environment per call, as a served tool must.
	allow(r.Define("item").Verb("list", func(c ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done(map[string]any{"items": read(), "value": os.Getenv("FIXTURE_VALUE"), "actor": c.Actor().Kind, "name": c.Actor().Name})
	}, "List the items").Fetch())
	allow(r.Define("item").Verb("add", func(c ochazuke.Ctx) ochazuke.Result {
		name := ochazuke.StringArg(c.Args, "name", "")
		if name == "" {
			return ochazuke.Failed(errors.New("add needs a name"))
		}
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return ochazuke.Failed(err)
		}
		defer f.Close()
		fmt.Fprintln(f, name)
		return ochazuke.Done(map[string]any{"added": name})
	}, "Add an item"))
	allow(r.Define("item").Verb("slow", func(ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Pending(func(ctx context.Context) (any, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Minute):
				return "slept", nil
			}
		})
	}, "Wait a minute, or until cancelled").Fetch())
	allow(r.Define("item").Verb("stray", func(ochazuke.Ctx) ochazuke.Result {
		fmt.Println("stray print")
		return ochazuke.Done("ok")
	}, "Print to stdout outside the result"))
	// die ends a served process (a crash under a call); run by exec it answers.
	allow(r.Define("item").Verb("die", func(ochazuke.Ctx) ochazuke.Result {
		if os.Getenv(cli.HostPipeEnv) == "1" {
			os.Exit(3)
		}
		return ochazuke.Done("alive")
	}, "Exit when served").Fetch())
	// host asks the host for peer.inspect (Host.Call): over the pipe when
	// served, else by exec of OCHAZUKE_HOST.
	allow(r.Define("item").Verb("host", func(ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Pending(func(ctx context.Context) (any, error) {
			host, ok, err := cli.HostFromEnv()
			if err != nil || !ok {
				return nil, fmt.Errorf("no host: %v", err)
			}
			raw, err := host.Run(ctx, "peer.inspect", nil)
			if err != nil {
				return nil, err
			}
			var v any
			err = json.Unmarshal(raw, &v)
			return v, err
		})
	}, "Ask the host"))
	allow(r.Define("peer").Verb("inspect", func(c ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done(map[string]any{"peer": "inspected", "caller": os.Getenv("OCHAZUKE_CALLER")})
	}, "Answer a host call"))
	if op, ok := r.Operations.Get("item.add"); ok {
		op.Parameters = map[string]ochazuke.Parameter{"name": {Type: "string"}}
	}
	sh := cli.New(r, "servetool", os.Stdin, os.Stdout, os.Stderr, cli.Serves())
	app := oden.New("servetool", args, oden.WithBuiltins())
	cli.Mount(app, sh)
	app.Add(oden.Command{Name: "extra", Summary: "An application command outside the nouns", Run: func(context.Context, []string) error {
		fmt.Println("extra ran")
		return nil
	}})
	return app.Run(context.Background())
}
