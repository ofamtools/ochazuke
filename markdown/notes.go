package markdown

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// NoteMax is the most bytes one note's text may hold; a longer note is
// refused, never cut.
const NoteMax = 4096

// noteHead is the start of a new note line: "> **author** date: ".
func noteHead(author, date string, resolved bool) string {
	head := "> **" + author + "**"
	if date != "" {
		head += " " + date
	}
	if resolved {
		head += " ✓"
	}
	return head + ": "
}

// NoteElement is the element index of the note with id (Notes' ID) in
// src, or -1.
func NoteElement(src, id string) int {
	for _, n := range Notes(src) {
		if n.ID == id {
			return n.Element
		}
	}
	return -1
}

// CleanNote is note text as it is written: control characters other than
// line ends dropped (a tab is a space), trailing blanks trimmed; an error
// when it is empty or longer than NoteMax.
func CleanNote(text string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ReplaceAll(text, "\r\n", "\n") {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), " \n")
	out = strings.TrimLeft(out, "\n")
	if strings.TrimSpace(out) == "" {
		return "", errors.New("the note is empty")
	}
	if len(out) > NoteMax {
		return "", fmt.Errorf("the note is %d bytes, more than the %d a note may hold", len(out), NoteMax)
	}
	return out, nil
}

// noteInsertion is where a new note under item goes: after the item's
// last note, else after the item's own lines; and the indent of its line
// (the notes', else the item's text column).
func noteInsertion(els []Element, item int) (at, indent int) {
	if item < 0 || item >= len(els) {
		return 0, 0
	}
	at, indent = els[item].End, els[item].Content
	for k := item + 1; k <= subtreeEnd(els, item); k++ {
		if els[k].Kind == KindNote && els[k].Item == item {
			at, indent = els[k].End, els[k].Indent
		}
	}
	return at, indent
}

// noteLines is a note's text as quote lines indent cells in: the head
// on the first, "> " before every other.
func noteLines(indent int, head, text string) string {
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, l := range lines {
		if i == 0 {
			b.WriteString(pad + head + l)
			continue
		}
		b.WriteString("\n" + pad + strings.TrimRight("> "+l, " "))
	}
	return b.String()
}

// AddNote is src with a note by author (dated date) added under list
// item element item, after its other notes, and where it starts.
func AddNote(src string, item int, author, date, text string) (string, int, error) {
	els := Elements(src)
	if item < 0 || item >= len(els) || !els[item].IsItem() {
		return "", 0, errors.New("notes go under a list item")
	}
	clean, err := CleanNote(text)
	if err != nil {
		return "", 0, err
	}
	at, indent := noteInsertion(els, item)
	line := "\n" + noteLines(indent, noteHead(author, date, false), clean)
	return src[:at] + line + src[at:], at + 1, nil
}

// ReplyNote is src with a note by author added right after note element
// note, in the same quote.
func ReplyNote(src string, note int, author, date, text string) (string, int, error) {
	els := Elements(src)
	if note < 0 || note >= len(els) || els[note].Kind != KindNote {
		return "", 0, errors.New("no such note")
	}
	clean, err := CleanNote(text)
	if err != nil {
		return "", 0, err
	}
	at := els[note].End
	line := "\n" + noteLines(els[note].Indent, noteHead(author, date, false), clean)
	return src[:at] + line + src[at:], at + 1, nil
}

// SetResolved is src with note n marked resolved (a ✓ after its date) or
// open; only the head of its first line changes.
func SetResolved(src string, n Element, resolved bool) (string, error) {
	return SetFlags(src, n, resolved, n.Sent)
}

// SetSent is src with note n marked sent (· sent after its date) or not.
func SetSent(src string, n Element, sent bool) (string, error) {
	return SetFlags(src, n, n.Resolved, sent)
}

// SetFlags rewrites note n's flags, after its date: " ✓" when resolved,
// " · sent" when sent. Only those bytes of its first line change.
func SetFlags(src string, n Element, resolved, sent bool) (string, error) {
	if n.Kind != KindNote || n.Author == "" {
		return "", errors.New("no note selected")
	}
	lineEnd := strings.IndexByte(src[n.Start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(src) - n.Start
	}
	line := src[n.Start : n.Start+lineEnd]
	k := strings.IndexByte(line, '>')
	if k < 0 {
		return "", errors.New("no note selected")
	}
	k++
	if k < len(line) && line[k] == ' ' {
		k++
	}
	m := noteAuthor.FindStringSubmatchIndex(line[k:])
	if m == nil {
		return "", errors.New("no note selected")
	}
	base := n.Start + k
	flags := ""
	if resolved {
		flags += " ✓"
	}
	if sent {
		flags += " · sent"
	}
	at, end := base+m[3]+2, base+m[3]+2 // after **author**
	if m[4] >= 0 {
		at, end = base+m[5], base+m[5]
	}
	if m[6] >= 0 {
		end = base + m[7]
	}
	return src[:at] + flags + src[end:], nil
}

// ItemText is a list item's text: its first line after the marker and box.
func ItemText(src string, e Element) string {
	raw := strings.TrimRight(strings.SplitN(src[e.Start:e.End], "\n", 2)[0], "\r")
	if m := itemMarker.FindStringIndex(raw); m != nil {
		return strings.TrimSpace(raw[m[1]:])
	}
	return strings.TrimSpace(raw)
}

// Note is one note as o-md notes lists it.
type Note struct {
	ID       string `json:"id"`
	Item     string `json:"item"`
	Path     string `json:"path"`
	ItemLine int    `json:"item_line"`
	Line     int    `json:"line"`
	Author   string `json:"author"`
	Date     string `json:"date"`
	Text     string `json:"text"`
	Checked  *bool  `json:"checked"`
	Resolved bool   `json:"resolved"`
	Sent     bool   `json:"sent"`
	// Element is the note's element index (for ReplyNote, SetResolved).
	Element int `json:"-"`
}

// Items are the list items of src (element indexes) in source order.
func Items(els []Element) []int {
	var out []int
	for i, e := range els {
		if e.IsItem() {
			out = append(out, i)
		}
	}
	return out
}

// ItemPath is a list item's place: its ancestor items' texts, outermost
// first, then its own, joined by " › ".
func ItemPath(src string, els []Element, item int) string {
	var parts []string
	for k := item; k >= 0; k = els[k].Item {
		parts = append([]string{ItemText(src, els[k])}, parts...)
		if !els[k].IsItem() {
			break
		}
	}
	return strings.Join(parts, " › ")
}

// Notes lists every note of src. A note's ID is its item's place among
// the list items (from 1) and its own among the item's notes:
// "12.2" is the second note under the twelfth item. It stays the same
// until the file is edited.
func Notes(src string) []Note {
	els := Elements(src)
	ordinal := map[int]int{}
	for k, i := range Items(els) {
		ordinal[i] = k + 1
	}
	count := map[int]int{}
	var out []Note
	for i, e := range els {
		if e.Kind != KindNote || e.Item < 0 {
			continue
		}
		count[e.Item]++
		item := els[e.Item]
		n := Note{
			ID: strconv.Itoa(ordinal[e.Item]) + "." + strconv.Itoa(count[e.Item]), Item: ItemText(src, item), Path: ItemPath(src, els, e.Item),
			ItemLine: item.Line, Line: e.Line, Author: e.Author, Date: e.Date, Text: e.Text, Resolved: e.Resolved, Sent: e.Sent, Element: i,
		}
		if item.Kind == KindTask {
			c := item.Checked
			n.Checked = &c
		}
		out = append(out, n)
	}
	return out
}

// FindItem is the first list item of src whose text holds match (case
// folded); with none, an error naming the closest items.
func FindItem(src, match string) (int, error) {
	els := Elements(src)
	want := strings.ToLower(strings.TrimSpace(match))
	if want == "" {
		return -1, errors.New("--item needs some text to match")
	}
	items := Items(els)
	for _, i := range items {
		if strings.Contains(strings.ToLower(ItemText(src, els[i])), want) {
			return i, nil
		}
	}
	type scored struct {
		text  string
		score int
	}
	var close []scored
	words := strings.Fields(want)
	for _, i := range items {
		text := ItemText(src, els[i])
		low := strings.ToLower(text)
		score := 0
		for _, w := range words {
			if strings.Contains(low, w) {
				score += 2 * len(w)
			}
		}
		score += common(low, want)
		if score > 0 {
			close = append(close, scored{text, score})
		}
	}
	sort.SliceStable(close, func(a, b int) bool { return close[a].score > close[b].score })
	msg := fmt.Sprintf("no list item matches %q", match)
	if len(close) > 0 {
		msg += "; closest:"
		for _, c := range close[:min(5, len(close))] {
			msg += "\n  " + c.text
		}
	}
	return -1, errors.New(msg)
}

// common is the length of the longest run of want's letters that a holds
// in order (a cheap closeness).
func common(a, want string) int {
	best, run, j := 0, 0, 0
	for i := 0; i < len(a) && j < len(want); i++ {
		if a[i] == want[j] {
			run++
			j++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}
