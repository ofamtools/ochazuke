// Command catalog-mcp is a small persistent task catalog served through the
// generic CLI/MCP shell. All data and configuration live under --data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("catalog-mcp", flag.ContinueOnError)
	dir := flags.String("data", "", "Directory containing example tasks and config (required)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--data DIR is required")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(*dir, "tasks.json")
	tasks := []ochazuke.Thing{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &tasks); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	save := func(next []ochazuke.Thing) error {
		raw, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		f, err := os.CreateTemp(*dir, "tasks-*.tmp")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(raw); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		if err = os.Rename(f.Name(), path); err != nil {
			return err
		}
		tasks = next
		return nil
	}
	find := func(id string) int {
		return slices.IndexFunc(tasks, func(t ochazuke.Thing) bool { return t["id"] == id })
	}
	r := ochazuke.NewRuntime()
	r.Config().App = "catalog-mcp"
	r.Config().Dir = filepath.Join(*dir, "config")
	r.Config().StateDir = filepath.Join(*dir, "state")
	defer r.Config().Flush()
	r.Define("task").About("Small tasks used to try catalog discovery, changes and undo").Props(
		ochazuke.PropSpec{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string"}},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel, Parameter: ochazuke.Parameter{Type: "string", Required: true}},
		ochazuke.PropSpec{Name: "done", Parameter: ochazuke.Parameter{Type: "boolean"}},
	).Store(ochazuke.Store{Kind: ochazuke.Collection, Undo: true, AcceptsKey: true,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			if i := find(id); i >= 0 {
				return tasks[i], nil
			}
			return nil, fmt.Errorf("unknown task %q", id)
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return slices.Clone(tasks), nil },
		Create: func(_ ochazuke.Ctx, values ochazuke.Thing) (ochazuke.Thing, error) {
			id, _ := values["id"].(string)
			if id == "" {
				for n := 1; ; n++ {
					id = "t" + strconv.Itoa(n)
					if find(id) < 0 {
						break
					}
				}
			}
			if find(id) >= 0 {
				return nil, fmt.Errorf("task %s exists", id)
			}
			thing := ochazuke.Thing{"id": id, "title": values["title"], "done": false}
			if v, ok := values["done"]; ok {
				thing["done"] = v
			}
			err := save(append(slices.Clone(tasks), thing))
			return thing, err
		},
		Patch: func(_ ochazuke.Ctx, id string, values ochazuke.Thing) (ochazuke.Thing, error) {
			i := find(id)
			if i < 0 {
				return nil, fmt.Errorf("unknown task %q", id)
			}
			thing := ochazuke.Thing{}
			for k, v := range tasks[i] {
				thing[k] = v
			}
			for k, v := range values {
				thing[k] = v
			}
			next := slices.Clone(tasks)
			next[i] = thing
			err := save(next)
			return thing, err
		},
		Delete: func(_ ochazuke.Ctx, id string) error {
			i := find(id)
			if i < 0 {
				return fmt.Errorf("unknown task %q", id)
			}
			next := slices.Delete(slices.Clone(tasks), i, i+1)
			return save(next)
		},
	})
	shell := cli.New(r, "catalog-mcp", os.Stdin, os.Stdout, os.Stderr, cli.About("A tiny task store for exploring the generic catalog MCP server."))
	shell.Version = "example"
	shell.MCPCommand = []string{os.Args[0], "--data", *dir}
	return shell.Run(context.Background(), flags.Args())
}
