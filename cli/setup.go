package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/setup"
	"ofam.dev/oden"
)

const setupInstance = "__setup"
const SetupSkipSetting = "setup.skip"

type setupDeclaration struct {
	items   []setup.Item
	options setup.Options
}

// setupRow keeps the status schema and supplies the registry's declared table
// columns, so --format table also works through Shell and mounted extensions.
type setupRow struct {
	setup.Status
	Cells []string `json:"cells"`
}

func setupRows(rows []setup.Status, err error) ([]setupRow, error) {
	if err != nil {
		return nil, err
	}
	out := make([]setupRow, 0, len(rows))
	for _, row := range rows {
		path := row.Path
		if path == "" && row.Probe != nil {
			path = row.Probe.Path
		}
		out = append(out, setupRow{row, []string{row.ID, string(row.Kind), string(row.State), path, row.Next}})
	}
	return out, nil
}

// DeclareSetup adds the shared registry verbs, without keys or a page. Call
// before Mount. Applications own the declarations; the library owns the kinds.
// With no items it does nothing. setup.json in the app's config directory adds
// user items; it has the same JSON array format as setup.Load(embedFS, path).
func DeclareSetup(r *ochazuke.Runtime, items []setup.Item, options setup.Options) error {
	if len(items) == 0 {
		return nil
	}
	if _, err := setup.New(items, options); err != nil {
		return err
	}
	if r.Instances[setupInstance] != nil {
		return fmt.Errorf("setup items are already declared")
	}
	d := &setupDeclaration{items: append([]setup.Item(nil), items...), options: options}
	r.Builtin(func() {
		r.DeclareSetting(ochazuke.Setting{Name: SetupSkipSetting, Type: ochazuke.SettingList, Default: []string{}, Description: "Setup item ids to skip", Section: "setup"})
		b := r.Define("setup").Columns(ochazuke.Column{Title: "Item"}, ochazuke.Column{Title: "Kind"}, ochazuke.Column{Title: "State"}, ochazuke.Column{Title: "Target"}, ochazuke.Column{Title: "Next step", Wrap: true})
		for _, verb := range setupVerbs {
			v := verb.name
			b.Verb(v, func(c ochazuke.Ctx) ochazuke.Result {
				opts := d.options
				if home, _ := c.Args["home"].(string); home != "" {
					opts.Home = home
				}
				opts.Skip = append(append([]string(nil), opts.Skip...), r.Settings().Strings(SetupSkipSetting)...)
				items := append([]setup.Item(nil), d.items...)
				config := r.Config().Path("setup.json")
				if opts.Home != "" {
					dir := opts.Env["XDG_CONFIG_HOME"]
					if dir == "" {
						dir = filepath.Join(opts.Home, ".config")
					}
					config = filepath.Join(dir, opts.Tool, "setup.json")
				}
				if config != "" {
					f, err := os.Open(config)
					if err == nil {
						user, e := setup.Decode(f)
						_ = f.Close()
						if e != nil {
							return ochazuke.Failed(fmt.Errorf("setup.json: %w", e))
						}
						items = append(items, user...)
					} else if !errors.Is(err, os.ErrNotExist) {
						return ochazuke.Failed(err)
					}
				}
				engine, err := setup.New(items, opts)
				if err != nil {
					return ochazuke.Failed(err)
				}
				ids, _ := c.Args["item"].(string)
				group, _ := c.Args["group"].(string)
				sel := setup.Selection{IDs: strings.Fields(ids), Group: group}
				return ochazuke.Pending(func(ctx context.Context) (any, error) {
					switch v {
					case "install":
						return setupRows(engine.Install(ctx, sel))
					case "uninstall":
						return setupRows(engine.Uninstall(ctx, sel))
					case "plan":
						return engine.Plan(ctx, sel)
					default:
						plan, err := engine.Plan(ctx, sel)
						if err != nil {
							return nil, err
						}
						if v == "show" || v == "copy" {
							if len(plan.Items) != 1 {
								return nil, fmt.Errorf("%s needs exactly one setup item", v)
							}
							if v == "copy" {
								text := plan.Items[0].Content
								if text == "" {
									text = plan.Items[0].Advice
								}
								return text, nil
							}
							return plan.Items[0], nil
						}
						return setupRows(plan.Items, nil)
					}
				}).Label("setup")
			}, verb.description).Quiet().Positional("item")
			if v == "install" || v == "uninstall" {
				b.Confirm("Apply setup " + v + "? Review setup plan first.")
			} else {
				b.Fetch()
			}
			if v == "copy" {
				b.TextResult()
			}
			if v == "list" || v == "status" || v == "install" || v == "uninstall" {
				b.Rows("setup")
			}
			op := r.Operations[r.CanonicalKey("setup."+v)]
			if v == "install" || v == "uninstall" {
				op.HistoryKind = ochazuke.HistoryChange
			}
			op.Parameters = map[string]ochazuke.Parameter{"item": {Type: "string", Rest: true, Required: v == "show" || v == "copy", Description: "Setup item ids (empty selects the default set)"}, "home": {Type: "string", Description: "Use this home directory and isolate harness HOME/XDG paths"}}
			if v == "install" {
				op.Parameters["group"] = ochazuke.Parameter{Type: "string", Description: "Only items in this group"}
			}
			r.Operations[op.Key()] = op
		}
		r.Instance(setupInstance, ochazuke.Kind("setup")).Component = d
	})
	return nil
}

var setupVerbs = []struct{ name, description string }{
	{"status", "Setup state and exact next steps"}, {"list", "List setup items and states"}, {"show", "Show one setup item in full"}, {"plan", "Show all writes and argv without installing"}, {"install", "Install the selected setup items"}, {"uninstall", "Remove only the selected owned integrations"}, {"copy", "Print an item's content or advice for pasting"},
}

// mountSetup is only presentation: every command still uses Shell.Run and the
// registry (confirmation, actors, history and palette share the same handler).
func (s *Shell) mountSetup(app *oden.App) {
	inst := s.Runtime.Instances[setupInstance]
	if inst == nil {
		return
	}
	if _, ok := inst.Component.(*setupDeclaration); !ok {
		return
	}
	if s.Runtime.Nouns["setup"] != nil {
		return
	}
	// An application's own command keeps its name, like every mounted noun.
	if app.Catalog().Lookup("setup") != nil {
		return
	}
	if len(app.Args) > 0 && app.Args[0] == "setup" && (len(app.Args) == 1 || strings.HasPrefix(app.Args[1], "--") && app.Args[1] != "--help") {
		app.Args = append([]string{"setup", "status"}, app.Args[1:]...)
	}
	children := make([]oden.Command, 0, len(setupVerbs))
	for _, verb := range setupVerbs {
		v := verb.name
		op := s.Runtime.Operations[s.Runtime.CanonicalKey("setup."+v)]
		params := make([]oden.Parameter, 0, len(op.Parameters))
		for _, name := range op.ParameterOrder() {
			p := op.Parameters[name]
			params = append(params, oden.Parameter{Name: name, Type: p.Type, Description: p.Description, Required: p.Required})
		}
		children = append(children, oden.Command{Name: v, Summary: verb.description, Source: "ochazuke.setup." + v, Parameters: params, Usage: "[ITEM...] [--home DIR] [--json] [--format table] [--yes]", Run: func(ctx context.Context, args []string) error {
			for _, a := range args {
				if a == "--json" || strings.HasPrefix(a, "--json=") || a == "--format" || strings.HasPrefix(a, "--format=") {
					return s.Run(ctx, append([]string{"ochazuke.setup", v}, args...))
				}
			}
			var out bytes.Buffer
			sh := *s
			sh.Stdout = &out
			if err := sh.Run(ctx, append([]string{"ochazuke.setup", v}, args...)); err != nil {
				return err
			}
			return printSetup(s.stdout(), s.Bin, v, out.Bytes())
		}})
	}
	app.Add(oden.Command{Name: "setup", Summary: "Check and install this tool's integrations", Source: "ochazuke.setup", Children: children})
}

func printSetup(out io.Writer, tool, verb string, raw []byte) error {
	switch verb {
	case "copy":
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, text)
		return err
	case "show":
		_, err := out.Write(raw)
		return err
	case "plan":
		var p setup.Plan
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		for _, st := range p.Items {
			fmt.Fprintf(out, "%s: %s\n", st.ID, st.State)
		}
		for _, w := range p.Writes {
			fmt.Fprintf(out, "write: %s\n%s\n", w.Path, w.Content)
		}
		for _, c := range p.Commands {
			data, _ := json.Marshal(c.Argv)
			fmt.Fprintf(out, "argv: %s\n", data)
		}
		return nil
	default:
		var rows []setup.Status
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		if verb == "list" {
			w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ITEM\tKIND\tSTATE\tTARGET\tNEXT STEP")
			for _, st := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", st.ID, st.Kind, st.State, st.Path, st.Next)
			}
			return w.Flush()
		}
		all := true
		for _, st := range rows {
			fmt.Fprintf(out, "%s: %s", st.ID, st.State)
			if st.State != setup.Installed {
				all = false
				if st.Reason != "" {
					fmt.Fprintf(out, " (%s)", st.Reason)
				}
				if st.Next != "" {
					fmt.Fprintf(out, " — %s", st.Next)
				}
			}
			fmt.Fprintln(out)
		}
		if verb == "status" {
			if all {
				fmt.Fprintln(out, "everything is set up")
			} else {
				fmt.Fprintf(out, "run: %s setup install\n", tool)
			}
		}
		return nil
	}
}
