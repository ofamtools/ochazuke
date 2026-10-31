package demo

import (
	"encoding/json"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli"
)

// Page is one synthetic page: lists, inputs, a fake terminal and jobs.
type Page struct {
	ID     string     `json:"id,omitempty"`
	Title  string     `json:"title,omitempty"`
	Height int        `json:"height,omitempty"` // legacy spelling of the story's height (first page only)
	Width  int        `json:"width,omitempty"`
	Layout string     `json:"layout,omitempty"` // stack (default), row (lists side by side), split (the first two lists in Layout.SplitFocusBlock, the focused one's panel focused) or panels (each list in a panel of its own, stacked, the focused one's panel focused, its border in the list's panelTone)
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
	// Modes are modes the story defines, by name, as an application's
	// bindings file declares them (ochazuke.ModeSpec: pending, extends),
	// before its bind rows and its lists' and inputs' pins.
	Modes map[string]ochazuke.ModeSpec `json:"modes,omitempty"`
	// ModeTones are badge hues the story's application declares by mode
	// (Appearance.SetModeTone: the tone row mode.<name>), under any
	// themes.json theme the story gives.
	ModeTones map[string]ochazuke.SchemeColor `json:"modeTones,omitempty"`
	// Verbs are demo verbs with declared parameters, for palette stories.
	Verbs []VerbSpec `json:"verbs,omitempty"`
	// Nouns are nouns besides demo, as a host defines an extension's
	// nouns mounted under its namespace (json.doc): each verb logs
	// NOUN.VERB and its arguments to writes, and each alias
	// (ochazuke.NounAlias: since/removeAfter for a deprecated one) is
	// registered with Runtime.AddNounAlias.
	Nouns []NounSpec `json:"nouns,omitempty"`
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
	// HeaderTones tone the header row's cells (List.SetHeaderTones): a
	// highlight layer over a column's header.
	HeaderTones []ochazuke.ToneSpan `json:"headerTones,omitempty"`
	// PanelTone is the tone of the list's panel border in the panels
	// layout (Layout.WithPanelTone): a state such as state.working.
	PanelTone string `json:"panelTone,omitempty"`
	// Pin is the base mode the list declares (Runtime.PinInstance): focus
	// on it puts the runtime in that mode.
	Pin string `json:"pin,omitempty"`
}

// TreeSpec is one tree: its nodes (parents before children), drawn in the
// page's order with the lists. Editable makes it an outline whose changes
// the session logs (the writes expect); FailWrites makes the first N writes
// fail.
type TreeSpec struct {
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
	Data  any            `json:"data,omitempty"`
	Props ochazuke.Thing `json:"props,omitempty"`
	ID    string         `json:"id"`
	Label string         `json:"label"`
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
	// Secret makes the input secret (Input.Secret): the key echo shows
	// nothing while it has the focus.
	Secret bool `json:"secret,omitempty"`
	// ReadOnly makes the input read-only (ochazuke.ReadOnly): motions,
	// visual modes and yanks work, edits are no-ops.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Multiline makes the input many lines (its text's newlines kept).
	Multiline bool `json:"multiline,omitempty"`
	// Pin is the base mode the input declares (Runtime.PinInstance).
	Pin string `json:"pin,omitempty"`
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
// NounSpec is one noun of a story's own (Page.Nouns).
type NounSpec struct {
	Props   []ochazuke.PropSpec    `json:"props,omitempty"`
	Filters []ochazuke.SavedFilter `json:"filters,omitempty"`
	Noun    string                 `json:"noun"`
	Verbs   []string               `json:"verbs,omitempty"`
	Aliases []ochazuke.NounAlias   `json:"aliases,omitempty"`
}

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
