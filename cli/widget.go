package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// CatalogWidget is a data-described widget a data page draws above its
// lists, full width, in order: the framework draws it from its kind and
// its data (the JSON answer of Source, or what the host pushes with
// PageView.SetWidget). A widget is not a focus stop.
//
//	{id: "ai/usage", kind: "kv", title: "usage", source: "usage.show", fields: ["skills", "agents"]}
//	{id: "ai/sync", kind: "status", title: "sync", source: "sync.status"}
//	{id: "ai/recent", kind: "table", title: "recent", source: "skill.recent",
//	 columns: [{title: "Name"}, {title: "When"}], fields: ["name", "at"], rows: 3}
//
// Kinds (WidgetKinds):
//   - kv: an object's fields as aligned key  value lines (Fields order when
//     given, else sorted keys), in a titled panel.
//   - status: one line, title  text  [██████░░░░] 60%  state (the bar at most 20 cells): the data is
//     a string (the text) or {text|label|message, state, progress (0..1, or
//     a percentage above 1), done and total (progress done/total)}.
//   - table: an array of rows in Columns (ochazuke.TableText), at most Rows
//     rows (default DefaultWidgetRows), in a titled panel; a row's cells are
//     its cells field, else its fields in Fields order (default each
//     column's title in lower case), a scalar one cell.
type CatalogWidget struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Title   string            `json:"title,omitempty"`
	Source  string            `json:"source,omitempty"` // NOUN.VERB whose JSON answer is the data
	Fields  []string          `json:"fields,omitempty"`
	Columns []ochazuke.Column `json:"columns,omitempty"`
	Rows    int               `json:"rows,omitempty"`
}

// The widget kinds (CatalogWidget.Kind).
const (
	WidgetKV     = "kv"
	WidgetStatus = "status"
	WidgetTable  = "table"
)

// WidgetKinds lists the kinds a widget may name.
var WidgetKinds = []string{WidgetKV, WidgetStatus, WidgetTable}

// DefaultWidgetRows is a table widget's row cap without Rows.
const DefaultWidgetRows = 5

// widgetBar is the cells of a status widget's progress bar.
const widgetBar = 20

func checkWidget(page string, w CatalogWidget) error {
	if w.ID == "" {
		return fmt.Errorf("page %s: a widget needs an id", page)
	}
	if !slices.Contains(WidgetKinds, w.Kind) {
		return fmt.Errorf("page %s: widget %s: kind %q is not one of %s", page, w.ID, w.Kind, strings.Join(WidgetKinds, ", "))
	}
	if w.Kind == WidgetTable && len(w.Columns) == 0 {
		return fmt.Errorf("page %s: widget %s: a table needs columns", page, w.ID)
	}
	if w.Source != "" {
		if noun, verb, ok := strings.Cut(w.Source, "."); !ok || noun == "" || verb == "" {
			return fmt.Errorf("page %s: widget %s: source %q is not NOUN.VERB", page, w.ID, w.Source)
		}
	}
	return nil
}

// widgetState is one mounted widget's data.
type widgetState struct {
	spec    CatalogWidget
	data    any
	has     bool
	err     string
	loading bool
}

// SetWidget sets a widget's data (any JSON-shaped value) and clears its
// error.
func (v *PageView) SetWidget(id string, data any) error {
	w := v.widget(id)
	if w == nil {
		return fmt.Errorf("page %s has no widget %s", v.spec.ID, id)
	}
	w.data, w.has, w.err = data, true, ""
	return nil
}

func (v *PageView) widget(id string) *widgetState {
	for _, w := range v.widgets {
		if w.spec.ID == id {
			return w
		}
	}
	return nil
}

// reloadWidgets fetches every widget with a source, each a job of the
// page in its own slot.
func (v *PageView) reloadWidgets() error {
	var errs []error
	for _, w := range v.widgets {
		if w.spec.Source == "" {
			continue
		}
		w.loading = true
		noun, verb, _ := strings.Cut(w.spec.Source, ".")
		errs = append(errs, v.launch(v.spec.ID, "widget:"+w.spec.ID, noun, verb, func(out []byte, err error) {
			w.loading = false
			var data any
			if err == nil {
				if err = json.Unmarshal(out, &data); err != nil {
					err = fmt.Errorf("%s: %w", w.spec.Source, err)
				}
			}
			if err != nil {
				w.err = err.Error()
				return
			}
			w.data, w.has, w.err = data, true, ""
		}))
	}
	return errors.Join(errs...)
}

// widgetsBlock draws the widgets one under the other in width, using at
// most half of height, and returns the rows it took.
func (v *PageView) widgetsBlock(layout ochazuke.Layout, width, height int) (ochazuke.Block, int) {
	budget := height / 2
	var parts []string
	used := 0
	for _, w := range v.widgets {
		lines := w.lines(layout.Styles, width)
		h := len(lines)
		if w.spec.Kind != WidgetStatus {
			h += 2 // the panel's border
		}
		if used+h > budget {
			h = budget - used
			if h < 1 || w.spec.Kind != WidgetStatus && h < 3 {
				break
			}
		}
		if w.spec.Kind == WidgetStatus {
			parts = append(parts, lines[0])
		} else {
			parts = append(parts, layout.Panel(w.title(), strings.Join(lines, "\n"), width, h, false))
		}
		used += h
	}
	return ochazuke.Text(strings.Join(parts, "\n")), used
}

func (w *widgetState) title() string {
	if w.spec.Title != "" {
		return w.spec.Title
	}
	return w.spec.ID
}

// lines are the widget's body lines in width (a status widget's one line
// whole, a panel's inside its border).
func (w *widgetState) lines(s ochazuke.Styles, width int) []string {
	cw := width
	if w.spec.Kind != WidgetStatus {
		cw, _ = ochazuke.PanelContentSize(width, 3)
	}
	note := ""
	switch {
	case w.err != "":
		note = w.err
	case !w.has && w.loading:
		note = "loading…"
	case !w.has:
		note = "no data"
	}
	if w.spec.Kind == WidgetStatus {
		text := note
		if note == "" {
			text = statusText(w.data, cw-ansi.StringWidth(w.title())-2)
		} else {
			text = s.Muted.Render(plainText(note))
		}
		return []string{ansi.Truncate(s.Title.Render(w.title())+"  "+text, cw, "…")}
	}
	if note != "" {
		return []string{s.Muted.Render(ansi.Truncate(plainText(note), cw, "…"))}
	}
	var text string
	switch w.spec.Kind {
	case WidgetKV:
		text = kvText(w.data, w.spec.Fields)
	case WidgetTable:
		text = strings.Join(tableWidget(w.spec, w.data, cw), "\n")
	}
	if text == "" {
		return []string{s.Muted.Render("no data")}
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, cw, "…")
	}
	return lines
}

// kvText is a kv widget's body.
func kvText(data any, fields []string) string {
	obj, ok := data.(map[string]any)
	if !ok {
		return valueText(data)
	}
	var keys []string
	for _, k := range fields {
		if _, ok := obj[k]; ok {
			keys = append(keys, k)
		}
	}
	if len(fields) == 0 {
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	return keyValues(keys, func(k string) string { return valueText(obj[k]) })
}

// statusText is a status widget's line after its title, in width.
func statusText(data any, width int) string {
	obj, ok := data.(map[string]any)
	if !ok {
		return plainText(valueText(data))
	}
	var parts []string
	for _, k := range []string{"text", "label", "message"} {
		if s, ok := obj[k].(string); ok && s != "" {
			parts = append(parts, plainText(s))
			break
		}
	}
	progress, has := -1.0, false
	if p, ok := obj["progress"].(float64); ok {
		progress, has = p, true
		if progress > 1 {
			progress /= 100
		}
	} else if done, ok := obj["done"].(float64); ok {
		if total, ok := obj["total"].(float64); ok && total > 0 {
			progress, has = done/total, true
		}
	}
	if has {
		progress = math.Min(1, math.Max(0, progress))
		bar := min(widgetBar, max(4, width/3))
		fill := int(math.Round(progress * float64(bar)))
		parts = append(parts, fmt.Sprintf("[%s%s] %d%%", strings.Repeat("█", fill), strings.Repeat("░", bar-fill), int(math.Round(progress*100))))
	}
	if s, ok := obj["state"].(string); ok && s != "" {
		parts = append(parts, plainText(s))
	}
	return strings.Join(parts, "  ")
}

// tableWidget is a table widget's lines: the header and up to Rows rows.
func tableWidget(spec CatalogWidget, data any, width int) []string {
	items, _ := data.([]any)
	limit := spec.Rows
	if limit <= 0 {
		limit = DefaultWidgetRows
	}
	fields := slices.Clone(spec.Fields)
	for i := len(fields); i < len(spec.Columns); i++ {
		fields = append(fields, strings.ToLower(spec.Columns[i].Title))
	}
	rows := make([][]string, 0, min(limit, len(items)))
	for _, item := range items {
		if len(rows) == limit {
			break
		}
		obj, ok := item.(map[string]any)
		if !ok {
			rows = append(rows, []string{plainText(valueText(item))})
			continue
		}
		if cells, ok := obj["cells"].([]any); ok {
			row := make([]string, len(cells))
			for i, c := range cells {
				if c != nil {
					row[i] = plainText(fmt.Sprint(c))
				}
			}
			rows = append(rows, row)
			continue
		}
		row := make([]string, len(spec.Columns))
		for i := range spec.Columns {
			if value, ok := obj[fields[i]]; ok && value != nil {
				row[i] = valueText(value)
			}
		}
		rows = append(rows, row)
	}
	lines := ochazuke.TableText(spec.Columns, rows, width)
	if more := len(items) - len(rows); more > 0 {
		lines = append(lines, fmt.Sprintf("… %d more", more))
	}
	return lines
}
