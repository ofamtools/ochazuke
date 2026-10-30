package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/refdoc"
)

// ReferenceModel is the runtime as the generated reference sees it: every
// declared setting (with the user's value where it differs from the
// default) and every default key row (DefaultBindingsDoc, with the user's
// bindings.json rows that change each table). refdoc.Render draws the
// pages from it; the committed docs/reference files are its render on the
// framework runtime, and docs.open renders it live.
func (r *Runtime) ReferenceModel() refdoc.Model {
	r.EnsureConfigSettings()
	m := refdoc.Model{Settings: []refdoc.Setting{}, Bindings: []refdoc.Group{}}
	s := r.Settings()
	for _, one := range s.List() {
		out := refdoc.Setting{
			Name: one.Name, Type: one.Type, Default: one.Default, Enum: slices.Clone(one.Enum),
			Description: one.Description, Section: s.sectionTitle(one.Owner, one.Section), Framework: s.FrameworkSetting(one.Name),
		}
		if (one.Set || one.Session) && !sameValue(one.Value, one.Default) {
			out.Yours, out.YoursFrom = one.Value, SettingsFile
			if one.Session {
				out.YoursFrom = "session"
			}
		}
		m.Settings = append(m.Settings, out)
	}
	slices.SortFunc(m.Settings, func(a, b refdoc.Setting) int { return strings.Compare(a.Name, b.Name) })
	yours := r.yoursByContext()
	seen := map[string]bool{}
	for _, g := range r.DefaultBindingsDoc().Groups {
		ctx := referenceContext(g.Noun, g.Instance, g.Mode)
		out := refdoc.Group{Context: ctx, Extends: g.Extends, Rows: make([]refdoc.Row, 0, len(g.Rows)), Yours: yours[ctx]}
		for _, b := range g.Rows {
			out.Rows = append(out.Rows, referenceRow(b))
		}
		seen[ctx] = true
		m.Bindings = append(m.Bindings, out)
	}
	// A table only the user's file has rows in.
	for _, ctx := range slices.Sorted(maps.Keys(yours)) {
		if !seen[ctx] {
			m.Bindings = append(m.Bindings, refdoc.Group{Context: ctx, Rows: []refdoc.Row{}, Yours: yours[ctx]})
		}
	}
	m.StandardVerbs = referenceStandardVerbs()
	m.Nouns = r.referenceNouns()
	return m
}

// standardNeeds says what each standard verb needs of a noun, as the
// reference prints it: the row's Needs, in words (a test keeps one entry
// per row).
var standardNeeds = map[string]string{
	"get":       "Store.Get",
	"list":      "Store.List",
	"watch":     "Store.Watch, else Store.List (a singleton: Store.Get)",
	"create":    "Store.Create and a prop create writes",
	"set":       "Store.Patch and a writable prop",
	"delete":    "Store.Delete",
	"rename":    "Store.Patch and a writable label prop (role label)",
	"move":      "Store.Patch and a writable owner reference (role owner)",
	"duplicate": "Store.Get, Store.Create and a prop create writes",
	"archive":   "Store.Patch and a writable archive prop (role archive)",
	"restore":   "Store.Patch and a writable archive prop (role archive)",
}

// standardAliases are the aliases a standard verb is registered with
// (patch with set, owner decision 1), as the reference names them.
var standardAliases = map[string][]string{"set": {"patch"}}

// referenceStandardVerbs are the standard verb table's rows as the
// reference shows them, in the table's order.
func referenceStandardVerbs() []refdoc.StandardVerb {
	out := make([]refdoc.StandardVerb, 0, len(standardVerbs))
	for _, v := range standardVerbs {
		row := refdoc.StandardVerb{
			Verb: v.Verb, Description: v.Description, Kinds: slices.Clone(v.Kinds), Needs: standardNeeds[v.Verb],
			Subject: v.Subject, Rows: v.Rows, Many: v.Many, Repeatable: v.Repeatable, History: v.HistoryKind, Confirm: v.Confirm,
		}
		row.Aliases = slices.Clone(standardAliases[v.Verb])
		out = append(out, row)
	}
	return out
}

// referenceNouns are the nouns that declare props, by name: each its
// store's kind, its props and its verbs (each verb's standard row and its
// aliases on the same noun).
func (r *Runtime) referenceNouns() []refdoc.Noun {
	var out []refdoc.Noun
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		n := r.Nouns[name]
		if len(n.Props) == 0 {
			continue
		}
		noun := refdoc.Noun{Name: name, Kind: n.StoreKind(), Props: make([]refdoc.Prop, 0, len(n.Props))}
		for _, p := range n.Props {
			noun.Props = append(noun.Props, refdoc.Prop{
				Name: p.Name, Type: p.Type, Description: p.Description, Noun: p.Noun, Enum: slices.Clone(p.Enum),
				Default: p.Default, Role: p.Role, Fallback: p.Fallback, When: p.When, Key: p.Key, Required: p.Required,
				ReadOnly: p.ReadOnly, CreateOnly: p.CreateOnly, FromFocus: p.FromFocus, Secret: p.Secret, Multiline: p.Multiline,
			})
		}
		var verbs []string
		for key, op := range r.Operations {
			if op.Noun == name {
				verbs = append(verbs, key)
			}
		}
		slices.Sort(verbs)
		for _, key := range verbs {
			op := r.Operations[key]
			verb := refdoc.Verb{Name: op.Verb, Standard: op.Standard}
			for _, a := range r.AliasesOf(key) {
				if a.Noun == name && !a.Bare {
					verb.Aliases = append(verb.Aliases, a.Verb)
				}
			}
			noun.Verbs = append(noun.Verbs, verb)
		}
		out = append(out, noun)
	}
	return out
}

// declareBuiltinSetting declares a setting the framework adds for a
// feature after the runtime is made, as the framework's (Builtin), so the
// reference says ochazuke declares it.
func (r *Runtime) declareBuiltinSetting(one Setting) error {
	r.builtinDepth++
	defer func() { r.builtinDepth-- }()
	return r.DeclareSetting(one)
}

// markFramework records whether the framework declared name.
func (s *Settings) markFramework(name string, framework bool) {
	if !framework {
		delete(s.byFramework, name)
		return
	}
	if s.byFramework == nil {
		s.byFramework = map[string]bool{}
	}
	s.byFramework[name] = true
}

// FrameworkSetting reports whether the framework declared the setting
// (its fixed rows, or a feature's), not the application.
func (s *Settings) FrameworkSetting(name string) bool {
	return s.framework[name] || s.byFramework[name]
}

// referenceContext names a table as the reference does: "noun/mode",
// "noun" for the any-mode table, "instance ID" or "instance ID/mode".
func referenceContext(noun, instance, mode string) string {
	ctx := noun
	if instance != "" {
		ctx = "instance " + instance
	}
	if mode != "" {
		ctx += "/" + mode
	}
	return ctx
}

// referenceRow is one row as the reference shows it.
func referenceRow(b Binding) refdoc.Row {
	out := refdoc.Row{Keys: b.Keys, When: b.When, Action: b.Request.Key(), Hint: b.Hint, Hidden: b.Hidden}
	if b.Keys == "" {
		out.Keys = "<" + b.Pattern + ">"
	}
	if len(b.Request.Args) > 0 {
		if data, err := marshalPlain(b.Request.Args); err == nil {
			out.Args = string(data)
		}
	}
	if len(b.From) > 0 {
		var from []string
		for _, name := range slices.Sorted(maps.Keys(b.From)) {
			from = append(from, name+"="+b.From[name])
		}
		out.From = strings.Join(from, ", ")
	}
	var flags []string
	if b.Request.Target != "" {
		flags = append(flags, "target "+b.Request.Target)
	}
	if b.Request.Confirmed {
		flags = append(flags, "confirmed")
	}
	if b.Capture {
		flags = append(flags, "capture")
	}
	if b.Replay {
		flags = append(flags, "replay")
	}
	out.Flags = strings.Join(flags, ", ")
	return out
}

// yoursByContext is the user's bindings.json rows as "yours:" lines, by
// the table they change, in the order they were applied.
func (r *Runtime) yoursByContext() map[string][]string {
	out := map[string][]string{}
	c := r.Config()
	if c == nil {
		return out
	}
	for _, a := range c.applied {
		row := a.row
		ctx := referenceContext(row.Noun, row.Instance, row.Mode)
		keys := row.Keys
		if keys == "" {
			keys = "<" + row.Pattern + ">"
		}
		text := markdownCode(keys)
		if row.When != "" {
			text += " when " + markdownCode(row.When)
		}
		switch {
		case row.Unbind:
			text += " unbound"
		default:
			ref := referenceRow(row)
			text += " → " + markdownCode(ref.Action)
			if ref.Args != "" {
				text += " " + markdownCode(ref.Args)
			}
			if len(a.replaced) > 0 {
				text += " (replaces the default)"
			} else {
				text += " (added)"
			}
		}
		out[ctx] = append(out[ctx], text+" ("+BindingsFile+")")
	}
	return out
}

// markdownCode is s as a Markdown code span.
func markdownCode(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// sameValue reports whether two values encode alike.
func sameValue(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(x) == string(y)
}

// The docs noun's setting and the viewer.
const (
	// DocsShowConfigSetting adds the user's "yours:" lines to the docs
	// viewer's pages (default on); off, a page reads as the committed
	// file below its header.
	DocsShowConfigSetting = "docs.show-config"
	// docsViewerID is the docs viewer modal (noun ochazuke.docs-viewer);
	// docsTextID the detail it scrolls.
	docsViewerID = "docs-viewer"
	docsTextID   = "__docs-viewer/text"
)

// DocsTool is the tool name the docs viewer's header and pages name: the
// config's App, else the binary's name.
func (r *Runtime) DocsTool() string {
	if c := r.Config(); c != nil && c.App != "" {
		return c.App
	}
	return strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
}

// DocsPage is a topic's page rendered from the live runtime with the
// running binary's stamp, and the viewer's header line naming that build:
// "TOOL COMMIT[+dirty] · VERSION · catalog FINGERPRINT". The page's text
// is its body below the generated header (the header line stands for it);
// with docs.show-config off it equals the committed file's body for the
// same catalog.
func (r *Runtime) DocsPage(topic string) (header, text string, err error) {
	if topic == "" {
		topic = "index"
	}
	file := refdoc.TopicFile(topic)
	if file == "" {
		return "", "", fmt.Errorf("unknown docs topic %q (topics: %s)", topic, strings.Join(docsTopics(), ", "))
	}
	show := true
	if v, ok := r.Settings().Value(DocsShowConfigSetting); ok {
		show, _ = v.(bool)
	}
	m := r.ReferenceModel()
	stamp := refdoc.BuildStamp(r.DocsTool())
	pages := refdoc.Render(m, refdoc.Options{Stamp: stamp, ShowConfig: show})
	parts := []string{stamp.Tool + " " + stamp.ShortCommit()}
	if stamp.Version != "" {
		parts = append(parts, stamp.Version)
	}
	parts = append(parts, "catalog "+m.Fingerprint())
	return strings.Join(parts, " · "), refdoc.Body(pages[file]), nil
}

// docsTopics are the topic names docs.open takes.
func docsTopics() []string {
	out := make([]string, len(refdoc.Topics))
	for i, t := range refdoc.Topics {
		out[i] = t.Topic
	}
	return out
}

// docsViewer is the docs viewer modal's component: the topic, its header
// line and text (rendered once, when it opened) and the detail that
// scrolls it. It draws the modal (ModalBody).
type docsViewer struct {
	r      *Runtime
	topic  string
	header string
	text   string
	detail *Detail
}

// ModalTitle is the build the page was rendered from.
func (v *docsViewer) ModalTitle() string { return v.header }

// ModalBlock draws the topic and the scroll position on a muted line, then
// the page's window.
func (v *docsViewer) ModalBlock(s Styles, width, height int) Block {
	if width <= 0 || height <= 0 {
		return Text("")
	}
	body, marker := v.detail.Window(v.text, width, max(height-1, 1))
	top := v.topic
	if marker != "" {
		top += " · " + marker
	}
	// The top line is as wide as the page's widest line, so the panel
	// (sized from the lines it draws) keeps one width while it scrolls.
	top = ansi.Truncate(top, width, "…")
	top += strings.Repeat(" ", max(0, min(width, v.detail.Width())-ansi.StringWidth(top)))
	return Text(s.Muted.Render(top) + "\n" + body)
}

// registerDocs registers the docs noun: docs.open shows a topic of the
// generated reference in a modal viewer. The viewer's keys are
// bindings/docs.json; esc closes it through the modal noun.
func registerDocs(r *Runtime) {
	r.Define("ochazuke.docs-viewer")
	r.Register(Operation{Verb: "open", Noun: "ochazuke.docs", Description: "Open a topic of the generated reference (index, settings, bindings, standard-verbs) in the docs viewer, rendered from the running catalog",
		Parameters: map[string]Parameter{
			"topic": {Type: "string", Enum: docsTopics(), Description: "The topic (default index)"},
		}, Positional: []string{"topic"}, Result: "docs page", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			topic := StringArg(a, "topic", "index")
			header, text, err := r.DocsPage(topic)
			if err != nil {
				return nil, err
			}
			// Opened again while it is the top modal (:help TOPIC inside
			// the viewer), the viewer is replaced by the new topic's.
			inst, err := r.OpenModal(docsViewerID, Payload{}, ReplaceOpen())
			if err != nil {
				return nil, err
			}
			v := &docsViewer{r: r, topic: topic, header: header, text: text}
			d, err := r.AddDetail(docsTextID, func() string { return v.topic }, Parent(docsViewerID))
			if err != nil {
				_ = r.Pop()
				return nil, err
			}
			v.detail = d
			inst.Component = v
			return map[string]any{"topic": topic, "header": header}, nil
		}})
	// :help and :h, Vim's names (owner decision 3). help shadows the verb
	// help (bindings help, command help) by what follows: help alone or
	// help TOPIC opens the docs, help bindings is bindings help. Both are
	// the palette's: the viewer needs the TUI, and bin help is the shell's
	// help.
	for _, a := range []ActionAlias{
		{Alias: "help", Action: "ochazuke.docs.open", Shadows: true, Palette: true, Note: "Vim's :help; help NOUN ... runs NOUN help"},
		{Alias: "h", Action: "ochazuke.docs.open", Palette: true, Note: "Vim's :h"},
	} {
		if err := r.AddActionAlias(a); err != nil {
			panic(err)
		}
	}
}
