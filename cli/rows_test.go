package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// A malformed line in the middle loses that line and nothing else.
func TestRowReaderSkipsAMalformedLine(t *testing.T) {
	in := `{"noun":"skill","id":"s1","label":"one"}
{"noun":"skill","id":"s2",
{"noun":"agent","id":"a1","label":"ada","data":{"path":"/a"}}

{"id":"no-noun"}
{"noun":"skill","id":"s3"}
`
	rr := NewRowReader(strings.NewReader(in))
	var got []Row
	for {
		rows, done, err := rr.Next(2, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) > 2 {
			t.Fatalf("batch of %d over max", len(rows))
		}
		got = append(got, rows...)
		if done {
			break
		}
	}
	ids := []string{}
	for _, r := range got {
		ids = append(ids, r.Noun+"/"+r.ID)
	}
	if strings.Join(ids, " ") != "skill/s1 agent/a1 skill/s3" || got[1].Data.(map[string]any)["path"] != "/a" {
		t.Fatalf("rows %+v", got)
	}
}

// A batch ends once wait has passed since its first line, so a slow
// writer's rows arrive before the stream ends.
func TestRowReaderBatchesByWait(t *testing.T) {
	pr, pw := io.Pipe()
	rr := NewRowReader(pr)
	go pw.Write([]byte(`{"noun":"n","id":"1"}` + "\n"))
	rows, done, err := rr.Next(100, 20*time.Millisecond)
	if err != nil || done || len(rows) != 1 {
		t.Fatalf("first batch %v %v %v", rows, done, err)
	}
	pw.Close()
	if rows, done, err := rr.Next(100, time.Second); err != nil || !done || len(rows) != 0 {
		t.Fatalf("end %v %v %v", rows, done, err)
	}
}

// Nothing decodable is the one decode failure.
func TestRowReaderNothingDecodable(t *testing.T) {
	_, done, err := NewRowReader(strings.NewReader("[1,2]\nnope\n")).Next(10, time.Second)
	if !done || !errors.Is(err, ErrNoRows) {
		t.Fatalf("done %v err %v", done, err)
	}
	if _, done, err := NewRowReader(strings.NewReader("")).Next(10, time.Second); !done || err != nil {
		t.Fatalf("empty stream: %v %v", done, err)
	}
}

// Lines print as JSON Lines: one compact value per line, no array.
func TestShellPrintsLines(t *testing.T) {
	var out bytes.Buffer
	sh := New(ochazuke.NewRuntime(), "tool", strings.NewReader(""), &out, &out)
	if err := sh.print(Lines{Row{Noun: "n", ID: "1", Label: "one"}, Row{Noun: "n", ID: "2"}}); err != nil {
		t.Fatal(err)
	}
	if want := `{"noun":"n","id":"1","label":"one"}` + "\n" + `{"noun":"n","id":"2"}` + "\n"; out.String() != want {
		t.Fatalf("got %q", out.String())
	}
}

// A synchronous verb's Lines survive the call's copy and print a line each.
func TestShellPrintsLinesFromAVerb(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Define("thing").Verb("state", func(ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done(Lines{Row{Noun: "thing", ID: "1"}})
	}, "rows")
	var out bytes.Buffer
	if err := New(r, "tool", strings.NewReader(""), &out, &out).Run(context.Background(), []string{"thing", "state"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != `{"noun":"thing","id":"1"}`+"\n" {
		t.Fatalf("got %q", out.String())
	}
}
