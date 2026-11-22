// Command views-json renders a Tier 1 UI made only of data. views.json is
// the view tree, bindings.json the keys and themes.json extra themes. All
// three are embedded and loaded with config.LoadView, config.LoadBindings
// and config.LoadTheme. Go adds the status line and two commands.
//
//	go run ./examples/views-json          interactive (h j k l tab move, d density, t theme, q quit)
//	go run ./examples/views-json --smoke  cycle density and every theme headlessly, exit 0 on success
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/config"
	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
)

//go:embed views.json bindings.json themes.json
var assets embed.FS

var specs = []core.CommandSpec{
	{ID: "view.density.cycle", Description: "Cycle compact, balanced and spacious density"},
	{ID: "view.theme.cycle", Description: "Cycle the theme presets and themes.json"},
}

type app struct {
	view   ui.ViewNode
	themes []ui.Theme
}

// load parses the embedded assets. Diagnostics from CheckBindings are errors.
func load() (*app, []core.Binding, error) {
	read := func(name string) []byte { b, _ := assets.ReadFile(name); return b }
	view, err := config.LoadView(read("views.json"))
	if err != nil {
		return nil, nil, err
	}
	binds, err := config.LoadBindings(read("bindings.json"))
	if err != nil {
		return nil, nil, err
	}
	if diags := config.CheckBindings(modes, binds, specs...); len(diags) > 0 {
		d := diags[0]
		return nil, nil, fmt.Errorf("bindings.json row %d: %s %s", d.Index, d.Severity, d.Message)
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(config.StripJSONC(read("themes.json")), &raws); err != nil {
		return nil, nil, fmt.Errorf("themes.json: %w", err)
	}
	a := &app{view: view, themes: ui.ThemePresets()}
	for _, raw := range raws {
		th, err := config.LoadTheme(raw, ui.DefaultDarkTheme())
		if err != nil {
			return nil, nil, err
		}
		a.themes = append(a.themes, th)
	}
	if len(a.themes) == 0 {
		a.themes = []ui.Theme{ui.DefaultDarkTheme()}
	}
	return a, binds, nil
}

var modes = []core.Mode{{Name: "normal"}}

// theme is the page data: the live theme, density included.
func theme(s core.State) ui.Theme { th, _ := s.Pages["main"].Data.(ui.Theme); return th }

// apply stores th in the page and asks the shell to switch to it. Name,
// Version, Density and BorderStyle key the renderer cache.
func apply(s core.State, th ui.Theme) core.StepResult {
	f := s.Pages["main"]
	f.Data = th
	return core.Next(s.WithActiveFrame(f), core.CustomEffect{Kind: "theme", Data: th})
}

func cycleDensity(th ui.Theme) ui.Theme {
	switch th.Density {
	case ui.DensityCompact:
		th.Density = ui.DensityBalanced
	case ui.DensityBalanced:
		th.Density = ui.DensitySpacious
	default:
		th.Density = ui.DensityCompact
	}
	return th
}

func (a *app) newShell(binds []core.Binding) (*otea.Shell, error) {
	km, err := core.NewKeymap(modes, binds, specs...)
	if err != nil {
		return nil, err
	}
	eng := core.NewEngine(km)
	eng.Handle("view.density.cycle", func(s core.State, _ core.Command) core.StepResult {
		return apply(s, cycleDensity(theme(s)))
	})
	eng.Handle("view.theme.cycle", func(s core.State, _ core.Command) core.StepResult {
		cur := theme(s)
		i := slices.IndexFunc(a.themes, func(t ui.Theme) bool { return t.Name == cur.Name })
		return apply(s, a.themes[(i+1)%len(a.themes)])
	})
	first := a.themes[0]
	first.Density = ui.DensityBalanced
	start := core.NewState(core.Frame{ID: "main", FocusID: "tier0", Data: first})
	sh := otea.New(eng, start, a.render, first)
	sh.OnCustom = func(e core.CustomEffect) tea.Cmd {
		if th, ok := e.Data.(ui.Theme); ok && e.Kind == "theme" {
			sh.Theme = th
		}
		return nil
	}
	return sh, nil
}

// render is the loaded tree plus a status line built from state.
func (a *app) render(s core.State) ui.ViewNode {
	th := theme(s)
	status := fmt.Sprintf(" theme %s · density %v · border %v · focus %s · h j k l tab move · d density · t theme · q quit", th.Name, th.Density, th.BorderStyle, s.ActiveFrame().FocusID)
	root := a.view
	root.Children = append(slices.Clone(root.Children), ui.ViewNode{Kind: ui.KindText, Tone: "muted", Props: map[string]any{"text": status}})
	return root
}

func main() {
	smoke := flag.Bool("smoke", false, "cycle density and themes headlessly and exit")
	flag.Parse()
	a, binds, err := load()
	var sh *otea.Shell
	if err == nil {
		sh, err = a.newShell(binds)
	}
	if err == nil {
		if *smoke {
			err = a.runSmoke(sh)
		} else {
			_, err = tea.NewProgram(sh).Run()
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "views-json:", err)
		os.Exit(1)
	}
}

func (a *app) runSmoke(sh *otea.Shell) error {
	var fails []string
	check := func(name string, ok bool) {
		status := "ok  "
		if !ok {
			status, fails = "FAIL", append(fails, name)
		}
		fmt.Printf("%s %-44s theme=%-24s density=%-8v border=%v\n", status, name, sh.Theme.Name, sh.Theme.Density, sh.Theme.BorderStyle)
	}
	press := func(keys ...string) {
		for _, k := range keys {
			msg := tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
			if k == "tab" {
				msg = tea.KeyPressMsg{Code: tea.KeyTab}
			}
			sh.Update(msg)
		}
	}
	frame := func() string { return sh.View().Content }

	sh.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	start := frame()
	check("views.json rendered with its status line", strings.Contains(start, "Packages") && strings.Contains(start, "density"))
	press("l", "tab")
	check("l and tab move focus through the data tree", sh.State.ActiveFrame().FocusID != "tier0")

	densities := map[string]bool{fmt.Sprint(sh.Theme.Density): true}
	for range 2 {
		before := frame()
		press("d")
		densities[fmt.Sprint(sh.Theme.Density)] = true
		check("d changes density and the frame", frame() != before)
	}
	press("d")
	check("d visits compact, balanced and spacious", len(densities) == 3 && fmt.Sprint(sh.Theme.Density) == fmt.Sprint(ui.DensityBalanced))

	seen := map[string]bool{sh.Theme.Name: true}
	for range len(a.themes) - 1 {
		before := frame()
		press("t")
		seen[sh.Theme.Name] = true
		check("t switches theme and repaints", frame() != before)
	}
	check("t reached every preset and themes.json theme", len(seen) == len(a.themes) && seen["matcha"] && seen["hojicha"])
	press("t")
	check("t wraps around to the first theme", sh.Theme.Name == a.themes[0].Name)

	if len(fails) > 0 {
		return fmt.Errorf("%d smoke checks failed: %s", len(fails), strings.Join(fails, "; "))
	}
	fmt.Println("smoke: all checks passed")
	return nil
}
