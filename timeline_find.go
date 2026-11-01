package ochazuke

import "strings"

// SearchOpenFoldsSetting opens a closed timeline card a find lands on by
// its body (settings/ochazuke.json), as Vim's foldopen holds search.
const SearchOpenFoldsSetting = "search.open-folds"

// findText is the card's text a find reads: its head (label, header,
// meta) and its body (Body, then each section's and step's title, summary
// and text) at the card's width, whether or not it is open.
func (t *Timeline) findText(c *Card) (head, body string) {
	head = strings.Join([]string{c.Label, c.Header, c.Meta}, "\n")
	width := t.innerOf(c)
	var b strings.Builder
	add := func(parts ...string) {
		for _, p := range parts {
			if p != "" {
				b.WriteString(p)
				b.WriteByte('\n')
			}
		}
	}
	text := func(f func(int) string) string {
		if f == nil {
			return ""
		}
		return f(width)
	}
	add(text(c.Body))
	for _, s := range c.Sections {
		add(s.Title, s.Summary, text(s.Body))
	}
	for _, s := range c.Steps {
		add(s.Title, s.Summary, text(s.Body))
	}
	return head, b.String()
}

// FindText selects the next card shown whose text the query matches
// (search.find): the timeline's cursor is a card. From inside a card it
// leaves the card first. A card found by its body opens, unless
// search.open-folds is off.
func (t *Timeline) FindText(m Matcher, back bool) bool {
	items := t.list.items
	from := t.list.selected
	if from < 0 {
		from = -1
		if back {
			from = len(items)
		}
	}
	byBody := false
	row, ok := FindLine(len(items), from, back, func(i int) bool {
		head, body := t.findText(t.shown(i))
		if m.Match(head) {
			byBody = false
			return true
		}
		byBody = m.Match(body)
		return byBody
	})
	if !ok {
		return false
	}
	if t.r.Focus == t.bodyID {
		t.leaveBody()
	}
	id := items[row].ID
	if err := t.list.Select(id); err != nil {
		return false
	}
	if byBody && !t.open[id] && t.r.Settings().Bool(SearchOpenFoldsSetting) {
		_ = t.SetOpen(id, true)
	}
	t.reveal = true
	return true
}
