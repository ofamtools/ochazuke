// Package jsontree parses JSON into an ordered tree for structure and value views.
package jsontree

// The document model, copied from o-json/doc.go (itself from gmtm's jsonexplorer) and
// adapted: a Node tree that keeps object keys in document order, a type
// label per node, Pretty, and a ParseError with a 1-based line and column.
// Changes from the original: paths are jq style (".items[0].owner") and
// computed on demand instead of stored per node, a node keeps its value
// once (Value, unquoted for strings) and quotes it when asked, strings are
// quoted as JSON (not Go), and a node marshals back to JSON in key order.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the JSON type of a node.
type Kind string

const (
	KString Kind = "string"
	KNumber Kind = "number"
	KBool   Kind = "bool"
	KNull   Kind = "null"
	KObject Kind = "object"
	KArray  Kind = "array"
)

// Node is one value in the document.
type Node struct {
	Key      string // object key; "" for array items and the root
	Index    int    // position among the parent's children
	Kind     Kind
	Value    string // scalars: the literal (strings unquoted)
	Children []*Node
	Parent   *Node
	Depth    int
	// Repeats is how many times the object holding this node named its
	// key, 0 when once: the parser reads a repeated key as jq does, the
	// last value at the first key's position.
	Repeats int
}

// IsContainer reports an object or an array.
func (n *Node) IsContainer() bool { return n.Kind == KObject || n.Kind == KArray }

// TypeLabel is the value-type text: string, number, bool, null, object{n}, array[n].
func (n *Node) TypeLabel() string {
	switch n.Kind {
	case KObject:
		return fmt.Sprintf("object{%d}", len(n.Children))
	case KArray:
		return fmt.Sprintf("array[%d]", len(n.Children))
	}
	return string(n.Kind)
}

// Scalar is a scalar's JSON text (strings quoted).
func (n *Node) Scalar() string {
	if n.Kind == KString {
		return quote(n.Value)
	}
	return n.Value
}

// Field is an object's child with this key, or nil.
func (n *Node) Field(key string) *Node {
	if n == nil || n.Kind != KObject {
		return nil
	}
	for _, c := range n.Children {
		if c.Key == key {
			return c
		}
	}
	return nil
}

// Path is the node's jq-style path: "." for the root, then ".key",
// `["odd key"]` and "[i]" steps.
func (n *Node) Path() string {
	var steps []string
	for at := n; at.Parent != nil; at = at.Parent {
		steps = append(steps, step(at))
	}
	if len(steps) == 0 {
		return "."
	}
	var b strings.Builder
	for i := len(steps) - 1; i >= 0; i-- {
		b.WriteString(steps[i])
	}
	return b.String()
}

// step is one path step from a node's parent to the node.
func step(n *Node) string {
	if n.Parent.Kind == KArray {
		return "[" + strconv.Itoa(n.Index) + "]"
	}
	return keyStep(n.Key)
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// keyStep is the path step naming an object key.
func keyStep(key string) string {
	if identRe.MatchString(key) {
		return "." + key
	}
	return "[" + quote(key) + "]"
}

// ParseError reports invalid JSON with a 1-based position.
type ParseError struct {
	Line, Col int
	Msg       string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("invalid JSON at line %d, column %d: %s", e.Line, e.Col, e.Msg)
}

func posError(data []byte, off int64, msg string) *ParseError {
	if off > int64(len(data)) {
		off = int64(len(data))
	}
	if off < 1 {
		off = 1
	}
	head := data[:off]
	line := bytes.Count(head, []byte("\n")) + 1
	col := int(off) - bytes.LastIndexByte(head, '\n')
	return &ParseError{Line: line, Col: col, Msg: msg}
}

// Build parses data into a tree, preserving key order.
func Build(data []byte) (*Node, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &ParseError{Line: 1, Col: 1, Msg: "empty input"}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	root, err := parseValue(dec, "", 0, nil)
	if err != nil {
		return nil, wrapErr(data, dec, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, posError(data, dec.InputOffset(), "unexpected data after top-level value")
		}
		return nil, wrapErr(data, dec, err)
	}
	return root, nil
}

func wrapErr(data []byte, dec *json.Decoder, err error) error {
	var se *json.SyntaxError
	switch {
	case errors.As(err, &se):
		return posError(data, se.Offset, se.Error())
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return posError(data, int64(len(data)), "unexpected end of input")
	}
	return posError(data, dec.InputOffset(), err.Error())
}

func parseValue(dec *json.Decoder, key string, index int, parent *Node) (*Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	n := &Node{Key: key, Index: index, Parent: parent}
	if parent != nil {
		n.Depth = parent.Depth + 1
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			n.Kind = KObject
			// A repeated key is read as jq reads it: it keeps the position
			// of its first occurrence and takes the last value, so every
			// read agrees with jq. Repeats retains the duplicate count.
			var at map[string]int
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				i, seen := at[k]
				if !seen {
					i = len(n.Children)
				}
				c, err := parseValue(dec, k, i, n)
				if err != nil {
					return nil, err
				}
				if seen {
					c.Repeats = max(n.Children[i].Repeats, 1) + 1
					n.Children[i] = c
					continue
				}
				if at == nil {
					at = map[string]int{}
				}
				at[k] = i
				n.Children = append(n.Children, c)
			}
		} else {
			n.Kind = KArray
			for i := 0; dec.More(); i++ {
				c, err := parseValue(dec, "", i, n)
				if err != nil {
					return nil, err
				}
				n.Children = append(n.Children, c)
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
	case string:
		n.Kind, n.Value = KString, t
	case json.Number:
		n.Kind, n.Value = KNumber, t.String()
	case bool:
		n.Kind, n.Value = KBool, strconv.FormatBool(t)
	case nil:
		n.Kind, n.Value = KNull, "null"
	}
	return n, nil
}

// Flatten returns all nodes in pre-order.
func Flatten(root *Node) []*Node {
	if root == nil {
		return nil
	}
	var out []*Node
	var walk func(*Node)
	walk = func(n *Node) {
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// Pretty renders the node's full value as indented JSON, keeping key order.
func Pretty(n *Node) string {
	if n == nil {
		return "null"
	}
	var sb strings.Builder
	writePretty(&sb, n, 0)
	return sb.String()
}

func writePretty(sb *strings.Builder, n *Node, ind int) {
	pad := strings.Repeat("  ", ind)
	switch n.Kind {
	case KObject, KArray:
		open, close := "{", "}"
		if n.Kind == KArray {
			open, close = "[", "]"
		}
		if len(n.Children) == 0 {
			sb.WriteString(open + close)
			return
		}
		sb.WriteString(open + "\n")
		for i, c := range n.Children {
			sb.WriteString(pad + "  ")
			if n.Kind == KObject {
				sb.WriteString(quote(c.Key) + ": ")
			}
			writePretty(sb, c, ind+1)
			if i < len(n.Children)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(pad + close)
	default:
		sb.WriteString(n.Scalar())
	}
}

// MarshalJSON writes the value with object keys in document order.
func (n *Node) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(Pretty(n))); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// quote is s as a JSON string, without HTML escaping.
func quote(s string) string {
	plain := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c == '"' || c == '\\' || c >= 0x80 {
			plain = false
			break
		}
	}
	if plain {
		return `"` + s + `"`
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
