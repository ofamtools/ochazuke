package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// The catalog lists the segments a tool answers from its verbs, never a
// provider's or a command's; a host mounts them against its own nouns,
// prefixed, and they run against the mounted verb.
func TestCatalogSegmentsRoundTrip(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("usage").Verb("brief", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("3 runs") }, "One line of usage")
	r.SegmentProvider("p", func(context.Context, ochazuke.Segment) (ochazuke.SegmentValue, error) {
		return ochazuke.SegmentValue{}, nil
	})
	if err := r.AddSegment(
		ochazuke.Segment{ID: "usage", Label: "ai", Verb: "usage.brief", Every: 30 * time.Second, Tone: ochazuke.ToneDone},
		ochazuke.Segment{ID: "local", Provider: "p"},
		ochazuke.Segment{ID: "cmd", Command: ochazuke.Argv{"date"}},
	); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(r, "tool", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Segments) != 1 || doc.Segments[0].ID != "usage" || doc.Segments[0].Verb != "usage.brief" || doc.Segments[0].Every != "30s" {
		t.Fatalf("catalog segments = %+v", doc.Segments)
	}
	rows := CatalogSegments(doc, "ai", func(n string) string { return "ext-ai." + n })
	if len(rows) != 1 || rows[0].ID != "ai-usage" || rows[0].Verb != "ext-ai.usage.brief" || rows[0].Every != 30*time.Second || rows[0].Command != nil {
		t.Fatalf("mounted rows = %+v", rows)
	}
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	host.Define("ext-ai.usage").Verb("brief", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("3 runs") }, "Proxy")
	if err := host.AddSegment(rows...); err != nil {
		t.Fatal(err)
	}
	if got := host.Segments(); len(got) != 1 || got[0].Error != "" || got[0].Source != "verb" {
		t.Fatalf("host segments = %+v", got)
	}
}
