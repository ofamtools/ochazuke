package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
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

// A row's tones (ochazuke.ToneSpan) ride every row decoder: DecodeRows
// (JSON Lines and an array of records), the row reader and a state
// answer; tones that do not decode in a record leave the row plain, and a
// record's tones are not part of its data.
func TestRowsCarryTones(t *testing.T) {
	want := []ochazuke.ToneSpan{{Cell: 1, Tone: "state.working"}, {Cell: 0, Tone: "marker.star", From: 0, To: 1}}
	line := `{"noun":"pane","id":"p1","label":"w","cells":["★ w","working"],"tones":[{"cell":1,"tone":"state.working"},{"cell":0,"tone":"marker.star","to":1}]}`
	rows, err := DecodeRows("pane", []byte(line+"\n"))
	if err != nil || len(rows) != 1 || !slices.Equal(rows[0].Tones, want) || rows[0].Data != nil {
		t.Fatalf("DecodeRows lines: %+v %v", rows, err)
	}
	rows, err = DecodeRows("pane", []byte(`[{"id":"p1","cells":["w","x"],"tones":[{"cell":1,"tone":"state.done"}]},{"id":"p2","tones":"bad"}]`))
	if err != nil || len(rows) != 2 || !slices.Equal(rows[0].Tones, []ochazuke.ToneSpan{{Cell: 1, Tone: "state.done"}}) || rows[1].Tones != nil {
		t.Fatalf("DecodeRows records: %+v %v", rows, err)
	}
	got, _, err := NewRowReader(strings.NewReader(line+"\n")).Next(5, time.Second)
	if err != nil || len(got) != 1 || !slices.Equal(got[0].Tones, want) {
		t.Fatalf("row reader: %+v %v", got, err)
	}
	a, err := DecodeState([]byte(`{"state":1,"cache":"c"}` + "\n" + `{"list":"pane","answer":"full","hash":"x","rows":1}` + "\n" + line + "\n"))
	if err != nil || len(a.Rows) != 1 || !slices.Equal(a.Rows[0].Tones, want) {
		t.Fatalf("state: %+v %v", a, err)
	}
	if out, _ := json.Marshal(Row{Noun: "pane", ID: "p"}); strings.Contains(string(out), "tones") {
		t.Fatalf("a row without tones writes none: %s", out)
	}
}
