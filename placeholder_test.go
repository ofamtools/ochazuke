package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The one empty format (R5.19): lower case, no parentheses, the noun named.
func TestPlaceholderTexts(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{EmptyText(""), "no rows"},
		{EmptyText("panes"), "no panes"},
		{LoadingText(""), "loading…"},
		{LoadingText("panes"), "loading panes…"},
		{NoMatchText("panes", "x"), "no panes match 'x'"},
		{NoMatchText("", " "), "no rows match the filter"},
		{Placeholder{Plural: "panes"}.Text(nil), "no panes"},
		{Placeholder{Empty: "none yet"}.Text(nil), "none yet"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

// A list draws its Loading while the job in its LoadingJob slot runs and
// its Empty once the job is done: "loading" never reads as "empty".
func TestListPlaceholderLoadingThenEmpty(t *testing.T) {
	r := NewRuntime()
	list, err := r.AddList("l", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ansi.Strip(list.emptyLine()); got != "no rows" {
		t.Fatalf("default empty line %q", got)
	}
	list.SetPlaceholder(Placeholder{Plural: "directories", LoadingJob: JobSlot{Scope: "l", Slot: "fill"}})
	if got := ansi.Strip(list.emptyLine()); got != "no directories" {
		t.Fatalf("empty line before the job %q", got)
	}
	ticket, _, err := r.IssueAt("l", "l", "test.fill", "fill", Ctx{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ansi.Strip(list.View(40, 1)); !strings.HasPrefix(got, "loading directories…") {
		t.Fatalf("while the job runs %q", got)
	}
	r.Settle(ticket, nil)
	if got := ansi.Strip(list.View(40, 1)); !strings.HasPrefix(got, "no directories") {
		t.Fatalf("after the job %q", got)
	}
}

// A chooser opened before its rows arrive says Loading while its
// LoadingJob runs, then Empty, and never its Empty while loading.
func TestChooserLoadingThenEmpty(t *testing.T) {
	r, m, c, _ := chooserTest(t, ChooserSpec{
		Loading: "loading known directories…", Plural: "known directories",
		LoadingJob: JobSlot{Scope: "app", Slot: "dirs"},
	})
	ticket, _, err := r.IssueAt("app", "app", "test.dirs", "dirs", Ctx{})
	if err != nil {
		t.Fatal(err)
	}
	if frame := screen(m); !strings.Contains(frame, "loading known directories…") || strings.Contains(frame, "no known directories") {
		t.Fatalf("while loading:\n%s", frame)
	}
	r.Settle(ticket, nil)
	if frame := screen(m); !strings.Contains(frame, "no known directories") || strings.Contains(frame, "loading") {
		t.Fatalf("after loading:\n%s", frame)
	}
	if c.emptyLine() != "no known directories" {
		t.Fatalf("empty line %q", c.emptyLine())
	}
}
