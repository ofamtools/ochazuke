package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli/inspect"
)

// InspectFile is the page catalog inspect writes without --out, in the
// state directory (the temp directory when the application has none).
const InspectFile = "catalog-inspect.html"

// catalogInspectUsage is the usage of BIN catalog inspect.
const catalogInspectUsage = `usage: %[1]s catalog inspect [--out FILE] [--open] [--bin PATH]... [--catalog FILE.json]...
       [--namespace PREFIX]... [--annotations FILE.json | --audit FILE.md] [--json FILE] [--timeout D]

Writes one self-contained HTML page for browsing %[1]s's nouns, verbs, key
rows (its effective rows, what config export writes), user actions
(actions.json) and the framework nouns, merged once. --bin and --catalog add
other tools' catalogs; --annotations (or --audit, the family verb-audit.md
format) fills the Gaps view. It prints the page's path.

`

// catalogCommand is BIN catalog: the catalog JSON, or with the word
// inspect the HTML inspector page (catalogInspect).
func (s *Shell) catalogCommand(ctx context.Context, args []string) error {
	if len(args) > 0 {
		if args[0] != "inspect" {
			return fmt.Errorf("catalog takes no arguments but inspect, got %q", args[0])
		}
		return s.catalogInspect(ctx, args[1:])
	}
	data, err := catalog(s.Runtime, s.Bin, s.version(), !s.StateCache.Off, s.catalogOptions...)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.stdout(), "%s\n", data)
	return err
}

// repeated is a flag that may be given several times.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

// openPage opens a written page in the user's browser (a variable so
// tests do not).
var openPage = func(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// catalogInspect is BIN catalog inspect (catalogInspectUsage).
func (s *Shell) catalogInspect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet(s.Bin+" catalog inspect", flag.ContinueOnError)
	fs.SetOutput(s.stderr())
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), catalogInspectUsage, s.Bin)
		fs.PrintDefaults()
	}
	out := fs.String("out", "", "the page to write (default <state dir>/"+InspectFile+")")
	open := fs.Bool("open", false, "open the page once written")
	var bins, files, namespaces repeated
	fs.Var(&bins, "bin", "another tool to read: runs PATH catalog (repeatable)")
	fs.Var(&files, "catalog", "a saved catalog, the JSON of BIN catalog (repeatable)")
	fs.Var(&namespaces, "namespace", "a noun prefix the host mirrors a tool under, when that tool is not read (repeatable)")
	annotations := fs.String("annotations", "", "annotations JSON for the Gaps view (inspect.Annotations)")
	audit := fs.String("audit", "", "a verb-audit.md (the family audit format) for the Gaps view")
	jsonOut := fs.String("json", "", "also write the merged data as JSON here")
	timeout := fs.Duration("timeout", 20*time.Second, "per --bin catalog")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("catalog inspect: unexpected arguments %s", strings.Join(fs.Args(), " "))
	}
	if *annotations != "" && *audit != "" {
		return errors.New("catalog inspect: pass --annotations or --audit, not both")
	}
	d, notes, err := s.inspectData(ctx, bins, files, namespaces, *timeout)
	if err != nil {
		return err
	}
	switch {
	case *annotations != "":
		b, err := os.ReadFile(*annotations)
		if err != nil {
			return err
		}
		if d.Annotations, err = inspect.ReadAnnotations(b); err != nil {
			return fmt.Errorf("%s: %w", *annotations, err)
		}
		if d.Annotations.Source == "" {
			d.Annotations.Source = *annotations
		}
	case *audit != "":
		b, err := os.ReadFile(*audit)
		if err != nil {
			return err
		}
		d.Annotations = inspect.AuditAnnotations(string(b))
		if d.Annotations.Source, err = filepath.Abs(*audit); err != nil {
			d.Annotations.Source = *audit
		}
	}
	d.Annotations.Match(d)
	d.Notes = append(d.Notes, notes...)

	path := *out
	if path == "" {
		dir := os.TempDir()
		if c := s.Runtime.Config(); c != nil && c.StateDirectory() != "" {
			dir = c.StateDirectory()
		}
		path = filepath.Join(dir, InspectFile)
	}
	var page bytes.Buffer
	if err := inspect.Write(d, &page); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeFile(path, page.Bytes()); err != nil {
		return err
	}
	if *jsonOut != "" {
		b, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFile(*jsonOut, append(b, '\n')); err != nil {
			return err
		}
	}
	for _, n := range d.Notes {
		fmt.Fprintln(s.stderr(), "note:", n)
	}
	fmt.Fprintf(s.stderr(), "tools %d, nouns %d, verbs %d, key rows %d, user actions %d\n",
		len(d.Tools), len(d.Nouns), len(d.Verbs), len(d.Keys), len(d.Actions))
	fmt.Fprintln(s.stdout(), path)
	if *open {
		return openPage(path)
	}
	return nil
}

// writeFile writes data to path through a temp file renamed over it.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// inspectData is the merged model of this runtime (the host: its catalog,
// its effective key rows, its user actions) and the tools and saved
// catalogs named.
func (s *Shell) inspectData(ctx context.Context, bins, files, namespaces []string, timeout time.Duration) (*inspect.Data, []string, error) {
	r := s.Runtime
	// As a command does: the user's actions, bindings and settings apply,
	// so the rows are the effective ones.
	for _, problem := range append(r.EnsureUserActions(), r.EnsureUserBindings()...) {
		fmt.Fprintln(s.stderr(), "warning:", problem)
	}
	r.EnsureConfigSettings()
	raw, err := catalog(r, s.Bin, s.version(), !s.StateCache.Off, s.catalogOptions...)
	if err != nil {
		return nil, nil, err
	}
	own, err := inspect.ParseCatalog(raw)
	if err != nil {
		return nil, nil, err
	}
	// The effective rows: bindings list --all, what config export writes
	// to bindings.reference.json.
	all, err := r.Execute(ochazuke.Request{Noun: "ochazuke.bindings", Verb: "list", Args: ochazuke.Arguments{"all": true}})
	if err != nil {
		return nil, nil, err
	}
	rows, err := recode[[]inspect.Binding](all)
	if err != nil {
		return nil, nil, err
	}
	actions, err := recode[[]inspect.UserAction](r.UserActions())
	if err != nil {
		return nil, nil, err
	}
	host := inspect.ToolInfo{Name: s.Bin, Role: inspect.RoleHost, Ochazuke: inspect.RunningOchazuke()}
	c := &inspect.Collector{Timeout: timeout}
	if exe, err := os.Executable(); err == nil {
		host.Bin = exe
		c.SeeBin(exe)
	}
	c.Add(inspect.Source{Tool: host, Catalog: own, Bindings: rows, BindingSource: "effective rows (config export)"})
	// Saved catalogs first: a file pins the catalog of its name, and a
	// --bin answering the same name is skipped.
	for _, f := range files {
		if err := c.AddFile(f); err != nil {
			return nil, nil, err
		}
	}
	for _, bin := range bins {
		c.AddBin(ctx, bin)
	}
	d := inspect.Merge(c.Sources, inspect.Namespaces(c.Sources, s.Bin, namespaces...), actions, s.Bin)
	d.Generated = time.Now().UTC().Format(time.RFC3339)
	if cfg := r.Config(); cfg != nil && cfg.Directory() != "" {
		d.ActionsAt = filepath.Join(cfg.Directory(), ochazuke.ActionsFile)
	}
	return d, c.Notes, nil
}

// recode converts v to T through its JSON.
func recode[T any](v any) (T, error) {
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}
