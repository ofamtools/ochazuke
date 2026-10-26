package ochazuke

import (
	"errors"
	"path/filepath"
)

// ReferenceDir is the directory config.export writes to by default, inside
// the config directory.
const ReferenceDir = "reference"

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

func registerConfigExport(r *Runtime) {
	r.Register(Operation{Verb: "path", Noun: "config", Description: "Print the config directory (bindings.json, settings.json, actions.json, themes.json), the state directory (history.jsonl, panes.json) and the default config export directory", Result: "paths", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		paths := r.Config().Paths()
		if paths.Config == "" && paths.State == "" {
			return nil, errors.New("this application has no config or state directory")
		}
		return paths, nil
	}})
}
