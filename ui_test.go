package ochazuke

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestUIRingIsBoundedWithoutASink(t *testing.T) {
	r := NewRuntime()
	ui := r.UI()
	for i := range 1000 {
		ui.Notify(fmt.Sprint("n", i))
		if len(ui.ring) > uiRing {
			t.Fatalf("ring holds %d messages after %d posts", len(ui.ring), i+1)
		}
	}
	if len(ui.ring) != uiRing || ui.ring[0].Text != "n968" || ui.ring[uiRing-1].Text != "n999" {
		t.Fatalf("ring keeps the last %d: %d, first %q last %q", uiRing, len(ui.ring), ui.ring[0].Text, ui.ring[len(ui.ring)-1].Text)
	}
}

func TestUIReplaysTheRingInOrderIntoTheFirstSink(t *testing.T) {
	r := NewRuntime()
	a := r.UI().Notify("loading")
	r.UI().Status("starting")
	b := Ctx{Runtime: r}.Notify("mounted")
	r.UI().ClearNotice(a)
	sink := &RecordingSink{}
	if prev := r.UI().Attach(sink); prev != nil {
		t.Fatalf("previous sink %v", prev)
	}
	want := []UIMessage{
		{Kind: UINotice, ID: a, Text: "loading"},
		{Kind: UIStatus, Text: "starting"},
		{Kind: UINotice, ID: b, Text: "mounted"},
		{Kind: UIClearNotice, ID: a},
	}
	if got := sink.Messages(); !reflect.DeepEqual(got, want) {
		t.Fatalf("replay\n got %v\nwant %v", got, want)
	}
	if got := sink.Notices(); !reflect.DeepEqual(got, []string{"mounted"}) || sink.LastStatus() != "starting" {
		t.Fatalf("notices %q status %q", got, sink.LastStatus())
	}
	if r.UI().ring != nil {
		t.Fatalf("ring kept after the replay: %v", r.UI().ring)
	}
	// Live messages go straight to the sink, ids keep counting.
	c := Ctx{Runtime: r}.Notify("live")
	if c <= b || len(sink.Messages()) != 5 {
		t.Fatalf("live id %d after %d, %d messages", c, b, len(sink.Messages()))
	}
}

func TestUIDiscardsAfterTheFirstAttach(t *testing.T) {
	r := NewRuntime()
	r.UI().Notify("early")
	first := &RecordingSink{}
	r.UI().Attach(first)
	if prev := r.UI().Attach(nil); prev != first {
		t.Fatalf("detach returned %v", prev)
	}
	for range 100 {
		r.UI().Notify("lost")
	}
	if r.UI().ring != nil {
		t.Fatalf("ring refilled after the first attach: %d", len(r.UI().ring))
	}
	second := &RecordingSink{}
	r.UI().Attach(second)
	if len(second.Messages()) != 0 || len(first.Messages()) != 1 {
		t.Fatalf("second sink got %v, first %v", second.Messages(), first.Messages())
	}
}

func TestCtxWithoutRuntimeDiscards(t *testing.T) {
	var c Ctx
	if id := c.Notify("x"); id != 0 {
		t.Fatalf("id %d", id)
	}
	c.ClearNotice(1)
	c.Status("x")
}

// The Model is the sink: setup's messages are replayed into the footer,
// a key's handler notifies, clears and sets the status (which stands
// after the key), Model.Notify goes through the same path.
func TestModelIsTheUISink(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "alpha")
	early := r.UI().Notify("before the model")
	var posted NoticeID
	r.Define("app").
		Verb("tell", func(c Ctx) Result {
			posted = c.Notify("told")
			c.ClearNotice(early)
			c.Status("done telling")
			return Done(nil)
		}, "Tell the user").
		Mode("normal").Bind("T", Do("app.tell"))
	m := NewModel(r)
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 12})
	if !reflect.DeepEqual(m.Notices, []string{"before the model"}) {
		t.Fatalf("replayed notices %q", m.Notices)
	}
	m.Update(tea.KeyPressMsg{Code: 'T', Text: "T"})
	if !reflect.DeepEqual(m.Notices, []string{"told"}) || m.Status != "done telling" || !strings.Contains(m.footer().Text, "done telling") {
		t.Fatalf("after T: notices %q status %q footer %q", m.Notices, m.Status, m.footer().Text)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Status != "" {
		t.Fatalf("the next key keeps the status %q", m.Status)
	}
	id := m.Notify("from the model")
	r.UI().ClearNotice(posted)
	if !reflect.DeepEqual(m.Notices, []string{"from the model"}) || id <= posted {
		t.Fatalf("notices %q id %d", m.Notices, id)
	}
	// A notice the application appended itself has no id; clearing others
	// keeps it.
	m.Notices = append(m.Notices, "direct")
	r.UI().ClearNotice(id)
	if !reflect.DeepEqual(m.Notices, []string{"direct"}) {
		t.Fatalf("notices %q", m.Notices)
	}
	rec := &RecordingSink{}
	r.UI().Attach(rec)
	m.Notify("recorded")
	if !reflect.DeepEqual(rec.Notices(), []string{"recorded"}) || len(m.Notices) != 1 {
		t.Fatalf("Model.Notify bypassed the UI: %v %q", rec.Messages(), m.Notices)
	}
}

// The status line has two tones in one slot: ctx.Error (and ctx.Status,
// its older spelling) is "Error: TEXT" in the error style in place of the
// hints; ctx.Info is plain text before the hints. Either replaces the
// other, and the next key clears both.
func TestStatusLineTones(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "alpha")
	r.Define("app").
		Verb("info", func(c Ctx) Result { c.Info("copied 3 rows"); return Done(nil) }, "Say something plain").
		Verb("fail", func(c Ctx) Result { c.Error("could not copy"); return Done(nil) }, "Say an error").
		Verb("legacy", func(c Ctx) Result { c.Status("old error"); return Done(nil) }, "Say an error the old way").
		Mode("normal").Bind("I", Do("app.info")).Bind("F", Do("app.fail")).Bind("L", Do("app.legacy"))
	m := NewModel(r)
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 12})
	press := func(key rune) Footer {
		m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		return m.footer()
	}
	if f := press('I'); f.Error || !strings.HasPrefix(f.Text, "copied 3 rows · ") || !strings.Contains(f.Text, "app.info") || m.Status != "" {
		t.Fatalf("info: %+v status %q", f, m.Status)
	}
	if f := press('F'); !f.Error || f.Text != "Error: could not copy" || m.Info != "" {
		t.Fatalf("error: %+v info %q", f, m.Info)
	}
	if f := press('L'); !f.Error || f.Text != "Error: old error" {
		t.Fatalf("status is the error tone: %+v", f)
	}
	r.UI().Info("plain again")
	if f := m.footer(); f.Error || !strings.HasPrefix(f.Text, "plain again · ") || m.Status != "" {
		t.Fatalf("info replaces the error: %+v", f)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Info != "" || m.Status != "" || strings.Contains(m.footer().Text, "plain again") {
		t.Fatalf("the next key keeps the line: info %q status %q", m.Info, m.Status)
	}
}

// statusOnlySink is a UISink from before the neutral tone: no Info.
type statusOnlySink struct{ got []string }

func (s *statusOnlySink) Notify(NoticeID, string) {}
func (s *statusOnlySink) ClearNotice(NoticeID)    {}
func (s *statusOnlySink) Status(text string)      { s.got = append(s.got, text) }

// A sink without Info gets the neutral tone through Status, as before;
// RecordingSink keeps the tones apart; Line prints each as a host does.
func TestStatusInfoReachesEverySink(t *testing.T) {
	r := NewRuntime()
	old := &statusOnlySink{}
	r.UI().Attach(old)
	r.UI().Info("plain")
	r.UI().Error("bad")
	if !reflect.DeepEqual(old.got, []string{"plain", "bad"}) {
		t.Fatalf("status-only sink got %q", old.got)
	}
	rec := &RecordingSink{}
	r.UI().Attach(rec)
	Ctx{Runtime: r}.Info("plain")
	Ctx{Runtime: r}.Error("bad")
	if rec.LastInfo() != "plain" || rec.LastStatus() != "bad" {
		t.Fatalf("recorded info %q status %q", rec.LastInfo(), rec.LastStatus())
	}
	for _, c := range []struct {
		msg  UIMessage
		want string
	}{
		{UIMessage{Kind: UINotice, ID: 1, Text: "n"}, "notice: n"},
		{UIMessage{Kind: UIStatus, Text: "e"}, "error: e"},
		{UIMessage{Kind: UIInfo, Text: "i"}, "status: i"},
		{UIMessage{Kind: UIInfo}, ""},
		{UIMessage{Kind: UIClearNotice, ID: 1}, ""},
	} {
		if got := c.msg.Line(); got != c.want {
			t.Fatalf("%+v: line %q, want %q", c.msg, got, c.want)
		}
	}
}

// HandleCall returns what the call told the user with its result; the
// sink gets the messages too.
func TestHandleCallReturnsMessages(t *testing.T) {
	r := NewRuntime()
	rec := &RecordingSink{}
	r.UI().Attach(rec)
	r.Define("app").Verb("tell", func(c Ctx) Result {
		c.Notify("hello")
		c.Status("busy")
		return Done("ok")
	}, "Tell the user").Verb("quiet", func(Ctx) Result { return Done(nil) }, "Say nothing")
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Request: Request{Noun: "app", Verb: "tell"}, Reply: reply})
	res := <-reply
	want := []UIMessage{{Kind: UINotice, ID: 1, Text: "hello"}, {Kind: UIStatus, Text: "busy"}}
	if !reflect.DeepEqual(res.Messages, want) || !reflect.DeepEqual(rec.Messages(), want) || fmt.Sprintf("%s", res.Result) != `"ok"` {
		t.Fatalf("response %+v sink %v", res, rec.Messages())
	}
	r.HandleCall(CallMsg{Request: Request{Noun: "app", Verb: "nope"}, Reply: reply})
	if res := <-reply; res.Error == "" || res.Messages != nil {
		t.Fatalf("failed call %+v", res)
	}
	r.UI().Notify("outside a call")
	r.HandleCall(CallMsg{Request: Request{Noun: "app", Verb: "quiet"}, Reply: reply})
	if res := <-reply; res.Messages != nil {
		t.Fatalf("a call captured an earlier message: %+v", res.Messages)
	}
}

// Over the socket the messages travel with the result.
func TestSocketCallCarriesMessages(t *testing.T) {
	r := NewRuntime()
	r.Define("app").Verb("tell", func(c Ctx) Result {
		c.Notify("over the wire")
		return Done(1)
	}, "Tell the user")
	dir, err := os.MkdirTemp("", "oz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	server, err := Listen(path, r.HandleCall)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	res, err := CallResponse(context.Background(), path, Request{Noun: "app", Verb: "tell"})
	if err != nil || len(res.Messages) != 1 || res.Messages[0].Text != "over the wire" || fmt.Sprint(res.Result) != "1" {
		t.Fatalf("response %+v err %v", res, err)
	}
}
