package ochazuke

import "testing"

func TestModeIsFollowsExtends(t *testing.T) {
	r := NewRuntime()
	if err := r.DefineMode("jq", false, ModeExtends("insert")); err != nil {
		t.Fatal(err)
	}
	if err := r.DefineMode("jq-strict", false, ModeExtends("jq")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		mode, base string
		want       bool
	}{
		{"insert", "insert", true}, {"jq", "insert", true}, {"jq-strict", "insert", true},
		{"normal", "insert", false}, {"jq", "normal", false}, {"", "insert", false},
	} {
		if got := r.ModeIs(c.mode, c.base); got != c.want {
			t.Errorf("ModeIs(%q, %q) = %v", c.mode, c.base, got)
		}
	}
}

// A mode that extends insert is insert for the input: its typing is one
// undo change, and leaving it for a normal-like mode steps the cursor
// back as esc does.
func TestInsertLikeModeGroupsUndo(t *testing.T) {
	r := NewRuntime()
	if err := r.DefineMode("jq", false, ModeExtends("insert")); err != nil {
		t.Fatal(err)
	}
	if err := r.DefineMode("query", false, ModeExtends("normal")); err != nil {
		t.Fatal(err)
	}
	r.Instance("page")
	in := NewInput("line", "", false)
	if err := r.AddInput(in, Parent("page")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("") }, Focus("line")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("jq"); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{".", "a", "b"} {
		runInput(t, r, "insert", Arguments{"text": ch})
	}
	if err := r.SetMode("query"); err != nil {
		t.Fatal(err)
	}
	if in.Cursor != 2 {
		t.Fatalf("leaving jq for query: cursor %d, want 2 (stepped back)", in.Cursor)
	}
	runInput(t, r, "undo", nil)
	if string(in.Text) != "" {
		t.Fatalf("one undo after a jq session left %q", string(in.Text))
	}
}
