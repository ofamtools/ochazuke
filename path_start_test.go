package ochazuke

import "testing"

// startFixture registers a "project" path source (an application's own
// start) and a runtime with a page for the record editor.
func startFixture(t *testing.T, start string) (*Runtime, *Model, *RecordEditor) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	root := pathTree(t)
	r.EnsureConfigSettings()
	r.AddPathSource("project", func(p PathPick) (ChooserSource, bool) {
		return ChooserSource{Load: func(string) Result {
			return Done([]ChooserRow{{ID: "p", Label: "p", Data: root}})
		}}, p.Dir()
	})
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	fields := []RecordField{{Name: "folder", Parameter: Parameter{Type: PathParameterType, Path: PathDir, Base: root, Start: start}}}
	ed, err := r.AddRecordEditor("ed", RecordSpec{Fields: fields, Save: func(RecordWrite) Result { return Done(nil) }}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.Focus(); err != nil {
		t.Fatal(err)
	}
	m.View()
	return r, m, ed
}

// A path parameter's Start names the source its chooser opens on, in the
// record editor and in the args form's lone-path chooser; an unknown name
// or none opens on the default (browse), and a PathChooserSpec.Source wins.
func TestPathStartDeclaresSource(t *testing.T) {
	for _, tc := range []struct{ start, want string }{
		{"project", "project"},
		{"recent", PathSourceRecent},
		{"nonesuch", PathSourceBrowse},
		{"", PathSourceBrowse},
	} {
		r, m, ed := startFixture(t, tc.start)
		if err := ed.choosePath(ed.field("folder")); err != nil {
			t.Fatal(err)
		}
		landLoads(m, r.PendingCommands())
		if c := r.Chooser(PathChooserKind); c == nil || c.Source() != tc.want {
			t.Fatalf("record editor, Start %q: opened on %v, want %q", tc.start, c, tc.want)
		}
		if err := r.Pop(); err != nil {
			t.Fatal(err)
		}
		f := RecordField{Name: "folder", Parameter: Parameter{Type: PathParameterType, Path: PathDir, Start: tc.start}}
		if err := r.choosePathArg(Request{}, f, "t", func(Request) error { return nil }); err != nil {
			t.Fatal(err)
		}
		landLoads(m, r.PendingCommands())
		if c := r.Chooser(PathChooserKind); c == nil || c.Source() != tc.want {
			t.Fatalf("args form, Start %q: opened on %v, want %q", tc.start, c, tc.want)
		}
		if err := r.Pop(); err != nil {
			t.Fatal(err)
		}
	}
	r, m, _ := startFixture(t, "project")
	c, err := r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Parameter: Parameter{Type: PathParameterType, Path: PathDir, Start: "project"}}, Source: PathSourceRecent})
	if err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	if c.Source() != PathSourceRecent {
		t.Fatalf("spec.Source over Start: %q", c.Source())
	}
}
