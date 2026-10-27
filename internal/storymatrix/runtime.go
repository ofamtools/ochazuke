package storymatrix

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/ochazuke/grid"
	"github.com/unixsuperhero/ochazuke/markdown"
)

// FixtureNouns are the nouns FrameworkRuntime defines for its own fixture
// (the catalog page's row noun): application nouns, mapped to no
// component.
var FixtureNouns = []string{"fixture-row"}

// RuntimeSet names what FrameworkRuntime builds, for the report.
const RuntimeSet = "ochazuke.NewRuntime + ochazuke.NewModel + ochazuke.NewPicker + grid.Register + markdown.Register + cli.AddCommands (one fake command) + cli.MountPage (a groups-layout catalog page of one list of fixture-row rows)"

// FrameworkRuntime builds the smallest runtime that registers every
// builtin noun: the core runtime and model, the picker, the grid and
// markdown packages, cli's Commands page and a catalog data page in the
// groups layout (which defines the page-group noun). It reads and writes
// no user files.
func FrameworkRuntime() (*ochazuke.Runtime, error) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	m := ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	grid.Register(r)
	markdown.Register(r)
	start := func(*exec.Cmd, int, int) (ochazuke.TerminalStream, error) {
		return nil, errors.New("the fixture starts no command")
	}
	if _, err := cli.AddCommands(m, fixtureCommands{}, cli.CommandsConfig{Start: start}); err != nil {
		return nil, err
	}
	doc := cli.CatalogDoc{Schema: cli.Schema, Name: "fixture", Nouns: []cli.CatalogNoun{{
		Name:  "fixture-row",
		Verbs: []cli.CatalogVerb{{Name: "list", Rows: "fixture-row"}},
	}}}
	page := cli.CatalogPage{ID: "fixture", Title: "Fixture", Layout: cli.LayoutGroups, Lists: []cli.CatalogList{{ID: "fixture/rows", Noun: "fixture-row"}}}
	doc.Pages = []cli.CatalogPage{page}
	if _, err := cli.MountPage(m, doc, page, cli.PageConfig{}); err != nil {
		return nil, err
	}
	return r, nil
}

type fixtureCommands struct{}

func (fixtureCommands) Commands() []cli.CommandEntry {
	return []cli.CommandEntry{{Name: "greet", Kind: "inline", Summary: "say hello", Source: "main.go:1", Help: "usage: greet"}}
}

func (fixtureCommands) Help(context.Context, string) (string, error) { return "usage: greet", nil }

func (fixtureCommands) Command(name string, args []string) (*exec.Cmd, error) {
	return exec.Command(name, args...), nil
}

// FixtureInstances maps the first segment of a fixture instance id to the
// component that built it: the catalog page's instance rows are catalog's.
var FixtureInstances = map[string]string{"fixture": "catalog"}

// Load parses components.json, checks it against FrameworkRuntime and
// builds the matrix.
func Load(data []byte) (*Map, *Matrix, error) {
	m, err := ParseMap(data)
	if err != nil {
		return nil, nil, err
	}
	r, err := FrameworkRuntime()
	if err != nil {
		return nil, nil, err
	}
	if problems := m.CheckRuntime(r, FixtureNouns); len(problems) > 0 {
		return nil, nil, fmt.Errorf("components.json: %s", strings.Join(problems, "; "))
	}
	inv, err := m.Derive(r, FixtureInstances)
	if err != nil {
		return nil, nil, fmt.Errorf("components.json: %w", err)
	}
	return m, m.Build(inv), nil
}
