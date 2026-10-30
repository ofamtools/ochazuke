package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	story "github.com/unixsuperhero/monogatari"
	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// The catalog is an ochazuke runtime behind ochazuke/cli, additive to the
// commands in main.go: data nouns story, recording, verb and edge, each
// with list (rows, declared with Rows), story and recording with show,
// story with run and state (every list's rows as JSON Lines). A row is
// {id, label, data}:
//
//	{id: "lists/spill", label: "...", data: {area: "lists", size: "80x24", chrome: "full", steps: 3, skipped: false}}
//	{id: "recording/1", label: "...", data: {ref: "1", story: "...", started: "...", events: 12}}
//	{id: "verb/normal/list.move", label: "normal list list.move", data: {mode: "normal", noun: "list", verb: "list.move"}}
//	{id: "edge/normal/insert/input.edit", label: "normal → insert via input.edit", data: {from, to, via}}

// catalogNouns are the catalog's data nouns, in state order.
var catalogNouns = []string{"story", "recording", "verb", "edge"}

// Row is one element of a list; Cells are its table cells when the noun
// declares columns (story: storyColumns).
type Row struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Data  any      `json:"data,omitempty"`
	Cells []string `json:"cells,omitempty"`
}

// storyColumns are the story noun's table, declared once: the catalog's
// columns (story list, state, nabe) and the TUI index (without area,
// which is the index's section heading). Size drops first, then steps,
// area and the gap; the story name is cut last.
var storyColumns = []ochazuke.Column{
	{Title: "story", Min: 24},
	{Title: "area", Priority: 3},
	{Title: "size", Priority: 4},
	{Title: "steps", Align: ochazuke.AlignRight, Priority: 2},
	{Title: "gap", Min: 12, Priority: 1},
}

// storyCells is a story's row of storyColumns.
func storyCells(s *story.Story) []string {
	w, h := s.Size()
	return []string{s.Name, s.Area, fmt.Sprintf("%dx%d", w, h), strconv.Itoa(len(s.Script)), strings.TrimPrefix(s.Skip, "gap: ")}
}

// withoutArea drops the area column (index 1) from storyColumns or a
// story's cells, for the index, where the area is the section heading.
func withoutArea[T any](row []T) []T { return slices.Delete(slices.Clone(row), 1, 2) }

// StoryRow is a story row's data.
type StoryRow struct {
	Area    string        `json:"area"`
	Size    string        `json:"size"`
	Chrome  string        `json:"chrome"`
	Steps   int           `json:"steps"`
	Skipped bool          `json:"skipped"`
	Expect  story.Expects `json:"expect,omitempty"`
}

// RecordingRow is a recording row's data: Ref is what replay, keep and
// run --from take (a history number, else the saved name).
type RecordingRow struct {
	Ref     string `json:"ref"`
	Kind    string `json:"kind"` // history or saved
	Story   string `json:"story"`
	Started string `json:"started"`
	Events  int    `json:"events"`
	Path    string `json:"path"`
}

// VerbRow is one (mode, noun, verb) of the derived minimum.
type VerbRow struct {
	Mode string `json:"mode"`
	Noun string `json:"noun"`
	Verb string `json:"verb"`
}

// EdgeRow is one mode edge: from to to via a noun.verb (or the row or
// tick that took it).
type EdgeRow struct {
	From string `json:"from"`
	To   string `json:"to"`
	Via  string `json:"via"`
}

// storyID is a story's row id: area/stem, which story.Find resolves.
func storyID(s *story.Story) string {
	return s.Area + "/" + strings.TrimSuffix(filepath.Base(s.Path), ".json")
}

func storyRows(lib story.Library) []Row {
	rows := []Row{}
	for _, s := range lib.Stories {
		w, h := s.Size()
		chrome := s.Chrome
		if chrome == "" {
			chrome = "full"
		}
		rows = append(rows, Row{ID: storyID(s), Label: s.Name, Data: StoryRow{
			Area: s.Area, Size: fmt.Sprintf("%dx%d", w, h), Chrome: chrome,
			Steps: len(s.Script), Skipped: s.Skip != "", Expect: s.Expect,
		}, Cells: storyCells(s)})
	}
	return rows
}

func recordingRows(store story.Store) ([]Row, error) {
	hist, err := store.History()
	if err != nil {
		return nil, err
	}
	saved, err := store.Saved()
	if err != nil {
		return nil, err
	}
	rows := []Row{}
	add := func(e story.Entry, kind, ref string) {
		started := e.Rec.Started.UTC().Format("2006-01-02T15:04:05Z")
		rows = append(rows, Row{ID: "recording/" + ref, Label: kind + " " + ref + ": " + e.Rec.Story, Data: RecordingRow{
			Ref: ref, Kind: kind, Story: e.Rec.Story, Started: started, Events: len(e.Rec.Events), Path: e.Path,
		}})
	}
	for _, e := range hist {
		add(e, "history", strconv.Itoa(e.N))
	}
	for _, e := range saved {
		add(e, "saved", e.Name)
	}
	return rows, nil
}

// minimumRows are the verb and edge rows of the core stories' minimum,
// the same set o-story verbs prints.
func minimumRows() (verbs, edges []Row, err error) {
	core, err := story.LoadFS(embedded, "stories")
	if err != nil {
		return nil, nil, err
	}
	m, err := coreMinimum(core)
	if err != nil {
		return nil, nil, err
	}
	verbs, edges = []Row{}, []Row{}
	for _, mode := range sortedKeys(m.Verbs) {
		for _, noun := range sortedKeys(m.Verbs[mode]) {
			for _, verb := range m.Verbs[mode][noun] {
				verbs = append(verbs, Row{ID: "verb/" + mode + "/" + verb, Label: mode + " " + noun + " " + verb, Data: VerbRow{mode, noun, verb}})
			}
		}
	}
	for _, e := range m.Edges {
		edges = append(edges, Row{ID: "edge/" + e.From + "/" + e.To + "/" + e.Via, Label: e.From + " → " + e.To + " via " + e.Via, Data: EdgeRow{e.From, e.To, e.Via}})
	}
	return verbs, edges, nil
}

// rows is one catalog noun's rows.
func (t *Tool) rows(noun string) ([]Row, error) {
	switch noun {
	case "story":
		lib, err := t.library("")
		if err != nil {
			return nil, err
		}
		return storyRows(lib), nil
	case "recording":
		return recordingRows(story.DefaultStore(t.Getenv))
	}
	verbs, edges, err := minimumRows()
	if noun == "verb" {
		return verbs, err
	}
	return edges, err
}

// lines is rows as JSON Lines tagged with their noun.
func lines(noun string, rows []Row) ocli.Lines {
	out := make(ocli.Lines, len(rows))
	for i, r := range rows {
		out[i] = ocli.Row{Noun: noun, ID: r.ID, Label: r.Label, Data: r.Data, Cells: r.Cells}
	}
	return out
}

// nounOf is the catalog noun a row id belongs to: its prefix, else story.
func nounOf(id string) string {
	prefix, _, _ := strings.Cut(id, "/")
	for _, noun := range catalogNouns[1:] {
		if prefix == noun {
			return noun
		}
	}
	return "story"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// shell is the catalog's runtime and command line.
func (t *Tool) shell() *ocli.Shell {
	r := ochazuke.NewRuntime()
	lists := map[string]*ochazuke.List{}
	for _, noun := range catalogNouns {
		var opts []ochazuke.InstanceOption
		if noun == "story" {
			opts = append(opts, ochazuke.Columns(storyColumns...))
		}
		list, err := r.AddList("list/"+noun, nil, opts...)
		if err != nil {
			panic(err) // a fresh runtime takes every list
		}
		lists[noun] = list
	}
	listed := func(noun string) ochazuke.Result {
		rows, err := t.rows(noun)
		if err != nil {
			return ochazuke.Failed(err)
		}
		items := make([]ochazuke.ListItem, len(rows))
		for i, row := range rows {
			items[i] = ochazuke.ListItem{ID: row.ID, Label: row.Label, Noun: noun, Data: row.ID, Cells: row.Cells}
		}
		if err := lists[noun].SetItems(items); err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(rows)
	}
	target := func(c ochazuke.Ctx, noun string) (string, error) {
		if c.Instance == nil || c.Instance.Noun != noun {
			return "", fmt.Errorf("%s needs --target ID (an id from %s list)", noun, noun)
		}
		id, _ := c.Instance.Data.(string)
		return id, nil
	}
	findStory := func(c ochazuke.Ctx) (story.Library, *story.Story, error) {
		id, err := target(c, "story")
		if err != nil {
			return story.Library{}, nil, err
		}
		lib, err := t.library("")
		if err != nil {
			return lib, nil, err
		}
		for _, s := range lib.Stories {
			if storyID(s) == id {
				return lib, s, nil
			}
		}
		return lib, nil, fmt.Errorf("no story %q", id)
	}
	r.Define("story").Columns(storyColumns...).
		Verb("list", func(ochazuke.Ctx) ochazuke.Result { return listed("story") },
			"the stories: {id: AREA/NAME, label, data: {area, size, chrome, steps, skipped, expect?}, cells: [story, area, size, steps, gap]}").Rows("story").
		Verb("show", func(c ochazuke.Ctx) ochazuke.Result {
			_, s, err := findStory(c)
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(s)
		}, "one story's JSON: --target ID").
		Verb("run", func(c ochazuke.Ctx) ochazuke.Result {
			lib, s, err := findStory(c)
			if err != nil {
				return ochazuke.Failed(err)
			}
			res := lib.Run(s, story.RunOptions{Golden: true})
			out := map[string]any{"id": storyID(s), "ok": res.OK(), "skipped": res.Skipped, "failed": res.Failed}
			if res.Failed == nil {
				out["failed"] = []string{}
			}
			if res.Err != nil {
				out["error"] = res.Err.Error()
			}
			return ochazuke.Done(out)
		}, "play one story headless and check it: --target ID; {id, ok, skipped, failed, error?}").
		Verb("state", func(ochazuke.Ctx) ochazuke.Result {
			var out ocli.Lines
			for _, noun := range catalogNouns {
				rows, err := t.rows(noun)
				if err != nil {
					return ochazuke.Failed(err)
				}
				out = append(out, lines(noun, rows)...)
			}
			return ochazuke.Done(out)
		}, "every list's rows in one call as JSON Lines tagged by noun: "+strings.Join(catalogNouns, ", ")+" (the catalog's state)").State()
	r.Define("recording").
		Verb("list", func(ochazuke.Ctx) ochazuke.Result { return listed("recording") },
			"the history ring (newest first) then the saved recordings: {id: recording/REF, label, data: {ref, kind, story, started, events, path}}").Rows("recording").
		Verb("show", func(c ochazuke.Ctx) ochazuke.Result {
			id, err := target(c, "recording")
			if err != nil {
				return ochazuke.Failed(err)
			}
			e, err := story.DefaultStore(t.Getenv).Load(strings.TrimPrefix(id, "recording/"))
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(e.Rec)
		}, "one recording's JSON: --target recording/REF")
	r.Define("verb").
		Verb("list", func(ochazuke.Ctx) ochazuke.Result { return listed("verb") },
			"the derived minimum, one row per (mode, noun, verb): {id, label, data: {mode, noun, verb}} (o-story verbs)").Rows("verb")
	r.Define("edge").
		Verb("list", func(ochazuke.Ctx) ochazuke.Result { return listed("edge") },
			"the mode edges the core stories take: {id, label, data: {from, to, via}} (o-story verbs)").Rows("edge")

	sh := ocli.New(r, "o-story", os.Stdin, t.Stdout, t.Stderr)
	sh.Usage = ocli.DefaultUsage(t.Prog)
	// Reveal makes the target row exist; show and run look it up afresh.
	sh.Reveal = func(id string) error {
		noun := nounOf(id)
		return lists[noun].SetItems([]ochazuke.ListItem{{ID: id, Label: id, Noun: noun, Data: id}})
	}
	return sh
}
