// Command hello is an oden tool whose TUI is ochazuke's Commands page: with
// no word it opens the page (cli.TUI as oden's Default), listing its inline
// commands and every hello-<name> on PATH. enter runs the selected command
// in a pseudo-terminal on the page, attached; ctrl+a d detaches; i shows
// its help. Try it:
//
//	PATH="$PWD/examples/hello/bin:$PATH" go run ./examples/hello
//	go run ./examples/hello greet ochazuke     # the same commands, no TUI
//	go run ./examples/hello --smoke            # render one run headless
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/oden"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--smoke" {
		if err := smoke(); err != nil {
			fmt.Fprintln(os.Stderr, "hello smoke:", err)
			os.Exit(1)
		}
		return
	}
	app := newApp(args)
	app.Default = cli.TUI(app)
	os.Exit(oden.Report(os.Stderr, "hello", app.Run(context.Background())))
}

// newApp is the tool: a few inline commands beside oden's builtins.
func newApp(args []string, opts ...oden.Option) *oden.App {
	app := oden.New("hello", args, append([]oden.Option{oden.WithBuiltins()}, opts...)...)
	app.Cmd("greet", "say hello", func(c *oden.Ctx) error {
		return c.Printf("hello, %s\n", c.Arg(0, "world"))
	}, "hi")
	app.Cmd("echo", "print the arguments", func(c *oden.Ctx) error {
		return c.Println(strings.Join(c.Args, " "))
	})
	app.Cmd("count", "count to N (default 5), one a second", func(c *oden.Ctx) error {
		n, err := strconv.Atoi(c.Arg(0, "5"))
		if err != nil {
			return err
		}
		for i := 1; i <= n; i++ {
			if err := c.Printf("%d\n", i); err != nil {
				return err
			}
			if i < n {
				time.Sleep(time.Second)
			}
		}
		return nil
	})
	app.Cmd("ask", "ask your name and greet you (reads the terminal)", func(c *oden.Ctx) error {
		if err := c.Printf("your name? "); err != nil {
			return err
		}
		line, err := bufio.NewReader(c.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return err
		}
		return c.Printf("hello, %s!\n", strings.TrimSpace(line))
	})
	return app
}
