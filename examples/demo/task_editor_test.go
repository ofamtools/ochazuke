package demo

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/keytest"
)

// recordPage opens the demo on the Record page, its editor focused on the
// title in normal mode.
func recordPage(t *testing.T) (*ochazuke.Model, func(string) string) {
	t.Helper()
	m := New()
	m.Appearance.Theme = "mono"
	m.Width, m.Height = 140, 30
	if err := m.Runtime.OpenPage("task"); err != nil {
		t.Fatal(err)
	}
	m.View()
	if r := m.Runtime; r.Focus != "task-form/title" || r.Mode.Name != "normal" {
		t.Fatalf("the Record page opens on focus %s mode %s", r.Focus, r.Mode.Name)
	}
	palette := func(line string) string {
		keytest.Press(m, ":")
		keytest.Type(m, line)
		keytest.Press(m, "enter")
		return ansi.Strip(m.View().Content)
	}
	return m, palette
}

// A palette write of the task the open editor edits leaves the editor
// open on the field it was on, showing the new value (forms.outside-write
// reload, the default); the editor's next write keeps it.
func TestDemoEditorReloadsAfterOutsideWrite(t *testing.T) {
	m, palette := recordPage(t)
	r := m.Runtime
	frame := palette("task status done")
	if r.Focus != "task-form/title" || r.Mode.Name != "normal" || m.Status != "" {
		t.Fatalf("after :task status done: focus %s mode %s status %q", r.Focus, r.Mode.Name, m.Status)
	}
	if strings.Contains(frame, "The editor is closed") || !strings.Contains(frame, "done ▾") {
		t.Fatalf("the editor did not reload:\n%s", frame)
	}
	keytest.Press(m, "A")
	keytest.Type(m, "!")
	keytest.Press(m, "esc")
	frame = ansi.Strip(m.View().Content)
	if !strings.Contains(frame, `title="Water the plants!" status="done"`) {
		t.Fatalf("the editor's write put the old status back:\n%s", frame)
	}
}

// e edits again after any outside write: x twice (the status set to done
// twice), and with forms.outside-write close, where the write closes the
// editor and the footer drops the form's hints.
func TestDemoEditorReopensAfterOutsideWrite(t *testing.T) {
	for _, mode := range []string{"reload", "close"} {
		for _, keys := range [][]string{{"x"}, {"x", "x"}, {":task status done"}} {
			t.Run(mode+"/"+strings.Join(keys, "+"), func(t *testing.T) {
				m, palette := recordPage(t)
				r := m.Runtime
				if mode != "reload" { // the default
					if err := r.Settings().Set(ochazuke.FormsOutsideWriteSetting, mode); err != nil {
						t.Fatal(err)
					}
				}
				for _, k := range keys {
					if strings.HasPrefix(k, ":") {
						palette(k[1:])
					} else {
						keytest.Press(m, k)
					}
				}
				frame := ansi.Strip(m.View().Content)
				if mode == "close" {
					if r.Focus != "task" || !strings.Contains(frame, "e edits the record again") {
						t.Fatalf("focus %s, want the closed editor's page:\n%s", r.Focus, frame)
					}
					if strings.Contains(frame, "form.leave") || strings.Contains(frame, "form.undo") {
						t.Fatalf("the footer still shows the closed editor's hints:\n%s", frame)
					}
				}
				keytest.Press(m, "e")
				if r.Focus != "task-form/title" || m.Status != "" {
					t.Fatalf("e: focus %s status %q", r.Focus, m.Status)
				}
				if v, _ := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": "t1", "prop": "status"}}); v != "done" {
					t.Fatalf("status %v, want done", v)
				}
			})
		}
	}
}

// Every page on the Overview has its description line.
func TestDemoOverviewDescribesEveryPage(t *testing.T) {
	m := New()
	m.Width, m.Height = 140, 60
	frame := ansi.Strip(m.View().Content)
	lines := strings.Split(frame, "\n")
	for _, page := range m.Runtime.PageList() {
		for i, line := range lines {
			if strings.TrimSpace(line) == page.Title && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "" {
				t.Errorf("%s has no description line", page.Title)
			}
		}
	}
}
