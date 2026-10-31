package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// reviewFixture is the Settings page fixture with a session, two session
// values (theme over settings.json's light, scrolloff over the default)
// and settings.review on ctrl+r.
func reviewFixture(t *testing.T) (*Runtime, *Model, string) {
	t.Helper()
	r, m, dir := settingsPageFixture(t)
	s := r.Settings()
	if err := s.Set("theme", "light"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSession("scrolloff", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSession("theme", "mono"); err != nil {
		t.Fatal(err)
	}
	r.Define("app").Mode("normal").Bind("ctrl+r", Do("ochazuke.settings.review"))
	return r, m, dir
}

// settings.review lists each session value beside its saved one, the
// detail saying what writing changes; w writes the focused row, r reverts
// it, each dropping out, and the last one closes the review with a notice.
func TestSettingsReviewWritesAndRevertsOneByOne(t *testing.T) {
	r, m, dir := reviewFixture(t)
	keytest.Press(m, "ctrl+r")
	if r.TopModal() != settingsReviewID || r.Focus != settingChangePrefix+"scrolloff" {
		t.Fatalf("modal %q focus %q status %q", r.TopModal(), r.Focus, m.Status)
	}
	if n := r.Instances[settingsReviewID].Noun; n != settingsReviewNoun {
		t.Fatalf("body noun %q", n)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"Review settings", "2 session values differ", "Setting", "Saved", "Session", "scrolloff", "2 (default)", "5", "theme", "light (file)", "mono", "Writing adds 5 to settings.json", "w setting.write", "r setting.revert", "s skip", "W settings.write", "R settings.revert"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	keytest.Press(m, "s")
	if r.Focus != settingChangePrefix+"theme" {
		t.Fatalf("skip: focus %q", r.Focus)
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Writing puts mono in settings.json in place of light") {
		t.Fatalf("theme detail:\n%s", frame)
	}
	keytest.Press(m, "w")
	if got := readSettingsFile(t, dir)["theme"]; got != "mono" {
		t.Fatalf("w wrote theme %v", got)
	}
	if r.List(settingsReviewListID).Len() != 1 || r.Focus != settingChangePrefix+"scrolloff" {
		t.Fatalf("after w: %d rows, focus %q", r.List(settingsReviewListID).Len(), r.Focus)
	}
	keytest.Press(m, "r")
	if r.Settings().Int("scrolloff") != 2 || r.TopModal() != "" || r.Focus != "b" {
		t.Fatalf("after r: scrolloff %d modal %q focus %q", r.Settings().Int("scrolloff"), r.TopModal(), r.Focus)
	}
	if m.Info != "No session changes left to review" {
		t.Fatalf("info %q", m.Info)
	}
	if _, ok := readSettingsFile(t, dir)["scrolloff"]; ok {
		t.Fatal("r wrote scrolloff")
	}
}

// W writes every session value at once; R asks first, then reverts all.
func TestSettingsReviewAllAtOnce(t *testing.T) {
	r, m, dir := reviewFixture(t)
	keytest.Press(m, "ctrl+r")
	keytest.Press(m, "W")
	if got := readSettingsFile(t, dir); got["theme"] != "mono" || got["scrolloff"] != 5.0 || r.TopModal() != "" {
		t.Fatalf("W: file %v modal %q", got, r.TopModal())
	}

	r, m, dir = reviewFixture(t)
	keytest.Press(m, "ctrl+r")
	keytest.Press(m, "R")
	if r.TopModal() != "confirm" {
		t.Fatalf("R did not ask: %q", r.TopModal())
	}
	if p := r.Instances["confirm"].Payload.Prompt; p != "Revert 2 session values (scrolloff, theme) to the saved ones?" {
		t.Fatalf("prompt %q", p)
	}
	keytest.Press(m, "n")
	if r.TopModal() != settingsReviewID || r.Settings().Int("scrolloff") != 5 {
		t.Fatalf("n: modal %q", r.TopModal())
	}
	keytest.Press(m, "R")
	keytest.Press(m, "y")
	if r.TopModal() != "" || r.Settings().Int("scrolloff") != 2 || r.Settings().String("theme") != "light" || m.Info != "No session changes left to review" {
		t.Fatalf("R y: modal %q scrolloff %d theme %q info %q", r.TopModal(), r.Settings().Int("scrolloff"), r.Settings().String("theme"), m.Info)
	}
	if got := readSettingsFile(t, dir); got["theme"] != "light" {
		t.Fatalf("R wrote: %v", got)
	}
}

// q and esc close the review where it was opened, changing nothing.
func TestSettingsReviewCloses(t *testing.T) {
	for _, key := range []string{"q", "esc"} {
		r, m, _ := reviewFixture(t)
		keytest.Press(m, "ctrl+r")
		keytest.Press(m, key)
		if r.TopModal() != "" || r.Focus != "b" || len(r.Settings().Diff()) != 2 {
			t.Fatalf("%s: modal %q focus %q", key, r.TopModal(), r.Focus)
		}
	}
}

// With nothing to review it says so plainly and opens nothing; without a
// session (a CLI process) it says there is none and points at settings
// diff.
func TestSettingsReviewEmptyAndNoSession(t *testing.T) {
	r, m, _ := settingsPageFixture(t)
	r.Define("app").Mode("normal").Bind("ctrl+r", Do("ochazuke.settings.review"))
	keytest.Press(m, "ctrl+r")
	if r.TopModal() != "" || m.Info != "No session changes to review" || m.Status != "" {
		t.Fatalf("empty: modal %q info %q status %q", r.TopModal(), m.Info, m.Status)
	}
	plain, _, _ := settingsFixture(t)
	got, err := plain.Execute(Request{Noun: "ochazuke.settings", Verb: "review"})
	if text, _ := got.(string); err != nil || !strings.Contains(text, "no session to review") || !strings.Contains(text, "settings diff") {
		t.Fatalf("no session: %v %v", got, err)
	}
}

// A reset session value (the default over a saved value) says writing
// removes the key.
func TestSettingsReviewDetailOfAReset(t *testing.T) {
	r, m, _ := reviewFixture(t)
	if _, err := r.Settings().ResetSession("theme"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "ctrl+r")
	keytest.Press(m, "j")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Writing removes theme from settings.json, so the default dark applies") {
		t.Fatalf("reset detail:\n%s", frame)
	}
}
