package ochazuke

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Row files: config export's settings.reference.json and
// actions.example.json are JSON arrays written one row per line, so a row
// reads, diffs and greps as a line: "[" and "]" each on a line of their
// own, a row compact JSON with a space after every ":" and ",", rows
// separated by ",\n", "<", ">" and "&" written as themselves; an empty
// array is "[]"; a final newline. Bindings files have their own grouped
// form (FormatBindings).

// FormatRows writes any JSON array of rows (a slice of structs or
// objects) one row per line, as FormatBindingRows does, each row's fields
// in their marshalled order (a struct's declaration order).
func FormatRows(rows any) ([]byte, error) {
	data, err := marshalPlain(rows)
	if err != nil {
		return nil, err
	}
	return FormatRowsJSON(data)
}

// FormatRowsJSON rewrites a JSON array one element per line, keeping each
// element's key order; an error when data is not a JSON array.
func FormatRowsJSON(data []byte) ([]byte, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("not a JSON array of rows: %w", err)
	}
	lines := make([][]byte, len(items))
	for i, item := range items {
		var b bytes.Buffer
		if err := json.Compact(&b, item); err != nil {
			return nil, err
		}
		lines[i] = spaceJSON(b.Bytes())
	}
	return joinRows(lines), nil
}

// joinRows is the file: "[", the rows one per line, "]", a final newline.
func joinRows(lines [][]byte) []byte {
	if len(lines) == 0 {
		return []byte("[]\n")
	}
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, line := range lines {
		b.Write(line)
		if i < len(lines)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("]\n")
	return b.Bytes()
}

// orderedObject builds one JSON object with its keys in the order added.
type orderedObject struct {
	keys []string
	vals [][]byte
	err  error
}

// add appends key with value when keep is set.
func (o *orderedObject) add(key string, value any, keep bool) {
	if !keep || o.err != nil {
		return
	}
	data, err := marshalPlain(value)
	if err != nil {
		o.err = fmt.Errorf("%s: %w", key, err)
		return
	}
	o.raw(key, spaceJSON(data))
}

// raw appends key with an encoded value.
func (o *orderedObject) raw(key string, value []byte) {
	o.keys = append(o.keys, key)
	o.vals = append(o.vals, value)
}

func (o *orderedObject) bytes() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			b.WriteString(", ")
		}
		name, _ := marshalPlain(key)
		b.Write(name)
		b.WriteString(": ")
		b.Write(o.vals[i])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalPlain is compact JSON without HTML escaping (a key "<" stays
// "<") and without the encoder's trailing newline.
func marshalPlain(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// spaceJSON puts a space after every ":" and "," outside strings in
// compact JSON.
func spaceJSON(data []byte) []byte {
	var b bytes.Buffer
	b.Grow(len(data) + len(data)/8)
	inString, escaped := false, false
	for _, c := range data {
		b.WriteByte(c)
		switch {
		case inString && escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == ':' || c == ','):
			b.WriteByte(' ')
		}
	}
	return b.Bytes()
}
