//go:build !ochazuke_strict

package ochazuke

import (
	"reflect"
	"testing"
)

// The deprecated names keep working for their last release: Status is
// the error tone, UIStatus is UIError, LastStatus reads LastError, and
// a sink typed as InfoSink is still accepted.
func TestDeprecatedStatusNamesStillWork(t *testing.T) {
	r := NewRuntime()
	rec := &RecordingSink{}
	r.UI().Attach(rec)
	r.UI().Status("ui")
	Ctx{Runtime: r}.Status("ctx")
	if want := []UIMessage{{Kind: UIStatus, Text: "ui"}, {Kind: UIError, Text: "ctx"}}; !reflect.DeepEqual(rec.Messages(), want) {
		t.Fatalf("recorded %v", rec.Messages())
	}
	if rec.LastStatus() != "ctx" || rec.LastStatus() != rec.LastError() {
		t.Fatalf("LastStatus %q LastError %q", rec.LastStatus(), rec.LastError())
	}
	var _ InfoSink = rec
}
