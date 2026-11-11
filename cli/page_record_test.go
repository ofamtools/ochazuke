package cli

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountedOpenRowSelectsRequestedRecordAcrossReload(t *testing.T) {
	t.Parallel()
	doc := pageDoc(LayoutSplit, true)
	cfg := PageConfig{Fetch: answers(pageAnswers), FetchRecord: func(_ context.Context, noun, key string) ([]byte, error) {
		return []byte(fmt.Sprintf(`{"id":%q,"path":%q,"origin":"opened"}`, key, "/real/"+key)), nil
	}}
	m, v := mountedPage(t, doc, cfg, 100, 30)
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := v.OpenRow("skill", "s2"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := v.List("ai/skills").Selected(); !ok || got.ID != "ai/skills/s2" {
		t.Fatalf("selection: %+v %v", got, ok)
	}
	if m.Runtime.Focus != "ai/skills/s2" {
		t.Fatalf("focus: %s", m.Runtime.Focus)
	}
	if err := v.OpenRow("skill", "outside"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := v.List("ai/skills").Selected(); !ok || got.ID != "ai/skills/outside" {
		t.Fatalf("explicit row lost on reload: %+v %v", got, ok)
	}
	if f := frameOf(m); !strings.Contains(f, "/real/outside") {
		t.Fatalf("detail did not show fetched record:\n%s", f)
	}
	if err := v.OpenRow("missing", "s2"); err == nil {
		t.Fatal("absent noun was accepted")
	}
}

func TestMountedDetailFetchesOnlySelectedMissingFields(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cfg := PageConfig{FetchRecord: func(_ context.Context, noun, key string) ([]byte, error) {
		calls.Add(1)
		return []byte(fmt.Sprintf(`{"id":%q,"path":%q,"origin":"selected"}`, key, "/record/"+key)), nil
	}}
	m, v := mountedPage(t, pageDoc(LayoutSplit, true), cfg, 100, 30)
	if err := v.SetRows("ai/skills", []Row{{Noun: "skill", ID: "s1", Label: "one"}, {Noun: "skill", ID: "s2", Label: "two"}}); err != nil {
		t.Fatal(err)
	}
	frameOf(m)
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if f := frameOf(m); !strings.Contains(f, "/record/s1") {
		t.Fatalf("missing selected detail:\n%s", f)
	}
	frameOf(m)
	if calls.Load() != 1 {
		t.Fatalf("read all rows or reread every frame: %d", calls.Load())
	}
	if err := m.Runtime.SetFocus("ai/skills/s2"); err != nil {
		t.Fatal(err)
	}
	frameOf(m)
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if f := frameOf(m); !strings.Contains(f, "/record/s2") || calls.Load() != 2 {
		t.Fatalf("selection did not hydrate independently (%d):\n%s", calls.Load(), f)
	}
}

func TestMountedOpenRowDoesNotStealAnotherPagesFocus(t *testing.T) {
	t.Parallel()
	m, v := mountedPage(t, pageDoc(LayoutSplit, true), PageConfig{FetchRecord: func(_ context.Context, noun, key string) ([]byte, error) {
		return []byte(`{"id":"s2","path":"/real/s2"}`), nil
	}}, 100, 30)
	if err := v.OpenRow("skill", "s2"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Runtime.AddPage("other", "Other", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("Other") }); err != nil {
		t.Fatal(err)
	}
	if err := m.Runtime.OpenPage("other"); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if m.Runtime.Page != "other" || m.Runtime.PageOf("ai") == nil {
		t.Fatalf("late open stole page: %s", m.Runtime.Page)
	}
}
