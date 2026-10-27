// Package story is o-story's engine: a story is data (a synthetic page, a
// script of events, an expectation), Build turns it into a live runtime and
// model, a Driver plays events against it headless, and a Recorder turns
// the messages of a live session into the same events. State changes only
// through events; a frame is a pure function of the story and its events.
// Stories never read production state: every row, label and pane is in the
// file.
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

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli"
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
	// State gives the session a state directory of its own (a temp dir,
	// removed when the session ends), kept across a restart event, so a
	// story shows what the application remembers (pane dividers).
	State  bool    `json:"state,omitempty"`
	Expect Expects `json:"expect,omitempty"`

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
	Bind    []BindSpec   `json:"bind,omitempty"`
	Detail  *DetailSpec  `json:"detail,omitempty"`
	// Markdown is a Markdown document (markdown.New) drawn in the page's
	// order with the lists, its task items a focus stop.
	Markdown *MarkdownSpec `json:"markdown,omitempty"`
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
}

// ChooserSpec is a chooser opened over the page's focus: its rows and the
// columns cut last (Keep). A row with a Preview gives the chooser a
// preview column (ochazuke.ChooserSpec.Preview). Picking a row closes it.
type ChooserSpec struct {
	Kind  string           `json:"kind,omitempty"` // default "story-chooser"
	Title string           `json:"title,omitempty"`
	Rows  []ChooserRowSpec `json:"rows"`
	Keep  []int            `json:"keep,omitempty"`
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
	ID       string     `json:"id"`
	Rows     int        `json:"rows,omitempty"`
	Labels   []string   `json:"labels,omitempty"`
	Items    []ItemSpec `json:"items,omitempty"` // explicit ids and labels (a snapshot writes these)
	NoSpill  bool       `json:"noSpill,omitempty"`
	NoMarks  bool       `json:"noMarks,omitempty"` // a read-only view: space does not mark
	Height   int        `json:"height,omitempty"`  // rows drawn; 0 = every row, -1 = the rest of the page
	Selected int        `json:"selected,omitempty"`
	Marked   []int      `json:"marked,omitempty"`
	// Columns make the list a table (ochazuke.Columns); rows bring their
	// cells in Items.
	Columns []ochazuke.Column `json:"columns,omitempty"`
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
}

// ItemSpec is an explicit row; Cells are its table cells.
type ItemSpec struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Cells []string `json:"cells,omitempty"`
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
// width. Its source holds Text; every toggle it writes is logged (the
// writes expect, "toggle LINE [x] TEXT"). External is what the source
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
	Fake   bool   `json:"fake"`
	Attach string `json:"attach,omitempty"`
	Height int    `json:"height,omitempty"`
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

// BindSpec is an extra binding row: keys on noun in mode run do.
type BindSpec struct {
	Noun string         `json:"noun"`
	Mode string         `json:"mode,omitempty"`
	Keys string         `json:"keys"`
	Do   string         `json:"do"`
	Args map[string]any `json:"args,omitempty"`
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
	Tick  string `json:"tick,omitempty"`  // linger, binding, terminal (a pane frame), output (fake output line) or debounce (a tree line's, a form's or a completion's pause)
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
	HighlightedText string   `json:"highlightedText,omitempty"`
	Contains        []string `json:"contains,omitempty"`
	Lacks           []string `json:"lacks,omitempty"`
	Order           []string `json:"order,omitempty"` // substrings that appear on screen in this order
	Ticker          []string `json:"ticker,omitempty"`
	NoTicker        bool     `json:"noTicker,omitempty"`
	Notices         []string `json:"notices,omitempty"`
	NoNotices       bool     `json:"noNotices,omitempty"`
	// Colored maps on-screen text to a "#RRGGBB" foreground some row holding
	// that text draws in; NoColor lists foregrounds no cell may use.
	Colored map[string]string `json:"colored,omitempty"`
	NoColor []string          `json:"noColor,omitempty"`
	// Styled maps on-screen text to whether it starts a run drawn in Hex
	// ("#RRGGBB"): the SGR right before the text sets that foreground,
	// cell-exact where Colored is row-wide. "!#RRGGBB" expects any other.
	Styled map[string]string `json:"styled,omitempty"`
	// Writes is every change the page's editable trees, record editors and
	// Markdown documents wrote, in order (TreeChange.String,
	// RecordWrite.String, "toggle LINE [x] TEXT"); [] expects none.
	Writes *[]string `json:"writes,omitempty"`
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
