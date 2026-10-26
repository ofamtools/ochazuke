package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ReferenceDir is the directory config.export writes to by default, inside
// the config directory.
const ReferenceDir = "reference"

// The files config.export writes.
const (
	SettingsReferenceFile = "settings.reference.json"
	BindingsReferenceFile = "bindings.reference.json"
)

// ConfigPaths are where the application's user files live: Config is the
// config directory (bindings.json, settings.json, actions.json,
// themes.json), State the state directory (history.jsonl, panes.json) and
// Reference the directory config.export writes to by default. A directory
// the application has none of is "".
type ConfigPaths struct {
	Config    string `json:"config"`
	State     string `json:"state"`
	Reference string `json:"reference"`
}

// Paths are the config, state and default reference directories.
func (c *Config) Paths() ConfigPaths {
	out := ConfigPaths{Config: c.Directory(), State: c.StateDirectory()}
	if out.Config != "" {
		out.Reference = filepath.Join(out.Config, ReferenceDir)
	}
	return out
}

// ConfigExport is what config.export wrote: the directory, the files in
// it (absolute paths, in the order written) and where the live files are.
type ConfigExport struct {
	Dir    string   `json:"dir"`
	Files  []string `json:"files"`
	Config string   `json:"config"`
	State  string   `json:"state"`
}

// exportFile is one file of an export: its name in the directory and its
// content.
type exportFile struct {
	name string
	data []byte
}

// ExportConfig writes a complete reference of the user configuration into
// dir ("" is <config dir>/reference): settings.json with every declared
// setting at its current value, settings.reference.json with each
// setting's type, default, choices, description and value, and
// bindings.reference.json with every stored binding row in the
// bindings.load shape (bindings.list --all). Each file is written
// atomically, a temp file renamed over it. A file that would land on one
// of the live config files (dir is the config directory) is refused unless
// force is set, and then nothing is written.
func (r *Runtime) ExportConfig(dir string, force bool) (ConfigExport, error) {
	c := r.Config()
	r.EnsureConfigSettings()
	paths := c.Paths()
	if dir == "" {
		dir = paths.Reference
	}
	if dir == "" {
		return ConfigExport{}, errors.New("this application has no config directory: name a directory to export to")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ConfigExport{}, err
	}
	files, err := r.exportFiles()
	if err != nil {
		return ConfigExport{}, err
	}
	if !force {
		for _, f := range files {
			if live := c.liveFile(filepath.Join(abs, f.name)); live != "" {
				return ConfigExport{}, fmt.Errorf("%s is the live %s; export elsewhere or pass force to overwrite it", filepath.Join(abs, f.name), live)
			}
		}
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return ConfigExport{}, err
	}
	out := ConfigExport{Dir: abs, Files: []string{}, Config: paths.Config, State: paths.State}
	for _, f := range files {
		path := filepath.Join(abs, f.name)
		if err := writeFileAtomic(path, f.data); err != nil {
			return out, err
		}
		out.Files = append(out.Files, path)
	}
	return out, nil
}

// exportFiles are the files of an export, in the order they are written.
func (r *Runtime) exportFiles() ([]exportFile, error) {
	settings := r.Settings().List()
	current, err := settingsObject(settings)
	if err != nil {
		return nil, err
	}
	reference, err := indentJSON(settings)
	if err != nil {
		return nil, err
	}
	bindings, err := indentJSON(r.allBindings())
	if err != nil {
		return nil, err
	}
	return []exportFile{
		{SettingsFile, current},
		{SettingsReferenceFile, reference},
		{BindingsReferenceFile, bindings},
	}, nil
}

// settingsObject is settings.json holding every setting at its value, in
// declaration order.
func settingsObject(settings []SettingValue) ([]byte, error) {
	if len(settings) == 0 {
		return []byte("{}\n"), nil
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, one := range settings {
		name, err := json.Marshal(one.Name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(one.Value)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "  %s: %s", name, value)
		if i < len(settings)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}

func indentJSON(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// liveFiles are the files the framework reads from the config directory.
var liveFiles = []string{BindingsFile, SettingsFile, ActionsFile, ThemesFile}

// liveFile is the name of the live config file path is, "" when it is
// none: the same file name in the config directory, compared after
// resolving symbolic links in both directories.
func (c *Config) liveFile(path string) string {
	name := filepath.Base(path)
	if !slices.Contains(liveFiles, name) {
		return ""
	}
	dir := c.Directory()
	if dir == "" {
		return ""
	}
	if resolveDir(filepath.Dir(path)) == resolveDir(dir) {
		return name
	}
	return ""
}

// resolveDir is a directory as an absolute path with its symbolic links
// resolved, as far as it exists.
func resolveDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	var rest []string
	for p := abs; ; p = filepath.Dir(p) {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			slices.Reverse(rest)
			return filepath.Join(append([]string{real}, rest...)...)
		}
		if parent := filepath.Dir(p); parent == p {
			return abs
		}
		rest = append(rest, filepath.Base(p))
	}
}

// writeFileAtomic replaces path with data: a temp file in the same
// directory, renamed over it, so a reader sees the old file or the new
// one, never part of either.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func registerConfigExport(r *Runtime) {
	r.Register(Operation{Verb: "path", Noun: "config", Description: "Print the config directory (bindings.json, settings.json, actions.json, themes.json), the state directory (history.jsonl, panes.json) and the default config export directory", Result: "paths", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		paths := r.Config().Paths()
		if paths.Config == "" && paths.State == "" {
			return nil, errors.New("this application has no config or state directory")
		}
		return paths, nil
	}})
	r.Register(Operation{Verb: "export", Noun: "config", Description: "Write a reference of the user configuration (every setting, every key row) into a directory, default <config dir>/reference; never over the live files without force", Parameters: map[string]Parameter{
		"dir":   {Type: "string", Description: "Directory to write to (default <config dir>/reference)"},
		"force": {Type: "boolean", Description: "Allow writing over a live config file (dir is the config directory)"},
	}, Positional: []string{"dir"}, Result: "export", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		force, _ := a["force"].(bool)
		return r.ExportConfig(StringArg(a, "dir", ""), force)
	}})
}
