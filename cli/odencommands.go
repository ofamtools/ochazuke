package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ofam.dev/oden"
)

// HelpTimeout bounds an external command's --help run for command help.
const HelpTimeout = 5 * time.Second

// odenCommands is the CommandSource of an oden App: its catalog (inline
// commands and <bin>-<name> externals on PATH, with what each shadows).
type odenCommands struct {
	app  *oden.App
	self []string
}

// OdenCommands answers the Commands page from app's catalog. self is the
// argv that runs the app itself, which an inline command is run under
// (self NAME ARGS..., in a pty like any other process); none is
// os.Executable(). An external runs as its path with ODEN_PARENT and
// ODEN_ROOT set, as oden's RunEntry runs it.
func OdenCommands(app *oden.App, self ...string) CommandSource {
	return &odenCommands{app: app, self: self}
}

func (o *odenCommands) Commands() []CommandEntry {
	var out []CommandEntry
	for _, e := range o.app.Catalog().Entries {
		entry := CommandEntry{Name: e.Name, Kind: string(e.Kind), Summary: e.Summary, Source: e.Source, Aliases: e.Aliases}
		for _, claim := range e.Shadowed {
			s := string(claim.Kind) + " " + claim.Source
			if claim.AliasOf != "" {
				s += " (alias of " + claim.AliasOf + ")"
			}
			entry.Shadowed = append(entry.Shadowed, s)
		}
		if e.Kind == oden.KindInline && e.Command != nil {
			entry.Help = o.inlineHelp(e.Command)
		}
		if e.Kind == oden.KindExternal && entry.Summary == "" {
			entry.Summary = filepath.Base(e.Path) + " on PATH"
		}
		out = append(out, entry)
	}
	return out
}

func (o *odenCommands) entry(name string) (*oden.Entry, error) {
	if e := o.app.Catalog().Lookup(name); e != nil {
		return e, nil
	}
	return nil, fmt.Errorf("unknown command %q", name)
}

// Help is an inline command's help as oden's contextual help prints it,
// or an external's own --help output.
func (o *odenCommands) Help(ctx context.Context, name string) (string, error) {
	e, err := o.entry(name)
	if err != nil {
		return "", err
	}
	if e.Kind == oden.KindInline {
		return o.inlineHelp(e.Command), nil
	}
	ctx, cancel := context.WithTimeout(ctx, HelpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Path, append(o.subOf(e), "--help")...)
	cmd.Env = o.externalEnv()
	out, err := cmd.CombinedOutput()
	if len(out) == 0 && err != nil {
		return "", fmt.Errorf("%s --help: %w", e.Path, err)
	}
	return string(out), nil
}

// inlineHelp mirrors oden's contextual help for an inline command: the
// usage line, summary, details and parameters; a group lists its
// subcommands.
func (o *odenCommands) inlineHelp(c *oden.Command) string {
	var b strings.Builder
	if c.Children != nil {
		fmt.Fprintf(&b, "usage: %s %s COMMAND [ARGS...]\n", o.app.Name, c.Name)
		if c.Summary != "" {
			fmt.Fprintf(&b, "\n%s\n", c.Summary)
		}
		children := append([]oden.Command(nil), c.Children...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
		b.WriteString("\ncommands:\n")
		for _, child := range children {
			fmt.Fprintf(&b, "  %-12s %s\n", child.Name, child.Summary)
		}
		return b.String()
	}
	usage := c.Usage
	if usage == "" {
		usage = "[ARGS...]"
	}
	fmt.Fprintf(&b, "usage: %s %s %s\n", o.app.Name, c.Name, usage)
	if c.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Summary)
	}
	if c.Details != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Details)
	}
	for _, p := range c.Parameters {
		required := "optional"
		if p.Required {
			required = "required"
		}
		fmt.Fprintf(&b, "\n  --%s <%s> (%s)", p.Name, p.Type, required)
		if p.Description != "" {
			fmt.Fprintf(&b, ": %s", p.Description)
		}
		b.WriteString("\n")
		if len(p.Choices) != 0 {
			fmt.Fprintf(&b, "    choices: %s\n", strings.Join(p.Choices, ", "))
		}
	}
	return b.String()
}

// Command is the process that runs name: an external's path (with a
// merged subcommand's word first), or the app itself for an inline
// command.
func (o *odenCommands) Command(name string, args []string) (*exec.Cmd, error) {
	e, err := o.entry(name)
	if err != nil {
		return nil, err
	}
	if e.Kind == oden.KindExternal {
		cmd := exec.Command(e.Path, append(o.subOf(e), args...)...)
		cmd.Env = o.externalEnv()
		return cmd, nil
	}
	self := o.self
	if len(self) == 0 {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("find this program to run %s: %w", name, err)
		}
		self = []string{exe}
	}
	if self[0] == "" {
		return nil, errors.New("the program's own path is empty")
	}
	cmd := exec.Command(self[0], append(append(append([]string{}, self[1:]...), e.Name), args...)...)
	cmd.Env = o.env()
	return cmd, nil
}

func (o *odenCommands) subOf(e *oden.Entry) []string {
	if e.Sub != "" {
		return []string{e.Sub}
	}
	return nil
}

func (o *odenCommands) env() []string {
	if o.app.Env != nil {
		return append([]string{}, o.app.Env...)
	}
	return os.Environ()
}

// externalEnv is what oden gives an external: the app's environment plus
// ODEN_PARENT and ODEN_ROOT.
func (o *odenCommands) externalEnv() []string {
	return append(o.env(), oden.EnvParent+"="+o.app.Name, oden.EnvRoot+"="+o.app.RootName())
}
