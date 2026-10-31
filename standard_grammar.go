package ochazuke

import (
	"slices"
	"strings"
)

// The standard verbs' grammar (standard verbs phase 1, lane S3): one
// reading for the CLI and the palette, read from the props and the
// parameters, never from a noun's or a verb's name.
//
//	NOUN WORD ...           verb > prop > key/instance (owner decision 18):
//	                        a verb of the noun is the verb; else a prop of
//	                        it is the NOUN PROP [VALUE] shorthand (a read,
//	                        or a write with a value or --toggle); else a
//	                        key or an instance, which keeps today's reading
//	                        (the palette's default verb)
//	NOUN VERB [KEY] ...     the subject (a Focus parameter) may be left
//	                        out: a first word that is the next parameter's
//	                        (a prop name, or prop=value for a pairs
//	                        parameter) skips it, and the subject comes from
//	                        the focus, a target or --KEY (SubjectOmitted)
//	set NOUN ...            a bare alias that shadows a verb reads only
//	                        one-value nouns (owner decision 17): a noun
//	                        whose verb is the application's own, or whose
//	                        store is a singleton (shadowsNoun)
//
// Each tier matches a whole word before a prefix: an exact verb, an exact
// prop, a unique verb prefix, a unique prop prefix.

// Word kinds ReadNounWord answers.
const (
	NounWordVerb = "verb"
	NounWordProp = "prop"
)

// NounWord is how the word after a noun reads (ReadNounWord): Kind is
// NounWordVerb or NounWordProp with Value the whole word, or "" when it is
// neither (a key or an instance: the caller's own reading), with
// Candidates when it is a prefix of several verbs or several props.
type NounWord struct {
	Kind       string
	Value      string
	Candidates []string
}

// ReadNounWord reads the word after noun (owner decision 18): verbs are the
// noun's verb words where the line is read. An exact verb wins, then an
// exact prop, then a unique verb prefix, then a unique prop prefix.
func (r *Runtime) ReadNounWord(noun, word string, verbs []string) NounWord {
	options := make([]option, len(verbs))
	for i, v := range verbs {
		options[i] = option{Kind: NounWordVerb, Value: v}
	}
	return r.readNounWord(noun, word, options)
}

func (r *Runtime) readNounWord(noun, word string, verbs []option) NounWord {
	verb, verbs, verbExact := matchWord(word, verbs)
	if verb.Value != "" && verbExact {
		return NounWord{Kind: NounWordVerb, Value: verb.Value}
	}
	if len(verbs) > 1 && verbExact {
		return NounWord{Candidates: optionValues(verbs)}
	}
	if p := r.Noun(noun).prop(word); p != nil && len(r.propOptions(noun)) > 0 {
		return NounWord{Kind: NounWordProp, Value: p.Name}
	}
	prop, props, propExact := matchWord(word, r.propOptions(noun))
	switch {
	case prop.Value != "" && propExact:
		return NounWord{Kind: NounWordProp, Value: prop.Value}
	case verb.Value != "":
		return NounWord{Kind: NounWordVerb, Value: verb.Value}
	case len(verbs) > 1:
		return NounWord{Candidates: optionValues(verbs)}
	case prop.Value != "":
		return NounWord{Kind: NounWordProp, Value: prop.Value}
	case len(props) > 1:
		return NounWord{Candidates: optionValues(props)}
	}
	return NounWord{}
}

// propOptions are the props a word after noun may name: every prop of a
// noun with a store that reads or writes its things (get or set).
func (r *Runtime) propOptions(noun string) []option {
	n := r.Noun(noun)
	if n == nil || n.Store == nil {
		return nil
	}
	_, get := r.Operations[n.Name+".get"]
	_, set := r.Operations[n.Name+".set"]
	if !get && !set {
		return nil
	}
	out := make([]option, len(n.Props))
	for i, p := range n.Props {
		out[i] = option{Kind: NounWordProp, Value: p.Name}
	}
	return out
}

// PropShorthand is the request NOUN PROP [VALUE] runs (plan 2.6): a
// prop-shaped request, so it reads (no value), writes (a value) or flips
// a boolean (toggle) the thing it acts for, as the noun's standard get or
// set; args hold at most the subject's key (--KEY).
func PropShorthand(noun, prop string, value any, toggle bool, args Arguments) Request {
	q := Request{Noun: noun, Prop: prop, Value: value, Toggle: toggle}
	if len(args) > 0 {
		q.Args = args
	}
	return q
}

// SubjectOmitted reports whether word, met where the first of open (the
// parameters still unfilled, in line order) is a subject filled from the
// focus (Parameter.Focus), is the next parameter's word instead: one of
// its Enum values, or NAME=VALUE with NAME one of them when it takes Pairs.
// The subject then comes from the focus, a target or --KEY (the flag named
// after the key prop). A thing whose key equals a prop name is reached
// with --KEY.
func (o Operation) SubjectOmitted(open []string, word string) bool {
	if len(open) < 2 || !o.Parameters[open[0]].Focus {
		return false
	}
	next := o.Parameters[open[1]]
	name := word
	if before, _, ok := strings.Cut(word, "="); ok && next.Pairs {
		name = before
	}
	return slices.Contains(next.Enum, name)
}

// shadowsNoun reports whether a bare alias that shadows verb (set) reads
// word as a noun (owner decision 17): a noun whose verb of that name is the
// application's own (not Standard), or whose store is a singleton. A
// collection's or an instances store's standard verb is reached as NOUN
// VERB, never as VERB NOUN, so its name never takes a setting's prefix.
func (r *Runtime) shadowsNoun(word, verb string) bool {
	op, ok := r.Operation(r.CanonicalNoun(word) + "." + verb)
	if !ok || op.Standard == "" {
		return true
	}
	n := r.Nouns[op.Noun]
	return n != nil && n.Store != nil && n.Store.Kind == Singleton
}

// shadowWords keeps the noun words a bare alias that shadows verb reads
// (shadowsNoun).
func (r *Runtime) shadowWords(words []string, verb string) []string {
	return slices.DeleteFunc(slices.Clone(words), func(w string) bool { return !r.shadowsNoun(w, verb) })
}

// choiceParameter is the parameter whose choices a word for op's
// parameter name completes to, and is checked against, given the
// arguments the line has so far: a ValueOf parameter's is the prop it
// names (its Enum, Source or Noun, a boolean's type), else the parameter
// itself. The prop is "" when the parameter is not ValueOf or names none.
func (r *Runtime) choiceParameter(op Operation, name string, args Arguments) (Parameter, string) {
	param := op.Parameters[name]
	if param.ValueOf == "" {
		return param, ""
	}
	prop := StringArg(args, param.ValueOf, "")
	p := r.Noun(op.Noun).prop(prop)
	if p == nil {
		return Parameter{Type: param.Type, Free: true, Description: param.Description}, ""
	}
	out := p.Parameter
	out.Enum = slices.Clone(p.Enum)
	out.Required = false
	if out.Type == "" {
		out.Type = SettingString
	}
	if out.Type != SettingBoolean && len(out.Enum) == 0 && out.Source == "" && out.Noun == "" {
		out.Free = true
	}
	if out.Description == "" {
		out.Description = param.Description
	}
	return out, p.Name
}

// pairChoices are a Pairs parameter's choices for word (owner decision
// 18's where and set's prop=value words): with NAME= typed, the values of
// prop NAME as NAME=VALUE; without, NAME= for each prop of the noun (the
// caller offers the Enum instead when the parameter has one).
func (r *Runtime) pairChoices(c Context, op Operation, word string, siblings ...Arguments) ([]Choice, error) {
	n := r.Noun(op.Noun)
	if n == nil {
		return nil, nil
	}
	name, _, typed := strings.Cut(word, "=")
	if !typed {
		out := make([]Choice, 0, len(n.Props))
		for _, p := range n.Props {
			out = append(out, Choice{ID: p.Name + "=", Label: p.Name + "="})
		}
		return out, nil
	}
	p := n.prop(name)
	if p == nil {
		return nil, nil
	}
	values, err := r.propChoices(c, *p, siblings...)
	out := make([]Choice, len(values))
	for i, v := range values {
		label := v.Label
		if label == "" {
			label = v.ID
		}
		out[i] = Choice{ID: name + "=" + v.ID, Label: name + "=" + label}
	}
	return out, err
}

// propChoices are the values a prop takes: true and false for a boolean,
// else its Enum, Source or Noun (Runtime.Choices).
func (r *Runtime) propChoices(c Context, p PropSpec, siblings ...Arguments) ([]Choice, error) {
	if p.Type == SettingBoolean {
		return []Choice{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}}, nil
	}
	return r.Choices(c, p.Parameter, siblings...)
}

// The palette.standard-verbs setting (plan 2.10, owner decision 25): one
// list setting for every standard-verb change to the palette. Its parts
// are an ordering (StandardVerbsLast, StandardVerbsMixed or
// StandardVerbsHidden; none is mixed, today's one alphabetical list) and
// the flags StandardVerbsTag and StandardVerbsRecord. A part left out
// returns its piece to today's behaviour, so [] turns them all off.
const (
	StandardVerbsSetting = "palette.standard-verbs"
	// StandardVerbsLast lists a noun's named verbs before its standard
	// ones in the palette's verb column (the default).
	StandardVerbsLast = "last"
	// StandardVerbsMixed lists them in one alphabetical list.
	StandardVerbsMixed = "mixed"
	// StandardVerbsHidden leaves standard verbs out of the verb column;
	// typing the name, the CLI and bindings still reach them.
	StandardVerbsHidden = "hidden"
	// StandardVerbsTag marks a standard verb "std" in the verb column.
	StandardVerbsTag = "tag"
	// StandardVerbsRecord shows a standard get's answer (one thing) as
	// prop and value rows in the palette's results view.
	StandardVerbsRecord = "record"
)

// StandardVerbsView is what palette.standard-verbs asks of the palette:
// the ordering of the verb column and whether the std tag and get's record
// rows show.
type StandardVerbsView struct {
	Order  string
	Tag    bool
	Record bool
}

// StandardVerbsView reads palette.standard-verbs: the first ordering part
// it holds (mixed when none), and the tag and record parts. Unknown parts
// are ignored. Read each time the palette lists or shows a result.
func (r *Runtime) StandardVerbsView() StandardVerbsView {
	parts := r.Settings().Strings(StandardVerbsSetting)
	var out StandardVerbsView
	for _, part := range parts {
		switch part {
		case StandardVerbsLast, StandardVerbsMixed, StandardVerbsHidden:
			if out.Order == "" {
				out.Order = part
			}
		case StandardVerbsTag:
			out.Tag = true
		case StandardVerbsRecord:
			out.Record = true
		}
	}
	if out.Order == "" {
		out.Order = StandardVerbsMixed
	}
	return out
}

// standardOnly reports whether every offered operation the palette's verb
// word names (on noun word, or on any noun when word is "") is a
// generated standard verb.
func (ix lineIndex) standardOnly(verb, word string) bool {
	if word != "" {
		op := ix.ops[word+"."+verb]
		return op.Standard != ""
	}
	nouns := ix.verbs[verb]
	if len(nouns) == 0 {
		return false
	}
	for _, n := range nouns {
		if ix.ops[n+"."+verb].Standard == "" {
			return false
		}
	}
	return true
}

// recordRows is a standard get's answer as the palette's results show it
// (StandardVerbsRecord): one row per prop, in the noun's declaration order,
// {prop, value}; false when value is not one thing's record.
func recordRows(n *Noun, value any) ([]map[string]any, bool) {
	record, ok := value.(map[string]any)
	if !ok || n == nil {
		return nil, false
	}
	rows := []map[string]any{}
	for _, p := range n.Props {
		if v, has := record[p.Name]; has {
			rows = append(rows, map[string]any{"prop": p.Name, "value": v})
		}
	}
	return rows, true
}
