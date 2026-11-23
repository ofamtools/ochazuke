package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/widgets/bindings"
	"ofam.dev/oden"
)

// AppInfo describes an Ochazuke TUI application for NewApp.
type AppInfo struct {
	Name         string
	Version      string
	Summary      string
	HelpText     string
	Modes        func() []core.Mode
	Bindings     func() []core.Binding
	Specs        func() []core.CommandSpec
	ExportConfig func(jsonl bool) ([]byte, error)
	RunTUI       func(ctx context.Context, args []string) error
	RunSmoke     func(out io.Writer, dump bool) error
	Manifest     *oden.ExtManifest
	Capabilities *oden.Capabilities
	Query        func(ctx context.Context, kind string, args []string) (oden.ExtQueryResult, error)
}

// BindingIssue is one finding reported by `binding check`.
type BindingIssue struct {
	Severity string `json:"severity"` // "error" or "warning"
	Kind     string `json:"kind"`
	Index    int    `json:"index"`
	Other    int    `json:"other,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Scope    string `json:"scope,omitempty"`
	Keys     string `json:"keys,omitempty"`
	Command  string `json:"command,omitempty"`
	Message  string `json:"message,omitempty"`
}

// BindingCheckReport is the JSON payload emitted by `binding check --json`.
type BindingCheckReport struct {
	OK       bool           `json:"ok"`
	Checked  int            `json:"checked"`
	Errors   int            `json:"errors"`
	Warnings int            `json:"warnings"`
	Issues   []BindingIssue `json:"issues"`
}

// NewApp constructs a uniform *oden.App for an Ochazuke TUI application with
// built-in commands, interactive TTY picker, binding/inspect/config/smoke/version
// commands, optional extension protocol registration, and root TUI fallback.
func NewApp(info AppInfo, args []string, opts ...oden.Option) *oden.App {
	baseOpts := make([]oden.Option, 0, 2+len(opts))
	baseOpts = append(baseOpts, oden.WithBuiltins(), oden.WithPicker(DefaultPicker()))
	baseOpts = append(baseOpts, opts...)

	app := oden.New(info.Name, args, baseOpts...)
	if info.HelpText != "" && app.HelpText == "" {
		app.HelpText = info.HelpText
	}

	app.Add(
		versionCommand(info),
		bindingCommand(info),
		inspectCommand(info),
	)

	if info.RunSmoke != nil {
		app.Add(smokeCommand(info))
	}

	if info.ExportConfig != nil {
		app.Add(configCommand(info))
	}

	if info.Manifest != nil {
		manifest := *info.Manifest
		if manifest.Name == "" {
			manifest.Name = info.Name
		}
		if manifest.Version == "" {
			manifest.Version = info.Version
		}
		var caps oden.Capabilities
		if info.Capabilities != nil {
			caps = *info.Capabilities
		}
		if caps.Name == "" {
			caps.Name = info.Name
		}
		if caps.Version == "" {
			caps.Version = info.Version
		}
		if caps.Summary == "" {
			caps.Summary = info.Summary
		}
		app.AddExtension(manifest, caps, info.Query)
	}

	if app.Default == nil {
		app.Default = func(ctx context.Context, rootArgs []string) error {
			if len(rootArgs) == 0 {
				if info.RunTUI != nil {
					return info.RunTUI(ctx, nil)
				}
				app.Help(app.Stderr)
				return &oden.ExitError{Code: 1}
			}
			// Root-level flags not claimed by --help/-h/--version/-v/--smoke
			// are forwarded to the TUI when RunTUI is provided.
			if strings.HasPrefix(rootArgs[0], "-") && info.RunTUI != nil {
				return info.RunTUI(ctx, rootArgs)
			}
			// Unknown non-flag words defer back to oden for "did you mean?" reporting.
			return oden.ErrNotFound
		}
	}

	return app
}

func versionCommand(info AppInfo) oden.Command {
	return oden.Command{
		Name:    "version",
		Aliases: []string{"--version", "-v"},
		Summary: "print the application version",
		Usage:   "[--json]",
		Handle: func(c *oden.Ctx) error {
			p, err := c.Parse(nil)
			if err != nil {
				return err
			}
			ver := info.Version
			if ver == "" {
				ver = "dev"
			}
			name := info.Name
			if name == "" {
				name = c.App.Name
			}
			if p.Format() == "json" {
				return c.WriteJSON(map[string]string{
					"name":    name,
					"version": ver,
				})
			}
			return c.Printf("%s %s\n", name, ver)
		},
	}
}

func smokeCommand(info AppInfo) oden.Command {
	params := []oden.Parameter{
		{Name: "dump", Type: "bool", Description: "Dump full rendered frame output"},
	}
	return oden.Command{
		Name:       "smoke",
		Aliases:    []string{"--smoke"},
		Summary:    "run a headless smoke render check",
		Usage:      "[--dump]",
		Parameters: params,
		Handle: func(c *oden.Ctx) error {
			p, err := c.Parse(params)
			if err != nil {
				return err
			}
			return info.RunSmoke(c.Stdout, p.Bool("dump"))
		},
	}
}

func bindingCommand(info AppInfo) oden.Command {
	listParams := []oden.Parameter{
		{Name: "all", Type: "bool", Description: "Include hidden bindings"},
	}
	return oden.Command{
		Name:    "binding",
		Summary: "list and check key bindings",
		Children: []oden.Command{
			{
				Name:       "list",
				Summary:    "list effective key bindings",
				Usage:      "[--all] [--json]",
				Parameters: listParams,
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(listParams)
					if err != nil {
						return err
					}
					all := p.Bool("all")
					var bs []core.Binding
					if info.Bindings != nil {
						for _, b := range info.Bindings() {
							if !all && b.Hidden {
								continue
							}
							bs = append(bs, b)
						}
					}
					if bs == nil {
						bs = []core.Binding{}
					}
					if p.Format() == "json" {
						return c.WriteJSON(bs)
					}
					return writeBindingsTable(c, bs)
				},
			},
			{
				Name:    "check",
				Summary: "validate key bindings for conflicts and errors",
				Usage:   "[--json]",
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(nil)
					if err != nil {
						return err
					}
					var modes []core.Mode
					var bs []core.Binding
					var specs []core.CommandSpec
					if info.Modes != nil {
						modes = info.Modes()
					}
					if info.Bindings != nil {
						bs = info.Bindings()
					}
					if info.Specs != nil {
						specs = info.Specs()
					}

					analysis := bindings.AnalyzeBindings(modes, bs, nil, specs...)
					report := buildBindingCheckReport(bs, analysis)

					if p.Format() == "json" {
						if err := c.WriteJSON(report); err != nil {
							return err
						}
					} else if len(report.Issues) == 0 {
						if err := c.Printf("ok: %d bindings checked, 0 errors\n", report.Checked); err != nil {
							return err
						}
					} else {
						rows := make([][]string, 0, len(report.Issues))
						for _, iss := range report.Issues {
							rows = append(rows, []string{
								iss.Severity,
								iss.Kind,
								iss.Mode,
								iss.Scope,
								iss.Keys,
								iss.Command,
								iss.Message,
							})
						}
						if err := c.WriteTable([]string{"severity", "kind", "mode", "scope", "keys", "command", "message"}, rows); err != nil {
							return err
						}
					}

					if report.Errors > 0 {
						return &oden.ExitError{
							Code: 1,
							Err:  fmt.Errorf("binding check failed: %d error(s)", report.Errors),
						}
					}
					return nil
				},
			},
		},
	}
}

func inspectCommand(info AppInfo) oden.Command {
	return oden.Command{
		Name:    "inspect",
		Summary: "inspect registered actions, bindings, and modes",
		Children: []oden.Command{
			{
				Name:    "actions",
				Summary: "list registered command specs",
				Usage:   "[--json]",
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(nil)
					if err != nil {
						return err
					}
					var specs []core.CommandSpec
					if info.Specs != nil {
						specs = info.Specs()
					}
					if specs == nil {
						specs = []core.CommandSpec{}
					}
					if p.Format() == "json" {
						return c.WriteJSON(specs)
					}
					rows := make([][]string, 0, len(specs))
					for _, s := range specs {
						rows = append(rows, []string{s.ID, s.Description, strings.Join(s.Tags, ", ")})
					}
					return c.WriteTable([]string{"id", "description", "tags"}, rows)
				},
			},
			{
				Name:    "bindings",
				Summary: "list all effective bindings",
				Usage:   "[--json]",
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(nil)
					if err != nil {
						return err
					}
					var bs []core.Binding
					if info.Bindings != nil {
						bs = info.Bindings()
					}
					if bs == nil {
						bs = []core.Binding{}
					}
					if p.Format() == "json" {
						return c.WriteJSON(bs)
					}
					return writeBindingsTable(c, bs)
				},
			},
			{
				Name:    "modes",
				Summary: "list registered input modes",
				Usage:   "[--json]",
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(nil)
					if err != nil {
						return err
					}
					var modes []core.Mode
					if info.Modes != nil {
						modes = info.Modes()
					}
					if modes == nil {
						modes = []core.Mode{}
					}
					if p.Format() == "json" {
						return c.WriteJSON(modes)
					}
					rows := make([][]string, 0, len(modes))
					for _, m := range modes {
						rows = append(rows, []string{m.Name, m.Base, strconv.FormatBool(m.Passthrough)})
					}
					return c.WriteTable([]string{"name", "base", "passthrough"}, rows)
				},
			},
		},
	}
}

func configCommand(info AppInfo) oden.Command {
	exportParams := []oden.Parameter{
		{Name: "jsonl", Type: "bool", Description: "Emit configuration as JSON Lines"},
	}
	return oden.Command{
		Name:    "config",
		Summary: "inspect and export configuration",
		Children: []oden.Command{
			{
				Name:       "export",
				Summary:    "export effective configuration as JSON or JSONL",
				Usage:      "[--jsonl]",
				Parameters: exportParams,
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(exportParams)
					if err != nil {
						return err
					}
					data, err := info.ExportConfig(p.Bool("jsonl"))
					if err != nil {
						return err
					}
					if _, err := c.Stdout.Write(data); err != nil {
						return err
					}
					if len(data) > 0 && data[len(data)-1] != '\n' {
						_, err = io.WriteString(c.Stdout, "\n")
						return err
					}
					return nil
				},
			},
		},
	}
}

func bindingKeysLabel(b core.Binding) string {
	if len(b.Keys) > 0 {
		return strings.Join(b.Keys, " ")
	}
	if b.Class != "" {
		return "<" + b.Class + ">"
	}
	return ""
}

func writeBindingsTable(c *oden.Ctx, bs []core.Binding) error {
	rows := make([][]string, 0, len(bs))
	for _, b := range bs {
		cmd := b.Command
		if cmd == "" && b.Layer != nil {
			cmd = "layer:" + b.Layer.Name
		} else if b.Unbind {
			cmd = "<unbind>"
		}
		rows = append(rows, []string{
			b.Mode,
			b.Scope,
			bindingKeysLabel(b),
			cmd,
			b.When,
			b.Hint,
		})
	}
	return c.WriteTable([]string{"mode", "scope", "keys", "command", "when", "hint"}, rows)
}

func conflictSeverity(kind string) string {
	switch kind {
	case "bad-when", "unknown-command", "duplicate", "prefix-collision":
		return "error"
	default:
		return "warning"
	}
}

func buildBindingCheckReport(bs []core.Binding, analysis []bindings.Analysis) BindingCheckReport {
	report := BindingCheckReport{
		OK:      true,
		Checked: len(bs),
		Issues:  []BindingIssue{},
	}
	for i, a := range analysis {
		b := bs[i]
		for _, cf := range a.Conflicts {
			sev := conflictSeverity(cf.Kind)
			msg := cf.Message
			if msg == "" && cf.Other >= 0 {
				msg = fmt.Sprintf("conflicts with binding #%d", cf.Other)
			}
			iss := BindingIssue{
				Severity: sev,
				Kind:     cf.Kind,
				Index:    i,
				Mode:     b.Mode,
				Scope:    b.Scope,
				Keys:     bindingKeysLabel(b),
				Command:  b.Command,
				Message:  msg,
			}
			if cf.Other >= 0 {
				iss.Other = cf.Other
			}
			report.Issues = append(report.Issues, iss)
			if sev == "error" {
				report.Errors++
				report.OK = false
			} else {
				report.Warnings++
			}
		}
	}
	return report
}
