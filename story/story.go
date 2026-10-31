// Package story is o-story's engine: a story is data (a synthetic page, a
// script of events, an expectation), Build turns it into a live runtime and
// model, a Driver plays events against it headless, and a Recorder turns
// the messages of a live session into the same events. State changes only
// through events; a frame is a pure function of the story and its events.
// Stories never read production state: every row, label and pane is in the
// file.
//
// Frozen: this package is kept unchanged for one release so its importers
// (o-json) keep compiling, then deleted. o-story no longer uses it.
//
// Deprecated: use ofam.dev/monogatari, the story library
// with an app seam (App, Fixture, Env, Built). ochazuke's demo pages are
// the demo app in ochazuke's cmd/o-story/demo.
package story

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
)

// Story is one scenario file.
type Story struct {
	Name   string `json:"name"`
	Area   string `json:"area"`
	Skip   string `json:"skip,omitempty"` // "gap: ..." marks a known framework gap; the story runs but cannot fail the suite
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Chrome string `json:"chrome,omitempty"` // full (default), none (page body only) or ticker-only
	// Theme renders the story in a theme other than mono (adaptive, dark,
	// light); Accent ("#RRGGBB") is the Appearance accent override. Goldens
	// stay escape-free, so a coloured story asserts colours with colored.
	Theme  string `json:"theme,omitempty"`
	Accent string `json:"accent,omitempty"`
	// Themes are colour schemes in the themes.json shape the session adds
	// to the built-in themes (Theme may name one); a scheme that does not
	// validate is a notice, as in an application.
	Themes json.RawMessage `json:"themes,omitempty"`
	Page   *Page           `json:"page,omitempty"`
	Pages  []*Page         `json:"pages,omitempty"`
	// Base and Start make a trimmed story: replay the base's events before
	// Start headless, then play Script as the window under test.
	Base   string  `json:"base,omitempty"`
	Start  int     `json:"start,omitempty"`
	Script []Event `json:"script,omitempty"`
	// Settings are declared on the session's runtime; with no config
	// directory (stories read no user files) their values last the session.
	Settings []ochazuke.Setting `json:"settings,omitempty"`
	// Values are setting values the session starts with, set as
	// settings.json would hold them (Settings.Set; with no config
	// directory they last the session): the saved layer.
	Values map[string]any `json:"values,omitempty"`
	// Session are session values set at the start, above Values (the
	// session started as Model.Init starts it, then Settings.SetSession):
	// what a :set line leaves unwritten.
	Session map[string]any `json:"session,omitempty"`
	// State gives the session a state directory of its own (a temp dir,
	// removed when the session ends), kept across a restart event, so a
	// story shows what the application remembers (pane dividers).
	State bool `json:"state,omitempty"`
	// Config is the user's config files the session starts with, by file
	// name: written into a temp config directory of its own (removed at
	// the end); actions.json and then bindings.json are loaded the way
	// Model.Init loads them, after the pages are built. A problem a load
	// reports is a notice.
	Config map[string]json.RawMessage `json:"config,omitempty"`
	// Checks hang verb checks on the session's verbs (Runtime.AddVerbCheck;
	// app.quit is BeforeQuit's), after the pages are built.
	Checks []CheckSpec `json:"checks,omitempty"`
	// Files is a file tree the session makes in a temp directory of its
	// own (removed at the end): paths relative to it, a trailing / for a
	// folder. {{files}} in a record field's base or value names it, and
	// writes show it as {{files}}.
	Files  []string `json:"files,omitempty"`
	Expect Expects  `json:"expect,omitempty"`

	// Path is where the file was loaded from (not part of the data); rel
	// is the path inside fsys, disk the directory fsys reads when it is on
	// disk (goldens are written there).
	Path string `json:"-"`
	fsys fs.FS
	rel  string
	disk string
}

// Page is one synthetic page: lists, inputs, a fake terminal and jobs.
type Page struct {
	ID     string     `json:"id,omitempty"`
	Title  string     `json:"title,omitempty"`
	Height int        `json:"height,omitempty"` // legacy spelling of the story's height (first page only)
	Width  int        `json:"width,omitempty"`
	Layout string     `json:"layout,omitempty"` // stack (default), row (lists side by side) or split (the first two lists in Layout.SplitFocusBlock, the focused one's panel focused)
	Order  []string   `json:"order,omitempty"`  // drawing order of list ids; default the order given
	Focus  string     `json:"focus,omitempty"`  // instance id focused when the page opens
	Offset int        `json:"offset,omitempty"` // page scroll offset after the first frame
	NoHead bool       `json:"noHeadings,omitempty"`
	Lists  []ListSpec `json:"lists,omitempty"`
	Trees  []TreeSpec `json:"trees,omitempty"`
	// Records are record editors (forms), drawn in the page's order with
	// the lists, or opened as a modal over the page.
	Records []RecordSpec `json:"records,omitempty"`
	Inputs  []InputSpec  `json:"inputs,omitempty"`
	Term    *TermSpec    `json:"terminal,omitempty"`
	Jobs    []JobSpec    `json:"jobs,omitempty"`
	// Segments are status segments in the header rule, each answering
	// its Texts in turn, one per refresh (tick segment).
	Segments []SegmentSpec `json:"segments,omitempty"`
	Bind     []BindSpec    `json:"bind,omitempty"`
	// Pending are pending modes the story defines (DefineMode with
	// pending), before its bind rows, which may enter them (do
	// "ochazuke.mode.enter", args {"mode": NAME}) or be bound in them.
	Pending []string `json:"pending,omitempty"`
	// Verbs are demo verbs with declared parameters, for palette stories.
	Verbs []VerbSpec `json:"verbs,omitempty"`
	// Store gives the demo noun props and an in-memory store
	// (ochazuke.NounBuilder.Props and Store): demo.get, list, create, set
	// (alias patch) and delete are then standard verbs over Things (the
	// standard delete replaces the row-removing demo.delete).
	Store  *StoreSpec  `json:"store,omitempty"`
	Detail *DetailSpec `json:"detail,omitempty"`
	// Markdown is a Markdown document (markdown.New) drawn in the page's
	// order with the lists, its task items a focus stop.
	Markdown *MarkdownSpec `json:"markdown,omitempty"`
	// Timeline is a card timeline (ochazuke.AddTimeline) drawn in the
	// page's order with the lists (its id, default "timeline", names it in
	// Order), its cards a focus stop.
	Timeline *TimelineSpec `json:"timeline,omitempty"`
	// Commands makes the page cli's Commands page (cli.AddCommands) over
	// these entries instead of a synthetic one; a run opens a fake pane.
	Commands *CommandsSpec `json:"commands,omitempty"`
	// Catalog makes the page a catalog data page (cli.MountPage) instead
	// of a synthetic one.
	Catalog *CatalogSpec `json:"catalog,omitempty"`
	// Chooser opens a chooser (ochazuke.OpenChooser) over the page's
	// focus when the story starts.
	Chooser *ChooserSpec `json:"chooser,omitempty"`
	Open    bool         `json:"open,omitempty"` // the page opened first (default the first page)
	// DefaultHidden declares the page hidden by default (ochazuke.DefaultHidden):
	// out of the tabs until page.show; a hidden page opened first is left
	// for the first page in the tabs, as Model.Init does.
	DefaultHidden bool `json:"defaultHidden,omitempty"`
	// Texts are fixed lines (a settings line, a caption), drawn in the
	// page's order by id, or after the inputs when Order leaves them out.
	Texts []TextSpec `json:"texts,omitempty"`
	// Readout draws the key readout in the page's bottom-right corner (or
	// in the page's order as "readout"): the mode, the focus path
	// innermost first, and the last key a row answered with what it ran.
	Readout bool `json:"readout,omitempty"`
}

// TextSpec is a fixed text drawn on the page.
type TextSpec struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// CheckSpec is one verb check: on Verb (default app.quit), Ask or Refuse
// while the page's first editable tree has unwritten changes (Dirty: any
// write since the story started), else always (Dirty false).
type CheckSpec struct {
	Verb   string `json:"verb,omitempty"`
	Name   string `json:"name,omitempty"`
	Ask    string `json:"ask,omitempty"`
	Refuse string `json:"refuse,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

// ChooserSpec is a chooser opened over the page's focus: its rows and the
// columns cut last (Keep). A row with a Preview gives the chooser a
// preview column (ochazuke.ChooserSpec.Preview). Picking a row closes it.
type ChooserSpec struct {
	Kind  string           `json:"kind,omitempty"` // default "story-chooser"
	Title string           `json:"title,omitempty"`
	Rows  []ChooserRowSpec `json:"rows"`
	Keep  []int            `json:"keep,omitempty"`
	// Sources make it a chooser with sources (tab cycles them); Rows are
	// then unused. demo.choose opens the chooser again.
	Sources []ChooserSourceSpec `json:"sources,omitempty"`
}

// ChooserSourceSpec is one source of a story chooser: its rows, or, Live,
// a row "typed: QUERY" and then the rows whose label holds the query (it
// sees the typed text).
type ChooserSourceSpec struct {
	Name string           `json:"name"`
	Rows []ChooserRowSpec `json:"rows"`
	Live bool             `json:"live,omitempty"`
}

// ChooserRowSpec is one chooser row; Cells draw it as columns, Preview is
// its preview text.
type ChooserRowSpec struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Cells   []string `json:"cells,omitempty"`
	Preview string   `json:"preview,omitempty"`
}

// ListSpec is one list: rows a1..aN labelled by id unless Labels or Items
// say otherwise.
type ListSpec struct {
	ID      string     `json:"id"`
	Rows    int        `json:"rows,omitempty"`
	Labels  []string   `json:"labels,omitempty"`
	Items   []ItemSpec `json:"items,omitempty"` // explicit ids and labels (a snapshot writes these)
	NoSpill bool       `json:"noSpill,omitempty"`
	NoMarks bool       `json:"noMarks,omitempty"` // a read-only view: space does not mark
	// ClearFilterOnEnter: enter in the filter line chooses the match and
	// clears the filter (ochazuke.ClearFilterOnEnter).
	ClearFilterOnEnter bool `json:"clearFilterOnEnter,omitempty"`
	// CellSelect: a table with a current column (ochazuke.CellSelect);
	// without it a table selects rows.
	CellSelect bool  `json:"cellSelect,omitempty"`
	Height     int   `json:"height,omitempty"` // rows drawn; 0 = every row, -1 = the rest of the page
	Selected   int   `json:"selected,omitempty"`
	Marked     []int `json:"marked,omitempty"`
	// Columns make the list a table (ochazuke.Columns); rows bring their
	// cells in Items.
	Columns []ochazuke.Column `json:"columns,omitempty"`
}

// TreeSpec is one tree: its nodes (parents before children), drawn in the
// page's order with the lists. Editable makes it an outline whose changes
// the session logs (the writes expect); FailWrites makes the first N writes
// fail.
type TreeSpec struct {
	Guides     bool                `json:"guides,omitempty"` // branch lines (ochazuke.Guides)
	ID         string              `json:"id"`
	Nodes      []ochazuke.TreeNode `json:"nodes,omitempty"`
	Editable   bool                `json:"editable,omitempty"`
	FailWrites int                 `json:"failWrites,omitempty"`
	Collapsed  []string            `json:"collapsed,omitempty"`
	Height     int                 `json:"height,omitempty"` // rows drawn; 0 = every visible row, -1 = the rest of the page
	// Columns make the tree's rows a table (ochazuke.Columns); nodes bring
	// their cells.
	Columns []ochazuke.Column `json:"columns,omitempty"`
}

// RecordSpec is one record editor: its fields (ochazuke.RecordField as
// JSON), the values it loads, and whether it opens as a modal over the
// page's focus (Modal) instead of being drawn in the page. Every write is
// logged (the writes expect, RecordWrite.String); FailWrites makes the
// first N fail.
type RecordSpec struct {
	ID         string                 `json:"id"`
	Title      string                 `json:"title,omitempty"`
	Fields     []ochazuke.RecordField `json:"fields"`
	Values     map[string]string      `json:"values,omitempty"`
	Modal      bool                   `json:"modal,omitempty"`
	Insert     bool                   `json:"insert,omitempty"`
	FailWrites int                    `json:"failWrites,omitempty"`
	Height     int                    `json:"height,omitempty"` // lines drawn; 0 = the rest of the page
	// Thing makes the editor edit one of the page's store things (its
	// key): it loads the thing's props, a write copies the record into
	// it, and the editor watches the store's noun (RecordSpec.Watch), so
	// a set from outside reaches it.
	Thing string `json:"thing,omitempty"`
	// Reopen is a key (normal mode) that edits the record again once the
	// editor is closed: a new editor under the same id, focused, as an
	// application's edit verb places one.
	Reopen string `json:"reopen,omitempty"`
}

// ItemSpec is an explicit row; Cells are its table cells, Tones the
// tones of its cells (ochazuke.ToneSpan).
type ItemSpec struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Noun is the row's noun (default the demo noun): a row of another
	// noun (project) is a thing a demo prop may name (a reference).
	Noun  string              `json:"noun,omitempty"`
	Cells []string            `json:"cells,omitempty"`
	Tones []ochazuke.ToneSpan `json:"tones,omitempty"`
}

// DetailSpec is a scrollable detail (ochazuke.AddDetail) drawn to the
// right of the page: a "detail" heading with its position marker, then
// Lines lines ("<item> line N") about the selected row of the focused list
// (else the first list's).
type DetailSpec struct {
	Lines int `json:"lines"`
	Width int `json:"width,omitempty"` // columns; default half the page
}

// MarkdownSpec is a Markdown document drawn in the page's order with the
// lists (its id, default "markdown", names it in Order) at the page's
// width. Its source holds Text; every write is logged (the writes
// expect: "toggle LINE [x] TEXT" for a toggle, "write LINE -OLD +NEW" for
// any other edit). External is what the source
// holds from the first toggle on, a change made elsewhere after the page
// loaded, so a story can show a toggle found again or refused.
type MarkdownSpec struct {
	ID       string  `json:"id,omitempty"`
	Text     string  `json:"text"`
	External *string `json:"external,omitempty"`
	Height   int     `json:"height,omitempty"` // lines drawn; 0 = every line, -1 = the rest of the page
	// PreserveNewlines draws every source line of a paragraph as a line
	// (markdown.PreserveNewlines).
	PreserveNewlines bool `json:"preserveNewlines,omitempty"`
	// Author signs new notes (Doc.Author) and Today dates them
	// (YYYY-MM-DD, Doc.Now), so a story's writes are the same every run.
	Author string `json:"author,omitempty"`
	Today  string `json:"today,omitempty"`
}

// TimelineSpec is a card timeline: its cards (of the story-card noun),
// FollowTail and HeaderLines as the options of the same names, and Append,
// cards a (timeline-append on story-card) adds one at a time at the end,
// while s (timeline-stream) adds a line, "streamed line N", to the last
// card's body, as a streaming answer does.
type TimelineSpec struct {
	ID          string     `json:"id,omitempty"`
	Cards       []CardSpec `json:"cards"`
	Append      []CardSpec `json:"append,omitempty"`
	FollowTail  bool       `json:"followTail,omitempty"`
	HeaderLines int        `json:"headerLines,omitempty"`
	NoSpill     bool       `json:"noSpill,omitempty"`
	Height      int        `json:"height,omitempty"` // lines drawn; 0 = the rest of the page
	// ParentStop is where h on a closed card goes (ochazuke.ParentStop):
	// a list's id lands on its selected row.
	ParentStop string `json:"parentStop,omitempty"`
}

// CardSpec is one card (ochazuke.Card) with its body and sections as text.
type CardSpec struct {
	ID       string        `json:"id"`
	Header   string        `json:"header"`
	Meta     string        `json:"meta,omitempty"`
	Body     string        `json:"body,omitempty"`
	Sections []SectionSpec `json:"sections,omitempty"`
	Steps    []StepSpec    `json:"steps,omitempty"`
	Open     bool          `json:"open,omitempty"`
	Active   bool          `json:"active,omitempty"`
	Muted    bool          `json:"muted,omitempty"`
	Group    string        `json:"group,omitempty"`
	// Event draws the card as something that happened (Card.Event): a
	// task's notification, indented, its border dashed and muted.
	Event bool `json:"event,omitempty"`
	// MetaTones and Tone are Card.MetaTones and Card.Tone: tone runs of
	// the meta line, and the tone the border draws in.
	MetaTones []ochazuke.ToneSpan `json:"metaTones,omitempty"`
	Tone      string              `json:"tone,omitempty"`
}

// SectionSpec is one card section (ochazuke.CardSection) with its text;
// Role is muted, accent, error or empty for text.
type SectionSpec struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Text    string `json:"text,omitempty"`
	Role    string `json:"role,omitempty"`
	Folded  bool   `json:"folded,omitempty"`
}

// StepSpec is one step inside a card (ochazuke.CardStep) with its text.
type StepSpec struct {
	ID         string `json:"id,omitempty"`
	Title      string `json:"title,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Text       string `json:"text,omitempty"`
	Role       string `json:"role,omitempty"`
	Folded     bool   `json:"folded,omitempty"`
	Header     bool   `json:"header,omitempty"`
	Paragraphs bool   `json:"paragraphs,omitempty"`
}

// CommandsSpec is a Commands page: its entries (cli.CommandEntry as JSON)
// and the help text command help fetches for an entry without its own.
type CommandsSpec struct {
	Entries []cli.CommandEntry `json:"entries"`
	Help    map[string]string  `json:"help,omitempty"`
}

// CatalogSpec is a data page as an extension's catalog declares it: the
// catalog's nouns (columns, verbs, rows) and state source, the page
// (cli.CatalogPage; its id is the story page's, its title defaults to it), and
// each verb's answer by NOUN.VERB, the stdout the page's Fetch returns (a
// JSON string is the text itself, so JSON Lines fit; any other value its
// JSON). A verb without an answer fails. The loads settle before the story
// starts unless Pending, which leaves them to the script (job
// catalog-load).
type CatalogSpec struct {
	State   string                     `json:"state,omitempty"`
	Nouns   []cli.CatalogNoun          `json:"nouns"`
	Page    cli.CatalogPage            `json:"page"`
	Answers map[string]json.RawMessage `json:"answers,omitempty"`
	Pending bool                       `json:"pending,omitempty"`
	// Files are the documents a markdown detail (detail kind markdown,
	// markdown/cliview) opens, by the path its rows' pathField names; a
	// write is logged as a markdown page's (writes), a path not here fails
	// to load.
	Files map[string]string `json:"files,omitempty"`
	// Author and Today sign and date the notes added in a markdown detail.
	Author string `json:"author,omitempty"`
	Today  string `json:"today,omitempty"`
}

// InputSpec is one single-line input drawn under the lists. Completions
// give it completion triggers answered by the completion fake.
type InputSpec struct {
	ID          string           `json:"id"`
	Label       string           `json:"label,omitempty"`
	Text        string           `json:"text,omitempty"`
	Completions []CompletionSpec `json:"completions,omitempty"`
}

// CompletionSpec is one completion trigger of a story input: its search
// answers the items holding the query (ignoring case), in order, or fails
// with Fail. Its job is completion.search.
type CompletionSpec struct {
	Trigger string   `json:"trigger"`
	Items   []string `json:"items,omitempty"`
	Fail    string   `json:"fail,omitempty"`
}

// TermSpec is a fake pane attached from a row: enter on Attach (default the
// first row) adds the terminal, CloseOnDetach, focus returning to the row.
type TermSpec struct {
	Fake   bool       `json:"fake"`
	Attach string     `json:"attach,omitempty"`
	Height int        `json:"height,omitempty"`
	Hints  *HintsSpec `json:"hints,omitempty"`
}

// HintsSpec gives the pane the hints recipe: the leader then Key (default
// f) runs demo.hints, which sets an overlay of Marks (dimmed with Dim) and
// enters the passthrough hints mode. There a letter is demo.hint copy, an
// uppercase letter type, a ctrl+letter open (each logs "hint ACTION
// LETTER" to writes, clears the overlay and returns to terminal mode, the
// focus unchanged), esc demo.hints-cancel, and any other key nothing; no
// key reaches the pane.
type HintsSpec struct {
	Key   string     `json:"key,omitempty"`
	Dim   bool       `json:"dim,omitempty"`
	Marks []MarkSpec `json:"marks,omitempty"`
}

// MarkSpec is one overlay mark: Text drawn from the pane cell Row, Col.
type MarkSpec struct {
	Row  int    `json:"row"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}

// JobSpec is a demo application verb whose job runs until FinishOnKey (or
// FailOnKey) is pressed, so ticker stories need no daemon.
type JobSpec struct {
	Verb        string `json:"verb"` // demo.<name>
	Key         string `json:"key,omitempty"`
	FinishOnKey string `json:"finishOnKey,omitempty"`
	FailOnKey   string `json:"failOnKey,omitempty"`
	Quiet       bool   `json:"quiet,omitempty"`
	Start       int    `json:"start,omitempty"` // jobs started when the story opens
	Label       string `json:"label,omitempty"` // the job's Result.Label
}

// SegmentSpec is a status segment whose provider answers Texts in turn,
// the last one again once they run out; Tone is the row's tone, Enabled
// its setting's default (nil: on).
type SegmentSpec struct {
	ID      string   `json:"id"`
	Label   string   `json:"label,omitempty"`
	Texts   []string `json:"texts"`
	Tone    string   `json:"tone,omitempty"`
	Order   int      `json:"order,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"`
}

// BindSpec is an extra binding row: keys on noun in mode run do.
type BindSpec struct {
	Noun string         `json:"noun"`
	Mode string         `json:"mode,omitempty"`
	Keys string         `json:"keys"`
	Do   string         `json:"do"`
	Args map[string]any `json:"args,omitempty"`
}

// StoreSpec is the demo noun's props (ochazuke.PropSpec as JSON) and its
// things, a collection store in memory that reads and writes. Every call
// logs to the writes: "list" for a list, "get KEY" for a get, "set KEY
// prop=value ..." for a set, "create KEY prop=value ..." and "delete KEY".
// A page's rows of the demo noun stand for the things whose key is their
// id.
type StoreSpec struct {
	// Noun is the noun that declares the store (default the demo noun):
	// a story about a noun's name (a collection named pa..., against the
	// palette.* settings) names it.
	Noun   string              `json:"noun,omitempty"`
	Props  []ochazuke.PropSpec `json:"props"`
	Things []ochazuke.Thing    `json:"things"`
}

// VerbSpec is a demo application verb with parameters: running it logs
// "NOUN.VERB name=value ..." to the writes, and returns Rows (so another
// verb's parameter can name it as its Source).
type VerbSpec struct {
	Verb        string                        `json:"verb"` // demo.<name>
	Description string                        `json:"description,omitempty"`
	Parameters  map[string]ochazuke.Parameter `json:"parameters,omitempty"`
	Positional  []string                      `json:"positional,omitempty"`
	Rows        []string                      `json:"rows,omitempty"`
	// Aliases are the verb's other names (ochazuke.ActionAlias: alias as
	// noun.verb, since/removeAfter for a deprecated one; action is the
	// verb). A run through an alias logs " via=ALIAS" after its arguments.
	Aliases []ochazuke.ActionAlias `json:"aliases,omitempty"`
	// Row logs the row the verb ran on (" row=ID") and Fail makes it fail
	// on these rows: a verb over a visual selection, run once per row.
	Row  bool     `json:"row,omitempty"`
	Fail []string `json:"fail,omitempty"`
	// Many makes the verb take every selected row at once
	// (ochazuke.NounBuilder.Many): one run logs " subjects=A,B,C".
	Many bool `json:"many,omitempty"`
}

// Event is one action: a key press, typed text, a paste, a job delivering,
// or a framework timer firing. T is milliseconds since the session started
// (recordings only; scripts leave it 0).
type Event struct {
	T     int64  `json:"t,omitempty"`
	Key   string `json:"key,omitempty"`
	Type  string `json:"type,omitempty"`
	Paste string `json:"paste,omitempty"`
	Job   string `json:"job,omitempty"`   // noun.verb of the job that delivered
	State string `json:"state,omitempty"` // done or failed
	Tick  string `json:"tick,omitempty"`  // linger, binding, terminal (a pane frame), output (fake output line), debounce (a tree line's, a form's or a completion's pause) or segment (the status segments' next refresh, answered)
	// Restart quits the session and builds it again from the story with
	// the same state directory (State), as a user reopening the app.
	Restart bool `json:"restart,omitempty"`
}

// UnmarshalJSON reads a bare string as a key press.
func (e *Event) UnmarshalJSON(b []byte) error {
	var key string
	if err := json.Unmarshal(b, &key); err == nil {
		*e = Event{Key: key}
		return nil
	}
	type plain Event
	return json.Unmarshal(b, (*plain)(e))
}

// MarshalJSON writes an untimed key press as a bare string.
func (e Event) MarshalJSON() ([]byte, error) {
	if e.T == 0 && e.Key != "" && e.Type == "" && e.Paste == "" && e.Job == "" && e.Tick == "" && !e.Restart {
		return json.Marshal(e.Key)
	}
	type plain Event
	return json.Marshal(plain(e))
}

// String is the event as a script reads it.
func (e Event) String() string {
	switch {
	case e.Key != "":
		return e.Key
	case e.Type != "":
		return fmt.Sprintf("type %q", e.Type)
	case e.Paste != "":
		return fmt.Sprintf("paste %q", e.Paste)
	case e.Job != "":
		return "job " + e.Job + " " + e.State
	case e.Tick != "":
		return "tick " + e.Tick
	case e.Restart:
		return "restart"
	}
	return "(empty)"
}

// Expect is what must hold after a step (After events) or at the end
// (After 0). Every field is optional.
type Expect struct {
	After int     `json:"after,omitempty"`
	Focus string  `json:"focus,omitempty"`
	Mode  string  `json:"mode,omitempty"`
	Modal *string `json:"modal,omitempty"` // top modal instance, "" for none
	Quit  *bool   `json:"quit,omitempty"`  // Runtime.Quit: app.quit ran
	Page  string  `json:"page,omitempty"`
	// Shift is the current page's pane divider (Runtime.PaneShift).
	Shift   *int  `json:"shift,omitempty"`
	Visible *bool `json:"visible,omitempty"` // the focused element's label is on screen
	// Highlighted counts rows holding a full-strength highlight (reverse
	// video not under faint); Dimmed counts rows holding a dimmed one (an
	// unfocused page list's selected row, a highlight beneath a modal).
	// Dimmed is read in the mono theme, where a dim is the faint attribute.
	Highlighted *int `json:"highlighted,omitempty"`
	Dimmed      *int `json:"dimmed,omitempty"`
	// HighlightedText is text every highlighted row holds (the selected row's label).
	HighlightedText string `json:"highlightedText,omitempty"`
	// Cell is the text of a table's current cell (drawn reverse and
	// underlined inside its row, dimmed or not); "" for none.
	Cell *string `json:"cell,omitempty"`
	// Badge is the mode badge at the right of the title row (full
	// chrome): its whole text, padded by a cell on each side, followed by
	// the end of the row or a " · " (the focus display; a recording's
	// "· REC @q" too, so expect the whole badge while recording) (R2.10).
	Badge    string   `json:"badge,omitempty"`
	Contains []string `json:"contains,omitempty"`
	Lacks    []string `json:"lacks,omitempty"`
	Order    []string `json:"order,omitempty"` // substrings that appear on screen in this order
	Ticker   []string `json:"ticker,omitempty"`
	NoTicker bool     `json:"noTicker,omitempty"`
	// Segments are the status segments the header rule draws
	// (Model.StatusSegments); NoSegments expects none.
	Segments   []string `json:"segments,omitempty"`
	NoSegments bool     `json:"noSegments,omitempty"`
	Notices    []string `json:"notices,omitempty"`
	NoNotices  bool     `json:"noNotices,omitempty"`
	// Colored maps on-screen text to a "#RRGGBB" foreground some row holding
	// that text draws in; NoColor lists foregrounds no cell may use.
	Colored map[string]string `json:"colored,omitempty"`
	NoColor []string          `json:"noColor,omitempty"`
	// Styled maps on-screen text to whether it starts a run drawn in Hex
	// ("#RRGGBB"): the SGR right before the text sets that foreground,
	// cell-exact where Colored is row-wide. "!#RRGGBB" expects any other.
	Styled map[string]string `json:"styled,omitempty"`
	// Attrs maps on-screen text to the attributes its first cell is drawn
	// with, followed along its row's SGR state: a comma list of bold,
	// faint, reverse, underline, fg (any foreground colour) and bg (any
	// background colour), each
	// "-name" for absent. "bold,-fg" reads a mono tone.
	Attrs map[string]string `json:"attrs,omitempty"`
	// Writes is every change the page's editable trees, record editors and
	// Markdown documents wrote, in order (TreeChange.String,
	// RecordWrite.String, "toggle LINE [x] TEXT"); [] expects none.
	Writes *[]string `json:"writes,omitempty"`
	// Register is the unnamed register's text (a yank).
	Register *string `json:"register,omitempty"`
}

// Expects is one final Expect or a list of per-step ones.
type Expects []Expect

func (x *Expects) UnmarshalJSON(b []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(b)), "[") {
		var list []Expect
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}
		*x = list
		return nil
	}
	var one Expect
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*x = Expects{one}
	return nil
}

func (x Expects) MarshalJSON() ([]byte, error) {
	if len(x) == 1 && x[0].After == 0 {
		return json.Marshal(x[0])
	}
	return json.Marshal([]Expect(x))
}

// Size is the story's width and height (80x24 unless given).
func (s *Story) Size() (int, int) {
	w, h := s.Width, s.Height
	if pages := s.AllPages(); len(pages) > 0 {
		w, h = cmp(w, pages[0].Width), cmp(h, pages[0].Height)
	}
	return cmp(w, 80), cmp(h, 24)
}

func cmp(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

// AllPages is Page followed by Pages.
func (s *Story) AllPages() []*Page {
	var out []*Page
	if s.Page != nil {
		out = append(out, s.Page)
	}
	return append(out, s.Pages...)
}

// Slug is the file name stem of a name.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// Parse reads one story file.
func Parse(data []byte, where string) (*Story, error) {
	var s Story
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("%s: a story needs a name", where)
	}
	if s.Area == "" {
		s.Area = path.Base(path.Dir(filepath.ToSlash(where)))
	}
	if len(s.AllPages()) == 0 && s.Base == "" {
		return nil, fmt.Errorf("%s: a story needs a page or a base", where)
	}
	s.Path = where
	return &s, nil
}

// LoadFS reads every <area>/<name>.json under root in fsys.
func LoadFS(fsys fs.FS, root string) ([]*Story, error) {
	var out []*Story
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".json" {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		s, err := Parse(data, p)
		if err != nil {
			return err
		}
		s.fsys, s.rel = fsys, p
		out = append(out, s)
		return nil
	})
	return out, err
}

// LoadDir reads the stories under a directory on disk; a missing directory
// is no stories.
func LoadDir(dir string) ([]*Story, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	stories, err := LoadFS(os.DirFS(dir), ".")
	for _, s := range stories {
		s.Path, s.disk = filepath.Join(dir, s.Path), dir
	}
	return stories, err
}

// Sort orders stories by area, then file path.
func Sort(stories []*Story) {
	slices.SortStableFunc(stories, func(a, b *Story) int {
		if c := strings.Compare(a.Area, b.Area); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
}

// Find returns the story whose name, slug or area/slug is name.
func Find(stories []*Story, name string) *Story {
	for _, s := range stories {
		stem := strings.TrimSuffix(path.Base(filepath.ToSlash(s.Path)), ".json")
		if s.Name == name || stem == name || s.Area+"/"+stem == name {
			return s
		}
	}
	return nil
}

// GoldenPath is the golden frame file beside the story.
func (s *Story) GoldenPath() string { return strings.TrimSuffix(s.Path, ".json") + ".golden" }

// ReadGolden reads the golden frame, or reports fs.ErrNotExist.
func (s *Story) ReadGolden() ([]byte, error) {
	if s.fsys == nil {
		return os.ReadFile(s.GoldenPath())
	}
	return fs.ReadFile(s.fsys, strings.TrimSuffix(s.rel, ".json")+".golden")
}

// WriteGolden writes the golden frame beside a story loaded from disk.
func (s *Story) WriteGolden(frame string) error {
	if s.fsys != nil && s.disk == "" {
		return fmt.Errorf("%s: embedded story; pass --dir to update goldens on disk", s.Path)
	}
	return os.WriteFile(s.GoldenPath(), []byte(frame), 0o644)
}
