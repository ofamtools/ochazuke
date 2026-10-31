package ochazuke

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInspectorSnapshotAndPanel(t *testing.T) {
	r, m, list, _ := motionPage(t)
	r.Config().Disable()
	list.SetItems([]ListItem{{ID: "path", Noun: "row", Label: ".items[3].owner.id"}})
	if err := r.SetFocus("path"); err != nil {
		t.Fatal(err)
	}
	if r.Settings().Bool(InspectorEnabledSetting) {
		t.Fatal("inspector must default off")
	}
	r.Define("row").Verb("copy-path", func(c Ctx) Result {
		if err := c.Runtime.StoreYank(Yank{Text: c.Instance.Label, Shape: "char"}, "a", false); err != nil {
			return Failed(err)
		}
		c.Info("copied path")
		return Done(c.Instance.Label)
	}, "Copy the row path")
	value, err := r.Execute(Request{Noun: "row", Verb: "copy-path"})
	if err != nil || value != ".items[3].owner.id" {
		t.Fatalf("%v %v", value, err)
	}
	r.AddInspectField("json.path", func() any { return list.Items()[0].Label })
	s := r.Inspect()
	if s.Mode != "normal" || s.DeclaredMode != "normal" || s.Focus[len(s.Focus)-1].Row != "path" {
		t.Fatalf("state: %+v", s)
	}
	if s.LastRequest.Key != "row.copy-path" || s.LastRequest.Result.Text != value || s.Message.Kind != UIInfo || s.Message.Text != "copied path" {
		t.Fatalf("outcome: %+v", s)
	}
	if s.YankHead == nil || s.YankHead.Text != value || len(s.Registers) != 3 {
		t.Fatalf("registers: %+v", s.Registers)
	}
	if !strings.Contains(s.Compact(), `register " = ".items[3].owner.id"`) {
		t.Fatal(s.Compact())
	}
	s.Fields["json.path"] = InspectValue{Text: "changed"}
	s.Focus[0].Instance = "changed"
	s.LastRequest.Key = "changed"
	if again := r.Inspect(); again.Fields["json.path"].Text != value || again.Focus[0].Instance == "changed" || again.LastRequest.Key == "changed" {
		t.Fatal("snapshot aliases runtime")
	}
	before := r.Inspect().LastRequest.Key
	got, err := r.Execute(Request{Noun: "ochazuke.state", Verb: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(got); err != nil {
		t.Fatal(err)
	}
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Request: Request{Noun: "ochazuke.state", Verb: "inspect"}, Reply: reply})
	response := <-reply
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	data, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var fromCLI Inspection
	if err := json.Unmarshal(data, &fromCLI); err != nil {
		t.Fatal(err)
	}
	if fromCLI.LastRequest.Key != before || fromCLI.Message.Tone != "info" {
		t.Fatal("CLI lost live inspection")
	}

	if r.Inspect().LastRequest.Key != before {
		t.Fatal("inspect hid the last action")
	}
	problems, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "check"})
	if err != nil || len(problems.([]string)) != 0 {
		t.Fatalf("bindings check: %v, %v", problems, err)
	}
	motionKeys(t, r, m, "alt+?")
	if !r.Settings().Bool(InspectorEnabledSetting) || !strings.Contains(m.inspectorLine(), ".items[3].owner.id") {
		t.Fatal("alt+? did not show inspector")
	}
	motionKeys(t, r, m, "alt+?")
	if m.inspectorLine() != "" {
		t.Fatal("alt+? did not hide inspector")
	}
}

func TestInspectorPendingSelectionAndMessages(t *testing.T) {
	r, m, _, _ := motionPage(t)
	r.Config().Disable()
	motionKeys(t, r, m, "V j")
	if s := r.Inspect(); s.Selection != 2 || s.Marks != 2 {
		t.Fatalf("selection %+v", s)
	}
	motionKeys(t, r, m, "esc 3")
	if r.Inspect().Count != 3 {
		t.Fatalf("count %+v", r.Inspect())
	}
	motionKeys(t, r, m, "esc g")
	if r.Inspect().Pending != "g" {
		t.Fatalf("pending %+v", r.Inspect())
	}
	r.UI().Attach(&RecordingSink{})
	r.UI().Notify("copied path")
	r.UI().Error("failed")
	if r.Inspect().Message.Kind != UIError {
		t.Fatal("lost error tone")
	}
	r.UI().Info("")
	if r.Inspect().Message.Text != "failed" {
		t.Fatal("clearing chrome erased the last message")
	}
	text := strings.Repeat("界", 300)
	r.StoreYank(Yank{Text: text, Shape: "char"}, "", false)
	if s := r.Inspect(); !s.Registers[0].Value.Truncated || len([]rune(s.Registers[0].Value.Text)) != 257 {
		t.Fatal("bad unicode truncation")
	}
}

func TestInspectorJobResultAndWrittenFiles(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.Define("app").Verb("fetch", func(Ctx) Result { return Pending(func(context.Context) (any, error) { return "loaded", nil }) }, "Fetch")
	v, err := r.Execute(Request{Noun: "app", Verb: "fetch"})
	if err != nil {
		t.Fatal(err)
	}
	ticket := v.(Ticket)
	if s := r.Inspect(); !s.LastRequest.Pending || len(s.Jobs) != 1 {
		t.Fatalf("job %+v", s)
	}
	if _, err := r.Deliver(Completion{Ticket: ticket, Value: "loaded"}); err != nil {
		t.Fatal(err)
	}
	if s := r.Inspect(); s.LastRequest.Pending || s.LastRequest.Result.Text != "loaded" || len(s.Jobs) != 0 {
		t.Fatalf("completion %+v", s)
	}
	r.Define("app").Verb("fail", func(Ctx) Result { return Failed(errors.New("read failed")) }, "Fail")
	r.Execute(Request{Noun: "app", Verb: "fail"})
	if r.Inspect().LastRequest.Error != "read failed" {
		t.Fatal("lost failure")
	}
	for range 2 {
		r.RecordWrittenFile("reshaped.json")
	}
	if s := r.Inspect(); len(s.FilesWritten) != 1 || !strings.Contains(s.Compact(), `wrote "reshaped.json"`) {
		t.Fatal(s.Compact())
	}
	written, err := r.ExportConfig(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written.Files) == 0 || len(r.Inspect().FilesWritten) != len(written.Files)+1 {
		t.Fatal("export writes were not reported")
	}
	r.AddInspectField("unsupported", func() any { return make(chan int) })
	if _, err := json.Marshal(r.Inspect()); err != nil {
		t.Fatal(err)
	}
}

func TestInspectorLayoutReservesOneRow(t *testing.T) {
	l := Layout{Width: 80, Height: 20, Styles: NewStyles(Appearance{Theme: "mono"})}
	w, h := l.BodySize()
	iw, ih := l.BodySizeWith(Footer{Inspector: "register"})
	if iw != w || ih != h-1 {
		t.Fatalf("%dx%d -> %dx%d", w, h, iw, ih)
	}
	block := l.Shell(Header{}, Text("body"), Footer{Inspector: `register " = "path"`})
	if len(strings.Split(block.Text, "\n")) != 20 || !strings.Contains(block.Text, "register") {
		t.Fatal(block.Text)
	}
}

func TestInspectorCompactKeepsControlTextInert(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.StoreYank(Yank{Text: "path\n\x1b[31m", Shape: "char"}, "", false)
	r.UI().Info("copied\npath\x1b[31m")
	r.AddInspectField("field\nname", func() any { return "value\nnext" })
	line := r.Inspect().Compact()
	if strings.ContainsAny(line, "\n\r\x1b") {
		t.Fatalf("unsafe compact line %q", line)
	}
}

func TestInspectorModelErrorsKeepTheirTone(t *testing.T) {
	r, m, _, _ := motionPage(t)
	r.Config().Disable()
	r.Define("row").Verb("fail", func(Ctx) Result { return Failed(errors.New("cannot copy path")) }, "Fail").Mode("normal").Bind("x", Do("row.fail"))
	m.handleKey(MacroKey{Key: "x"})
	if m.Status != "cannot copy path" || r.Inspect().Message.Tone != "error" {
		t.Fatal("model error missing from inspector")
	}
	m.handleKey(MacroKey{Key: "j"})
	if m.Status != "" || r.Inspect().Message.Text != "cannot copy path" {
		t.Fatal("next key erased the diagnostic")
	}
}

func TestInspectorBoundsResultTraversal(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	encoded := 0
	r.Define("app").Verb("result", func(Ctx) Result {
		return Done(map[string]any{"rows": make([]countedData, 10000), "small": countedData{n: &encoded}})
	}, "Large result")
	if _, err := r.Execute(Request{Noun: "app", Verb: "result"}); err != nil {
		t.Fatal(err)
	}
	if encoded != 0 {
		t.Fatal("inspector invoked application MarshalJSON")
	}
	if got := r.Inspect().LastRequest.Result; !got.Truncated || !strings.Contains(got.Text, "10000 items") {
		t.Fatalf("bad preview %+v", got)
	}
}
