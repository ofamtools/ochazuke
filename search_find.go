package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Text find: the jump search of text-like components (UX-RULES R5.19),
// Vim's / and n/N (:help /, :help n). On a list-like component / is the
// list filter (R5.6), which hides rows; on a text-like one (the text view,
// the docs viewer, the Markdown viewer, a timeline) / opens the find line
// and enter moves the component's cursor to the next match, the text
// staying whole. n repeats the last find in its direction, N the other
// way, with a count. The query follows search.case (R5.17) and the find
// line's match mode starts at search.match (R5.18); tab cycles it there.
//
// A component takes part by implementing TextFinder; the rows binding /,
// n and N to search.find and search.next are data on its noun
// (bindings/search.json), so an application's own component gets the same
// keys with three rows of its own.

// TextFinder is a text-like component the jump search moves through: an
// input (the text view), the docs viewer, a Markdown Doc, a timeline.
// search.find and search.next run on the innermost one on the focus path.
type TextFinder interface {
	// FindText moves the component's cursor to the next place after it
	// (before it with back) whose text m matches, wrapping once around the
	// text, and reports whether there was one. A component whose cursor is
	// a row (a Markdown line, a card) finds rows; an input finds the
	// match's first character.
	FindText(m Matcher, back bool) bool
}

// The find line and its noun.
const (
	// FindLineNoun is the find line's noun: the one-line input / opens,
	// pinned to insert (enter finds, esc closes, tab cycles the match).
	FindLineNoun = "ochazuke.find-line"
	// findLineID is the find line's modal; findLineInputID its input.
	findLineID      = "find-line"
	findLineInputID = "__find-line/text"
)

// findState is the last find: what n and N repeat (Vim's last search
// pattern and direction, one for the whole runtime).
type findState struct {
	query string
	back  bool
	mode  string
}

// LastFind is the last search.find's query and direction ("" before the
// first).
func (r *Runtime) LastFind() (query string, back bool) {
	return r.lastFind.query, r.lastFind.back
}

// textFinderAt is the innermost TextFinder on focus's path, or nil.
func (r *Runtime) textFinderAt(focus string) TextFinder {
	for _, id := range r.Ancestry(focus) {
		if inst := r.Instances[id]; inst != nil {
			if f, ok := inst.Component.(TextFinder); ok {
				return f
			}
		}
	}
	return nil
}

// FindText runs a find at focus: query compiled in mode (search.match for
// "") with case as search.case says, count times from the cursor, down
// or up with back. It becomes the last find even when nothing matches,
// as in Vim.
func (r *Runtime) FindText(focus, query, mode string, back bool, count int) error {
	f := r.textFinderAt(focus)
	if f == nil {
		return errors.New("nothing to search here (a text view, the docs viewer, a Markdown viewer or a timeline)")
	}
	if mode == "" {
		mode = r.SearchMatch()
	}
	if strings.TrimSpace(query) == "" {
		return errors.New("no search pattern")
	}
	m := r.Matcher(mode, query)
	if err := m.Err(); err != nil {
		return err
	}
	r.lastFind = findState{query: query, back: back, mode: m.Mode()}
	for range max(1, count) {
		if !f.FindText(m, back) {
			return fmt.Errorf("pattern not found: %s", query)
		}
	}
	return nil
}

// findLine is the find line's component: it draws the modal and holds
// the open search's direction, match mode and the reason its query
// matches nothing (an invalid regex), shown under the line.
type findLine struct {
	r    *Runtime
	back bool
	mode string
	err  error
}

// ModalTitle names the line.
func (f *findLine) ModalTitle() string { return "Find" }

// ModalBlock draws / (? going up), the query and, at the right, the match
// mode's tag (search.show-match); an error goes on the line below.
func (f *findLine) ModalBlock(s Styles, width, height int) Block {
	in := f.r.Input(findLineInputID)
	if in == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	prefix := "/"
	if f.back {
		prefix = "?"
	}
	tag := f.r.matchTagFor(f.mode)
	room := max(1, width-len(prefix)-cellWidth(tag)-1)
	field := InputBlock(in, room, 1)
	line := prefix + field.Text
	if tag != "" {
		line += strings.Repeat(" ", max(1, width-cellWidth(tag)-cellWidth(line))) + s.Muted.Render(tag)
	}
	if f.err != nil && height > 1 {
		line += "\n" + s.Error.Render(f.err.Error())
	}
	return field.Offset(len(prefix), 0).WithText(line)
}

// The find line's search is a match search (search.cycle-match, tab).
func (f *findLine) matchMode() string { return f.mode }
func (f *findLine) setMatchMode(mode string) {
	f.mode, f.err = matchOr(mode, MatchSubstring), nil
}
func (f *findLine) matchApplies() bool { return true }

// openFindLine opens the find line over focus, empty, in insert mode.
func (r *Runtime) openFindLine(back bool) error {
	in := NewInput(findLineInputID, "", false)
	inst, err := r.OpenModal(findLineID, Payload{}, WithInput(in, Kind(FindLineNoun)))
	if err != nil {
		return err
	}
	inst.Component = &findLine{r: r, back: back, mode: r.SearchMatch()}
	return nil
}

func registerSearchFind(r *Runtime) {
	r.DefineGuard("text-findable", func(r *Runtime, ev KeyEvent) bool { return r.textFinderAt(ev.Focus) != nil })
	r.DefineGuard("text-refindable", func(r *Runtime, ev KeyEvent) bool {
		return r.lastFind.query != "" && r.textFinderAt(ev.Focus) != nil
	})
	focus := func(c Context) string {
		if f := focusOf(c); f != "" {
			return f
		}
		return r.Focus
	}
	r.Register(Operation{Verb: "find", Noun: "ochazuke.search", Description: "Find text in the focused text view, docs viewer, Markdown viewer or timeline (Vim's /): with no query the find line opens; the cursor jumps to the next match, wrapping, as search.case and search.match say",
		Parameters: map[string]Parameter{
			"query": {Type: "string", Description: "Find this at once instead of opening the find line"},
			"back":  {Type: "boolean", Description: "Search up instead of down (Vim's ?)"},
			"count": {Type: "integer", Description: "Find the count-th match (default 1)"},
		}, Positional: []string{"query"}, Result: "the find", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			at, back := focus(c), boolArg(a, "back")
			query := StringArg(a, "query", "")
			if query == "" {
				if r.textFinderAt(at) == nil {
					return nil, errors.New("nothing to search here (a text view, the docs viewer, a Markdown viewer or a timeline)")
				}
				return nil, r.openFindLine(back)
			}
			if err := r.FindText(at, query, "", back, IntArg(a, "count", 1)); err != nil {
				return nil, err
			}
			return map[string]any{"query": query, "back": back}, nil
		}})
	r.Register(Operation{Verb: "next", Noun: "ochazuke.search", Description: "Repeat the last find (Vim's n), or in the other direction with reverse (N), count times",
		Parameters: map[string]Parameter{
			"reverse": {Type: "boolean", Description: "Search the other way from the last find (N)"},
			"count":   {Type: "integer", Description: "Repeat count (default 1)"},
		}, Result: "the find", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			last := r.lastFind
			if last.query == "" {
				return nil, errors.New("no previous search")
			}
			back := last.back != boolArg(a, "reverse")
			err := r.FindText(focus(c), last.query, last.mode, back, IntArg(a, "count", 1))
			r.lastFind.back = last.back // N keeps the direction n goes, as in Vim
			if err != nil {
				return nil, err
			}
			return map[string]any{"query": last.query, "back": back}, nil
		}})
	guards := r.Nouns["ochazuke.search"].Guards
	if guards == nil {
		guards = map[string]string{}
		r.Nouns["ochazuke.search"].Guards = guards
	}
	guards["find"], guards["next"] = "text-findable", "text-refindable"
	r.Define(FindLineNoun).Pin("insert").
		Verb("commit", func(r *Runtime, _ Context, _ Arguments) (any, error) {
			body := r.Instances[r.TopModal()]
			in := r.Input(findLineInputID)
			if body == nil || body.ID != findLineID || in == nil {
				return nil, errors.New("no find line is open")
			}
			line, _ := body.Component.(*findLine)
			if line == nil {
				return nil, errors.New("no find line is open")
			}
			query := string(in.Text)
			if query == "" {
				// An empty line finds the last query again (Vim's /<CR>).
				query = r.lastFind.query
			}
			// An invalid regex keeps the line open and says why on it
			// (R5.18), never in a notice.
			if m := r.Matcher(line.mode, query); m.Err() != nil {
				line.err = m.Err()
				return nil, nil
			}
			if err := r.Pop(); err != nil {
				return nil, err
			}
			if err := r.FindText(r.Focus, query, line.mode, line.back, 1); err != nil {
				return nil, err
			}
			return map[string]any{"query": query, "back": line.back}, nil
		}, "Find the typed text and close the find line").
		Verb("cancel", func(r *Runtime, _ Context, _ Arguments) (any, error) {
			if r.TopModal() != findLineID {
				return nil, errors.New("no find line is open")
			}
			return nil, r.Pop()
		}, "Close the find line without finding")
}

// Spans are the places in text the query matches, as rune offsets
// [start, end) in order: each match of a regex, each occurrence of every
// word of a substring query (when text holds them all), the run from the
// first to the last matched character of a fuzzy one. A blank query and
// an invalid regex give none.
func (m Matcher) Spans(text string) [][2]int {
	switch {
	case m.err != nil:
		return nil
	case m.re != nil:
		var out [][2]int
		for _, loc := range m.re.FindAllStringIndex(text, -1) {
			out = append(out, [2]int{utf8.RuneCountInString(text[:loc[0]]), utf8.RuneCountInString(text[:loc[1]])})
		}
		return out
	}
	hay := []rune(text)
	for i, ch := range hay {
		hay[i] = m.cm.Rune(ch)
	}
	if m.needle != nil {
		start, k := -1, 0
		for i, ch := range hay {
			if k < len(m.needle) && ch == m.needle[k] {
				if start < 0 {
					start = i
				}
				k++
				if k == len(m.needle) {
					return [][2]int{{start, i + 1}}
				}
			}
		}
		return nil
	}
	if len(m.terms) == 0 {
		return nil
	}
	var out [][2]int
	for _, term := range m.terms {
		needle := []rune(term)
		found := false
		for i := 0; i+len(needle) <= len(hay); i++ {
			if slices.Equal(hay[i:i+len(needle)], needle) {
				out = append(out, [2]int{i, i + len(needle)})
				found = true
			}
		}
		if !found {
			return nil
		}
	}
	slices.SortFunc(out, func(a, b [2]int) int { return a[0] - b[0] })
	return out
}

// nextSpan walks n lines from the cursor (row, col), down or up with
// back, wrapping once, and returns the first match start past the
// cursor: starts lists each line's match starts in order. A col of -1
// makes the cursor's own line count from its start (a viewer without a
// cursor column).
func nextSpan(n, row, col int, back bool, starts func(line int) []int) (int, int, bool) {
	if n <= 0 {
		return 0, 0, false
	}
	row = min(max(0, row), n-1)
	for i := 0; i <= n; i++ {
		line := (row + i) % n
		if back {
			line = ((row-i)%n + n) % n
		}
		at := starts(line)
		if back {
			at = slices.Clone(at)
			slices.Reverse(at)
		}
		for _, s := range at {
			switch {
			case i == 0 && !back && s <= col,
				i == 0 && back && s >= col,
				i == n && !back && s > col,
				i == n && back && s < col:
				continue
			}
			return line, s, true
		}
	}
	return 0, 0, false
}

// FindLine is the row a find whose cursor is a whole row lands on: the
// first of n rows after from (before it with back) that match reports,
// wrapping once around to from itself. from may be -1 (a find down then
// starts at the first row) or n (a find up starts at the last).
func FindLine(n, from int, back bool, match func(row int) bool) (int, bool) {
	if n <= 0 {
		return 0, false
	}
	step := 1
	if back {
		step = -1
	}
	for i := 1; i <= n; i++ {
		row := ((from+i*step)%n + n) % n
		if match(row) {
			return row, true
		}
	}
	return 0, false
}

// FindText moves the input's cursor to the first character of the next
// match (the text view's /).
func (in *Input) FindText(m Matcher, back bool) bool {
	lines := strings.Split(string(in.Text), "\n")
	cursor := max(0, min(in.Cursor, len(in.Text)))
	row := strings.Count(string(in.Text[:cursor]), "\n")
	start, _ := in.lineBounds(cursor)
	row, col, ok := nextSpan(len(lines), row, cursor-start, back, func(line int) []int {
		var out []int
		for _, s := range m.Spans(lines[line]) {
			out = append(out, s[0])
		}
		return out
	})
	if !ok {
		return false
	}
	at := 0
	for i := range row {
		at += utf8.RuneCountInString(lines[i]) + 1
	}
	in.Cursor = at + col
	in.goalColumn = -1
	in.clampCursor()
	return true
}
