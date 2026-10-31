// Ordered JSON editing adapts matomeru/internal/setup/hooks.go (MIT,
// Copyright Joshua Toyota). Marker blocks adapt tuios/internal/integration
// (MIT, Copyright Gaurav Gosain and contributors).
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseObject(data []byte) (*object, error) {
	o := &object{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return o, nil
	}
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("not a JSON object")
	}
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return nil, err
		}
		key := tok.(string)
		if _, ok := o.vals[key]; ok {
			return nil, fmt.Errorf("duplicate JSON key %q", key)
		}
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return nil, err
		}
		o.set(key, raw)
	}
	if _, err = d.Token(); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return o, nil
}
func (o *object) set(k string, v json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}
func (o *object) render() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, k := range o.keys {
		v, ok := o.vals[k]
		if !ok {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.Write(plain(k))
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes()
}
func plain(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return bytes.TrimSpace(b.Bytes())
}
func validJSON(s string) bool { return json.Valid([]byte(s)) }
func sameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

func pointerParts(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("invalid JSON pointer %q", p)
	}
	out := strings.Split(p[1:], "/")
	for n, k := range out {
		for i := 0; i < len(k); i++ {
			if k[i] == '~' {
				if i+1 == len(k) || (k[i+1] != '0' && k[i+1] != '1') {
					return nil, fmt.Errorf("invalid JSON pointer escape")
				}
				i++
			}
		}
		out[n] = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
	}
	return out, nil
}
func valueAt(raw []byte, p string) (json.RawMessage, bool, error) {
	parts, err := pointerParts(p)
	if err != nil {
		return nil, false, err
	}
	if !json.Valid(raw) {
		return nil, false, fmt.Errorf("invalid JSON")
	}
	var cur any
	if err = json.Unmarshal(raw, &cur); err != nil {
		return nil, false, err
	}
	for _, k := range parts {
		switch v := cur.(type) {
		case map[string]any:
			var ok bool
			cur, ok = v[k]
			if !ok {
				return nil, false, nil
			}
		case []any:
			n, e := strconv.Atoi(k)
			if e != nil || n < 0 || n >= len(v) {
				return nil, false, nil
			}
			cur = v[n]
		default:
			return nil, false, fmt.Errorf("pointer traverses a scalar")
		}
	}
	return plain(cur), true, nil
}
func editJSON(raw []byte, p string, v json.RawMessage, remove bool) ([]byte, error) {
	parts, err := pointerParts(p)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("refusing to replace the JSON root")
	}
	var edit func([]byte, []string) ([]byte, error)
	edit = func(data []byte, keys []string) ([]byte, error) {
		o, err := parseObject(data)
		if err != nil {
			return nil, err
		}
		k := keys[0]
		if len(keys) == 1 {
			if remove {
				delete(o.vals, k)
			} else {
				o.set(k, v)
			}
		} else {
			child, ok := o.vals[k]
			if !ok && remove {
				return data, nil
			}
			child, err = edit(child, keys[1:])
			if err != nil {
				return nil, err
			}
			if remove && sameJSON(child, []byte("{}")) {
				delete(o.vals, k)
			} else {
				o.set(k, child)
			}
		}
		return o.render(), nil
	}
	out, err := edit(raw, parts)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err = json.Indent(&b, out, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

func markers(i Item, tool string) (string, string) {
	if i.Begin != "" {
		return i.Begin, i.End
	}
	comment := i.Comment
	if comment == "" {
		if strings.EqualFold(filepath.Ext(i.Path), ".md") {
			comment = "<!--"
		} else {
			comment = "#"
		}
	}
	end := ""
	if comment == "<!--" {
		end = " -->"
	}
	return comment + " >>> " + tool + ": " + i.ID + end, comment + " <<< " + tool + ": " + i.ID + end
}
func blockBounds(data []byte, begin, end string) (int, int, error) {
	text := string(data)
	a, b := strings.Index(text, begin), strings.Index(text, end)
	if a < 0 && b < 0 {
		return -1, -1, nil
	}
	if a < 0 || b < a || strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		return 0, 0, fmt.Errorf("incomplete or duplicate setup markers")
	}
	if (a > 0 && text[a-1] != '\n') || (b > 0 && text[b-1] != '\n') {
		return 0, 0, fmt.Errorf("setup markers must occupy their own lines")
	}
	b += len(end)
	if b < len(text) && text[b] != '\n' && text[b] != '\r' {
		return 0, 0, fmt.Errorf("setup marker has trailing text")
	}
	if b < len(text) && text[b] == '\r' {
		b++
	}
	if b < len(text) && text[b] == '\n' {
		b++
	}
	return a, b, nil
}

func compare(i Item, have, want []byte) State {
	if i.Match.Command != "" {
		h, hok, _ := valueAt(have, i.Match.Command)
		w, wok, _ := valueAt(want, i.Match.Command)
		var hc, wc string
		if !hok || !wok || json.Unmarshal(h, &hc) != nil || json.Unmarshal(w, &wc) != nil || filepath.Base(hc) != filepath.Base(wc) {
			return Conflict
		}
		if i.Match.Args != "" {
			ha, ho, _ := valueAt(have, i.Match.Args)
			wa, wo, _ := valueAt(want, i.Match.Args)
			if ho != wo || ho && !sameJSON(ha, wa) {
				return Conflict
			}
		}
		if filepath.IsAbs(hc) {
			if hc != wc {
				return Outdated
			}
			if info, err := os.Stat(hc); err != nil || !info.Mode().IsRegular() {
				return Outdated
			}
		}
		return Installed
	}
	if sameJSON(have, want) {
		return Installed
	}
	if len(i.Match.Fields) > 0 {
		for _, p := range i.Match.Fields {
			h, ho, _ := valueAt(have, p)
			w, wo, _ := valueAt(want, p)
			if !ho || !wo || !sameJSON(h, w) {
				return Conflict
			}
		}
		return Outdated
	}
	return Conflict
}

func arrayEdit(i Item, raw []byte, remove bool) ([]byte, State, error) {
	at, exists, err := valueAt(raw, i.Pointer)
	if err != nil {
		return nil, Conflict, err
	}
	var entries []json.RawMessage
	if exists && json.Unmarshal(at, &entries) != nil {
		return nil, Conflict, fmt.Errorf("target is not an array")
	}
	var want []json.RawMessage
	if json.Unmarshal([]byte(i.Content), &want) != nil {
		return nil, Conflict, fmt.Errorf("json-array content must be an array")
	}
	owns := func(v []byte) bool {
		p, ok, _ := valueAt(v, i.Match.Field)
		var str string
		return ok && json.Unmarshal(p, &str) == nil && strings.Contains(str, i.Match.Contains)
	}
	for _, v := range want {
		if !owns(v) {
			return nil, Conflict, fmt.Errorf("content entry does not carry match.contains")
		}
	}
	owned, keep := []json.RawMessage{}, []json.RawMessage{}
	for _, v := range entries {
		if owns(v) {
			owned = append(owned, v)
		} else {
			keep = append(keep, v)
		}
	}
	state := Missing
	if len(owned) > 0 {
		state = Outdated
		if sameJSON(plain(owned), plain(want)) {
			state = Installed
		}
	}
	if !remove {
		keep = append(keep, want...)
	}
	out, err := editJSON(raw, i.Pointer, plain(keep), remove && len(keep) == 0)
	return out, state, err
}

func probeValue(p Probe, raw []byte) ([]byte, bool, error) {
	if p.Format == "toml" {
		var v map[string]any
		if err := toml.Unmarshal(raw, &v); err != nil {
			return nil, false, err
		}
		var cur any = v
		for _, k := range p.Table {
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("TOML table traverses a scalar")
			}
			cur, ok = m[k]
			if !ok {
				return nil, false, nil
			}
		}
		raw = plain(cur)
	}
	if len(p.Where) > 0 {
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, false, fmt.Errorf("probe where needs an array")
		}
		var found []byte
		for _, row := range rows {
			matches := true
			for field, want := range p.Where {
				v, ok, _ := valueAt(row, field)
				var have string
				matches = matches && ok && json.Unmarshal(v, &have) == nil && have == want
			}
			if matches {
				if found != nil {
					return nil, false, fmt.Errorf("probe where matches several rows")
				}
				found = row
			}
		}
		if found == nil {
			return nil, false, nil
		}
		raw = found
	}
	return valueAt(raw, p.Pointer)
}
