package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Generated noun keys are for record nouns' rows. Inside a path picker (any
// style, normal mode) every key keeps meaning what the picker says: n new
// folder, u use this folder, R reveal, y copy path, and the record block's
// d d, y y, r, a answer nothing there.
func TestGeneratedKeysDoNotAnswerInAPicker(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			r, m, _, _ := styleFixture(t, style, styleDirPick(root), func(r *Runtime) {
				r.Config().Disable()
				r.Define("task").Props(
					PropSpec{Name: "id", Key: true},
					PropSpec{Name: "title", Role: RoleLabel},
					PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
					PropSpec{Name: "file", Parameter: Parameter{Type: PropPath}},
				).Store(DocStore(NewMemDocTable(r, "task"), "task"))
			}, nil)
			if style == PathStyleList {
				return // the list style opens in insert; its keys are alt chords
			}
			want := map[string]string{"n": "ochazuke.picker.new", "u": "ochazuke.picker.pick", "R": "ochazuke.picker.reveal", "y": "ochazuke.picker.yank-path"}
			got := map[string]string{}
			for _, b := range r.ListBindings(r.Focus) {
				got[b.Keys] = b.Request.Key()
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s in the picker: %q, want %s", k, got[k], v)
				}
			}
			for k, v := range got {
				if strings.HasPrefix(v, "task.") {
					t.Errorf("a task row answers in the picker: %s -> %s", k, v)
				}
			}
			_ = m
		})
	}
}

// The palette's own keys stay its own with the record block generated for
// every stored noun: no task row answers while the palette is open, in
// either style, and n r u a R d d y y are the palette's (or nothing's).
func TestGeneratedKeysDoNotAnswerInThePalette(t *testing.T) {
	for _, style := range []string{"classic", "board"} {
		t.Run(style, func(t *testing.T) {
			r, _ := scaffoldFixture(t)
			if err := r.Settings().Set("palette.style", style); err != nil {
				t.Fatal(err)
			}
			id := onlyTask(t, r)["id"].(string)
			if err := r.OpenIndex("task", id); err != nil {
				t.Fatal(err)
			}
			mustRun(t, r, Request{Noun: "ochazuke.palette", Verb: "open"})
			if !strings.Contains(r.Focus, "palette") && !strings.Contains(r.TopModal(), "palette") {
				t.Fatal("palette did not open")
			}
			for _, b := range r.ListBindings(r.Focus) {
				if b.Generated || strings.HasPrefix(b.Request.Key(), "task.") {
					t.Errorf("%s answers in the palette: %s -> %s", style, b.Keys, b.Request.Key())
				}
			}
		})
	}
}

// The generated a (associate) and annotate with no text are fill-in
// needs: a person who runs them is asked on the field (with the noun's
// picker for the other end), not refused; the CLI reads the plain error.
func TestScaffoldVerbsAskForWhatTheyLack(t *testing.T) {
	fixture := func(t *testing.T) (*Runtime, string) {
		r, _ := scaffoldFixture(t)
		r.SetRelationStore(NewMemRelations())
		id := onlyTask(t, r)["id"].(string)
		mustRun(t, r, Request{Noun: "note", Verb: "create", Args: Arguments{"title": "see", "about": "task:" + id}})
		return r, id
	}
	t.Run("a", func(t *testing.T) {
		r, _ := fixture(t)
		if err := r.OpenIndex("note", ""); err != nil {
			t.Fatal(err)
		}
		press(t, r, "a")
		if r.TopModal() != ArgsFormID || !strings.HasSuffix(r.Focus, "/to") {
			t.Fatalf("a on a note asks for the other end: modal %q focus %q", r.TopModal(), r.Focus)
		}
	})
	t.Run("annotate", func(t *testing.T) {
		r, id := fixture(t)
		if err := r.OpenIndex("task", id); err != nil {
			t.Fatal(err)
		}
		mustRun(t, r, Request{Noun: "ochazuke.palette", Verb: "open"})
		paletteLine(t, r, "task annotate")
		if r.TopModal() != ArgsFormID || !strings.HasSuffix(r.Focus, "/text") {
			t.Fatalf("annotate with no text asks for it: modal %q focus %q", r.TopModal(), r.Focus)
		}
	})
	t.Run("program", func(t *testing.T) {
		r, id := fixture(t)
		_, err := r.Execute(Request{Noun: "task", Verb: "annotate", Args: Arguments{"id": id, "text": "  "}})
		if err == nil || !strings.Contains(err.Error(), "text is required") {
			t.Fatalf("blank text from a program: %v", err)
		}
		if r.TopModal() != "" {
			t.Fatal("a program is never asked")
		}
	})
}

// show run at the UI opens the noun's index with the form focused, and
// that page is no tab (the opener stays marked); with no key it asks for
// the thing with the noun's picker (every scope), then shows it.
func TestShowAtTheUIAsksForTheKeyThenOpensTheIndex(t *testing.T) {
	r, m := scaffoldFixture(t)
	id := onlyTask(t, r)["id"].(string)
	mustRun(t, r, Request{Noun: "ochazuke.palette", Verb: "open"})
	paletteLine(t, r, "task show")
	if r.TopModal() != ArgsFormID || !strings.HasSuffix(r.Focus, "/id") {
		t.Fatalf("show with no key asks for it: modal %q focus %q", r.TopModal(), r.Focus)
	}
	for i := 0; i < 4 && r.TopModal() != ""; i++ {
		if err := r.CloseModal(r.Context()); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, r, Request{Noun: "ochazuke.palette", Verb: "open"})
	paletteLine(t, r, "task show "+id)
	if !strings.HasPrefix(r.Focus, IndexPageID("task")) {
		t.Fatalf("show opened %q, want the task index", r.Focus)
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "Work") {
		t.Fatalf("the opener's tab stays on the tab row:\n%s", v)
	}

}
