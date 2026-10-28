package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The default bindings file: the grouped context-table format.
//
//	{
//	  "modes": {"leader_pending": {"pending": true}, "terminal": {"passthrough": true}},
//	  "bindings": [
//	    {"context": "ochazuke.list/normal", "bindings": {
//	      "j": {"action": "ochazuke.list.move", "args": {"delta": 1}, "from": {"steps": "count"}},
//	      "space": "ochazuke.list.mark"
//	    }},
//	    {"context": "ochazuke.list/count_pending", "extends": "normal"}
//	  ]
//	}
//
// "modes" declares the modes the rows need: pending (exclusive, one-shot),
// passthrough (capture rows stand aside), or {} for a base mode, defined
// only when no mode of that name exists yet.
//
// A group is one table. "context" is "noun/mode", split at the last "/";
// a bare "noun" is the any-mode table (mode ""). An instance table is
// {"instance": "id", "mode": "normal", ...} in place of "context".
// "extends" makes the noun's mode table read the parent mode's rows after
// its own (ModeBuilder.Extends). "fallback" keeps the group's rows to the
// keys its table did not bind before the file loaded: an application's rows
// bound earlier keep the key (the app noun's palette and help keys).
//
// "bindings" maps a key to a row. The key is a key name, a sequence ("g
// g") or a key class (digit, alpha-q, one-of:h j k l, paste, any, upper,
// ctrl+alpha: a pattern row). The value is an action ("noun.verb"), an
// object {action, target, args, from, when, hint, hidden, help, replay,
// capture}, null (an unbind), or an array of rows (actions or objects)
// for a key with several guarded rows in one table, in table order.
//
// parseDefaultBindings is the one reader of the format inside the package:
// the bindings-format lane's reader (FormatBindings and its parser) is meant
// to replace it at merge.

// bindingFile is a parsed default bindings file.
type bindingFile struct {
	Modes  []modeSpec
	Groups []bindingGroup
}

// modeSpec is one entry of "modes".
type modeSpec struct {
	Name        string
	Pending     bool
	Passthrough bool
}

// bindingGroup is one table of the file: its rows in file order, each with
// Noun or Instance, Mode, and Keys or Pattern filled.
type bindingGroup struct {
	Context  string
	Noun     string
	Instance string
	Mode     string
	Extends  string
	Fallback bool
	Rows     []Binding
}

// bindingValue is a row value's object form.
type bindingValue struct {
	Action  string            `json:"action"`
	Target  string            `json:"target,omitempty"`
	Args    Arguments         `json:"args,omitempty"`
	From    map[string]string `json:"from,omitempty"`
	When    string            `json:"when,omitempty"`
	Hint    string            `json:"hint,omitempty"`
	Hidden  bool              `json:"hidden,omitempty"`
	Help    string            `json:"help,omitempty"`
	Replay  bool              `json:"replay,omitempty"`
	Capture bool              `json:"capture,omitempty"`
}

func parseDefaultBindings(data []byte) (bindingFile, error) {
	var raw struct {
		Modes    json.RawMessage `json:"modes"`
		Bindings []struct {
			Context  string          `json:"context"`
			Instance string          `json:"instance"`
			Mode     string          `json:"mode"`
			Extends  string          `json:"extends"`
			Fallback bool            `json:"fallback"`
			Source   string          `json:"source"` // bindings defaults writes it; ignored
			Bindings json.RawMessage `json:"bindings"`
		} `json:"bindings"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return bindingFile{}, err
	}
	var file bindingFile
	if err := eachMember(raw.Modes, func(name string, value json.RawMessage) error {
		var m struct {
			Pending     bool `json:"pending"`
			Passthrough bool `json:"passthrough"`
		}
		dec := json.NewDecoder(bytes.NewReader(value))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m); err != nil {
			return fmt.Errorf("mode %q: %w", name, err)
		}
		if m.Pending && m.Passthrough {
			return fmt.Errorf("mode %q: a pending mode cannot be passthrough", name)
		}
		file.Modes = append(file.Modes, modeSpec{Name: name, Pending: m.Pending, Passthrough: m.Passthrough})
		return nil
	}); err != nil {
		return bindingFile{}, err
	}
	for i, g := range raw.Bindings {
		group := bindingGroup{Context: g.Context, Extends: g.Extends, Fallback: g.Fallback}
		switch {
		case g.Context != "" && g.Instance != "":
			return bindingFile{}, fmt.Errorf("bindings[%d]: context or instance, not both", i)
		case g.Context != "":
			if g.Mode != "" {
				return bindingFile{}, fmt.Errorf("bindings[%d] %s: the mode is part of the context", i, g.Context)
			}
			group.Noun, group.Mode = g.Context, ""
			if at := strings.LastIndex(g.Context, "/"); at >= 0 {
				group.Noun, group.Mode = g.Context[:at], g.Context[at+1:]
			}
			if group.Noun == "" {
				return bindingFile{}, fmt.Errorf("bindings[%d] %s: no noun", i, g.Context)
			}
		case g.Instance != "":
			group.Instance, group.Mode = g.Instance, g.Mode
			group.Context = "instance " + g.Instance + "/" + g.Mode
			if g.Extends != "" {
				return bindingFile{}, fmt.Errorf("bindings[%d] %s: an instance table cannot extend a mode", i, group.Context)
			}
		default:
			return bindingFile{}, fmt.Errorf("bindings[%d]: needs a context", i)
		}
		if err := eachMember(g.Bindings, func(key string, value json.RawMessage) error {
			rows, err := parseBindingValue(value)
			if err != nil {
				return fmt.Errorf("%s %q: %w", group.Context, key, err)
			}
			for _, row := range rows {
				row.Noun, row.Instance, row.Mode = group.Noun, group.Instance, group.Mode
				if isPattern(key) {
					row.Pattern = key
				} else {
					row.Keys = strings.Join(strings.Fields(key), " ")
				}
				group.Rows = append(group.Rows, row)
			}
			return nil
		}); err != nil {
			return bindingFile{}, err
		}
		file.Groups = append(file.Groups, group)
	}
	return file, nil
}

// parseBindingValue reads one key's value: an action, an object, null or
// an array of objects.
func parseBindingValue(value json.RawMessage) ([]Binding, error) {
	value = bytes.TrimSpace(value)
	switch {
	case bytes.Equal(value, []byte("null")):
		return []Binding{{Unbind: true}}, nil
	case len(value) > 0 && value[0] == '"':
		var action string
		if err := json.Unmarshal(value, &action); err != nil {
			return nil, err
		}
		row, err := bindingValue{Action: action}.row()
		return []Binding{row}, err
	case len(value) > 0 && value[0] == '[':
		var items []json.RawMessage
		if err := json.Unmarshal(value, &items); err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, errors.New("an empty array binds nothing")
		}
		var rows []Binding
		for _, item := range items {
			if item = bytes.TrimSpace(item); len(item) == 0 || item[0] != '{' && item[0] != '"' {
				return nil, errors.New("an array holds rows: actions or objects")
			}
			more, err := parseBindingValue(item)
			if err != nil {
				return nil, err
			}
			rows = append(rows, more...)
		}
		return rows, nil
	}
	var v bindingValue
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	row, err := v.row()
	return []Binding{row}, err
}

func (v bindingValue) row() (Binding, error) {
	if v.Action == "" {
		return Binding{}, errors.New("a row needs an action (null unbinds)")
	}
	row := Do(v.Action)
	row.Request.Target, row.Request.Args = v.Target, v.Args
	row.From, row.When, row.Hint, row.Hidden, row.Help, row.Replay, row.Capture = v.From, v.When, v.Hint, v.Hidden, v.Help, v.Replay, v.Capture
	return row, nil
}

// eachMember calls fn for each member of a JSON object in file order;
// empty data is no members.
func eachMember(data json.RawMessage, fn func(string, json.RawMessage) error) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return errors.New("want an object")
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key := tok.(string)
		if seen[key] {
			return fmt.Errorf("%q appears twice (several rows on one key are an array)", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("%q: %w", key, err)
		}
		if err := fn(key, value); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

// formatDefaultBindings writes groups in the file format, one key per line
// (bindings defaults). Each group carries its "source" (where its rows are
// declared), which a reader of the file ignores. The bindings-format
// lane's writer (FormatBindings) is meant to replace it at merge.
func formatDefaultBindings(modes []modeSpec, groups []sourcedGroup) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{\n")
	if len(modes) > 0 {
		b.WriteString("  \"modes\": {\n")
		for i, m := range modes {
			spec := map[string]bool{}
			if m.Pending {
				spec["pending"] = true
			}
			if m.Passthrough {
				spec["passthrough"] = true
			}
			fmt.Fprintf(&b, "    %s: %s", plainJSON(m.Name), plainJSON(spec))
			if i < len(modes)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString("  },\n")
	}
	b.WriteString("  \"bindings\": [\n")
	for gi, g := range groups {
		if g.Instance != "" {
			fmt.Fprintf(&b, "    {\"instance\": %s, \"mode\": %s", plainJSON(g.Instance), plainJSON(g.Mode))
		} else {
			ctx := g.Noun
			if g.Mode != "" {
				ctx += "/" + g.Mode
			}
			fmt.Fprintf(&b, "    {\"context\": %s", plainJSON(ctx))
		}
		if g.Extends != "" {
			fmt.Fprintf(&b, ", \"extends\": %s", plainJSON(g.Extends))
		}
		fmt.Fprintf(&b, ", \"source\": %s", plainJSON(g.Source))
		keys, values := groupValues(g.Rows)
		if len(keys) == 0 {
			b.WriteString("}")
		} else {
			b.WriteString(", \"bindings\": {\n")
			for i, key := range keys {
				fmt.Fprintf(&b, "      %s: %s", plainJSON(key), values[i])
				if i < len(keys)-1 {
					b.WriteByte(',')
				}
				b.WriteByte('\n')
			}
			b.WriteString("    }}")
		}
		if gi < len(groups)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]\n}\n")
	return b.Bytes(), nil
}

// groupValues are a table's rows as file members: keys in first-row
// order, a key with several rows an array of them.
func groupValues(rows []Binding) ([]string, []string) {
	var keys []string
	byKey := map[string][]string{}
	for _, row := range rows {
		key := row.Keys
		if key == "" {
			key = row.Pattern
		}
		if _, ok := byKey[key]; !ok {
			keys = append(keys, key)
		}
		byKey[key] = append(byKey[key], rowValue(row))
	}
	values := make([]string, len(keys))
	for i, key := range keys {
		if vs := byKey[key]; len(vs) == 1 {
			values[i] = vs[0]
		} else {
			values[i] = "[" + strings.Join(vs, ", ") + "]"
		}
	}
	return keys, values
}

// rowValue is one row's value: null, the action alone, or the object.
func rowValue(row Binding) string {
	if row.Unbind || row.Request.Verb == "" {
		return "null"
	}
	action := row.Request.Key()
	when := row.When
	if withImplicitGuard(Binding{Noun: row.Noun, Instance: row.Instance, Request: row.Request}).When == when && when != "" {
		when = "" // the editor noun's implicit guard is the framework's
	}
	if row.Request.Target == "" && len(row.Request.Args) == 0 && len(row.From) == 0 && when == "" && row.Hint == "" && !row.Hidden && row.Help == "" && !row.Replay && !row.Capture {
		return plainJSON(action)
	}
	var parts []string
	add := func(key string, value any) { parts = append(parts, plainJSON(key)+": "+plainJSON(value)) }
	add("action", action)
	if row.Request.Target != "" {
		add("target", row.Request.Target)
	}
	if len(row.Request.Args) > 0 {
		add("args", row.Request.Args)
	}
	if len(row.From) > 0 {
		add("from", row.From)
	}
	if when != "" {
		add("when", when)
	}
	if row.Hint != "" {
		add("hint", row.Hint)
	}
	if row.Hidden {
		add("hidden", true)
	}
	if row.Help != "" {
		add("help", row.Help)
	}
	if row.Replay {
		add("replay", true)
	}
	if row.Capture {
		add("capture", true)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// plainJSON is compact JSON without HTML escaping, a space after each ":"
// and "," outside strings.
func plainJSON(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "null"
	}
	data := bytes.TrimRight(b.Bytes(), "\n")
	var out bytes.Buffer
	inString, escaped := false, false
	for _, c := range data {
		out.WriteByte(c)
		switch {
		case inString && escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == ':' || c == ','):
			out.WriteByte(' ')
		}
	}
	return out.String()
}
