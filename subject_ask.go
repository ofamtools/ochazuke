package ochazuke

import (
	"fmt"
	"slices"
)

// SubjectAskSetting enables asking for a declared subject a human request lacks.
const SubjectAskSetting = "subject.ask"

// WalkColumnsSetting is the number of visible columns in the relation walk.
const WalkColumnsSetting = "walk.columns"

func (r *Runtime) declaredSubject(op Operation, p Parameter) bool {
	if p.Plumbing || op.Motion || op.FocusBound || !p.Required && (p.Covers || op.Creates) {
		return false
	}
	own := p.Noun != "" && r.CanonicalNoun(p.Noun) == r.CanonicalNoun(op.Noun())
	n := r.Nouns[op.Noun()]
	implicit := op.Kind == KindApplication && n != nil && StoredKind(storedNounKind(n))
	return p.Focus || p.From != "" || own && implicit || p.Required && asksSubjectParameter(p)
}

func asksSubjectParameter(p Parameter) bool {
	return !p.Plumbing && (p.Noun != "" || p.IsPath() || p.Focus || p.From != "")
}

func (r *Runtime) subjectAsks() bool { return r.settingOn(SubjectAskSetting) }

// subjectHere reads only candidates already available at the origin: declared
// row sources, the nearest noun or its associations, and typed path props.
// Store and relation graph reads belong to walkSource, not this scope.
func (r *Runtime) subjectHere(p Parameter, origin Context) []Choice {
	focus := focusOf(origin)
	var out []Choice
	add := func(c Choice) {
		if c.ID != "" && !slices.ContainsFunc(out, func(one Choice) bool { return one.ID == c.ID }) {
			out = append(out, c)
		}
	}
	if p.From != "" {
		if value := r.argument(p.From, KeyEvent{Focus: focus}); !emptyProp(value) {
			text := fmt.Sprint(value)
			add(Choice{ID: text, Label: text, Noun: p.Noun})
		}
	}
	if p.Noun != "" && p.Noun != AnyNoun {
		id, candidates := r.nearestOf(p.Noun, focus)
		if id != "" {
			inst := r.Instances[id]
			t := r.instanceThing(inst)
			add(Choice{ID: t.Key, Label: t.Label, Noun: inst.Noun})
		}
		for _, t := range candidates {
			add(Choice{ID: t.Key, Label: t.Label, Noun: p.Noun})
		}
	}
	for _, id := range r.Ancestry(focus) {
		inst := r.Instances[id]
		if p.Noun == AnyNoun && inst.Parent != "" {
			t := r.instanceThing(inst)
			add(Choice{ID: AnyRef(inst.Noun, t.Key), Label: t.Label, Noun: inst.Noun})
			break
		}
		if !p.IsPath() {
			continue
		}
		n := r.Nouns[inst.Noun]
		if n == nil {
			continue
		}
		for _, prop := range n.Props {
			if !prop.IsPath() {
				continue
			}
			if value := r.rowValue(inst, prop.Name); !emptyProp(value) {
				add(Choice{ID: fmt.Sprint(value), Label: prop.Name + " · " + fmt.Sprint(value), Noun: inst.Noun})
			}
		}
		if len(out) > 0 {
			break
		}
	}
	return out
}

// subjectScopes adds nearby candidates and the graph walk only when a request
// asks for a missing subject. Ordinary record-choice editing keeps its sources.
func (r *Runtime) subjectScopes(spec PickerSpec, l choiceLoad) (PickerSpec, bool) {
	if !l.ask || l.param.Noun == "" || !r.subjectAsks() || !r.settingOn(PickerScopesSetting) {
		return spec, false
	}
	origin := l.ctx
	if r.TopModal() != "" {
		origin = r.contextOf(r.BaseFocus())
	}
	spec.Scoped, spec.NormalMode, spec.StartNormal = true, true, l.ask
	if here := r.subjectHere(l.param, origin); len(here) > 0 {
		rows := make([]PickerRow, 0, len(here)+len(l.fixed))
		for _, ch := range here {
			rows = append(rows, l.choiceRow(ch))
		}
		rows = append(rows, l.fixed...)
		spec.Sources = append(spec.Sources, PickerSource{
			Name: "here", Load: func(string) Result { return Done(rows) },
			Count: func(string) (int, bool) { return len(here), true },
		})
	}
	scopes := r.scopesOf(l.param, l.siblings())
	if len(scopes) == 0 {
		scopes = []Scope{{Name: "list"}}
	}
	var countWidest func(string)
	for _, scope := range scopes {
		param := scope.within(l.param, l.siblings())
		source, count := r.subjectListScope(scope.Name, param, l, spec.Kind)
		countWidest = count
		if param.Noun == AnyNoun && param.Source == "" {
			anyRefNavigation(&source)
		}
		spec.Sources = append(spec.Sources, source)
	}
	// Count only the widest list scope in advance, through the same replacing
	// choices delivery as the palette. Count callbacks never perform IO.
	for i := range spec.Sources[:len(spec.Sources)-1] {
		load := spec.Sources[i].Load
		spec.Sources[i].Load = func(query string) Result {
			result := load(query)
			countWidest(query)
			return result
		}
	}
	spec.Sources = append(spec.Sources, r.walkSource(l.param, origin, l.choiceRow))
	choose := spec.Choose
	spec.Choose = func(row PickerRow) Result {
		// A walk may reach an archived thing or a thing absent from the list
		// source. Keep the selected value as a valid choice for the form.
		if l.landed != nil {
			l.landed([]Choice{{ID: row.ID, Label: row.Label, Noun: row.Noun, Props: row.Props}})
		}
		if choose != nil {
			return choose(row)
		}
		return Done(row.ID)
	}
	return spec, true
}

// subjectListScope reuses the choice loader and caches its count. prefetch
// reads the widest scope asynchronously before it is selected, so the tag can
// say what archived/all adds without executing a source during rendering.
func (r *Runtime) subjectListScope(name string, param Parameter, l choiceLoad, owner string) (source PickerSource, prefetch func(string)) {
	live := r.ChoicesTakeQuery(param)
	key := func(query string) string {
		if live {
			return query
		}
		return ""
	}
	var query string
	var total int
	var ready, pending bool
	original := l.landed
	l.landed = func(choices []Choice) {
		total, ready, pending = len(choices), true, false
		if original != nil {
			original(choices)
		}
	}
	load := r.choiceSourceLoad(l, param)
	source = PickerSource{
		Name: name, Live: live,
		Load: func(text string) Result {
			query, ready, pending = key(text), false, true
			result := load(text)
			if result.err != nil {
				pending = false
			}
			return result
		},
		Count: func(text string) (int, bool) { return total, ready && query == key(text) },
	}
	prefetch = func(text string) {
		want := key(text)
		if query == want && (ready || pending) {
			return
		}
		query, ready, pending = want, false, true
		done := func(choices []Choice, err error) {
			if query == want {
				total, ready, pending = len(choices), err == nil, false
			}
		}
		choices, waiting, err := r.startChoices(l.ctx, param, text, owner, "subject-count", done, l.siblings())
		if !waiting {
			done(choices, err)
		}
	}
	return source, prefetch
}

// canAskSubject lets a bound row reach the ordinary ask path even when a
// subject-dependent verb guard cannot hold at the current focus. Row guards,
// disabled settings and actor rights are not bypassed.
func (r *Runtime) canAskSubject(q Request, focus string) bool {
	op, ok := r.Operation(q.Key())
	if !ok || !r.subjectAsks() || !r.missingOpensForm() || r.unavailableReason(op) != "" || r.missingRole(q) != nil {
		return false
	}
	return len(r.missingTargets(op, q.Args, focus)) > 0
}

func (r *Runtime) mayAskParameter(p Parameter) bool {
	return !asksSubjectParameter(p) || r.subjectAsks()
}

func (r *Runtime) pathSubjectScopes(sources []PickerSource, pick PathPick, origin Context, start string) ([]PickerSource, string) {
	graph := r.walkSource(pick.Parameter, origin, func(ch Choice) PickerRow {
		row := ch.Row()
		row.Data = ch.ID
		return row
	})
	var typed PickerSource
	disk := sources[:0]
	for _, source := range sources {
		if source.Name == PathSourceTyped {
			typed = source
			continue
		}
		name, label := source.Name, source.Label
		source.Label = func() string {
			if label != nil {
				return "on disk · " + label()
			}
			return "on disk · " + name
		}
		if source.Name == graph.Name {
			if start == source.Name {
				start = "on disk · " + source.Name
			}
			source.Name = "on disk · " + source.Name
		}
		disk = append(disk, source)
	}
	return append(disk, graph, typed), start
}
