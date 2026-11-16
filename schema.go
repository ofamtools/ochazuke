package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Schemas (spine S13): every file kind a program reads has a JSON Schema
// generated from the Go types that declare it (JSONSchemaOf), never
// written by hand, a version, and a stable URL (SchemaURL) a file names in
// "$schema". The runtime keeps the kinds it knows (AddSchema) as the
// ochazuke.schema noun: list them, get one to read its schema.

// SchemaBase is where the family's schemas are published.
const SchemaBase = "https://ofam.dev/schemas/"

// SchemaURL is the URL of version v of kind's schema: SchemaBase +
// KIND.vN.json.
func SchemaURL(kind string, v int) string { return fmt.Sprintf("%s%s.v%d.json", SchemaBase, kind, v) }

// SchemaVersion reads the kind and version a "$schema" URL (or local path)
// names: .../KIND.vN.json. ok is false for anything else.
func SchemaVersion(url string) (kind string, v int, ok bool) {
	base := url[strings.LastIndex(url, "/")+1:]
	base, found := strings.CutSuffix(base, ".json")
	if !found {
		return "", 0, false
	}
	i := strings.LastIndex(base, ".v")
	if i <= 0 {
		return "", 0, false
	}
	if _, err := fmt.Sscanf(base[i+2:], "%d", &v); err != nil || v <= 0 {
		return "", 0, false
	}
	return base[:i], v, true
}

// SchemaDoc is one file kind's schema at its current version.
type SchemaDoc struct {
	Kind    string         `json:"kind"`
	Version int            `json:"version"`
	URL     string         `json:"url"`
	About   string         `json:"about,omitempty"`
	Schema  map[string]any `json:"schema"`
}

// AddSchema registers kind's schema at version v; a kind registered again
// is replaced.
func (r *Runtime) AddSchema(doc SchemaDoc) {
	if doc.URL == "" {
		doc.URL = SchemaURL(doc.Kind, doc.Version)
	}
	if i := slices.IndexFunc(r.SchemaDocs, func(d SchemaDoc) bool { return d.Kind == doc.Kind }); i >= 0 {
		r.SchemaDocs[i] = doc
		return
	}
	r.SchemaDocs = append(r.SchemaDocs, doc)
}

// SchemaOf is kind's registered schema.
func (r *Runtime) SchemaOf(kind string) (SchemaDoc, bool) {
	i := slices.IndexFunc(r.SchemaDocs, func(d SchemaDoc) bool { return d.Kind == kind })
	if i < 0 {
		return SchemaDoc{}, false
	}
	return r.SchemaDocs[i], true
}

// registerSchemas declares the schema noun.
func registerSchemas(r *Runtime) {
	r.defineFacts(factsSpec{Noun: "ochazuke.schema", StableKeys: true, Sample: SchemaDoc{}, Key: "kind", Label: "kind",
		Docs: map[string]string{"kind": "The file kind (noun: a nouns/NOUN.json declaration)", "version": "The schema's version; a file names it in $schema",
			"url": "The URL a file's $schema names", "about": "What the files of the kind hold", "schema": "The JSON Schema, generated from the declarations"},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) { return factThings(c.Runtime.SchemaDocs, nil) }})
}

// JSONSchemaOf is the JSON Schema (draft 2020-12) of the values a Go type
// encodes to with encoding/json: an object per struct (its json-tagged
// fields, embedded structs' fields promoted as encoding/json does, a
// shallower field winning), arrays, maps as objects, any for an interface
// or a type with its own MarshalJSON. Named struct types are $defs.
// additionalProperties is false on structs, so an editor flags a typo; a
// loader keeps and warns about unknown keys instead (S13 rule 3).
func JSONSchemaOf(t reflect.Type, title string) map[string]any {
	g := schemaGen{defs: map[string]any{}}
	root := g.of(t, true)
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	if title != "" {
		root["title"] = title
	}
	if len(g.defs) > 0 {
		root["$defs"] = g.defs
	}
	return root
}

type schemaGen struct{ defs map[string]any }

var marshalerType = reflect.TypeFor[json.Marshaler]()

func (g *schemaGen) of(t reflect.Type, root bool) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Implements(marshalerType) || reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{} // []byte and json.RawMessage: any value
		}
		return map[string]any{"type": "array", "items": g.of(t.Elem(), false)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.of(t.Elem(), false)}
	case reflect.Struct:
		if root || t.Name() == "" {
			return g.object(t)
		}
		name := defName(t)
		if _, ok := g.defs[name]; !ok {
			g.defs[name] = map[string]any{} // a placeholder ends a cycle
			g.defs[name] = g.object(t)
		}
		return map[string]any{"$ref": "#/$defs/" + name}
	}
	return map[string]any{}
}

// defName is a named type's $defs key: package.Type.
func defName(t reflect.Type) string {
	pkg := t.PkgPath()
	return pkg[strings.LastIndex(pkg, "/")+1:] + "." + t.Name()
}

func (g *schemaGen) object(t reflect.Type) map[string]any {
	props := map[string]any{}
	for _, f := range schemaFields(t) {
		props[f.name] = g.of(f.typ, false)
	}
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}

// schemaField is one field encoding/json writes for a struct.
type schemaField struct {
	name  string
	typ   reflect.Type
	depth int
}

// schemaFields are the fields encoding/json writes for struct type t, by
// name: tagged or exported fields, embedded structs' promoted, the
// shallowest of a name winning (two at one depth cancel, as in
// encoding/json, unless one is tagged).
func schemaFields(t reflect.Type) []schemaField {
	type found struct {
		schemaField
		tagged bool
		count  int
	}
	best := map[string]*found{}
	var walk func(t reflect.Type, depth int, seen map[reflect.Type]bool)
	walk = func(t reflect.Type, depth int, seen map[reflect.Type]bool) {
		if seen[t] {
			return
		}
		seen[t] = true
		for i := range t.NumField() {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				walk(ft, depth+1, seen)
				continue
			}
			if !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			cur := best[name]
			switch {
			case cur == nil || depth < cur.depth:
				best[name] = &found{schemaField{name, f.Type, depth}, name != f.Name || tag != "", 1}
			case depth == cur.depth:
				cur.count++
			}
		}
		delete(seen, t)
	}
	walk(t, 0, map[reflect.Type]bool{})
	var out []schemaField
	for _, f := range best {
		if f.count == 1 {
			out = append(out, f.schemaField)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// SchemaProblem is one place a value does not fit a schema: the JSON
// pointer of the value and why.
type SchemaProblem struct {
	Path string `json:"path"`
	Text string `json:"text"`
	// Unknown marks a key the schema does not know (additionalProperties
	// false): a loader keeps it and warns.
	Unknown bool `json:"unknown,omitempty"`
}

func (p SchemaProblem) String() string { return p.Path + ": " + p.Text }

// ValidateSchema checks a decoded JSON value against a schema JSONSchemaOf
// wrote (the subset it uses: type, properties, additionalProperties,
// items, $ref into $defs, required, enum). Problems are in document order.
func ValidateSchema(schema map[string]any, value any) []SchemaProblem {
	defs, _ := schema["$defs"].(map[string]any)
	var out []SchemaProblem
	validateAt(schema, value, "", defs, &out)
	return out
}

func validateAt(s map[string]any, v any, path string, defs map[string]any, out *[]SchemaProblem) {
	if ref, ok := s["$ref"].(string); ok {
		d, _ := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if d == nil {
			*out = append(*out, SchemaProblem{Path: path, Text: "unknown $ref " + ref})
			return
		}
		s = d
	}
	if enum, ok := s["enum"].([]any); ok && !slices.ContainsFunc(enum, func(e any) bool { return reflect.DeepEqual(e, v) }) {
		*out = append(*out, SchemaProblem{Path: path, Text: fmt.Sprintf("%v is not one of %v", v, enum)})
	}
	typ, _ := s["type"].(string)
	if typ == "" || v == nil {
		return // any value; null stands for an omitted field
	}
	if !jsonTypeIs(typ, v) {
		*out = append(*out, SchemaProblem{Path: path, Text: fmt.Sprintf("want %s, got %s", typ, jsonTypeName(v))})
		return
	}
	switch typ {
	case "array":
		items, _ := s["items"].(map[string]any)
		for i, e := range v.([]any) {
			if items != nil {
				validateAt(items, e, fmt.Sprintf("%s/%d", path, i), defs, out)
			}
		}
	case "object":
		obj := v.(map[string]any)
		props, _ := s["properties"].(map[string]any)
		for _, req := range anyStrings(s["required"]) {
			if _, ok := obj[req]; !ok {
				*out = append(*out, SchemaProblem{Path: path, Text: "missing " + req})
			}
		}
		for _, k := range slices.Sorted(maps.Keys(obj)) {
			at := path + "/" + strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
			if p, ok := props[k].(map[string]any); ok {
				validateAt(p, obj[k], at, defs, out)
				continue
			}
			switch extra := s["additionalProperties"].(type) {
			case bool:
				if !extra && props != nil {
					*out = append(*out, SchemaProblem{Path: at, Text: "unknown key " + k, Unknown: true})
				}
			case map[string]any:
				validateAt(extra, obj[k], at, defs, out)
			}
		}
	}
}

func anyStrings(v any) []string {
	var out []string
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		for _, e := range l {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func jsonTypeIs(typ string, v any) bool {
	switch typ {
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "number":
		_, ok := v.(float64)
		return ok
	}
	return jsonTypeName(v) == typ
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}
