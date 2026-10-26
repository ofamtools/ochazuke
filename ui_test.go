package ochazuke

import (
	"fmt"
	"reflect"
	"testing"
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
