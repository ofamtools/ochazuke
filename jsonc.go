package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tailscale/hujson"
)

// Config files are JSONC: JSON with // line comments, /* block */
// comments and trailing commas (github.com/tailscale/hujson). Every config
// file ochazuke reads (bindings.json, actions.json, settings.json,
// themes.json, bindings load, the embedded binding specs) goes through
// StandardizeJSONC or DecodeJSONC; the files keep their .json names.

// StandardizeJSONC is data as standard JSON: each comment and trailing
// comma becomes spaces, so every byte offset (and so every line and
// column) is the original file's. Plain JSON is returned as is, without a
// copy. A file that is not JSONC is an error naming its line and column.
func StandardizeJSONC(data []byte) ([]byte, error) {
	// Fast path: without a "/" there is no comment, and json.Valid is
	// several times cheaper than a parse (BenchmarkStandardizeJSONC); a
	// trailing comma, or a syntax error, fails it and takes the slow path.
	if bytes.IndexByte(data, '/') < 0 && json.Valid(data) {
		return data, nil
	}
	// Standardize blanks comments in the buffer it parses: a copy, so the
	// caller's data (a file it may write back, an embedded spec) stays.
	std, err := hujson.Standardize(bytes.Clone(data))
	if err != nil {
		return nil, errors.New(strings.TrimPrefix(err.Error(), "hujson: "))
	}
	return std, nil
}

// DecodeJSONC decodes a JSONC document into v. A syntax error or a value
// of the wrong type names its line and column in data.
func DecodeJSONC(data []byte, v any) error {
	std, err := StandardizeJSONC(data)
	if err != nil {
		return err
	}
	return jsonPositionError(data, json.Unmarshal(std, v))
}

// jsonPositionError adds the line and column of a json syntax or type
// error to its message; offsets are the original file's (StandardizeJSONC
// keeps them). Any other error, or nil, is returned as is.
func jsonPositionError(data []byte, err error) error {
	var offset int64 = -1
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		offset = syntax.Offset
	case errors.As(err, &typed):
		offset = typed.Offset
	}
	if offset < 0 || offset > int64(len(data)) {
		return err
	}
	line, column := lineColumn(data, int(offset))
	return &jsoncError{line: line, column: column, err: err}
}

// jsoncError is a decode error at a line and column of the original file;
// errors.As still reaches the json error inside.
type jsoncError struct {
	line, column int
	err          error
}

func (e *jsoncError) Error() string {
	return fmt.Sprintf("line %d, column %d: %v", e.line, e.column, e.err)
}

func (e *jsoncError) Unwrap() error { return e.err }

// lineColumn is the 1-based line and column of byte offset n in data.
func lineColumn(data []byte, n int) (line, column int) {
	line = 1 + bytes.Count(data[:n], []byte("\n"))
	column = 1 + n - (bytes.LastIndexByte(data[:n], '\n') + 1)
	return line, column
}
