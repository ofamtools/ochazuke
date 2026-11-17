package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

// Noun files (spine S1, S2, S13): a noun is a declaration, one data file
// per noun (nouns/NOUN.json, embedded in the program that owns the noun),
// read by one loader (LoadNouns). The format is CatalogNoun, the JSON
// tools already publish their nouns in, extended with what a catalog
// leaves out because it is code today: the store's source, the owner
// reference, relation kinds, the verbs with the handler each names, the
// key rows, settings and hooks. Go code is cited by name from the
// runtime's name table (ochazuke.Runtime.AddHandler, AddSource, AddHook);
// the file holds no Go structure and Go holds none of the file's data.
//
// The loader reports every problem (a missing handler, parameters that
// differ from the handler's, a key the schema does not know, a row that
// does not apply) as an ochazuke.DeclProblem, on the runtime and in its
// answer, and loads everything else: it never drops a row silently and
// never refuses the file (record first, never refuse).

// NounFileKind is the noun file's schema kind and NounFileVersion its
// current version: a file names NounSchemaURL in "$schema".
const (
	NounFileKind    = "noun"
	NounFileVersion = 1
)

// NounSchemaURL is the "$schema" a noun file names.
var NounSchemaURL = ochazuke.SchemaURL(NounFileKind, NounFileVersion)

// NounFile is one noun's declaration file. Embedded CatalogNoun gives the
// noun's identity, flags, props, columns, filters and propsOff as a
// catalog writes them (its derived fields, kind, nounKind, facts and
// referencedBy, are the catalog's and a file leaves them out); Store and
// Verbs replace the catalog's fields of the same names.
type NounFile struct {
	Schema string `json:"$schema,omitempty"`
	CatalogNoun
	// Store names the Go source that answers the noun's store
	// (Runtime.AddSource), called once when the noun loads.
	Store *NounStore `json:"store,omitempty"`
	// Relations are relation kinds this noun declares (Runtime.DeclareRelationKind).
	Relations []ochazuke.RelationKind `json:"relations,omitempty"`
	// Verbs are the noun's own verbs; the standard verbs its store
	// supports are implied and never listed.
	Verbs []NounVerb `json:"verbs,omitempty"`
	// Keys are the noun's key rows, binding groups in the grouped
	// bindings-file format ({"context": "NOUN/MODE", "extends": ...,
	// "bindings": [rows]}), loaded as default rows from this file.
	Keys []json.RawMessage `json:"keys,omitempty"`
	// Settings are the noun's settings (Runtime.DeclareSetting).
	Settings []ochazuke.Setting `json:"settings,omitempty"`
	// Hooks name Go hooks by the role they play (beforeWrite, afterWrite,
	// beforeDelete, confirmFor, ...): role to name in the table. The loader
	// checks each name and records its use; the store kinds that run them
	// read them by role (run plan 5.4).
	Hooks map[string]string `json:"hooks,omitempty"`
	// Unknown are the top-level keys the schema does not know: kept as
	// read and warned about (S13 rule 3).
	Unknown map[string]json.RawMessage `json:"-"`
}

// NounStore is a noun file's store: the source that answers it.
type NounStore struct {
	Source string `json:"source"`
}

// NounVerb is one verb row: the verb's name, the handler it names, the
// guard that must hold for it to be available (NounBuilder.When) and
// every field of the operation (ochazuke.Operation's JSON: description,
// parameters, result, kind, confirm, agent, ...). A row whose handler
// declares its parameters (a typed handler, WithParams) states none.
type NounVerb struct {
	Verb    string `json:"verb"`
	Handler string `json:"handler"`
	When    string `json:"when,omitempty"`
	VerbFields
}

// VerbFields are the operation's fields as a verb row holds them:
// ochazuke.Operation without its methods, so its JSON is the fields alone.
type VerbFields ochazuke.Operation

// NounFileSchema is the noun file's schema document, generated from
// NounFile.
func NounFileSchema() ochazuke.SchemaDoc {
	s := ochazuke.JSONSchemaOf(reflect.TypeFor[NounFile](), "ochazuke noun file")
	s["$id"] = NounSchemaURL
	s["required"] = []string{"name"}
	return ochazuke.SchemaDoc{Kind: NounFileKind, Version: NounFileVersion, URL: NounSchemaURL,
		About: "A noun's declaration: nouns/NOUN.json (props, store source, verbs with their handlers, key rows, settings, hooks)", Schema: s}
}

// ReadNounFile decodes one noun file. Problems are the keys the schema
// does not know (kept in Unknown) and values of the wrong type; err is set
// only when the file is not a JSON object at all.
func ReadNounFile(name string, data []byte) (NounFile, []ochazuke.DeclProblem, error) {
	var f NounFile
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return f, nil, err
	}
	var problems []ochazuke.DeclProblem
	var value any
	_ = json.Unmarshal(data, &value)
	for _, p := range ochazuke.ValidateSchema(NounFileSchema().Schema, value) {
		kind := ochazuke.ProblemBadRow
		if p.Unknown {
			kind = ochazuke.ProblemUnknownKey
		}
		problems = append(problems, ochazuke.DeclProblem{File: name, Row: p.Path, Kind: kind, Text: p.Text})
	}
	if err := json.Unmarshal(data, &f); err != nil {
		// A value of the wrong type: decode field by field, so the rest loads.
		f = NounFile{}
		for key, v := range raw {
			one, _ := json.Marshal(map[string]json.RawMessage{key: v})
			if err := json.Unmarshal(one, &f); err != nil {
				problems = append(problems, ochazuke.DeclProblem{File: name, Row: "/" + key, Kind: ochazuke.ProblemBadRow, Text: err.Error()})
			}
		}
	}
	known := knownKeys()
	for key, v := range raw {
		if !known[key] {
			if f.Unknown == nil {
				f.Unknown = map[string]json.RawMessage{}
			}
			f.Unknown[key] = v
		}
	}
	if f.Schema != "" {
		if kind, v, ok := ochazuke.SchemaVersion(f.Schema); !ok || ochazuke.SchemaURL(kind, 1) != ochazuke.SchemaURL(NounFileKind, 1) {
			problems = append(problems, ochazuke.DeclProblem{File: name, Row: "/$schema", Kind: ochazuke.ProblemBadRow, Text: fmt.Sprintf("%q is not a noun schema; read as version %d", f.Schema, NounFileVersion)})
		} else if v > NounFileVersion {
			problems = append(problems, ochazuke.DeclProblem{File: name, Row: "/$schema", Kind: ochazuke.ProblemUnknownKey, Text: fmt.Sprintf("version %d is newer than this program's %d: unknown keys are kept and warned", v, NounFileVersion)})
		}
	}
	return f, problems, nil
}

// knownKeys are the top-level keys of a noun file.
func knownKeys() map[string]bool {
	props, _ := NounFileSchema().Schema["properties"].(map[string]any)
	out := map[string]bool{}
	for k := range props {
		out[k] = true
	}
	return out
}

// NounFiles are the noun files of one directory, read (ReadNouns) and
// not yet declared: Declare declares their nouns, DeclareSettings their
// settings, Bind loads their key rows. A program declares its nouns before the code that uses them and
// binds their keys once every verb, alias and guard the rows name is
// registered, where it loads its other default rows; LoadNouns does both
// at once.
type NounFiles struct {
	Files []NounFile
	// Names are the files' paths, by Files' index.
	Names []string
	// read are the problems reading found, recorded on the runtime by
	// Declare.
	read []ochazuke.DeclProblem
}

// ReadNouns reads every *.json file in dir of fsys, in name order. A file
// that cannot be read or is not a JSON object is a problem; the rest are
// read.
func ReadNouns(fsys fs.FS, dir string) *NounFiles {
	s := &NounFiles{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		s.read = append(s.read, ochazuke.DeclProblem{File: dir, Kind: ochazuke.ProblemBadRow, Text: err.Error()})
		return s
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := path.Join(dir, e.Name())
		data, err := fs.ReadFile(fsys, name)
		if err == nil {
			var f NounFile
			var problems []ochazuke.DeclProblem
			f, problems, err = ReadNounFile(name, data)
			s.read = append(s.read, problems...)
			if err == nil && f.Name == "" {
				s.read = append(s.read, ochazuke.DeclProblem{File: name, Kind: ochazuke.ProblemBadRow, Text: "no \"name\": the file declares no noun and is skipped"})
				continue
			}
			if err == nil {
				s.Files, s.Names = append(s.Files, f), append(s.Names, name)
				continue
			}
		}
		s.read = append(s.read, ochazuke.DeclProblem{File: name, Kind: ochazuke.ProblemBadRow, Text: err.Error()})
	}
	return s
}

// Declares says one of the files declares noun.
func (s *NounFiles) Declares(noun string) bool {
	return s != nil && slices.ContainsFunc(s.Files, func(f NounFile) bool { return f.Name == noun })
}

// Declare registers the noun file schema (the ochazuke.schema noun lists
// it) and declares each file's noun on r: flags, props (with the owner),
// store, verbs, relation kinds and hooks, in that order (settings come
// with DeclareSettings, keys with Bind). Every
// problem, reading's included, is answered and kept on r
// (Runtime.DeclProblems); a row that cannot apply is a problem and the
// rest loads.
func (s *NounFiles) Declare(r *ochazuke.Runtime) []ochazuke.DeclProblem {
	r.AddSchema(NounFileSchema())
	var out []ochazuke.DeclProblem
	for _, p := range s.read {
		r.AddDeclProblem(p)
		out = append(out, p)
	}
	for i, f := range s.Files {
		l := &nounLoader{r: r, file: s.Names[i]}
		l.load(f)
		out = append(out, l.out...)
	}
	return out
}

// DeclareSettings declares each file's settings (Runtime.DeclareSetting),
// where the program declares its other settings, so the Settings page
// keeps its order.
func (s *NounFiles) DeclareSettings(r *ochazuke.Runtime) []ochazuke.DeclProblem {
	var out []ochazuke.DeclProblem
	for i, f := range s.Files {
		if len(f.Settings) == 0 {
			continue
		}
		l := &nounLoader{r: r, file: s.Names[i]}
		if err := r.DeclareSetting(f.Settings...); err != nil {
			l.add(f.Name+".settings", ochazuke.ProblemBadRow, "%v", err)
		}
		out = append(out, l.out...)
	}
	return out
}

// Bind loads each file's key rows as default rows sourced from the file.
func (s *NounFiles) Bind(r *ochazuke.Runtime) []ochazuke.DeclProblem {
	var out []ochazuke.DeclProblem
	for i, f := range s.Files {
		l := &nounLoader{r: r, file: s.Names[i]}
		l.keys(f.Name, f.Keys)
		out = append(out, l.out...)
	}
	return out
}

// LoadNouns reads the noun files of dir and declares their nouns and
// settings and binds their keys at once.
func LoadNouns(r *ochazuke.Runtime, fsys fs.FS, dir string) []ochazuke.DeclProblem {
	s := ReadNouns(fsys, dir)
	out := s.Declare(r)
	out = append(out, s.DeclareSettings(r)...)
	return append(out, s.Bind(r)...)
}

type nounLoader struct {
	r    *ochazuke.Runtime
	file string
	out  []ochazuke.DeclProblem
}

func (l *nounLoader) problem(p ochazuke.DeclProblem) {
	l.r.AddDeclProblem(p)
	l.out = append(l.out, p)
}

func (l *nounLoader) add(row, kind, format string, args ...any) {
	l.problem(ochazuke.DeclProblem{File: l.file, Row: row, Kind: kind, Text: fmt.Sprintf(format, args...)})
}

// addNamed is add for a problem about one name in the table.
func (l *nounLoader) addNamed(row, kind, name, format string, args ...any) {
	l.problem(ochazuke.DeclProblem{File: l.file, Row: row, Name: name, Kind: kind, Text: fmt.Sprintf(format, args...)})
}

// try runs one row's declaration, a panic of the builder becoming the
// row's problem.
func (l *nounLoader) try(row string, fn func()) bool {
	if err := try(fn); err != nil {
		l.add(row, ochazuke.ProblemBadRow, "%v", err)
		return false
	}
	return true
}

func (l *nounLoader) load(f NounFile) {
	r, noun := l.r, f.Name
	b := r.Define(noun)
	l.flags(b, f)
	props := l.props(f)
	for _, p := range props {
		l.try(noun+".props."+p.Name, func() { b.Props(p) })
	}
	if f.Store != nil {
		l.store(b, noun, f.Store.Source)
	}
	for _, v := range f.Verbs {
		l.verb(b, noun, v)
	}
	for _, k := range f.Relations {
		if err := r.DeclareRelationKind(k); err != nil {
			l.add(noun+".relations."+k.Name, ochazuke.ProblemBadRow, "%v", err)
		}
	}
	for _, role := range sortedKeys(f.Hooks) {
		row := noun + ".hooks." + role
		if _, ok := r.Named(ochazuke.NamedHook, f.Hooks[role]); !ok {
			l.addNamed(row, ochazuke.ProblemMissingName, f.Hooks[role], "no hook %q is registered (Runtime.AddHook)", f.Hooks[role])
			continue
		}
		r.UseNamed(f.Hooks[role], row)
	}
}

// flags sets the noun's identity and flags, only those the file gives.
func (l *nounLoader) flags(b *ochazuke.NounBuilder, f NounFile) {
	if f.About != "" {
		b.About(f.About)
	}
	if f.Pin != "" {
		b.Pin(f.Pin)
	}
	if f.NoEditKey {
		b.EditKey(false)
	}
	if f.NoDefaultKeys {
		b.DefaultKeys(false)
	}
	if f.Unpersisted {
		b.Unpersisted()
	}
	if f.NoPicker {
		b.NoPicker()
	}
	if f.Main {
		b.Main()
	}
	if f.Live {
		b.Live()
	}
	if f.DefaultAction != "" {
		b.DefaultAction(f.DefaultAction)
	}
	if f.Parent != "" {
		b.Parent(f.Parent)
	}
	if len(f.Columns) > 0 {
		b.Columns(f.Columns...)
	}
	if len(f.Filters) > 0 {
		b.Filters(f.Filters...)
	}
	if len(f.PropsOff) > 0 {
		b.PropsOff(f.PropsOff...)
	}
}

// props are the file's props, each Inherit and the parent checked to name
// a reference prop of the noun (a prop with the owner role is an ordinary
// prop: its role and its Ref are on the prop).
func (l *nounLoader) props(f NounFile) []ochazuke.PropSpec {
	props := slices.Clone(f.Props)
	isRef := func(name string) bool {
		i := slices.IndexFunc(props, func(p ochazuke.PropSpec) bool { return p.Name == name })
		return i >= 0 && (len(props[i].Ref) > 0 || props[i].Noun != "")
	}
	for _, p := range props {
		if p.Inherit != "" && !isRef(p.Inherit) {
			l.add(f.Name+"."+p.Name+".inherit", ochazuke.ProblemBadRow, "inherit names %q, which is not a reference prop of the noun", p.Inherit)
		}
	}
	if f.Parent != "" && !slices.ContainsFunc(props, func(p ochazuke.PropSpec) bool { return p.Name == f.Parent }) {
		l.add(f.Name+".parent", ochazuke.ProblemBadRow, "the parent prop %q is not one of the noun's props", f.Parent)
	}
	return props
}

func (l *nounLoader) store(b *ochazuke.NounBuilder, noun, source string) {
	row := noun + ".store"
	n, ok := l.r.Named(ochazuke.NamedSource, source)
	if !ok {
		l.addNamed(row, ochazuke.ProblemMissingName, source, "no source %q is registered (Runtime.AddSource): the noun has no store", source)
		return
	}
	l.r.UseNamed(source, row)
	l.try(row, func() { b.Store(n.Value.(func() ochazuke.Store)()) })
}

// verb declares one verb row: the handler the table names, then the row's
// operation fields over what Verb registered, then its guard.
func (l *nounLoader) verb(b *ochazuke.NounBuilder, noun string, v NounVerb) {
	r := l.r
	row := noun + "." + v.Verb
	if v.Verb == "" {
		l.add(noun+".verbs", ochazuke.ProblemBadRow, "a verb row names no verb")
		return
	}
	h, ok := r.Named(ochazuke.NamedHandler, v.Handler)
	if !ok {
		l.addNamed(row, ochazuke.ProblemMissingName, v.Handler, "no handler %q is registered (Runtime.AddHandler): the verb is not declared", v.Handler)
		return
	}
	if !l.try(row, func() { b.Verb(v.Verb, h.Value, v.Description) }) {
		return
	}
	key := ochazuke.Key(r.CanonicalKey(row))
	registered := r.Operations.At(key)
	op := ochazuke.Operation(v.VerbFields)
	keepUnwritten(&op, registered)
	// What Verb fills is kept where the row leaves it out.
	op.Result, op.Kind = cmp.Or(op.Result, registered.Result), cmp.Or(op.Kind, registered.Kind)
	if op.Parameters == nil {
		op.Parameters = registered.Parameters
	}
	if h.Params != nil {
		if len(op.Parameters) > 0 && !ochazuke.SameParams(op.Parameters, h.Params) {
			l.addNamed(row, ochazuke.ProblemParams, v.Handler, "the row's parameters differ from handler %q's (%s); the handler's are used", v.Handler, cmp.Or(h.ParamType, "declared with WithParams"))
		}
		op.Parameters = h.Params
	}
	r.Operations.Set(op)
	if v.When != "" {
		l.try(row, func() { b.When(v.When) })
	}
	r.UseNamed(v.Handler, key.Short())
}

// keepUnwritten copies onto op every field of registered a row cannot
// hold (json "-": the key, the handler, the verb checks Register attached,
// ConfirmFor, ...), so a row sets only what its JSON says.
func keepUnwritten(op *ochazuke.Operation, registered ochazuke.Operation) {
	dst, src := reflect.ValueOf(op).Elem(), reflect.ValueOf(registered)
	t := dst.Type()
	for i := range t.NumField() {
		if f := t.Field(i); f.IsExported() && f.Tag.Get("json") == "-" {
			dst.Field(i).Set(src.Field(i))
		}
	}
}

// keys loads the file's binding groups as default rows sourced from the
// file, group by group, so a bad group is reported and the rest load.
func (l *nounLoader) keys(noun string, groups []json.RawMessage) {
	for i, g := range groups {
		data := append(append([]byte(`{"bindings": [`), g...), "]}"...)
		if err := l.r.LoadDefaultBindingsAhead(l.file, data); err != nil {
			l.add(fmt.Sprintf("%s.keys[%d]", noun, i), ochazuke.ProblemBadRow, "%v", err)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
