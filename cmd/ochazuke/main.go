package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"ochazuke"
	"ochazuke/examples/demo"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ochazuke:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("ochazuke", flag.ContinueOnError)
	socket := flags.String("socket", filepath.Join(cache, "ochazuke", "app.sock"), "Unix socket identifying the running app")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage:\n  ochazuke keybinding\n  ochazuke [--socket PATH] run [--headless]\n  ochazuke [--socket PATH] NOUN VERB [--json '{...}'] [--target INSTANCE_ID]\n\nDiscover actions: ochazuke operations list\nExample: ochazuke input undo --json '{\"steps\":2}'\nEvery registered operation is available as NOUN VERB.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	rest := flags.Args()
	if len(rest) == 0 {
		flags.Usage()
		return nil
	}
	if rest[0] == "keybinding" {
		return runKeybinding(rest[1:])
	}
	if rest[0] == "run" {
		runFlags := flag.NewFlagSet("run", flag.ContinueOnError)
		headless := runFlags.Bool("headless", false, "Run the same Bubble Tea event loop without terminal rendering")
		if err := runFlags.Parse(rest[1:]); err != nil {
			return err
		}
		if runFlags.NArg() != 0 {
			return fmt.Errorf("unexpected run arguments")
		}
		model := demo.New()
		warnings, err := model.Runtime.ValidateBindings()
		if err != nil {
			return err
		}
		for _, warning := range warnings {
			fmt.Fprintln(os.Stderr, "ochazuke: deprecated:", warning)
		}
		options := []tea.ProgramOption{}
		if *headless {
			options = append(options, tea.WithInput(nil), tea.WithoutRenderer(), tea.WithoutSignalHandler())
		}
		program := tea.NewProgram(model, options...)
		server, err := ochazuke.Listen(*socket, func(msg ochazuke.CallMsg) { program.Send(msg) })
		if err != nil {
			return err
		}
		defer server.Close()
		if *headless {
			fmt.Fprintln(os.Stderr, "Listening:", *socket)
		}
		_, err = program.Run()
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("expected NOUN VERB; use operations list to discover actions")
	}
	callFlags := flag.NewFlagSet(rest[0]+" "+rest[1], flag.ContinueOnError)
	raw := callFlags.String("json", "{}", "Operation arguments as a JSON object")
	target := callFlags.String("target", "", "Instance ID (input, list, row, page, modal body), or focused-input (default current context)")
	if err := callFlags.Parse(rest[2:]); err != nil {
		if err == flag.ErrHelp {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return operationHelp(ctx, *socket, rest[0], rest[1], callFlags.Output())
		}
		return err
	}
	if callFlags.NArg() != 0 {
		return fmt.Errorf("unexpected operation arguments")
	}
	operationArgs, err := ochazuke.ParseArguments(*raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := ochazuke.Call(ctx, *socket, ochazuke.Request{Noun: rest[0], Verb: rest[1], Target: *target, Args: operationArgs})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
