package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"

	"ochazuke"
)

//go:embed config/*.json
var embeddedConfig embed.FS

type Config struct {
	App      AppConfig
	Layers   []LayerConfig
	Inputs   []InputConfig
	Pages    []PageConfig
	Actions  []ActionConfig
	Bindings []BindingConfig
	Modals   []ModalConfig
}

type AppConfig struct {
	Title      string              `json:"title"`
	StartPage  string              `json:"startPage"`
	Modes      []string            `json:"modes"`
	Appearance ochazuke.Appearance `json:"appearance"`
	Palette    []PaletteConfig     `json:"palette"`
}

type PaletteConfig struct {
	Noun        string `json:"noun"`
	Source      string `json:"source"`
	DefaultVerb string `json:"defaultVerb"`
	Argument    string `json:"argument"`
}

type LayerConfig struct {
	ID   string `json:"id"`
	Noun string `json:"noun"`
}

type InputConfig struct {
	ID         string   `json:"id"`
	Value      string   `json:"value"`
	Kind       string   `json:"kind"`
	PathFilter string   `json:"pathFilter"`
	Multiline  bool     `json:"multiline"`
	Path       []string `json:"path"`
}

type PageConfig struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Renderer string           `json:"renderer"`
	Context  ochazuke.Context `json:"context"`
	Inputs   []string         `json:"inputs"`
	Text     string           `json:"text"`
}

type ActionConfig struct {
	Verb        string                        `json:"verb"`
	Noun        string                        `json:"noun"`
	Description string                        `json:"description"`
	Handler     string                        `json:"handler"`
	Result      string                        `json:"result"`
	Parameters  map[string]ochazuke.Parameter `json:"parameters"`
	Layer       string                        `json:"layer"`
}

type BindingConfig struct {
	Layer   string           `json:"layer"`
	Mode    string           `json:"mode"`
	Keys    string           `json:"keys"`
	Request ochazuke.Request `json:"request"`
}

type ModalConfig struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Renderer string `json:"renderer"`
	Input    string `json:"input"`
	Text     string `json:"text"`
}

// LoadConfig reads all seven files from dir, or the embedded example when dir is empty.
// External configuration replaces the whole configuration, never individual files.
func LoadConfig(dir string) (Config, error) {
	var cfg Config
	var source fs.FS
	if dir == "" {
		var err error
		source, err = fs.Sub(embeddedConfig, "config")
		if err != nil {
			return Config{}, err
		}
	} else {
		source = os.DirFS(dir)
	}
	files := []struct {
		name        string
		destination any
	}{
		{"app.json", &cfg.App},
		{"layers.json", &cfg.Layers},
		{"inputs.json", &cfg.Inputs},
		{"pages.json", &cfg.Pages},
		{"actions.json", &cfg.Actions},
		{"bindings.json", &cfg.Bindings},
		{"modals.json", &cfg.Modals},
	}
	for _, file := range files {
		data, err := fs.ReadFile(source, file.name)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", file.name, err)
		}
		if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return Config{}, fmt.Errorf("%s: expected a JSON object or array, not null", file.name)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(file.destination); err != nil {
			return Config{}, fmt.Errorf("%s: %w", file.name, err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			if err == nil {
				err = fmt.Errorf("expected exactly one JSON value")
			}
			return Config{}, fmt.Errorf("%s: %w", file.name, err)
		}
	}
	return cfg, nil
}
