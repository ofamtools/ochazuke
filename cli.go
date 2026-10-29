package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func decodeRequest(data []byte, q *Request) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(q); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("expected exactly one JSON request")
	}
	if q.Empty() || q.Noun == "" {
		return errors.New("noun and a verb or prop are required")
	}
	return nil
}

// ParseArguments parses the same JSON object used by the wire API.
func ParseArguments(raw string) (Arguments, error) {
	var args Arguments
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("arguments: %w", err)
	}
	if args == nil {
		return nil, errors.New("arguments must be a JSON object")
	}
	return args, nil
}
