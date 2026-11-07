package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func pickerNames(defs []PickerDef) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

func chooserRowIDs(c *Chooser) []string {
	var out []string
	for _, row := range c.Matches() {
		out = append(out, row.ID)
	}
	return out
}

// The framework declares pages, path and the pickers picker; the palette
// adds commands; a thing noun with a list verb gets a derived picker named
// after it.
func TestPickerRegistryDeclaredAndDerived(t *testing.T) {
	r, _ := newTaskRuntime(t)
	NewPalette(r)
	names := pickerNames(r.PickerDefs())
	for _, want := range []string{PickersPickerName, PagesPickerName, PathPickerName, CommandsPickerName, "task"} {
		if !slices.Contains(names, want) {
			t.Fatalf("pickers %v lack %q", names, want)
		}
	}
	// A declared picker naming a noun stands for it: pages covers the
	// page noun, pickers the picker noun; no derived duplicates.
	for _, d := range r.PickerDefs() {
		if d.Derived && (d.Noun == "ochazuke.page" || d.Noun == PickerNoun) {
			t.Fatalf("derived picker %q duplicates a declared one", d.Name)
		}
	}
	def, ok := r.PickerFor("task")
	if !ok || def.Name != "task" || !def.Derived {
		t.Fatalf("task picker = %+v %v", def, ok)
	}
	if _, ok := r.PickerDef("task"); !ok {
		t.Fatal("PickerDef(task)")
	}
}

// picker.open pickers lists every picker; picking one opens it.
func TestPickerOpenPickersAndPickOne(t *testing.T) {
	r, _ := newTaskRuntime(t)
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	m.Width, m.Height = 100, 30
	if _, err := r.Execute(Request{Noun: PickerNoun, Verb: "open", Args: Arguments{"name": PickersPickerName}}); err != nil {
		t.Fatal(err)
	}
	c := r.Chooser("picker-pickers")
	if c == nil {
		t.Fatalf("top modal %q", r.TopModal())
	}
	ids := chooserRowIDs(c)
	for _, want := range []string{PagesPickerName, PathPickerName, "task"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("pickers picker rows %v lack %q", ids, want)
		}
	}
	if slices.Contains(ids, PickersPickerName) {
		t.Fatalf("the pickers picker lists itself: %v", ids)
	}
	c.SetQuery("task")
	if res := c.pick(); res.err != nil {
		t.Fatal(res.err)
	}
	task := r.Chooser("picker-task")
	if task == nil {
		t.Fatalf("picking task opened %q", r.TopModal())
	}
	if got := chooserRowIDs(task); !slices.Equal(got, []string{"t1", "t2", "t3"}) {
		t.Fatalf("task picker rows %v", got)
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Water the plants") {
		t.Fatalf("task picker frame:\n%s", frame)
	}
}

// A derived picker previews its noun's get and picks with it (no open
// verb): the row's key reaches the subject parameter.
func TestDerivedPickerPreviewAndPick(t *testing.T) {
	r, s := newTaskRuntime(t)
	c, err := r.OpenNamedPicker("task", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.spec.Preview == nil {
		t.Fatal("derived picker has no preview")
	}
	row, _ := c.Highlighted()
	if text := c.spec.Preview(row); !strings.Contains(text, "title: Water the plants") {
		t.Fatalf("preview %q", text)
	}
	s.reads = nil
	c.SetQuery("notes")
	res := c.pick()
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !slices.Contains(s.reads, "t2") {
		t.Fatalf("pick read %v, want t2", s.reads)
	}
	if r.TopModal() != "" {
		t.Fatalf("picker stayed open: %q", r.TopModal())
	}
}

// picker.derive off leaves only the declared pickers; a noun opts out
// with NoPicker.
func TestPickerDeriveOptOut(t *testing.T) {
	r, _ := newTaskRuntime(t)
	r.Define("note").NoPicker().Props(PropSpec{Name: "id", Key: true}).Store(Store{List: func(Ctx, ListQuery) ([]Thing, error) { return nil, nil }})
	if names := pickerNames(r.PickerDefs()); slices.Contains(names, "note") || !slices.Contains(names, "task") {
		t.Fatalf("NoPicker: %v", names)
	}
	if err := r.Settings().Set(PickerDeriveSetting, false); err != nil {
		t.Fatal(err)
	}
	for _, d := range r.PickerDefs() {
		if d.Derived {
			t.Fatalf("picker.derive off still derives %q", d.Name)
		}
	}
}

// picker.picker-list declared hides the derived pickers from the pickers
// picker; picker.list (the standard list: the picker noun is a thing noun)
// still lists every one, and picker.get reads one.
func TestPickerListAndPickerListSetting(t *testing.T) {
	r, _ := newTaskRuntime(t)
	v, err := r.Execute(Request{Noun: PickerNoun, Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	choices, err := ParseChoices(v)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ch := range choices {
		names = append(names, ch.ID)
	}
	if !slices.Contains(names, "task") || !slices.Contains(names, PagesPickerName) {
		t.Fatalf("picker.list %v", names)
	}
	if err := r.Settings().Set(PickerListSetting, "declared"); err != nil {
		t.Fatal(err)
	}
	c, err := r.OpenNamedPicker(PickersPickerName, "")
	if err != nil {
		t.Fatal(err)
	}
	if ids := chooserRowIDs(c); slices.Contains(ids, "task") || !slices.Contains(ids, PagesPickerName) {
		t.Fatalf("declared list %v", ids)
	}
	got, err := r.Execute(Request{Noun: PickerNoun, Verb: "get", Args: Arguments{"name": "task"}})
	if err != nil {
		t.Fatal(err)
	}
	if thing, _ := got.(map[string]any); thing["noun"] != "task" || thing["derived"] != true {
		t.Fatalf("get = %#v", got)
	}
}

// A key bound to picker.open {name} shows as the picker's key; the
// pages picker opens the page picker through its verb.
func TestPickerKeysAndVerbPicker(t *testing.T) {
	r, _ := newTaskRuntime(t)
	r.Define("app").Mode("normal").Bind("X", Do(PickerNoun+".open", Arguments{"name": "task"}))
	def, _ := r.PickerDef("task")
	if keys := r.pickerKeys(def); !slices.Equal(keys, []string{"X"}) {
		t.Fatalf("task keys %v", keys)
	}
	if _, err := r.AddPage("one", "One", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenNamedPicker(PagesPickerName, ""); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != pagePickerID {
		t.Fatalf("pages opened %q", r.TopModal())
	}
}

// A declared picker: its source, match mode and pick verb; a Noun
// parameter's field chooser opens the noun's picker (picker.params).
func TestDeclaredPickerAndNounParameter(t *testing.T) {
	r, _ := newTaskRuntime(t)
	var picked []string
	r.Define("task").Verb("open-in", func(ctx Ctx) Result {
		picked = append(picked, StringArg(ctx.Args, "id", ""))
		return Done(nil)
	}).Params(map[string]Parameter{"id": {Type: "string", Required: true}}).
		Verb("todo", func(Ctx) Result { return Done([]string{"t1", "t3"}) })
	if err := r.AddPicker(PickerDef{Name: "todo", Noun: "task", Source: "task.todo", Pick: "task.open-in", Match: "regex"}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddPicker(PickerDef{Name: "bad", Noun: "task", Match: "glob"}); err == nil {
		t.Fatal("a bad match mode was accepted")
	}
	if err := r.AddPicker(PickerDef{Name: "nothing"}); err == nil {
		t.Fatal("a picker with no way to open was accepted")
	}
	if d, _ := r.PickerFor("task"); d.Name != "todo" {
		t.Fatalf("task's picker %q, want the declared todo", d.Name)
	}
	c, err := r.OpenNamedPicker("todo", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := chooserRowIDs(c); !slices.Equal(got, []string{"t1", "t3"}) || c.MatchMode() != "regex" {
		t.Fatalf("todo rows %v match %q", got, c.MatchMode())
	}
	if res := c.pick(); res.err != nil || !slices.Equal(picked, []string{"t1"}) {
		t.Fatalf("pick %v %v", picked, res.err)
	}

	field, err := r.openChoiceChooser(ChooserSpec{Kind: "field", Title: "Choose task", Choose: func(ChooserRow) Result { return Done(nil) }},
		choiceLoad{ctx: r.Context(), param: Parameter{Noun: "task"}, label: func(c Choice) string { return c.Label }, siblings: func() Arguments { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if got := chooserRowIDs(field); !slices.Equal(got, []string{"t1", "t3"}) || field.MatchMode() != "regex" {
		t.Fatalf("noun field rows %v match %q", got, field.MatchMode())
	}
	_ = field.close()

	if err := r.Settings().Set(PickerParamsSetting, false); err != nil {
		t.Fatal(err)
	}
	plain, err := r.openChoiceChooser(ChooserSpec{Kind: "field", Title: "Choose task", Choose: func(ChooserRow) Result { return Done(nil) }},
		choiceLoad{ctx: r.Context(), param: Parameter{Noun: "task"}, label: func(c Choice) string { return c.Label }, siblings: func() Arguments { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if got := chooserRowIDs(plain); len(got) != 3 || plain.MatchMode() == "regex" {
		t.Fatalf("picker.params off rows %v", got)
	}
}

// picker.open with no name opens the path picker on its find source,
// with or without the finder shim (NewPicker) and with no input in focus.
func TestPickerOpenWithoutName(t *testing.T) {
	for _, shim := range []bool{false, true} {
		r := NewRuntime()
		if shim {
			NewPicker(r)
		}
		if _, err := r.Execute(Request{Noun: PickerNoun, Verb: "open", Args: Arguments{"path": t.TempDir()}}); err != nil {
			t.Fatal(err)
		}
		c := r.Chooser(PathChooserKind)
		if r.TopModal() != PathChooserKind || c == nil || c.Source() != PathSourceFind {
			t.Fatalf("shim=%v: no name opened %q", shim, r.TopModal())
		}
		_ = r.Pop()
	}
	r := NewRuntime()
	if _, err := r.Execute(Request{Noun: PickerNoun, Verb: "open", Args: Arguments{"name": "nope"}}); err == nil {
		t.Fatal("an unknown picker opened")
	}
}
