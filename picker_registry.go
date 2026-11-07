package ochazuke

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Pickers as data: a picker is a declaration (PickerDef) of a name, the
// noun its rows belong to, the verb its rows come from, the verb run on the
// highlighted row for a preview and the verb run on pick. Declared three
// ways: AddPicker in Go, a catalog's pickers array (cli.MountCatalogPickers)
// and derived: every noun with a list verb (ChoicesSource of a Noun
// parameter) gets a picker named after it while picker.derive is on, unless
// the noun opts out (NounBuilder.NoPicker) or a declared picker names the
// noun. picker.open {name} opens one, picker.list lists them (the picker
// noun is a thing noun), and the
// pickers picker (a registered picker whose rows are the pickers) opens the
// one picked. A form field whose parameter names a noun opens that noun's
// picker (picker.params).

// PickerNoun is the picker registry's noun: open, and the standard get,
// list and watch over the pickers as things.
const PickerNoun = "ochazuke.picker"

// Picker settings (settings/ochazuke.json, section choosers).
const (
	PickerDeriveSetting = "picker.derive"
	PickerListSetting   = "picker.picker-list"
	PickerParamsSetting = "picker.params"
)

// The framework's own pickers.
const (
	PickersPickerName  = "pickers"
	PagesPickerName    = "pages"
	PathPickerName     = "path"
	CommandsPickerName = "commands"
)

// PickerDef declares a picker. A picker opens one of three ways, the first
// that is set: Open (Go only: the page picker, the path picker, the
// pickers picker), Verb (a verb that opens it: the palette), else a
// chooser over Source's rows (default the Noun's list verb).
type PickerDef struct {
	// Name is what picker.open takes ("task", "pages").
	Name string `json:"name"`
	// Title is the chooser's title; "" is the noun's name, else Name.
	Title string `json:"title,omitempty"`
	// About says what the picker lists; the pickers picker previews it.
	About string `json:"about,omitempty"`
	// Noun is the rows' noun: prop filters, the default Source, Preview
	// and Pick. A declared picker naming a noun stands for that noun: it
	// replaces its derived picker and a Noun parameter opens it.
	Noun string `json:"noun,omitempty"`
	// Source is a verb whose result is the rows (read as a parameter's
	// choices: ParseChoices); "" is the Noun's list verb.
	Source string `json:"source,omitempty"`
	// Sources are more named sources, cycled with the chooser's
	// next-source and previous-source keys; each is a verb as Source.
	Sources []string `json:"sources,omitempty"`
	// Preview is a verb run on the highlighted row (its subject parameter,
	// else its target, the row's id) whose answer is drawn beside the
	// rows; "" is the Noun's get verb when it has one. A verb that answers
	// Pending previews nothing.
	Preview string `json:"preview,omitempty"`
	// Pick is the verb run on the picked row as Preview runs; "" is the
	// Noun's open verb, else its get; with neither a pick answers the id.
	Pick string `json:"pick,omitempty"`
	// Verb, when set, opens the picker by running it (a verb that opens a
	// chooser or a modal of its own: ochazuke.palette.open).
	Verb string `json:"verb,omitempty"`
	// Match is the match mode the picker opens in (ChooserSpec.Match).
	Match string `json:"match,omitempty"`
	// Derived marks a picker derived from a noun's list verb.
	Derived bool `json:"derived,omitempty"`
	// Open, when set, opens the picker in Go with the query typed so far.
	Open func(r *Runtime, query string) (*Chooser, error) `json:"-"`

	// builtin marks the framework's own pickers, left out of a catalog.
	builtin bool
}

// title is the chooser's title.
func (d PickerDef) title(r *Runtime) string {
	switch {
	case d.Title != "":
		return d.Title
	case d.Noun != "":
		return r.DisplayNoun(r.CanonicalNoun(d.Noun))
	}
	return d.Name
}

// kind is the picker's modal kind when the registry opens its chooser.
func (d PickerDef) kind() string {
	return "picker-" + strings.ReplaceAll(d.Name, ".", "-")
}

// AddPicker registers a picker; adding a name again replaces it. Name is
// required, and so is a way to open it (Open, Verb, Source or Noun);
// Match, when set, is a match mode.
func (r *Runtime) AddPicker(def PickerDef) error {
	def.Name = strings.TrimSpace(def.Name)
	if def.Name == "" || strings.ContainsAny(def.Name, " \t\n") {
		return fmt.Errorf("picker name %q must be one word", def.Name)
	}
	if def.Open == nil && def.Verb == "" && def.Source == "" && def.Noun == "" {
		return fmt.Errorf("picker %s needs open, verb, source or noun", def.Name)
	}
	if def.Match != "" && !slices.Contains(MatchModes(), def.Match) {
		return fmt.Errorf("picker %s: match %q is not one of %s", def.Name, def.Match, strings.Join(MatchModes(), ", "))
	}
	def.Derived = false
	def.builtin = r.builtinDepth > 0
	mem := r.chooserMemory()
	if i := slices.IndexFunc(mem.pickers, func(d PickerDef) bool { return d.Name == def.Name }); i >= 0 {
		mem.pickers[i] = def
		return nil
	}
	mem.pickers = append(mem.pickers, def)
	return nil
}

// DeclaredPickers are the pickers AddPicker registered, in order.
func (r *Runtime) DeclaredPickers() []PickerDef {
	return slices.Clone(r.chooserMemory().pickers)
}

// CatalogPickerDefs are the declared pickers a catalog carries: the
// application's, less the ones only Go can open.
func (r *Runtime) CatalogPickerDefs() []PickerDef {
	var out []PickerDef
	for _, d := range r.chooserMemory().pickers {
		if d.builtin || (d.Verb == "" && d.Source == "" && d.Noun == "") {
			continue
		}
		d.Open = nil
		out = append(out, d)
	}
	return out
}

// derivedPickers are the noun pickers picker.derive gives: one per noun
// whose list verb is a choices source (needs no argument, asks nothing),
// named by the noun's display name, by name; none for a noun that opts
// out or one a declared picker names.
func (r *Runtime) derivedPickers(declared []PickerDef) []PickerDef {
	if !r.settingOn(PickerDeriveSetting) {
		return nil
	}
	covered := map[string]bool{}
	taken := map[string]bool{}
	for _, d := range declared {
		taken[d.Name] = true
		if d.Noun != "" {
			covered[r.CanonicalNoun(d.Noun)] = true
		}
	}
	var out []PickerDef
	for name, n := range r.Nouns {
		if n == nil || n.NoPicker || covered[name] {
			continue
		}
		if r.ChoicesSource(Parameter{Noun: name}) == "" {
			continue
		}
		pick := r.DisplayNoun(name)
		if taken[pick] {
			pick = name
		}
		out = append(out, PickerDef{Name: pick, Noun: name, About: n.About, Derived: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PickerDefs are every picker: the declared ones in order, then the
// derived noun pickers by name.
func (r *Runtime) PickerDefs() []PickerDef {
	declared := r.DeclaredPickers()
	return append(declared, r.derivedPickers(declared)...)
}

// PickerDef is the picker named name: a picker's name, else the picker
// whose noun name names (a derived picker by the noun's canonical name).
func (r *Runtime) PickerDef(name string) (PickerDef, bool) {
	all := r.PickerDefs()
	if i := slices.IndexFunc(all, func(d PickerDef) bool { return d.Name == name }); i >= 0 {
		return all[i], true
	}
	noun := r.CanonicalNoun(name)
	if i := slices.IndexFunc(all, func(d PickerDef) bool { return d.Noun != "" && r.CanonicalNoun(d.Noun) == noun }); i >= 0 {
		return all[i], true
	}
	return PickerDef{}, false
}

// PickerFor is the picker of a noun: the declared one naming it, else its
// derived picker.
func (r *Runtime) PickerFor(noun string) (PickerDef, bool) {
	noun = r.CanonicalNoun(noun)
	for _, d := range r.PickerDefs() {
		if d.Noun != "" && r.CanonicalNoun(d.Noun) == noun {
			return d, true
		}
	}
	return PickerDef{}, false
}

// OpenNamedPicker opens the picker named name (PickerDef) with query typed
// in its search line.
func (r *Runtime) OpenNamedPicker(name, query string) (*Chooser, error) {
	def, ok := r.PickerDef(name)
	if !ok {
		return nil, fmt.Errorf("no picker named %q (picker.list lists them)", name)
	}
	var c *Chooser
	var err error
	switch {
	case def.Open != nil:
		c, err = def.Open(r, query)
	case def.Verb != "":
		noun, verb := SplitKey(r.CanonicalKey(def.Verb))
		if _, err = r.Execute(Request{Noun: noun, Verb: verb}); err == nil {
			c = r.chooserAt(r.Instances[r.TopModal()])
		}
	default:
		c, err = r.openSourcePicker(def)
	}
	if err != nil {
		return nil, err
	}
	if c != nil && query != "" && c.Query() == "" {
		c.SetQuery(query)
	}
	return c, nil
}

// pickerParam is the parameter whose choices are one of a picker's
// sources: source, else the noun's list verb.
func pickerParam(def PickerDef, source string) Parameter {
	if source != "" {
		return Parameter{Source: source}
	}
	return Parameter{Noun: def.Noun}
}

// pickerSources are a picker's sources: Source (or the noun's list) then
// Sources, each named by its verb.
func (r *Runtime) pickerSources(def PickerDef, c Context) []ChooserSource {
	verbs := append([]string{def.Source}, def.Sources...)
	out := make([]ChooserSource, 0, len(verbs))
	for _, verb := range verbs {
		param := pickerParam(def, verb)
		name := verb
		if name == "" {
			name = r.DisplayKey(r.ChoicesSource(param))
		}
		out = append(out, ChooserSource{Name: name, Load: func(query string) Result {
			res := r.choicesLoad(c, param, query)
			if res.work == nil {
				if res.err != nil {
					return res
				}
				return Done(choiceRows(res.value))
			}
			return res.bare().Map(func(value any, err error) (any, error) {
				if err != nil {
					return nil, err
				}
				return choiceRows(value), nil
			})
		}})
	}
	return out
}

// choiceRows are a choices load's value ([]Choice) as chooser rows.
func choiceRows(value any) []ChooserRow {
	choices, _ := value.([]Choice)
	rows := make([]ChooserRow, len(choices))
	for i, ch := range choices {
		rows[i] = ChooserRow{ID: ch.ID, Label: ch.Label}
	}
	return rows
}

// openSourcePicker opens the chooser of a picker declared by its rows:
// its sources, its preview, and its pick verb on enter.
func (r *Runtime) openSourcePicker(def PickerDef) (*Chooser, error) {
	ctx := r.Context()
	noun := ""
	if def.Noun != "" {
		noun = r.CanonicalNoun(def.Noun)
	}
	title := def.title(r)
	spec := ChooserSpec{
		Kind:    def.kind(),
		Title:   title,
		Noun:    noun,
		Match:   def.Match,
		Plural:  title,
		Sources: r.pickerSources(def, ctx),
		Preview: r.pickerPreview(def),
		Choose:  func(row ChooserRow) Result { return r.runPickerPick(def, row) },
	}
	return r.OpenChooser(spec)
}

// pickerVerb is one of a picker's row verbs: the one it declares, else the
// first of the noun's verbs named in fallbacks that is registered and
// takes the row's id.
func (r *Runtime) pickerVerb(def PickerDef, declared string, fallbacks ...string) (Operation, bool) {
	if declared != "" {
		op, ok := r.Operations[r.CanonicalKey(declared)]
		return op, ok
	}
	if def.Noun == "" {
		return Operation{}, false
	}
	noun := r.CanonicalNoun(def.Noun)
	for _, verb := range fallbacks {
		// A fallback must take the row's id (its subject parameter): an
		// application's open that acts on the focused row does not.
		if op, ok := r.Operations[noun+"."+verb]; ok && subjectParam(op) != "" {
			return op, true
		}
	}
	return Operation{}, false
}

// subjectParam is the parameter a row verb takes the row's id in: the one
// filled from the focus (the key prop of a thing noun), else the only
// required one; "" passes the id as the target.
func subjectParam(op Operation) string {
	var required []string
	for name, p := range op.Parameters {
		if p.Focus {
			return name
		}
		if p.Required {
			required = append(required, name)
		}
	}
	if len(required) == 1 {
		return required[0]
	}
	return ""
}

// rowRequest is op's request for one row: the id in its subject parameter,
// else as the target.
func rowRequest(op Operation, id string) Request {
	q := Request{Noun: op.Noun, Verb: op.Verb}
	if name := subjectParam(op); name != "" {
		q.Args = Arguments{name: id}
	} else {
		q.Target = id
	}
	return q
}

// runPickerPick runs a picker's pick verb on the picked row.
func (r *Runtime) runPickerPick(def PickerDef, row ChooserRow) Result {
	op, ok := r.pickerVerb(def, def.Pick, "open", "get")
	if !ok {
		if def.Pick != "" {
			return Failed(fmt.Errorf("picker %s: unknown pick verb %q", def.Name, def.Pick))
		}
		return Done(row.ID)
	}
	v, err := r.Execute(rowRequest(op, row.ID))
	if err != nil {
		return Failed(err)
	}
	return Done(v)
}

// pickerPreview is a picker's preview: its preview verb's answer for the
// highlighted row, read once per row and drawn as fields; nil without a
// preview verb.
func (r *Runtime) pickerPreview(def PickerDef) func(ChooserRow) string {
	op, ok := r.pickerVerb(def, def.Preview, "get")
	if !ok {
		return nil
	}
	cache := map[string]string{}
	return func(row ChooserRow) string {
		if text, ok := cache[row.ID]; ok {
			return text
		}
		text := previewText(r.previewValue(op, row.ID))
		cache[row.ID] = text
		return text
	}
}

// previewValue runs a preview verb on one row without history: a verb
// that answers Pending, or fails, previews nothing.
func (r *Runtime) previewValue(op Operation, id string) any {
	q := rowRequest(op, id)
	c := r.Context()
	if q.Target != "" {
		if r.Instances[q.Target] == nil {
			return nil
		}
		c = r.contextOf(q.Target)
	}
	if op.App == nil {
		v, err := op.Run(r, c, q.Args)
		if err != nil {
			return nil
		}
		return v
	}
	res, _, err := r.runChoicesApp(op, c, q.Args)
	if err != nil || res.work != nil {
		return nil
	}
	return res.value
}

// previewText draws a preview answer: text as it is, an object as one
// "field: value" line per field by name, anything else as indented JSON.
func previewText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil {
		var out any
		_ = json.Unmarshal(data, &out)
		indented, _ := json.MarshalIndent(out, "", "  ")
		return string(indented)
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		value := fields[name]
		text, isText := value.(string)
		if !isText {
			raw, _ := json.Marshal(value)
			text = string(raw)
		}
		lines = append(lines, name+": "+text)
	}
	return strings.Join(lines, "\n")
}

// pickerKeys are the keys bound to open a picker: rows running
// picker.open with its name, or its Verb.
func (r *Runtime) pickerKeys(def PickerDef) []string {
	verb := ""
	if def.Verb != "" {
		verb = r.CanonicalKey(def.Verb)
	}
	var keys []string
	for _, n := range r.Nouns {
		for _, rows := range n.Bindings {
			for _, row := range rows {
				key := r.CanonicalKey(row.Request.Key())
				opens := key == PickerNoun+".open" && StringArg(row.Request.Args, "name", "") == def.Name
				if (opens || (verb != "" && key == verb)) && row.Keys != "" && !slices.Contains(keys, row.Keys) {
					keys = append(keys, row.Keys)
				}
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// pickerProps are a picker thing's props (picker.get, picker.list).
var pickerProps = []PropSpec{
	{Name: "name", Key: true, Parameter: Parameter{Description: "What picker.open takes"}},
	{Name: "title", Role: RoleLabel, ReadOnly: true, Parameter: Parameter{Description: "The picker's title"}},
	{Name: "noun", ReadOnly: true, Parameter: Parameter{Description: "The noun its rows belong to"}},
	{Name: "about", ReadOnly: true, Parameter: Parameter{Description: "What it lists"}},
	{Name: "derived", ReadOnly: true, Parameter: Parameter{Type: SettingBoolean, Description: "Derived from the noun's list verb (picker.derive), not declared"}},
	{Name: "keys", ReadOnly: true, Parameter: Parameter{Description: "The keys bound to open it, separated by spaces"}},
	{Name: "source", ReadOnly: true, Parameter: Parameter{Description: "The verb its rows come from; empty is the noun's list"}},
	{Name: "preview", ReadOnly: true, Parameter: Parameter{Description: "The verb run on the highlighted row for its preview"}},
	{Name: "pick-verb", ReadOnly: true, Parameter: Parameter{Description: "The verb run on the picked row (PickerDef.Pick; a prop named pick would clash with the pick verb)"}},
	{Name: "verb", ReadOnly: true, Parameter: Parameter{Description: "The verb that opens it, when it opens by one"}},
	{Name: "match", ReadOnly: true, Parameter: Parameter{Description: "The match mode it opens in"}},
}

// pickerRow is a picker as a thing of the picker noun.
func (r *Runtime) pickerRow(def PickerDef) Thing {
	row := Thing{"name": def.Name, "title": def.title(r), "derived": def.Derived, "keys": strings.Join(r.pickerKeys(def), " ")}
	if def.Noun != "" {
		row["noun"] = r.CanonicalNoun(def.Noun)
	}
	for field, text := range map[string]string{"about": def.About, "source": def.Source, "preview": def.Preview, "pick-verb": def.Pick, "verb": def.Verb, "match": def.Match} {
		if text != "" {
			row[field] = text
		}
	}
	return row
}

// openPickersPicker opens the pickers picker: a row per picker (the
// derived ones too unless picker.picker-list is declared); a pick opens
// the picker picked.
func (r *Runtime) openPickersPicker() (*Chooser, error) {
	defs := r.PickerDefs()
	if v, _ := r.settingText(PickerListSetting); v == "declared" {
		defs = slices.DeleteFunc(defs, func(d PickerDef) bool { return d.Derived })
	}
	about := map[string]string{}
	rows := make([]ChooserRow, 0, len(defs))
	for _, d := range defs {
		if d.Name == PickersPickerName {
			continue
		}
		keys := strings.Join(r.pickerKeys(d), " ")
		rows = append(rows, ChooserRow{ID: d.Name, Label: d.Name, Cells: []string{d.Name, d.title(r), keys}, Noun: PickerNoun})
		text := d.About
		if d.Noun != "" {
			text = strings.TrimSpace(text + "\n\nnoun: " + r.CanonicalNoun(d.Noun))
		}
		if d.Derived {
			text += "\nderived from its list verb"
		}
		about[d.Name] = text
	}
	return r.OpenChooser(ChooserSpec{
		Kind:    "picker-" + PickersPickerName,
		Title:   "Pickers",
		Noun:    PickerNoun,
		Rows:    rows,
		Plural:  "pickers",
		Preview: func(row ChooserRow) string { return about[row.ID] },
		Choose: func(row ChooserRow) Result {
			c, err := r.OpenNamedPicker(row.ID, "")
			if err != nil {
				return Failed(err)
			}
			if c == nil {
				return Done(r.Context())
			}
			return Done(c.state())
		},
	})
}

// setPickerFinder keeps the path finder's open (NewPicker), which picker.open
// with no name runs when the application made one.
func (r *Runtime) setPickerFinder(open func(Context, Arguments) (any, error)) {
	r.chooserMemory().finderOpen = open
}

// registerPickerRegistry registers the picker noun's registry verbs and
// the framework's pickers: pages, path and the pickers picker.
func registerPickerRegistry(r *Runtime) {
	r.Define(PickerNoun)
	r.Register(Operation{Verb: "open", Noun: PickerNoun, Description: "Open a picker by name (picker.list lists them; pickers is the picker of pickers); with no name, the path picker (the fuzzy path finder when the application has one)", Parameters: map[string]Parameter{
		"name":   {Type: "string", Noun: PickerNoun, Description: "The picker to open; empty opens the path picker"},
		"query":  {Type: "string", Description: "Text typed into the picker's search line when it opens"},
		"filter": {Type: "string", Description: "Path finder only: any, file or dir; defaults to the originating path field's filter"},
		"path":   {Type: "string", Description: "Path finder only: initial path/query; otherwise path value, current paths line, or cwd"},
	}, Positional: []string{"name"}, Result: "picker state", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		name := StringArg(a, "name", "")
		if name == "" {
			if open := r.chooserMemory().finderOpen; open != nil {
				return open(c, a)
			}
			name = PathPickerName
		}
		chooser, err := r.OpenNamedPicker(name, StringArg(a, "query", ""))
		if err != nil {
			return nil, err
		}
		if chooser == nil {
			return r.Context(), nil
		}
		return chooser.state(), nil
	}})
	// The picker noun is a thing noun: its things are the pickers, keyed by
	// name, so get, list and watch are its standard verbs (picker.list,
	// picker.get).
	r.Define(PickerNoun).About("The pickers: declared (AddPicker, a catalog's pickers) and one per noun with a list verb (picker.derive)").Props(pickerProps...).Store(Store{
		Kind: Collection,
		Get: func(c Ctx, name string) (Thing, error) {
			def, ok := c.Runtime.PickerDef(name)
			if !ok {
				return nil, fmt.Errorf("no picker named %q", name)
			}
			return c.Runtime.pickerRow(def), nil
		},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			defs := c.Runtime.PickerDefs()
			out := make([]Thing, 0, len(defs))
			for _, d := range defs {
				out = append(out, c.Runtime.pickerRow(d))
			}
			return out, nil
		},
	})
	for _, def := range []PickerDef{
		{Name: PickersPickerName, Title: "Pickers", Noun: PickerNoun, About: "Every picker: pick one to open it", Open: func(r *Runtime, _ string) (*Chooser, error) { return r.openPickersPicker() }},
		{Name: PagesPickerName, Title: "Pages", Noun: "ochazuke.page", About: "The pages, most recently visited first; pick one to open it", Verb: "ochazuke.page.pick"},
		{Name: PathPickerName, Title: "Path", About: "A file or folder: browse, recent and typed paths", Open: func(r *Runtime, _ string) (*Chooser, error) {
			return r.OpenPathChooser(PathChooserSpec{})
		}},
	} {
		if err := r.AddPicker(def); err != nil {
			panic(err)
		}
	}
}

// nounPicker gives a choice chooser over a Noun parameter (one with no
// source of its own) the noun's registered picker while picker.params is
// on: the picker's source for its rows and its match mode. The chooser
// keeps its title, its width (no preview column: a field's chooser says a
// failed load in full) and its pick (the field's value); a picker that
// opens in Go or by a verb leaves it as it is.
func (r *Runtime) nounPicker(spec ChooserSpec, p Parameter) (ChooserSpec, Parameter) {
	if p.Noun == "" || p.Source != "" || !r.settingOn(PickerParamsSetting) {
		return spec, p
	}
	def, ok := r.PickerFor(p.Noun)
	if !ok || def.Open != nil || def.Verb != "" {
		return spec, p
	}
	if def.Source != "" {
		p.Source = def.Source
	}
	if spec.Match == "" {
		spec.Match = def.Match
	}
	return spec, p
}
