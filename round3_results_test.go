package ochazuke

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func round3Frame(t *testing.T, name string, m *Model) string {
	t.Helper()
	frame := ansi.Strip(m.View().Content)
	if dir := os.Getenv("ROUND3_FRAMES"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(frame), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return frame
}

func TestRound3MessagesAndWrap(t *testing.T) {
	r, m, _, _ := motionPage(t)
	m.Width, m.Height = 100, 30
	text := "/tmp/" + strings.Repeat("long-path/", 12) + ": No such file or directory"
	r.UI().Error(text)
	if frame := round3Frame(t, "status-cut-full-text", m); !strings.Contains(frame, "g <") {
		t.Fatal(frame)
	}
	r.Settings().Set("messages.hint", false)
	if frame := round3Frame(t, "status-hint-off", m); strings.Contains(frame, "g <") {
		t.Fatal(frame)
	}
	r.Settings().Set("messages.hint", true)
	motionKeys(t, r, m, "j g <")
	if frame := round3Frame(t, "messages-full-text", m); !strings.Contains(frame, "No such file or directory") {
		t.Fatal(frame)
	}
	if in := r.Input(r.Focus); in == nil || !strings.Contains(string(in.Text), text) {
		t.Fatal("full message lost")
	}
	editorTestKeys(t, r, "q")
	r.OpenTextView("failure", text)
	if frame := round3Frame(t, "long-line-wraps", m); !strings.Contains(frame, "No such file or directory") {
		t.Fatal(frame)
	}
	r.Settings().Set("text-view.wrap", false)
	if frame := round3Frame(t, "long-line-nowrap-marker", m); !strings.Contains(frame, "›") {
		t.Fatal(frame)
	}
	for range uiRing + 1 {
		r.UI().Info("new")
	}
	if len(r.UI().messages) != uiRing || strings.Contains(r.UI().messages[0].Text, "long-path") {
		t.Fatal("unbounded messages")
	}
}

func TestRound3PalettePendingInsideHelpAndLate(t *testing.T) {
	for _, change := range []string{"none", "focus", "mode", "modal"} {
		t.Run(change, func(t *testing.T) {
			r, m, _, _ := motionPage(t)
			m.Width, m.Height = 100, 30
			r.Define("app").Verb("later", func(Ctx) Result {
				return Pending(func(context.Context) (any, error) { return "readable delayed answer", nil })
			}).TextResult()
			if change == "none" {
				editorTestKeys(t, r, "?")
			}
			under := r.TopModal()
			runPaletteResults(t, r, "app later")
			switch change {
			case "focus":
				editorTestKeys(t, r, "j")
			case "mode":
				r.SetMode("insert")
			case "modal":
				editorTestKeys(t, r, "?")
			}
			for _, msg := range runCmd(r.PendingCommands()) {
				if _, err := r.Deliver(msg); err != nil {
					t.Fatal(err)
				}
			}
			if change == "none" {
				if r.TopModal() != TextViewID {
					t.Fatal(r.TopModal())
				}
				round3Frame(t, "palette-pending-inside-help", m)
				editorTestKeys(t, r, "q")
				if r.TopModal() != under {
					t.Fatal("lost underlying help")
				}
			} else {
				if r.TopModal() == TextViewID {
					t.Fatal("late answer covered changed interaction")
				}
				if !strings.Contains(m.Info, "answer not opened") || !strings.Contains(r.UI().messages[len(r.UI().messages)-1].Text, "readable delayed answer") {
					t.Fatal("lost dropped answer", m.Info)
				}
				round3Frame(t, "palette-late-answer-"+change, m)
			}
		})
	}
}

func TestRound3SingleFlightAndCancel(t *testing.T) {
	r, _ := textResultFixture(t)
	calls := 0
	r.Define("record").Verb("work", func(Ctx) Result {
		calls++
		return Pending(func(ctx context.Context) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	})
	op := r.Operations["record.work"]
	op.Slot = "run"
	r.Operations[op.Key()] = op
	q := Request{Noun: "record", Verb: "work"}
	v, err := r.Execute(q)
	if err != nil {
		t.Fatal(err)
	}
	ticket := v.(Ticket)
	if _, err = r.Execute(q); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("ran twice")
	}
	if _, err = r.Execute(Request{Noun: "ochazuke.job", Verb: "cancel", Args: Arguments{"job": int(ticket.Job)}}); err != nil {
		t.Fatal(err)
	}
	if r.Accepts(ticket) {
		t.Fatal("cancel did not settle job")
	}
	for _, msg := range runCmd(r.PendingCommands()) {
		r.Deliver(msg)
	}
}

func TestRound3TextTitlePolicy(t *testing.T) {
	r, m, _, _ := motionPage(t)
	r.Define("app").Verb("answer", func(Ctx) Result { return Done(nil) }).TextResult()
	q := Request{Noun: "app", Verb: "answer"}
	answer := TextAnswer{Title: "record · exit 3", Text: "readable body"}
	if _, err := r.PresentResult(q, answer); err != nil {
		t.Fatal(err)
	}
	if frame := round3Frame(t, "data-title", m); !strings.Contains(frame, "record · exit 3") {
		t.Fatal(frame)
	}
	r.Settings().Set("text-view.result-title", false)
	if _, err := r.PresentResult(q, answer); err != nil {
		t.Fatal(err)
	}
	if frame := round3Frame(t, "data-title-off", m); strings.Contains(frame, "record · exit 3") || !strings.Contains(frame, "app.answer") {
		t.Fatal(frame)
	}
}
