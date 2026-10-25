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
type Row struct {
	Noun  string   `json:"noun"`
	ID    string   `json:"id"`
	Label string   `json:"label,omitempty"`
	Data  any      `json:"data,omitempty"`
	Cells []string `json:"cells,omitempty"`
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
type RowReader struct {
	lines            chan []byte
	err              error
	decoded, skipped int
}

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
				if rr.err == nil && rr.decoded == 0 && rr.skipped > 0 {
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
