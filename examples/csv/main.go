package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ochazuke"
	"ochazuke/grid"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "csv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("csv", flag.ContinueOnError)
	smoke := flags.Bool("smoke", false, "Open the file, render one 80x24 frame to stdout and exit")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage:\n  csv [--smoke] [FILE.csv]\n\nA modal CSV editor on ochazuke. h j k l move (with counts), i edits a cell, dd deletes a row, v V ctrl+v select, t u/l/t transform, u undoes, ctrl+s saves, : opens the palette.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one CSV file")
	}
	path := ""
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	app, err := New(path)
	if err != nil {
		return err
	}
	if *smoke {
		app.m.Width, app.m.Height = 80, 24
		fmt.Println(ansi.Strip(app.m.View().Content))
		return nil
	}
	_, err = tea.NewProgram(app.m).Run()
	return err
}

// New builds the application on a fresh runtime: the sheet page holding the
// grid, the prompt field, the picker, the nouns, verbs and bindings. An
// empty path starts an untitled, empty sheet.
func New(path string) (*App, error) {
	r := ochazuke.NewRuntime()
	a := &App{r: r}
	a.m = ochazuke.NewModel(r)
	a.m.Title = "CSV"
	// The sheet instance is registered first so the grid can sit under it;
	// AddPage then adopts it as the page of the sheet noun.
	r.Instance("sheet", ochazuke.Kind("sheet"))
	g, err := grid.New(r, "grid", "sheet", nil, nil)
	if err != nil {
		return nil, err
	}
	a.grid = g
	a.registerPrompt()
	a.registerVerbs()
	a.bind()
	picker := ochazuke.NewPicker(r)
	a.m.ModalViews["picker"] = ochazuke.ModalView{Title: "Choose a path", Render: func(_ *ochazuke.Runtime, _ *ochazuke.Instance, w, h int) ochazuke.Block {
		return picker.ViewBlock(w, h)
	}}
	if path != "" {
		if err := a.Load(path); err != nil {
			return nil, err
		}
	}
	focus := "grid"
	if cursor := g.Cursor(); cursor.Row >= 0 {
		focus = cursor.ID()
	}
	if _, err := r.AddPage("sheet", "Sheet", a.render, ochazuke.Focus(focus)); err != nil {
		return nil, err
	}
	if err := r.OpenPage("sheet"); err != nil {
		return nil, err
	}
	return a, nil
}

// Model is the Bubble Tea model the application runs.
func (a *App) Model() *ochazuke.Model { return a.m }

// render is the sheet page: a status line, then the grid.
func (a *App) render(_ *ochazuke.Runtime, width, height int) ochazuke.Block {
	s := ochazuke.NewStyles(*a.m.Appearance)
	status := ansi.Truncate(strings.ReplaceAll(a.title(), "\n", " "), width, "…")
	return ochazuke.Text(s.Muted.Render(status) + "\n" + a.grid.View(width, max(1, height-1)))
}
