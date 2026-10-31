package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/jsontree"
)

const jsonNodeNoun = "ochazuke.json-node"

//go:embed detail_json.json
var jsonBindings []byte

// jsonSection owns a section's tree and independently scrollable value.
// It lives under the selected-row stand-in, so domain verbs still reach
// the original row without treating a JSON path as the application's id.
type jsonSection struct {
	spec     CatalogDetailSection
	tree     *ochazuke.Tree
	value    *ochazuke.Detail
	record   string
	fallback string
	active   bool
}

func (v *PageView) mountJSONSections() error {
	r := v.m.Runtime
	for i, spec := range v.spec.Detail.Sections {
		if spec.Kind != "json" {
			continue
		}
		if v.jsonParent == "" {
			v.jsonParent = v.detail.ID() + "/row"
			r.Define(detailRowNoun)
			r.Instance(v.jsonParent, ochazuke.Parent(v.detail.ID()), ochazuke.Kind(detailRowNoun)).Component = kindSync{v}
			registerJSONNodes(r)
		}
		s := &jsonSection{spec: spec}
		id := fmt.Sprintf("%s/json/%d", v.jsonParent, i)
		r.Instance(id, ochazuke.Parent(v.jsonParent), ochazuke.Kind("ochazuke.detail")).Data = s
		tree, err := r.AddTree(id+"/tree", nil, ochazuke.Parent(id), ochazuke.Guides(), ochazuke.NoMarks(), ochazuke.Columns(
			ochazuke.Column{Title: "path", Min: 8}, ochazuke.Column{Title: "type", Priority: 2}, ochazuke.Column{Title: "count", Priority: 1, Align: ochazuke.AlignRight}))
		if err != nil {
			return err
		}
		s.tree = tree
		value, err := r.AddDetail(id+"/value", func() string {
			if n, ok := s.tree.Selected(); ok {
				return s.record + n.ID
			}
			return ""
		}, ochazuke.Parent(id))
		if err != nil {
			return err
		}
		s.value = value
		v.jsonSections = append(v.jsonSections, s)
	}
	return nil
}

func registerJSONNodes(r *ochazuke.Runtime) {
	if _, ok := r.Operation(jsonNodeNoun + ".show"); ok {
		return
	}
	r.Builtin(func() {
		r.Define(jsonNodeNoun).
			Verb("show", func(c ochazuke.Ctx) ochazuke.Result {
				n := jsonNode(c)
				if n == nil {
					return ochazuke.Failed(errors.New("no JSON node selected"))
				}
				_, err := c.Runtime.OpenTextView(n.Path(), jsontree.Pretty(n))
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Done(nil)
			}, "Show the complete JSON value in a read-only text view").Fetch().
			Verb("copy", func(c ochazuke.Ctx) ochazuke.Result {
				n := jsonNode(c)
				if n == nil {
					return ochazuke.Failed(errors.New("no JSON node selected"))
				}
				text := jsontree.Pretty(n)
				if err := c.Runtime.StoreYank(ochazuke.Yank{Text: text, Shape: "char"}, ochazuke.StringArg(c.Args, "register", ""), false); err != nil {
					return ochazuke.Failed(err)
				}
				c.Info("Copied " + n.Path())
				return ochazuke.Done(text)
			}, "Copy the complete selected JSON value").Fetch().
			Verb("scroll", func(c ochazuke.Ctx) ochazuke.Result {
				if c.Instance == nil {
					return ochazuke.Failed(errors.New("no JSON node selected"))
				}
				for _, id := range c.Runtime.Ancestry(c.Instance.ID) {
					if inst := c.Runtime.Instances[id]; inst != nil {
						if s, ok := inst.Data.(*jsonSection); ok {
							return ochazuke.Done(s.value.Scroll(ochazuke.IntArg(c.Args, "lines", 1) * max(1, ochazuke.IntArg(c.Args, "count", 1))))
						}
					}
				}
				return ochazuke.Failed(errors.New("no JSON value pane"))
			}, "Scroll the selected JSON value").Motion()
		for verb, params := range map[string]map[string]ochazuke.Parameter{
			"copy":   {"register": {Type: "string", Description: "Register (default active register)"}},
			"scroll": {"lines": {Type: "integer"}, "count": {Type: "integer"}},
		} {
			op, _ := r.Operation(jsonNodeNoun + "." + verb)
			op.Parameters = params
			r.Operations[jsonNodeNoun+"."+verb] = op
		}
		r.MustLoadDefaultBindings(ochazuke.FrameworkBindingsSource+"cli/detail_json.json", jsonBindings)
	})
}

func jsonNode(c ochazuke.Ctx) *jsontree.Node {
	if c.Instance == nil {
		return nil
	}
	n, _ := c.Instance.Data.(*jsontree.Node)
	return n
}

func (v *PageView) syncJSONSections() {
	if v.jsonParent == "" {
		return
	}
	r := v.m.Runtime
	id := v.selectedID()
	row, ok := v.rows[id]
	parent := r.Instances[v.jsonParent]
	parent.Noun, parent.Data, parent.Label = detailRowNoun, nil, ""
	if ok {
		parent.Noun, parent.Data, parent.Label = v.rowNounOf(id), row.ID, row.Label
	}
	for _, s := range v.jsonSections {
		enabled := ok && r.Settings().Bool("detail.sections")
		raw, err := json.Marshal(detailValue(row.Data, s.spec.Field))
		// A changed row resets selection even when its JSON is identical.
		record := id + "\x00" + string(raw)
		if record != s.record {
			changedRow := !strings.HasPrefix(s.record, id+"\x00")
			s.record, s.fallback, s.active = record, "", false
			if s.spec.Parse {
				if text, yes := detailValue(row.Data, s.spec.Field).(string); yes {
					raw = []byte(text)
				}
			}
			var nodes []ochazuke.TreeNode
			if err == nil {
				root, parseErr := jsontree.Build(raw)
				if parseErr == nil {
					nodes = jsontree.TreeNodes(root, (s.tree.ID()+"/list")+"/node/")
					for i := range nodes {
						nodes[i].Noun = jsonNodeNoun
					}
					s.active = ok
				} else {
					s.fallback = string(raw)
				}
			} else {
				s.fallback = humanDetail(detailValue(row.Data, s.spec.Field))
			}
			_ = s.tree.SetNodes(nodes) // generated paths are unique, parents precede children
			if changedRow && len(nodes) > 0 {
				_ = s.tree.Select(nodes[0].ID)
			}
		}
		disabled := !enabled || !s.active
		r.Instances[(s.tree.ID() + "/list")].Unfocusable = disabled
		if disabled && s.tree.List().Focused() {
			_ = r.SetFocus(v.lists[v.current].list.FocusTarget())
		}
	}
}

func (v *PageView) jsonFocused() bool {
	for _, s := range v.jsonSections {
		if s.tree.List().Focused() {
			return true
		}
	}
	return false
}

// jsonSectionsText appends full-width JSON sections below main/side content.
// A focused section brings its bounded tree/value window into the shared
// detail window. Each value has its own J/K offset, independent of the tree.
func (v *PageView) jsonSectionsText(layout ochazuke.Layout, text string, width, height int) string {
	if width < 1 || len(v.jsonSections) == 0 {
		return text
	}
	v.syncJSONSections()
	if _, ok := v.rows[v.selectedID()]; !ok {
		return text
	}
	focusStart, focusEnd := -1, -1
	for _, s := range v.jsonSections {
		title := s.spec.Title
		if title == "" {
			title = s.spec.Field
		}
		var body string
		if !s.active {
			body = ansi.Hardwrap(plainText(s.fallback), width, true)
		} else {
			// Keep the structure and selected value together even in a short
			// bottom pane. Large documents window inside each half.
			h := max(1, min(8, (height-3)/2))
			tw := width
			wide := width >= 64
			if wide {
				tw = (width - 3) / 2
			}
			tree := s.tree.View(tw, h)
			n, selected := s.tree.Selected()
			data, path := "", "."
			if selected {
				node := n.Data.(*jsontree.Node)
				path, data = node.Path(), jsontree.Pretty(node)
			}
			dw := width
			if wide {
				dw = width - tw - 3
			}
			value, marker := s.value.Window(ansi.Hardwrap(plainText(data), dw, true), 0, h)
			heading := "Data · " + path
			if marker != "" {
				heading += " · " + marker
			}
			heading = layout.Styles.Title.Render(ansi.Truncate(heading, dw, "…"))
			structure := layout.Styles.Title.Render(ansi.Truncate("Structure", tw, "…")) + "\n" + tree
			value = heading + "\n" + value
			if wide {
				left, right := strings.Split(structure, "\n"), strings.Split(value, "\n")
				lines := make([]string, max(len(left), len(right)))
				for i := range lines {
					l, rr := "", ""
					if i < len(left) {
						l = left[i]
					}
					if i < len(right) {
						rr = right[i]
					}
					lines[i] = l + strings.Repeat(" ", max(0, tw-ansi.StringWidth(l))) + layout.Styles.Muted.Render(" │ ") + rr
				}
				body = strings.Join(lines, "\n")
			} else {
				body = structure + "\n" + value
			}
		}
		part := layout.Styles.Title.Render(ansi.Truncate(plainText(title), width, "…")) + "\n" + body
		start := 0
		if text != "" {
			start = strings.Count(text, "\n") + 2
			text += "\n\n"
		}
		text += part
		if s.tree.List().Focused() {
			focusStart, focusEnd = start, start+strings.Count(part, "\n")+1
		}
	}
	// Measure first, then scroll: Scroll clamps against this frame's text.
	v.detail.Window(text, 0, height)
	if focusStart >= 0 {
		offset := v.detail.Offset()
		if focusStart < offset {
			v.detail.Scroll(focusStart - offset)
		} else if focusEnd > offset+height {
			v.detail.Scroll(min(focusStart, focusEnd-height) - offset)
		}
	}
	return text
}
