package ochazuke

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// The settings the open resolver reads.
const (
	// OpenOpenersSetting holds the user's opener rows as a JSON array
	// (OpenerRow). They are tried before the built-in rows
	// (opener_rows.json) at the same specificity.
	OpenOpenersSetting = "open.openers"
	// OpenUnmatchedSetting says what open does with text no row matches:
	// OpenUnmatchedOS (the default) hands it to the OS opener, as record
	// first, never refuse asks; OpenUnmatchedNone does nothing and says so.
	OpenUnmatchedSetting = "open.unmatched"
	OpenUnmatchedOS      = "os"
	OpenUnmatchedNone    = "none"
)

// The actions an opener row names.
const (
	// OpenActionOS opens the target with the OS (Opener.Open).
	OpenActionOS = "os-open"
	// OpenActionReveal shows the target in the file manager (Opener.Reveal).
	OpenActionReveal = "os-reveal"
	// OpenActionPage opens the page named by To (ochazuke.page.open).
	OpenActionPage = "page"
	// OpenActionViewer runs the verb To ("noun.verb") with the resolved
	// target as its "target" argument: a viewer for a kind of file or link.
	OpenActionViewer = "viewer"
	// OpenActionVerb runs a verb of the noun a "noun:id" text names: To is
	// the verbs to try in order, space separated ("open show"), the first
	// the noun has. On a row with no noun match To is "noun.verb" and the
	// text is its "target" argument, as for a viewer.
	OpenActionVerb = "verb"
	// OpenActionNone is the action of text nothing opens (empty text, or
	// unmatched text with open.unmatched=none).
	OpenActionNone = "none"
)

// OpenerRow is one row of the opener table: exactly one of Scheme, Prefix,
// Ext and Noun says what text it matches, Action what to do with it.
type OpenerRow struct {
	// Scheme matches text that starts with "scheme:" (http, mailto, file).
	Scheme string `json:"scheme,omitempty"`
	// Prefix matches text that starts with it ("~/", "/", "./").
	Prefix string `json:"prefix,omitempty"`
	// Ext matches a file extension without the dot, any case (png, md).
	Ext string `json:"ext,omitempty"`
	// Noun matches "noun:id" text whose noun is registered: the noun's
	// name (or alias), or "*" for any noun.
	Noun   string `json:"noun,omitempty"`
	Action string `json:"action"`
	// To is the page, the verb or the verbs the action names.
	To string `json:"to,omitempty"`
}

// OpenTarget is what a text was read as.
type OpenTarget struct {
	Text string
	// Kind names what it was read as, for display: "noun" (a registered
	// noun's "noun:id"), "scheme", "path" (starts with ~, / or a dot
	// path) or "text". The resolver reads the fields below, not Kind.
	Kind   string
	Scheme string // lower case, for a "scheme:" prefix that is not a noun
	Ext    string // lower case, without the dot
	Noun   string // canonical noun name, for a "noun:id"
	ID     string
	Path   bool // starts like a path (~, /, ./, ../)
	// Arg is what the OS opener receives: the text, with a leading ~ expanded.
	Arg string
}

// OpenAction is what opening a target does.
type OpenAction struct {
	// Kind is an OpenAction constant.
	Kind string
	// Noun, Verb and ID are the request a verb action runs: the verb with
	// the thing's id as its "id" argument (as ochazuke.page.open takes it).
	Noun string
	Verb string
	ID   string
	// Page is the page a page action opens.
	Page string
	// Arg is what an OS action opens or reveals, or the "target" a viewer gets.
	Arg string
	// Row is the matching row; nil when nothing matched.
	Row *OpenerRow
	// Unmatched is set when no row matched and open.unmatched decided.
	Unmatched bool
}

// String says what the action would do, for the modal line and the CLI.
func (a OpenAction) String() string {
	if def, ok := openActionDefs[a.Kind]; ok {
		return def.describe(a)
	}
	return "nothing to open"
}

// openActionDef is what one action does; the table below is the only place
// the action names are read.
type openActionDef struct {
	check    func(row OpenerRow) error
	usable   func(r *Runtime, row OpenerRow, t OpenTarget) bool // nil: always
	resolve  func(r *Runtime, row OpenerRow, t OpenTarget, a *OpenAction)
	run      func(r *Runtime, a OpenAction) error
	describe func(a OpenAction) string
}

var openActionDefs map[string]openActionDef

func init() {
	viewer := openActionDef{
		check: func(row OpenerRow) error {
			if !strings.Contains(row.To, ".") {
				return errors.New(`needs to as "noun.verb"`)
			}
			return nil
		},
		resolve: func(r *Runtime, row OpenerRow, t OpenTarget, a *OpenAction) {
			a.Noun, a.Verb, _ = strings.Cut(row.To, ".")
			a.Arg = t.Text
		},
		run: func(r *Runtime, a OpenAction) error {
			_, err := r.Execute(Request{Noun: a.Noun, Verb: a.Verb, Args: Arguments{"target": a.Arg}})
			return err
		},
		describe: func(a OpenAction) string { return fmt.Sprintf("%s.%s %s", a.Noun, a.Verb, a.Arg) },
	}
	openActionDefs = map[string]openActionDef{
		OpenActionOS: {
			check:    func(OpenerRow) error { return nil },
			resolve:  func(r *Runtime, row OpenerRow, t OpenTarget, a *OpenAction) { a.Arg = t.Arg },
			run:      func(r *Runtime, a OpenAction) error { return r.Opener().Open(a.Arg) },
			describe: func(a OpenAction) string { return "open " + a.Arg },
		},
		OpenActionReveal: {
			check:    func(OpenerRow) error { return nil },
			resolve:  func(r *Runtime, row OpenerRow, t OpenTarget, a *OpenAction) { a.Arg = t.Arg },
			run:      func(r *Runtime, a OpenAction) error { return r.Opener().Reveal(a.Arg) },
			describe: func(a OpenAction) string { return "reveal " + a.Arg },
		},
		OpenActionPage: {
			check: func(row OpenerRow) error {
				if row.To == "" {
					return errors.New("needs to (the page id)")
				}
				return nil
			},
			resolve: func(r *Runtime, row OpenerRow, t OpenTarget, a *OpenAction) { a.Page = row.To },
			run: func(r *Runtime, a OpenAction) error {
				_, err := r.Execute(Request{Noun: "ochazuke.page", Verb: "open", Args: Arguments{"id": a.Page}})
				return err
			},
			describe: func(a OpenAction) string { return "page " + a.Page },
		},
		OpenActionViewer: viewer,
		OpenActionVerb: {
			check: func(row OpenerRow) error {
				if row.Noun == "" {
					return viewer.check(row)
				}
				if row.To == "" {
					return errors.New(`needs to (the verbs to try, such as "open show")`)
				}
				return nil
			},
			usable: func(r *Runtime, row OpenerRow, t OpenTarget) bool {
				if row.Noun == "" {
					return true
				}
				_, ok := r.openVerb(t.Noun, row.To)
				return ok
			},
			resolve: func(r *Runtime, row OpenerRow, t OpenTarget, a *OpenAction) {
				if row.Noun == "" { // no noun to read: a fixed noun.verb, as a viewer
					viewer.resolve(r, row, t, a)
					a.Kind = OpenActionViewer
					return
				}
				a.Noun, a.ID = t.Noun, t.ID
				a.Verb, _ = r.openVerb(t.Noun, row.To)
			},
			run: func(r *Runtime, a OpenAction) error {
				_, err := r.Execute(Request{Noun: a.Noun, Verb: a.Verb, Args: Arguments{"id": a.ID}})
				return err
			},
			describe: func(a OpenAction) string { return fmt.Sprintf("%s.%s %s", a.Noun, a.Verb, a.ID) },
		},
		OpenActionNone: {
			check:    func(OpenerRow) error { return nil },
			resolve:  func(r *Runtime, row OpenerRow, t OpenTarget, a *OpenAction) {},
			run:      func(r *Runtime, a OpenAction) error { return nil },
			describe: func(a OpenAction) string { return "nothing to open" },
		},
	}
}

//go:embed opener_rows.json
var builtinOpenerJSON []byte

var builtinOpeners = sync.OnceValue(func() []OpenerRow {
	var rows []OpenerRow
	if err := json.Unmarshal(builtinOpenerJSON, &rows); err != nil {
		panic("opener_rows.json: " + err.Error())
	}
	return rows
})

// BuiltinOpenerRows are the default rows; the user's rows come first.
func BuiltinOpenerRows() []OpenerRow { return slices.Clone(builtinOpeners()) }

// openRowKind is one way a row matches text. The table's order is the
// specificity: a noun's own row, any noun, scheme, extension, prefix (a
// prefix such as / is the broadest).
type openRowKind struct {
	field func(OpenerRow) string
	match func(row OpenerRow, t OpenTarget, canonical func(string) string) bool
}

var openRowKinds = []openRowKind{
	{func(row OpenerRow) string { return exactNoun(row) }, func(row OpenerRow, t OpenTarget, canonical func(string) string) bool {
		return t.Noun != "" && canonical(row.Noun) == t.Noun
	}},
	{func(row OpenerRow) string { return anyNoun(row) }, func(row OpenerRow, t OpenTarget, _ func(string) string) bool { return t.Noun != "" }},
	{func(row OpenerRow) string { return row.Scheme }, func(row OpenerRow, t OpenTarget, _ func(string) string) bool {
		return t.Scheme != "" && strings.EqualFold(row.Scheme, t.Scheme)
	}},
	{func(row OpenerRow) string { return row.Ext }, func(row OpenerRow, t OpenTarget, _ func(string) string) bool {
		return t.Noun == "" && t.Ext != "" && strings.EqualFold(row.Ext, t.Ext)
	}},
	{func(row OpenerRow) string { return row.Prefix }, func(row OpenerRow, t OpenTarget, _ func(string) string) bool {
		return t.Noun == "" && strings.HasPrefix(t.Text, row.Prefix)
	}},
}

// anyNounMark is the Noun value that matches a "noun:id" of any registered noun.
const anyNounMark = "*"

func exactNoun(row OpenerRow) string {
	if row.Noun == anyNounMark {
		return ""
	}
	return row.Noun
}

func anyNoun(row OpenerRow) string {
	if row.Noun == anyNounMark {
		return row.Noun
	}
	return ""
}

// level is the row's place in openRowKinds (its specificity; lower is
// more specific), or -1 for a row with no match field.
func (row OpenerRow) level() int {
	for i, k := range openRowKinds {
		if k.field(row) != "" {
			return i
		}
	}
	return -1
}

func (row OpenerRow) check() error {
	n := 0
	for _, v := range []string{row.Scheme, row.Prefix, row.Ext, row.Noun} {
		if v != "" {
			n++
		}
	}
	if n != 1 {
		return errors.New("a row needs exactly one of scheme, prefix, ext and noun")
	}
	def, ok := openActionDefs[row.Action]
	if !ok || row.Action == OpenActionNone {
		return fmt.Errorf("unknown action %q (os-open, os-reveal, page, viewer, verb)", row.Action)
	}
	if err := def.check(row); err != nil {
		return fmt.Errorf("action %s %w", row.Action, err)
	}
	return nil
}

func parseOpenerRows(text string) ([]OpenerRow, error) {
	var rows []OpenerRow
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rows); err != nil {
		return nil, fmt.Errorf("%s must be a JSON array of opener rows: %w", OpenOpenersSetting, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s must contain one JSON array", OpenOpenersSetting)
	}
	if rows == nil {
		return nil, fmt.Errorf("%s must be a JSON array, use [] for no rows", OpenOpenersSetting)
	}
	for i, row := range rows {
		if err := row.check(); err != nil {
			return nil, fmt.Errorf("%s row %d: %w", OpenOpenersSetting, i+1, err)
		}
	}
	return rows, nil
}

func validOpeners(v any) error {
	_, err := parseOpenerRows(v.(string))
	return err
}

var schemeRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]+):`)
var pathStarts = []string{"/", "./", "../", "~/"}

// ReadOpenTarget reads text as a target; r says which nouns exist.
func (r *Runtime) ReadOpenTarget(text string) OpenTarget {
	text = strings.TrimSpace(text)
	t := OpenTarget{Text: text, Kind: "text", Arg: text}
	if m := schemeRE.FindStringSubmatch(text); m != nil {
		after := text[len(m[0]):]
		if r.Noun(m[1]) != nil && after != "" && !strings.HasPrefix(after, "//") {
			t.Kind, t.Noun, t.ID = "noun", r.CanonicalNoun(m[1]), after
		} else {
			t.Kind, t.Scheme = "scheme", strings.ToLower(m[1])
		}
	}
	if t.Noun == "" && t.Scheme == "" {
		t.Path = text == "~" || slices.ContainsFunc(pathStarts, func(p string) bool { return strings.HasPrefix(text, p) })
	}
	if t.Path {
		t.Kind = "path"
		if home, err := os.UserHomeDir(); err == nil && (text == "~" || strings.HasPrefix(text, "~/")) {
			t.Arg = filepath.Join(home, strings.TrimPrefix(text, "~"))
		}
	}
	if t.Noun == "" {
		name := text
		if i := strings.IndexAny(name, "?#"); i >= 0 && t.Scheme != "" {
			name = name[:i]
		}
		t.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	}
	return t
}

// openVerb is the first of the space separated verbs the noun has.
func (r *Runtime) openVerb(noun, verbs string) (string, bool) {
	for _, v := range strings.Fields(verbs) {
		if _, ok := r.Operation(string(KeyOf(noun, v))); ok {
			return v, true
		}
	}
	return "", false
}

// OpenerRows are the rows in force: the user's (open.openers) then the
// built-in ones. A user value that does not parse is ignored with the
// error returned beside the built-in rows.
func (r *Runtime) OpenerRows() ([]OpenerRow, error) {
	user, err := parseOpenerRows(r.Settings().String(OpenOpenersSetting))
	return append(user, builtinOpeners()...), err
}

// ResolveOpen reads text and says what opening it would do, without doing
// it. Rows are tried by specificity (a noun's own row, any noun, scheme,
// extension, prefix), the user's before the built-in ones within a level.
// Text no row matches follows open.unmatched. Empty text resolves to
// OpenActionNone; the error is only a malformed open.openers value, and
// the built-in rows still resolve then.
func (r *Runtime) ResolveOpen(text string) (OpenTarget, OpenAction, error) {
	t := r.ReadOpenTarget(text)
	rows, err := r.OpenerRows()
	if t.Text == "" {
		return t, OpenAction{Kind: OpenActionNone}, err
	}
	best, bestLevel := -1, len(openRowKinds)
	for i, row := range rows {
		level := row.level()
		if level < 0 || level >= bestLevel || !openRowKinds[level].match(row, t, r.CanonicalNoun) {
			continue
		}
		if usable := openActionDefs[row.Action].usable; usable != nil && !usable(r, row, t) {
			continue
		}
		best, bestLevel = i, level
	}
	if best < 0 {
		unmatched := OpenAction{Kind: OpenActionNone, Unmatched: true}
		viaOS := OpenAction{Kind: OpenActionOS, Arg: t.Arg, Unmatched: true}
		return t, map[string]OpenAction{OpenUnmatchedOS: viaOS, OpenUnmatchedNone: unmatched}[r.Settings().String(OpenUnmatchedSetting)], err
	}
	row := rows[best]
	a := OpenAction{Kind: row.Action, Row: &row}
	openActionDefs[row.Action].resolve(r, row, t, &a)
	return t, a, err
}

// Open resolves text and does what the table says: the OS opener
// (Runtime.Opener), a page, or a verb request. It returns the action
// taken. Text nothing opens is not an error (OpenActionNone).
func (r *Runtime) Open(text string) (OpenAction, error) {
	// A malformed open.openers value is reported by ResolveOpen; the
	// built-in rows still act.
	_, a, _ := r.ResolveOpen(text)
	return a, openActionDefs[a.Kind].run(r, a)
}
