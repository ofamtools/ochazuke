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
	"slices"
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
		out = append(out, setupRow{row, []string{row.ID, string(row.Kind), string(row.Status), path, row.Next}})
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
		b := r.Define("setup").Columns(ochazuke.Column{Title: "Item"}, ochazuke.Column{Title: "Kind"}, ochazuke.Column{Title: "Status"}, ochazuke.Column{Title: "Target"}, ochazuke.Column{Title: "Next step", Wrap: true})
		b.Props(setupProps...).Store(ochazuke.Store{Kind: ochazuke.KindCollection, Facts: true,
			List: func(c ochazuke.Ctx, _ ochazuke.ListQuery) ([]ochazuke.Thing, error) { return d.things(r, "", nil) },
			Get: func(c ochazuke.Ctx, id string) (ochazuke.Thing, error) {
				things, err := d.things(r, "", []string{id})
				if err != nil {
					return nil, err
				}
				if len(things) != 1 {
					return nil, fmt.Errorf("unknown setup item %q", id)
				}
				return things[0], nil
			}})
		for _, verb := range setupVerbs {
			v := verb.name
			b.Verb(v, func(c ochazuke.Ctx) ochazuke.Result {
				home, _ := c.Args["home"].(string)
				engine, err := d.engine(r, home)
				if err != nil {
					return ochazuke.Failed(err)
				}
				ids, _ := c.Args["item"].(string)
				group, _ := c.Args["group"].(string)
				dry, _ := c.Args["dry-run"].(bool)
				sel := setup.Selection{IDs: strings.Fields(ids), Group: group}
				return ochazuke.Pending(func(ctx context.Context) (any, error) {
					switch v {
					case "install":
						if dry {
							return engine.Plan(ctx, sel)
						}
						return setupRows(engine.Install(ctx, sel))
					case "uninstall":
						return setupRows(engine.Uninstall(ctx, sel))
					default:
						plan, err := engine.Plan(ctx, sel)
						if err != nil {
							return nil, err
						}
						if v == "show" || v == "yank" {
							if len(plan.Items) != 1 {
								return nil, fmt.Errorf("%s needs exactly one setup item", v)
							}
							if v == "yank" {
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
				b.Confirm("Apply setup " + v + "? Review it first with setup install --dry-run.")
			} else {
				b.Fetch()
			}
			if v == "yank" {
				b.TextResult()
			}
			if v == "list" || v == "check" || v == "install" || v == "uninstall" {
				b.Rows("setup")
			}
			op := r.Operations.At(ochazuke.Key(r.CanonicalKey("setup." + v)))
			op.ShadowsStandard = verb.shadows
			if v == "install" || v == "uninstall" {
				op.HistoryKind = ochazuke.HistoryChange
			}
			op.Parameters = map[string]ochazuke.Parameter{"item": {Type: "string", Rest: true, Required: v == "show" || v == "yank", Description: "Setup item ids (empty selects the default set)"}, "home": {Type: "path", Path: "dir", Description: "Use this home directory and isolate harness HOME/XDG paths"}}
			if v == "install" {
				op.Parameters["group"] = ochazuke.Parameter{Type: "string", Description: "Only items in this group"}
				op.Parameters["dry-run"] = ochazuke.Parameter{Type: "boolean", Description: "Show all writes and argv without installing (no confirmation, nothing is written)"}
				op.ConfirmFor = unlessDryRun
			}
			r.Operations.Set(op)
		}
		r.Instance(setupInstance, ochazuke.Kind("setup")).Component = d
	})
	return nil
}

// unlessDryRun is install's confirm: nothing to ask when dry-run only shows
// the plan.
func unlessDryRun(_ *ochazuke.Runtime, _ ochazuke.Context, q ochazuke.Request, prompt string) string {
	if dry, _ := q.Args["dry-run"].(bool); dry {
		return ""
	}
	return prompt
}

// setupVerbs are the setup noun's verbs; shadows marks one that replaces
// the standard verb of its name over the noun's facts store on purpose
// (ochazuke.Operation.ShadowsStandard).
var setupVerbs = []struct {
	name, description string
	shadows           bool
}{
	{"check", "Check whether each setup item is in place, and say the exact next steps", false}, {"list", "List setup items and states", true}, {"show", "Show one setup item in full", false}, {"install", "Install the selected setup items", false}, {"uninstall", "Remove only the selected owned integrations", false}, {"yank", "Print an item's content or advice for pasting", false},
}

// setupProps are the setup noun's props: one thing per declared item (and
// each of the user's setup.json), read-only (a system fact), with its
// state as setup list answers it.
var setupProps = []ochazuke.PropSpec{
	{Name: "id", Key: true, Human: true, ReadOnly: true, Parameter: ochazuke.Parameter{Description: "The item's id"}},
	{Name: "name", Role: ochazuke.RoleLabel, ReadOnly: true, Parameter: ochazuke.Parameter{Description: "What the item is called"}},
	{Name: "description", ReadOnly: true, Parameter: ochazuke.Parameter{Description: "What it sets up"}},
	{Name: "kind", ReadOnly: true, Parameter: ochazuke.Parameter{Description: "How it is set up (block, file, json, command ...)"}},
	{Name: "group", ReadOnly: true, Parameter: ochazuke.Parameter{Description: "Its group, when it has one"}},
	{Name: "path", ReadOnly: true, Parameter: ochazuke.Parameter{Type: "path", Path: "file", Description: "The file it writes or probes"}},
	{Name: "status", ReadOnly: true, Parameter: ochazuke.Parameter{Description: "installed, missing, outdated, manual ..."}},
	{Name: "reason", ReadOnly: true, Parameter: ochazuke.Parameter{Description: "Why it is in that state, when it says"}},
	{Name: "next", ReadOnly: true, Parameter: ochazuke.Parameter{Description: "The exact next step"}},
	{Name: "opt_in", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Whether only an explicit install selects it"}},
}

// engine is the setup engine for one request: the declared items and the
// user's setup.json, the options with home (when given) and the skip
// setting.
func (d *setupDeclaration) engine(r *ochazuke.Runtime, home string) (*setup.Setup, error) {
	opts := d.options
	if home != "" {
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
				return nil, fmt.Errorf("setup.json: %w", e)
			}
			items = append(items, user...)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return setup.New(items, opts)
}

// things are the setup items (ids, else the default set) as the setup
// noun's things: their plan's status rows, the declared props only.
func (d *setupDeclaration) things(r *ochazuke.Runtime, home string, ids []string) ([]ochazuke.Thing, error) {
	engine, err := d.engine(r, home)
	if err != nil {
		return nil, err
	}
	plan, err := engine.Plan(context.Background(), setup.Selection{IDs: ids})
	if err != nil {
		return nil, err
	}
	out := make([]ochazuke.Thing, 0, len(plan.Items))
	for _, st := range plan.Items {
		path := st.Path
		if path == "" && st.Probe != nil {
			path = st.Probe.Path
		}
		out = append(out, ochazuke.Thing{"id": st.ID, "name": st.Name, "description": st.Description, "kind": string(st.Kind), "group": st.Group,
			"path": path, "status": string(st.Status), "reason": st.Reason, "next": st.Next, "opt_in": st.OptIn})
	}
	return out, nil
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
		app.Args = append([]string{"setup", "check"}, app.Args[1:]...)
	}
	children := make([]oden.Command, 0, len(setupVerbs))
	for _, verb := range setupVerbs {
		v := verb.name
		op := s.Runtime.Operations.At(ochazuke.Key(s.Runtime.CanonicalKey("setup." + v)))
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
			shown := v
			if v == "install" && slices.Contains(args, "--dry-run") {
				shown = "plan" // the plan printer: writes and argv
			}
			return printSetup(s.stdout(), s.Bin, shown, out.Bytes())
		}})
	}
	app.Add(oden.Command{Name: "setup", Summary: "Check and install this tool's integrations", Source: "ochazuke.setup", Children: children})
}

func printSetup(out io.Writer, tool, verb string, raw []byte) error {
	switch verb {
	case "yank":
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
			fmt.Fprintf(out, "%s: %s\n", st.ID, st.Status)
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
			fmt.Fprintln(w, "ITEM\tKIND\tSTATUS\tTARGET\tNEXT STEP")
			for _, st := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", st.ID, st.Kind, st.Status, st.Path, st.Next)
			}
			return w.Flush()
		}
		all := true
		for _, st := range rows {
			fmt.Fprintf(out, "%s: %s", st.ID, st.Status)
			if st.Status != setup.Installed {
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
		if verb == "check" {
			if all {
				fmt.Fprintln(out, "everything is set up")
			} else {
				fmt.Fprintf(out, "run: %s setup install\n", tool)
			}
		}
		return nil
	}
}
