package ochazuke

import "testing"

// A standard delete asks what it will do (Store.DeleteKeeps): hide a thing
// of a remote source that cannot delete it, archive one of a local source,
// and delete a thing of the user's own as ever.
func TestOverlayDeletePromptSaysTheEffect(t *testing.T) {
	ask := func(t *testing.T, r *Runtime, noun, id string) string {
		t.Helper()
		if _, err := r.Execute(Request{Noun: noun, Verb: "delete", Args: Arguments{"id": id}}); err != nil {
			t.Fatal(err)
		}
		p := r.Instances[r.TopModal()].Payload.Prompt
		mustRun(t, r, Request{Noun: "ochazuke.modal", Verb: "pop"})
		return p
	}
	t.Run("hide", func(t *testing.T) {
		r, _ := farRuntime(t, 3)
		if got, want := ask(t, r, "far", "t00001"), "Hide far t00001? It stays in its source; undo shows it again."; got != want {
			t.Fatalf("%q, want %q", got, want)
		}
	})
	t.Run("archive instead", func(t *testing.T) {
		r := NewRuntime()
		r.Config().Dir = t.TempDir()
		r.Define("shelf").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel}).Store(Store{
			Get:  func(Ctx, string) (Thing, error) { return Thing{"id": "a", "name": "Alpha"}, nil },
			List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "a", "name": "Alpha"}}, nil },
		})
		if got, want := ask(t, r, "shelf", "a"), "Archive shelf Alpha? Its source keeps it; restore shows it again."; got != want {
			t.Fatalf("%q, want %q", got, want)
		}
	})
	t.Run("real delete", func(t *testing.T) {
		r, _ := farRuntime(t, 3)
		o := overlayStep(t, r, Request{Noun: "far", Verb: "create", Args: Arguments{"name": "Of mine"}})
		key := answerKey(r.Noun("far"), o.Value)
		if got, want := ask(t, r, "far", key), "Delete far Of mine?"; got != want {
			t.Fatalf("%q, want %q", got, want)
		}
	})
}
