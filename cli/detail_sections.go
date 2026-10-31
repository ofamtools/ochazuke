package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/markdown"
)

// CatalogDetailSection is one ordered part of a read-only catalog detail.
// Field paths use dots for nested objects (an exact key wins). Markdown
// reads a string; Heading styles it as a heading. Thread reads an array,
// ordered by At oldest first (ties and undated entries retain input order).
// Its Author, At and Body paths are relative to each entry and default to
// author, at and body. Fields reads labelled values from the selected row.
// Region is main (default) or side. ReadOnly defaults true; false is
// refused: domain edits belong to the application's verbs.
type CatalogDetailSection struct {
	Kind     string               `json:"kind"`
	Region   string               `json:"region,omitempty"`
	Title    string               `json:"title,omitempty"`
	Field    string               `json:"field,omitempty"`
	Heading  bool                 `json:"heading,omitempty"`
	ReadOnly *bool                `json:"readOnly,omitempty"`
	Author   string               `json:"author,omitempty"`
	At       string               `json:"at,omitempty"`
	Body     string               `json:"body,omitempty"`
	Fields   []CatalogDetailField `json:"fields,omitempty"`
}

// CatalogDetailField is labelled metadata. Format is text (default),
// badge, relative-time, time, short-id, path, list or explicit json.
// Display is shortened; detail.fields opens the full values for show/copy.
type CatalogDetailField struct {
	Label  string `json:"label"`
	Field  string `json:"field"`
	Format string `json:"format,omitempty"`
}

func checkDetailSections(d CatalogDetail) error {
	if d.SideWidth < 0 {
		return fmt.Errorf("sideWidth must not be negative")
	}
	if d.Padding != nil && *d.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	for i, s := range d.Sections {
		bad := func(why string) error { return fmt.Errorf("section %d: %s", i+1, why) }
		if s.Region != "" && s.Region != "main" && s.Region != "side" {
			return bad("region must be main or side")
		}
		if s.ReadOnly != nil && !*s.ReadOnly {
			return bad("catalog sections are read-only; use application verbs to edit")
		}
		switch s.Kind {
		case "markdown", "thread":
			if s.Field == "" {
				return bad("markdown and thread need a field")
			}
		case "fields":
			if len(s.Fields) == 0 {
				return bad("fields needs labelled fields")
			}
		default:
			return bad("kind must be markdown, thread or fields")
		}
		for _, f := range s.Fields {
			if f.Label == "" || f.Field == "" {
				return bad("metadata needs a label and field")
			}
			if !slices.Contains([]string{"", "text", "badge", "relative-time", "time", "short-id", "path", "list", "json"}, f.Format) {
				return bad("unknown field format " + f.Format)
			}
		}
	}
	return nil
}

// detailValue follows a path in JSON data without interpreting its content.
func detailValue(data any, path string) any {
	if obj, ok := data.(map[string]any); ok {
		if v, ok := obj[path]; ok {
			return v
		}
		key, rest, more := strings.Cut(path, ".")
		if more {
			return detailValue(obj[key], rest)
		}
		return obj[key]
	}
	return nil
}

// humanDetail is deliberately not a JSON fallback: structured metadata is
// human-readable unless the catalog explicitly asks for the json format.
func humanDetail(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = humanDetail(item)
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+": "+humanDetail(v[k]))
		}
		return strings.Join(parts, "; ")
	default:
		return fmt.Sprint(value)
	}
}

func detailTime(value any) (time.Time, bool) {
	if t, ok := value.(time.Time); ok {
		return t, !t.IsZero()
	}
	t, err := time.Parse(time.RFC3339Nano, humanDetail(value))
	return t, err == nil && !t.IsZero()
}

func relativeDetail(t, now time.Time) string {
	d := now.Sub(t)
	future := d < 0
	if future {
		d = -d
	}
	if d < time.Minute {
		return "now"
	}
	n, unit := int(d/time.Minute), "minute"
	for _, span := range []struct {
		d time.Duration
		u string
	}{{24 * time.Hour, "day"}, {time.Hour, "hour"}} {
		if d >= span.d {
			n, unit = int(d/span.d), span.u
			break
		}
	}
	if n != 1 {
		unit += "s"
	}
	if future {
		return fmt.Sprintf("in %d %s", n, unit)
	}
	return fmt.Sprintf("%d %s ago", n, unit)
}

// full is the value copied/expanded, display its compact presentation.
func formatDetail(value any, format string, now time.Time) (display, full string) {
	full = humanDetail(value)
	display = full
	switch format {
	case "json":
		b, _ := json.MarshalIndent(value, "", "  ")
		full, display = string(b), string(b)
	case "badge":
		if full != "" {
			display = "[" + full + "]"
		}
	case "short-id":
		display = ansi.Truncate(full, 8, "")
	case "relative-time", "time":
		if t, ok := detailTime(value); ok {
			full = t.Format(time.RFC3339Nano)
			display = t.Format("2006-01-02 15:04 MST")
			if format == "relative-time" {
				display = relativeDetail(t, now)
			}
		}
	case "path":
		// Paths are display-only: never stat, expand ~, or read the file.
		display = full
	}
	return display, full
}

func (v *PageView) sideVisible() bool {
	d := v.spec.Detail
	if d.SideSetting != "" {
		if value, ok := v.m.Runtime.Settings().Value(d.SideSetting); ok {
			if enabled, ok := value.(bool); ok {
				return enabled
			}
		}
	}
	if d.Side != nil {
		return *d.Side
	}
	return v.m.Runtime.Settings().Bool("detail.side")
}

func (v *PageView) sectionPadding(width int) int {
	padding := v.m.Runtime.Settings().Int("detail.padding")
	if v.spec.Detail.Padding != nil {
		padding = *v.spec.Detail.Padding
	}
	// Always leave room for at least one content cell.
	return min(max(0, padding), max(0, (width-1)/4))
}

func insetDetail(text string, cells int) string {
	if text == "" || cells == 0 {
		return text
	}
	prefix := strings.Repeat(" ", cells)
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
}

func (v *PageView) sectionText(s CatalogDetailSection, data any, width int) string {
	if width < 1 {
		return ""
	}
	a := *v.m.Appearance
	styles := ochazuke.NewStyles(a)
	var text string
	switch s.Kind {
	case "markdown":
		text, _ = detailValue(data, s.Field).(string)
		if s.Heading && text != "" {
			text = "# " + text
		}
		text = markdown.Block(text, width, a, markdown.WrapAll()).Text
	case "thread":
		entries, _ := detailValue(data, s.Field).([]any)
		entries = slices.Clone(entries)
		author, at, body := s.Author, s.At, s.Body
		if author == "" {
			author = "author"
		}
		if at == "" {
			at = "at"
		}
		if body == "" {
			body = "body"
		}
		// Missing dates stay at the beginning, in input order; dated
		// entries sort stably, so the newest answer is always last.
		slices.SortStableFunc(entries, func(a, b any) int {
			ta, _ := detailTime(detailValue(a, at))
			tb, _ := detailTime(detailValue(b, at))
			return ta.Compare(tb)
		})
		var parts []string
		for _, e := range entries {
			label := humanDetail(detailValue(e, author))
			stamp, _ := formatDetail(detailValue(e, at), "time", v.m.Now())
			if stamp != "" {
				if label != "" {
					label += " · "
				}
				label += stamp
			}
			var lines []string
			if label != "" {
				lines = append(lines, styles.Muted.Render(ansi.Hardwrap(ansi.Wrap(plainText(label), width, ""), width, true)))
			}
			content, _ := detailValue(e, body).(string)
			indent := 2 * v.sectionPadding(width)
			if indent > 0 && label != "" {
				lines = append(lines, "")
			}
			lines = append(lines, insetDetail(markdown.Block(content, width-indent, a, markdown.WrapAll()).Text, indent))
			parts = append(parts, strings.Join(lines, "\n"))
		}
		text = strings.Join(parts, "\n\n")
	case "fields":
		var lines []string
		for _, f := range s.Fields {
			value := detailValue(data, f.Field)
			if value == nil {
				continue
			}
			display, _ := formatDetail(value, f.Format, v.m.Now())
			label := plainText(f.Label) + ": "
			// Keep a label readable even in a very narrow side region.
			line := label + plainText(strings.ReplaceAll(display, "\n", " "))
			lines = append(lines, styles.Muted.Render(ansi.Truncate(line, width, "…")))
		}
		text = strings.Join(lines, "\n")
	}
	if s.Title != "" {
		title := styles.Title.Render(ansi.Hardwrap(ansi.Wrap(plainText(s.Title), width, ""), width, true))
		if text != "" {
			gap := "\n"
			if v.sectionPadding(width) > 0 {
				gap = "\n\n"
			}
			title += gap + text
		}
		text = title
	}
	return text
}

func (v *PageView) regionText(region string, data any, width int) string {
	padding := 0
	if region == "main" {
		padding = v.sectionPadding(width)
	}
	width -= 4 * padding
	var parts []string
	for _, s := range v.spec.Detail.Sections {
		r := s.Region
		if r == "" {
			r = "main"
		}
		if r == region {
			if text := v.sectionText(s, data, width); text != "" {
				parts = append(parts, text)
			}
		}
	}
	text := strings.Join(parts, "\n\n")
	if text != "" && padding > 0 {
		margin := strings.Repeat("\n", padding)
		text = margin + insetDetail(text, 2*padding) + margin
	}
	return text
}

func (v *PageView) sectionsPanel(layout ochazuke.Layout, width, height int) ochazuke.Block {
	cw, ch := ochazuke.PanelContentSize(width, height)
	row, ok := v.rows[v.selectedID()]
	title, text := "", "no row selected"
	if ok {
		title, text = v.detailTitle(row), ""
	}
	if ok && cw > 0 {
		threshold := v.spec.Detail.SideWidth
		if threshold == 0 {
			threshold = v.m.Runtime.Settings().Int("detail.side-width")
		}
		side := v.sideVisible()
		if side && cw >= max(40, threshold) {
			sw := max(16, cw/4)
			mw := cw - sw - 3
			main, meta := v.regionText("main", row.Data, mw), v.regionText("side", row.Data, sw)
			if meta == "" {
				text = v.regionText("main", row.Data, cw)
			} else {
				left, right := strings.Split(main, "\n"), strings.Split(meta, "\n")
				lines := make([]string, max(len(left), len(right)))
				for i := range lines {
					l, r := "", ""
					if i < len(left) {
						l = left[i]
					}
					if i < len(right) {
						r = right[i]
					}
					lines[i] = l + strings.Repeat(" ", max(0, mw-ansi.StringWidth(l))) + layout.Styles.Muted.Render(" │ ") + r
				}
				text = strings.Join(lines, "\n")
			}
		} else {
			text = v.regionText("main", row.Data, cw)
			if side {
				if meta := v.regionText("side", row.Data, cw); meta != "" {
					if text != "" {
						text += "\n\n"
					}
					text += meta
				}
			}
		}
	}
	// Already wrapped at each region's width. One offset scrolls the whole
	// composed document, with no height allocation that can hide the answer.
	body, marker := v.detail.Window(text, 0, ch)
	if title == "" {
		title = "detail"
	}
	if marker != "" {
		title += " · " + marker
	}
	return layout.PanelBlock(title, ochazuke.Text(body), width, height, false)
}
