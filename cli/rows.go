package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// Row is one line of a state answer (CatalogDoc.State) or of a list asked
// for JSON Lines: the list noun it belongs to, its id (what --target
// takes), its label and, optionally, the record and its table cells (one
// per column its noun declares, CatalogNoun.Columns; ochazuke.ListItem.Cells).
//
//	{"noun":"skill","id":"skill/s1","label":"review [claude]","data":{...}}
//	{"noun":"pane","id":"w1","label":"work/claude","cells":["work/claude","idle","~/proj"]}
//	{"noun":"pane","id":"w2","label":"work/codex","cells":["work/codex","waiting"],"tones":[{"cell":1,"tone":"state.needs-input"}]}
//
// Tones give cells, or runs inside them, a named tone the host's theme
// colours (ochazuke.ToneSpan, ochazuke.ListItem.Tones); optional, and a
// host that does not know them draws the cells plain.
type Row struct {
	Noun  string              `json:"noun"`
	ID    string              `json:"id"`
	Label string              `json:"label,omitempty"`
	Data  any                 `json:"data,omitempty"`
	Cells []string            `json:"cells,omitempty"`
	Tones []ochazuke.ToneSpan `json:"tones,omitempty"`
}

// Lines is a result the shell prints as JSON Lines: one compact JSON value
// per line, no outer array (ochazuke.Lines).
type Lines = ochazuke.Lines

// ErrNoRows is Next's error at the end of a stream that had lines but not
// one decodable Row: the answer is not rows at all.
var ErrNoRows = errors.New("no line decodes as a row ({noun, id, label})")

// RowReader decodes a JSON Lines stream of Rows incrementally. A line that
// is not a Row with a noun and an id is skipped silently, never an error;
// only the stream's read error, or a stream of nothing but such lines
// (ErrNoRows), is one.
//
// A delta answer (the state delta protocol, StateDeltaVersion: its first
// line is the header) is read too: Next hands out its rows as they come,
// and Answer, once the stream is done, is the whole answer (its lists,
// rows and record hashes) for PageView.ApplyState. Its header and list
// lines are not rows and not skipped: an answer of unchanged lists has no
// rows and is no error.
type RowReader struct {
	lines            chan []byte
	err              error
	decoded, skipped int
	started          bool
	answer           StateAnswer
}

// Answer is what the stream answered so far: a delta answer's lists, rows
// and record hashes (Delta true), else Delta false and no rows (a plain
// answer's rows are Next's).
func (rr *RowReader) Answer() StateAnswer { return rr.answer }

// NewRowReader starts reading r; the caller drains it with Next until done.
func NewRowReader(r io.Reader) *RowReader {
	rr := &RowReader{lines: make(chan []byte, 256)}
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
		for sc.Scan() {
			rr.lines <- append([]byte(nil), sc.Bytes()...)
		}
		rr.err = sc.Err()
		close(rr.lines)
	}()
	return rr
}

// Next returns the next batch: up to max rows, or fewer once wait has
// passed since the batch's first line, and done when the stream ended
// (err is then the stream's read error, if any). A batch may be empty only
// when done.
func (rr *RowReader) Next(max int, wait time.Duration) (rows []Row, done bool, err error) {
	var timeout <-chan time.Time
	for len(rows) < max {
		select {
		case line, ok := <-rr.lines:
			if !ok {
				if rr.err == nil && rr.decoded == 0 && rr.skipped > 0 && !rr.answer.Delta {
					return rows, true, ErrNoRows
				}
				return rows, true, rr.err
			}
			if timeout == nil {
				timeout = time.After(wait)
			}
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			first := !rr.started
			rr.started = true
			if rr.answer.Delta || first {
				l, ok := decodeStateLine(line, rr.answer.Delta)
				switch {
				case ok && l.header != nil && first:
					rr.answer.Delta, rr.answer.Cache = true, l.header.Cache
					continue
				case ok && l.list != nil && rr.answer.Delta:
					rr.answer.Lists = append(rr.answer.Lists, *l.list)
					continue
				case ok && l.row != nil:
					if rr.answer.Delta {
						rr.answer.Rows = append(rr.answer.Rows, *l.row)
						rr.answer.Hashes = append(rr.answer.Hashes, l.hash)
					}
					rr.decoded++
					rows = append(rows, *l.row)
					continue
				}
				rr.skipped++
				continue
			}
			var row Row
			if json.Unmarshal(line, &row) != nil || row.Noun == "" || row.ID == "" {
				rr.skipped++
				continue
			}
			rr.decoded++
			rows = append(rows, row)
		case <-timeout:
			return rows, false, nil
		}
	}
	return rows, false, nil
}
