package cli

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// NewCommandsModel is a model on a fresh runtime whose open page is app's
// Commands page (AddCommands over OdenCommands(app, self...)), titled with
// the app's name.
func NewCommandsModel(app *oden.App, cfg CommandsConfig, self ...string) (*ochazuke.Model, *Commands, error) {
	m := ochazuke.NewModel(ochazuke.NewRuntime())
	m.Title = app.Name
	c, err := AddCommands(m, OdenCommands(app, self...), cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := m.Runtime.OpenPage(c.Page()); err != nil {
		return nil, nil, err
	}
	return m, c, nil
}

// TUI is an oden Default that gives any oden tool a TUI: with no word it
// runs the Commands page full screen; a word that names no command is
// handed back to oden (oden.ErrNotFound), which reports it.
//
//	app := oden.New("hello", os.Args[1:], oden.WithBuiltins())
//	app.Default = cli.TUI(app)
func TUI(app *oden.App) func(ctx context.Context, args []string) error {
	return func(ctx context.Context, args []string) error {
		if len(args) > 0 {
			return oden.ErrNotFound
		}
		m, _, err := NewCommandsModel(app, CommandsConfig{})
		if err != nil {
			return err
		}
		_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
		return err
	}
}
