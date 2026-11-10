package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The board is the palette's second style (palette.style board): the same
// line, index, parse, arguments and run path as the classic style, seen as
// a board of cells while the first two words are chosen. At the first word
// the cells are the nouns (each with its verb count) then the verbs (each
// with its noun count); typing narrows them by prefix, the shortest prefix
// that picks a cell is emphasised on it, and the one cell left commits
// itself. After a noun only its verbs are cells, after a verb only the
// nouns that have it. From the third word on the classic list shows the
// parameter's choices. The line stays the source of truth: the board
// holds only the reading chosen for the first word (chip) and the grid
// width it was last drawn at (cols).

// boardLayer is the word the board is choosing (0 the first, 1 the second)
// and the part of it typed so far; ok is false from the third word on, and
// in the classic style.
func (p *Palette) boardLayer() (layer int, partial string, ok bool) {
	if !p.styleBoard() || p.line == nil {
		return 0, "", false
	}
	tokens, trailing := tokenize(p.Text())
	tokens = p.index().nounRuns(p.r, tokens)
	layer = len(tokens)
	if !trailing {
		layer--
		partial = tokens[len(tokens)-1].Text
	}
	return layer, partial, layer < 2
}

// boardFilter narrows the cells to those partial spells: a prefix of the
// word (or of an alias word) first, then a prefix of the last part of a
// dotted word (typing after the dot: doc for json.doc); nouns, then verbs,
// then the rest, each group in its order. A fuzzy or infix hit is never a
// cell.
func boardFilter(entries []PaletteEntry, partial string) []PaletteEntry {
	var prefix, tail []PaletteEntry
	for _, e := range entries {
		switch {
		case partial == "" || hasPrefixFold(e.Value, partial) || slices.ContainsFunc(e.Aliases, func(a string) bool { return hasPrefixFold(a, partial) }):
			prefix = append(prefix, e)
		case !strings.Contains(partial, ".") && strings.Contains(e.Value, ".") && hasPrefixFold(e.Value[strings.LastIndex(e.Value, ".")+1:], partial):
			tail = append(tail, e)
		}
	}
	out := append(prefix, tail...)
	rank := func(e PaletteEntry) int {
		switch {
		case len(e.Verbs) > 0:
			return 0 // a noun
		case len(e.Nouns) > 0:
			return 1 // a verb
		}
		return 2
	}
	slices.SortStableFunc(out, func(a, b PaletteEntry) int { return rank(a) - rank(b) })
	return out
}

// chipFits reports whether first is the word the board committed (as the
// noun or the verb its chip says).
func (p *Palette) chipFits(first string) bool {
	return p.chip.Value != "" && strings.EqualFold(p.chip.Value, first)
}

// uniquePrefix is how many runes of word are enough to pick it among the
// words shown: the shortest prefix no other (different) word starts with,
// compared without case. A word that is a prefix of another needs itself
// whole, and so does a word shown twice (a noun and a verb of one spelling,
// told apart by the section). The prefix is over the whole displayed word,
// so git.repo and git.branch need git.r and git.b; the dot is no boundary
// (typing after it works too, boardFilter).
func uniquePrefix(word string, words []string) int {
	runes := []rune(strings.ToLower(word))
	need := 1
	for _, other := range words {
		o := []rune(strings.ToLower(other))
		if string(o) == string(runes) {
			continue
		}
		common := 0
		for common < len(runes) && common < len(o) && runes[common] == o[common] {
			common++
		}
		need = max(need, common+1)
	}
	return min(need, len(runes))
}

// boardRows are the grid's rows as indexes into the cells: the nouns,
// then the verbs (then the rest), each kind starting a row, cols to a row.
func boardRows(cells []PaletteEntry, cols int) [][]int {
	var rows [][]int
	cols = max(1, cols)
	for i := 0; i < len(cells); i++ {
		if i == 0 || cells[i].Kind != cells[i-1].Kind || len(rows[len(rows)-1]) == cols {
			rows = append(rows, nil)
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], i)
	}
	return rows
}

// The board's cell moves, in the order of the cell verb's "to" values.
const (
	cellLeft = iota
	cellRight
	cellUp
	cellDown
	cellFirst
	cellLast
	cellFirstCol
	cellLastCol
)

// cellMoves are the cell verb's "to" values, indexed by the constants above.
var cellMoves = []string{"left", "right", "up", "down", "first", "last", "first-col", "last-col"}

// cellTo moves the board's current cell: left, right, up, down, or to an
// edge (first, last, first-col, last-col). From no cell it lands on the
// first (or the last for up and left).
func (p *Palette) cellTo(to string) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	move := slices.Index(cellMoves, to)
	if move < 0 {
		return fmt.Errorf("unknown cell move %q", to)
	}
	cells := p.completions()
	if len(cells) == 0 {
		return errors.New("no cells for this word")
	}
	rows := boardRows(cells, p.boardCols())
	at := p.highlight
	if _, ok := p.highlighted(cells); !ok {
		at = -1
	}
	row, col := 0, 0
	for r, idx := range rows {
		if c := slices.Index(idx, at); c >= 0 {
			row, col = r, c
		}
	}
	p.highlight, p.marked, p.message = cellTarget(move, at, row, col, rows, len(cells)), p.Text(), ""
	return nil
}

// cellTarget is the cell a move lands on from cell at (row and col in
// rows; at -1 for none) among n cells.
func cellTarget(move, at, row, col int, rows [][]int, n int) int {
	backward := move == cellLeft || move == cellUp
	switch {
	case move == cellFirst:
		return 0
	case move == cellLast:
		return n - 1
	case move == cellFirstCol:
		return rows[row][0]
	case move == cellLastCol:
		return rows[row][len(rows[row])-1]
	case at < 0 && backward:
		return n - 1
	case at < 0:
		return 0
	case move == cellLeft:
		return max(at-1, 0)
	case move == cellRight:
		return min(at+1, n-1)
	}
	r := min(max(row+1-2*boolInt(move == cellUp), 0), len(rows)-1)
	return rows[r][min(col, len(rows[r])-1)]
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// boardCols is the grid's width in cells, as last drawn.
func (p *Palette) boardCols() int {
	if p.cols < 1 {
		return 3
	}
	return p.cols
}

// step is up or down: the board's cell above or below; in the classic style
// the completion highlight when the line has completions to walk, else the
// palette's history (older on up).
func (p *Palette) step(direction string) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if _, _, board := p.boardLayer(); board {
		return p.cellTo(direction)
	}
	delta := 1 - 2*boolInt(direction == cellMoves[cellUp])
	if p.Text() != "" && len(p.completions()) > 0 && p.recall == 0 {
		return p.move(delta)
	}
	return p.recallLine(-delta)
}

// back goes back one layer: the last word when the line ends in a space
// (a committed word), else one character of the word being typed.
func (p *Palette) back() error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	text := p.Text()
	if text == "" {
		return nil
	}
	tokens, trailing := tokenize(text)
	if trailing && len(tokens) > 0 {
		return p.setText(string([]rune(text)[:tokens[len(tokens)-1].Start]))
	}
	at := max(0, min(p.line.Cursor, len(p.line.Text)))
	if at == 0 {
		return nil
	}
	if err := p.line.Replace(at-1, at, ""); err != nil {
		return err
	}
	p.sync()
	return nil
}

// up drops the last word of the line, or the part of it being typed, and
// closes the palette from an empty line: esc goes up one layer.
func (p *Palette) up() error {
	text := p.Text()
	if text == "" {
		return p.close()
	}
	tokens, _ := tokenize(text)
	return p.setText(string([]rune(text)[:tokens[len(tokens)-1].Start]))
}

// reopenWord takes a committed first word back when the letter just typed
// at the second word fits no cell but continues the first word as it was
// being typed (quad typed as qua, committed, then d): the line goes back to
// the first word with the letter, which may commit again, so a whole word
// typed out reads as the word.
func (p *Palette) reopenWord() {
	layer, partial, ok := p.boardLayer()
	if !ok || layer != 1 || partial == "" || p.chip.Value == "" || p.typedFrom == "" || p.committing {
		return
	}
	joined := p.typedFrom + partial
	tokens, _ := tokenize(p.Text())
	if len(tokens) < 2 || !strings.EqualFold(tokens[0].Text, p.chip.Value) || len(p.completions()) > 0 || !hasPrefixFold(p.chip.Value, joined) {
		return
	}
	p.committing = true
	err := p.setText(joined)
	p.committing = false
	p.chip, p.typedFrom = PaletteEntry{}, ""
	if err != nil {
		p.message = err.Error()
	}
}

// autoCommit takes the one cell the typed letters leave, as a key on it
// would: the word is committed with its space, and the next layer begins.
// The pair that results runs when nothing more is asked of it.
func (p *Palette) autoCommit() {
	layer, partial, ok := p.boardLayer()
	if !ok || partial == "" || p.committing {
		return
	}
	cells := p.completions()
	if len(cells) != 1 || cells[0].Splice {
		return
	}
	p.committing = true
	defer func() { p.committing = false }()
	if err := p.takeCell(cells[0], layer); err != nil {
		p.message = err.Error()
	}
}

// takeCell commits a cell as the word of its layer; the second word runs
// the line when it asks for nothing more.
func (p *Palette) takeCell(entry PaletteEntry, layer int) error {
	if layer == 0 {
		_, typed, _ := p.boardLayer()
		p.chip, p.typedFrom = entry, typed
	}
	if err := p.appendWord(entry.Value); err != nil {
		return err
	}
	if layer == 1 {
		if c := p.parse(p.Text()); c.Err == nil && c.known() && !slices.ContainsFunc(remainingParams(c), func(name string) bool { return c.Op.lacks(name, c.Args) }) {
			// Nothing required is left to type: run, which asks for a
			// target nothing focused supplies (the fill-in form).
			return p.run()
		}
	}
	return nil
}

// boardHelp describes the current cell in a line: a noun with its verbs, a
// verb with its nouns, or the command a second word completes.
func (p *Palette) boardHelp(ix lineIndex, cell PaletteEntry, layer int, first string) string {
	list := func(words []string) string {
		shown := slices.Clone(words)
		more := ""
		if len(shown) > 8 {
			shown, more = shown[:8], " …"
		}
		return strings.Join(shown, " ") + more
	}
	if layer == 1 {
		noun, verb := first, cell.Verb
		if len(cell.Verbs) > 0 {
			noun, verb = cell.Value, first
		}
		op, _ := ix.op(noun, verb)
		out := noun + " " + verb
		if op.Description != "" {
			out += ": " + op.Description
		}
		if params := paramOrder(op); len(params) > 0 {
			out += "   args: " + strings.Join(params, " ")
		}
		if cell.Reason != "" {
			out += "   unavailable: " + cell.Reason
		}
		return out
	}
	switch {
	case len(cell.Verbs) > 0:
		return fmt.Sprintf("%s is a %s with %d verbs: %s", cell.Value, cell.Kind, len(cell.Verbs), list(cell.Verbs))
	case len(cell.Nouns) > 0:
		return fmt.Sprintf("%s is a %s with %d nouns: %s", cell.Value, cell.Kind, len(cell.Nouns), list(cell.Nouns))
	}
	return cell.Label
}

// boardView draws the board for the first two words: the cells in rows,
// the focused cell filled across its width, and the help line.
func (p *Palette) boardView(s Styles, width, height int, layer int, partial string) string {
	cells := p.completions()
	ix := p.index()
	tokens, _ := tokenize(p.Text())
	first := ""
	if layer == 1 && len(tokens) > 0 {
		first = tokens[0].Text
	}
	words := make([]string, len(cells))
	label := 0
	for i, c := range cells {
		words[i] = c.Value
		label = max(label, ansi.StringWidth(c.Value))
	}
	count := func(c PaletteEntry) string {
		if layer == 1 || len(c.Verbs)+len(c.Nouns) == 0 {
			return ""
		}
		return fmt.Sprint(len(c.Verbs) + len(c.Nouns))
	}
	cellW := min(max(label+4, 12), max(12, width-2))
	p.cols = max(1, (width-1)/cellW)
	current := p.highlight
	if _, ok := p.highlighted(cells); !ok {
		current = 0
	}
	focus, hasFocus := s.chromeLook("palette.focus")
	focusStyle := lipgloss.NewStyle().Reverse(true).Bold(true)
	if hasFocus {
		focusStyle = focus.over(lipgloss.NewStyle())
	}
	prefixLook, hasPrefix := s.chromeLook("palette.prefix")
	hintLook, hasHint := s.chromeLook("palette.resolve")
	var out strings.Builder
	rows := boardRows(cells, p.cols)
	// Sections (a kind each) share the height: every one keeps its heading
	// and at least two rows; the one holding the current cell keeps it in
	// view.
	var sections [][]int // rows (indexes into rows) of each section
	for r, idx := range rows {
		if r == 0 || cells[idx[0]].Kind != cells[rows[r-1][0]].Kind {
			sections = append(sections, nil)
		}
		sections[len(sections)-1] = append(sections[len(sections)-1], r)
	}
	budget := max(2, (max(3, height-6)-len(sections))/max(1, len(sections)))
	hidden := 0
	for _, section := range sections {
		k := cells[rows[section[0]][0]].Kind
		n := 0
		for _, r := range section {
			n += len(rows[r])
		}
		heading := k + "s"
		if layer == 1 {
			heading += " of " + first
			if len(cells[rows[section[0]][0]].Verbs) > 0 {
				heading = k + "s that have " + first
			}
		}
		out.WriteString(s.chromeRender("palette.heading", fmt.Sprintf(" %s (%d)", heading, n)) + "\n")
		start := 0
		for at, r := range section {
			if slices.Contains(rows[r], current) {
				start = max(0, at-budget+1)
			}
		}
		end := min(len(section), start+budget)
		hidden += len(section) - end
		for _, r := range section[start:end] {
			for _, i := range rows[r] {
				c := cells[i]
				k := uniquePrefix(c.Value, words)
				runes := []rune(c.Value)
				head, rest := string(runes[:min(k, len(runes))]), string(runes[min(k, len(runes)):])
				n := count(c)
				pad := max(1, cellW-2-ansi.StringWidth(c.Value)-ansi.StringWidth(n))
				base, emph, dim := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true).Underline(true), lipgloss.NewStyle().Faint(true)
				if hasPrefix {
					emph = prefixLook.over(emph)
				}
				if hasHint {
					dim = hintLook.over(lipgloss.NewStyle())
				}
				if i == current {
					base, emph, dim = focusStyle, focusStyle.Underline(true), focusStyle
				}
				out.WriteString(base.Render(" ") + emph.Render(head) + base.Render(rest+strings.Repeat(" ", pad)) + dim.Render(n) + base.Render(" "))
			}
			out.WriteString("\n")
		}
	}
	if len(cells) == 0 {
		fmt.Fprintf(&out, "  nothing starts with %q\n", partial)
	}
	if hidden > 0 {
		fmt.Fprintf(&out, "  (%d more rows: arrows move, letters narrow)\n", hidden)
	}
	if len(cells) > 0 {
		help := p.boardHelp(ix, cells[current], layer, first)
		out.WriteString(" " + ansi.Truncate(help, max(8, width-2), "…") + "\n")
	}
	return out.String()
}

// boardBlock is the board under the line (its error, then the cells and the
// help line) while the first two words are chosen; ok is false otherwise.
func (p *Palette) boardBlock(s Styles, width, height int) (string, bool) {
	layer, partial, ok := p.boardLayer()
	if !ok {
		return "", false
	}
	out := ""
	if p.message != "" {
		out = fmt.Sprintf("Error: %s\n", p.message)
	}
	return out + p.boardView(s, width, height, layer, partial), true
}

// boardWords are the entries the board lists after a first word it
// committed as a noun (its verbs and props) or as a verb (its nouns);
// ok is false when first is not that word.
func (p *Palette) boardWords(ix lineIndex, first string, verbs func([]string, string), props func(string), nouns func([]string)) bool {
	if !p.chipFits(first) {
		return false
	}
	if len(p.chip.Verbs) > 0 {
		verbs(p.chip.Verbs, p.chip.Value)
		props(p.chip.Value)
	} else {
		nouns(p.chip.Nouns)
	}
	return true
}

// boardCells applies the board's own narrowing and kind filter to the
// entries of the first two words.
func (p *Palette) boardCells(out []PaletteEntry, kind, partial string) []PaletteEntry {
	if kind != "" {
		out = slices.DeleteFunc(out, func(e PaletteEntry) bool { return e.Kind != kind })
	}
	return boardFilter(out, partial)
}
