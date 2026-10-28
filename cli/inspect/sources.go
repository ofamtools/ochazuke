package inspect

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// Roles of a Source's tool.
const (
	RoleHost      = "host"      // the tool generating the page (its effective rows, its actions)
	RoleTool      = "tool"      // another tool asked for its catalog (BIN catalog)
	RoleSaved     = "saved"     // a catalog read from a file
	RoleFramework = "framework" // the ochazuke pseudo-tool Merge adds
)

// ParseCatalog decodes one BIN catalog answer.
func ParseCatalog(b []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Collector gathers the sources of one page. A tool already read, by its
// resolved executable or by its catalog's name, is skipped with a note, so
// a host passed itself with --bin (or o-tui, a link to o-nabe) is read once.
type Collector struct {
	Sources []Source
	Notes   []string
	Timeout time.Duration // per BIN catalog; 0 is 20s

	seenBin, seenName map[string]bool
}

func (c *Collector) note(format string, args ...any) {
	c.Notes = append(c.Notes, fmt.Sprintf(format, args...))
}

// SeeBin records an executable as read (the host's own), so AddBin skips it.
func (c *Collector) SeeBin(path string) {
	if c.seenBin == nil {
		c.seenBin = map[string]bool{}
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	c.seenBin[path] = true
}

// Add appends a source whose catalog is already decoded, named by its
// catalog; a catalog name seen before is skipped. It reports whether the
// source was kept.
func (c *Collector) Add(s Source) bool {
	if c.seenName == nil {
		c.seenName = map[string]bool{}
	}
	if s.Catalog != nil {
		if c.seenName[s.Catalog.Name] {
			c.note("%s: same catalog as %s, skipped", s.Tool.Bin, s.Catalog.Name)
			return false
		}
		c.seenName[s.Catalog.Name] = true
		s.Tool.Name = s.Catalog.Name
		if s.Tool.Version == "" {
			s.Tool.Version = s.Catalog.Version
		}
		if s.Tool.Status == "" {
			s.Tool.Status = "ok"
		}
	}
	c.Sources = append(c.Sources, s)
	return true
}

// AddBin runs BIN catalog (BIN a path or a name on PATH) and adds its
// answer, with the ochazuke version from the binary's build info. A tool
// that fails is kept as a failed row, so the page shows the error.
func (c *Collector) AddBin(ctx context.Context, bin string) {
	path, err := exec.LookPath(bin)
	if err != nil {
		c.note("%s: not found", bin)
		return
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	if c.seenBin[real] {
		return
	}
	c.SeeBin(real)
	info := ToolInfo{Name: filepath.Base(bin), Bin: path, Role: RoleTool, Ochazuke: binaryOchazuke(real)}
	b, err := c.run(ctx, path, "catalog")
	var cat *Catalog
	if err == nil {
		if cat, err = ParseCatalog(b); err != nil {
			err = fmt.Errorf("%s catalog: %v", path, err)
		}
	}
	if err != nil {
		info.Status, info.Error = "failed", err.Error()
		c.Add(Source{Tool: info})
		return
	}
	c.Add(Source{Tool: info, Catalog: cat})
}

// AddFile adds a saved catalog (the output of BIN catalog in a file).
func (c *Collector) AddFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	cat, err := ParseCatalog(b)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	c.Add(Source{Tool: ToolInfo{Bin: path, Role: RoleSaved}, Catalog: cat})
	return nil
}

func (c *Collector) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return out, fmt.Errorf("%s %s: %v %s", bin, strings.Join(args, " "), err, msg)
	}
	return out, nil
}

// Namespaces are the noun prefixes each source may mount the others
// under: every other source's short name (o-ai is ai, mtm is mtm), so a
// host's ai-agent (or ai.agent) groups as the ai extension's mirror and
// resolves to o-ai's agent when o-ai's catalog is read too. extra adds
// prefixes for the host (named by host), for mirrors whose tool is not read.
func Namespaces(srcs []Source, host string, extra ...string) map[string][]string {
	out := map[string][]string{}
	for _, s := range srcs {
		if s.Catalog == nil {
			continue
		}
		for _, o := range srcs {
			if o.Catalog != nil && o.Tool.Name != s.Tool.Name {
				out[s.Tool.Name] = addUnique(out[s.Tool.Name], shortName(o.Tool.Name))
			}
		}
	}
	for _, p := range extra {
		out[host] = addUnique(out[host], p)
	}
	return out
}

// RunningOchazuke is the ochazuke version the running binary was built with.
func RunningOchazuke() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return ochazukeOf(bi)
}

// binaryOchazuke reads the ochazuke module version from a Go binary's build info.
func binaryOchazuke(bin string) string {
	bi, err := buildinfo.ReadFile(bin)
	if err != nil {
		return ""
	}
	return ochazukeOf(bi)
}

func ochazukeOf(bi *debug.BuildInfo) string {
	if strings.HasSuffix(bi.Main.Path, "/ochazuke") {
		return bi.Main.Version
	}
	for _, d := range bi.Deps {
		if strings.HasSuffix(d.Path, "/ochazuke") {
			if d.Replace != nil && d.Replace.Version != "" {
				return d.Replace.Version
			}
			return d.Version
		}
	}
	return ""
}
