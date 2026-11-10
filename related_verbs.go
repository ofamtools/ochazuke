package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// RelatedSection is a section as the noun's things: what list and get
// answer, in display order (hidden ones too).
type RelatedSection struct {
	ID        string `json:"id"`
	Noun      string `json:"noun"`
	Prop      string `json:"prop"`
	Label     string `json:"label"`
	Count     int    `json:"count"`
	Collapsed bool   `json:"collapsed"`
	Hidden    bool   `json:"hidden"`
	Position  int    `json:"position"`
}

// RelatedView says which placed view stands for the noun's things in a
// related section ("my things owned by K"): a tree that keeps its subtasks
// and layout, drawn under the section's heading, in place of the generic
// list. The application narrows it to the key (RelatedSpec.Scope). The view
// must be registered before the section is made; it is parented under the
// section so the section's verbs and motions reach it.
func (b *NounBuilder) RelatedView(componentID string) *NounBuilder {
	b.n.relatedView = componentID
	return b
}

// relatedParams are the parameters every verb takes beside its own: the
// stack (default the one at the focus) and, for a section verb, the
// section (default the one at the focus).
var relatedParams = map[string]Parameter{
	"related": {Type: "string", Named: true, Noun: RelatedStackNoun, Description: "The related stack (default: the one at the focus, else the first)"},
}

func sectionParams(extra ...map[string]Parameter) map[string]Parameter {
	out := map[string]Parameter{"section": {Type: "string", Named: true, Noun: RelatedNoun, Description: "The section, NOUN.PROP (default: the one at the focus)"}}
	for k, v := range relatedParams {
		out[k] = v
	}
	for _, e := range extra {
		for k, v := range e {
			out[k] = v
		}
	}
	return out
}

// registerRelated declares the related-section noun, its verbs, guards and
// default rows (bindings/related.json).
func registerRelated(r *Runtime) {
	r.Define(RelatedStackNoun).About("A stack of related sections: one section per noun that names the thing, drawn one under the other")
	registerRelatedFacts(r)
	registerRelatedGuards(r)
	b := r.Define(RelatedNoun)
	registerRelatedFolds(b)
	registerRelatedRefresh(b)
	registerRelatedMoves(b)
	registerRelatedWrites(b)
	registerRelatedSettings(b)
}

func registerRelatedFacts(r *Runtime) {
	r.defineFacts(factsSpec{Noun: RelatedNoun, Sample: RelatedSection{}, Key: "id", Label: "label",
		Docs: map[string]string{"id": "The section's place: NOUN.PROP of the referring prop", "noun": "The noun whose things the section lists", "prop": "The prop that names the thing",
			"label": "The section's heading", "count": "How many things it lists", "collapsed": "Whether it is folded to its heading",
			"hidden": "Whether the hidden setting keeps it out", "position": "Its place in the display order, from 1"},
		ListParams: relatedParams,
		List: func(c Ctx, q ListQuery) ([]Thing, error) {
			rel, err := relatedStack(c, StringArg(q.Args, "related", ""))
			if err != nil {
				return nil, err
			}
			rel.sync()
			rows := make([]Thing, 0, len(rel.sections))
			for _, s := range rel.order() {
				rows = append(rows, s.thing())
			}
			return rows, nil
		}})
}

func registerRelatedGuards(r *Runtime) {
	// A heading (or the empty row) is a row of the section list that is not
	// a thing of the section's noun.
	r.DefineGuard("related-on-heading", func(r *Runtime, ev KeyEvent) bool {
		inst := r.Instances[ev.Focus]
		s := r.relatedSectionOf(ev.Focus)
		return inst != nil && s != nil && inst.Component == nil && inst.Noun != s.ref.Noun
	})
	r.DefineGuard("related-on-row", func(r *Runtime, ev KeyEvent) bool {
		inst := r.Instances[ev.Focus]
		s := r.relatedSectionOf(ev.Focus)
		return inst != nil && s != nil && inst.Noun == s.ref.Noun
	})
}

// sectionVerb declares a verb that acts on one section.
func sectionVerb(b *NounBuilder, verb, desc string, run func(c Ctx, s *relatedSection) error, extra ...map[string]Parameter) {
	b.Verb(verb, func(c Ctx) Result {
		s, err := relatedTarget(c)
		if err != nil {
			return Failed(err)
		}
		if err := run(c, s); err != nil {
			return Failed(err)
		}
		return Done(s.thing())
	}, desc).Params(sectionParams(extra...))
}

// registerRelatedFolds declares the verbs that fold sections.
func registerRelatedFolds(b *NounBuilder) {
	sectionVerb(b, "toggle", "Fold the section to its heading, or unfold it", func(c Ctx, s *relatedSection) error {
		s.setCollapsed(!s.collapsed())
		return nil
	})
	sectionVerb(b, "open", "Unfold the section: its things show", func(c Ctx, s *relatedSection) error { s.setCollapsed(false); return nil })
	sectionVerb(b, "close", "Fold the section to its heading", func(c Ctx, s *relatedSection) error { s.setCollapsed(true); return nil })
	for _, spec := range []struct {
		verb, desc string
		fold       bool
	}{{"open-all", "Unfold every section", false}, {"close-all", "Fold every section to its heading", true}} {
		b.Verb(spec.verb, func(c Ctx) Result {
			rel, err := relatedStack(c, StringArg(c.Args, "related", ""))
			if err != nil {
				return Failed(err)
			}
			rel.sync()
			for _, s := range rel.sections {
				s.setCollapsed(spec.fold)
			}
			return Done(nil)
		}, spec.desc).Params(relatedParams)
	}
}

// registerRelatedRefresh declares refresh: read the section's things (every
// hook section when none is named) again.
func registerRelatedRefresh(b *NounBuilder) {
	b.Verb("refresh", func(c Ctx) Result {
		rel, err := relatedStack(c, StringArg(c.Args, "related", ""))
		if err != nil {
			return Failed(err)
		}
		rel.sync()
		if id := StringArg(c.Args, "section", ""); id != "" {
			s := rel.byID[id]
			if s == nil {
				return Failed(fmt.Errorf("unknown section %q", id))
			}
			s.fill()
			return Done(s.thing())
		}
		rel.fillAll()
		rel.read = rel.now()
		return Done(nil)
	}, "Read the sections' things again (every section, or the one named)").Params(sectionParams())
}

// registerRelatedMoves declares the verbs that move the focus from
// heading to heading.
func registerRelatedMoves(b *NounBuilder) {
	walk := func(verb, desc string, params map[string]Parameter, pick func(c Ctx, visible []*relatedSection, at int) int) {
		b.Verb(verb, func(c Ctx) Result {
			rel, err := relatedStack(c, StringArg(c.Args, "related", ""))
			if err != nil {
				return Failed(err)
			}
			rel.sync()
			visible := rel.visible()
			at := -1
			if s := c.Runtime.relatedSectionOf(focusOf(c.Context)); s != nil && s.rel == rel {
				at = slices.Index(visible, s)
			}
			to := pick(c, visible, at)
			if to < 0 || to >= len(visible) || to == at {
				return Done(nil)
			}
			s := visible[to]
			_ = s.list.Select(s.headID())
			if err := c.Runtime.SetFocus(s.list.FocusTarget()); err != nil {
				return Failed(err)
			}
			return Done(s.thing())
		}, desc).Params(params).Motion()
	}
	walk("next", "Go to the next section's heading", relatedParams, func(_ Ctx, v []*relatedSection, at int) int { return min(at+1, len(v)-1) })
	walk("previous", "Go to the previous section's heading", relatedParams, func(_ Ctx, v []*relatedSection, at int) int { return max(at-1, 0) })
	edge := func(last bool) func(Ctx, []*relatedSection, int) int {
		return func(_ Ctx, v []*relatedSection, _ int) int {
			if last {
				return len(v) - 1
			}
			return 0
		}
	}
	walk("first", "Go to the first section's heading", relatedParams, edge(false))
	walk("last", "Go to the last section's heading", relatedParams, edge(true))
}

// registerRelatedWrites declares the verbs that write things: new,
// associate and unassociate run the referring noun's own create and set
// (rights, history, undo and Through are the noun's).
func registerRelatedWrites(b *NounBuilder) {
	sectionVerb(b, "new", "Create a thing of the section's noun that belongs to the section's thing: the noun's create with the referring prop filled", func(c Ctx, s *relatedSection) error {
		return s.run(c, Request{Noun: s.ref.Noun, Verb: "create", Args: Arguments{s.ref.Prop: s.value()}}, false)
	})
	sectionVerb(b, "associate", "Set the referring prop of existing things of the section's noun to the section's thing: name one with thing, or be asked which", func(c Ctx, s *relatedSection) error {
		if err := s.readOnly("associate"); err != nil {
			return err
		}
		if thing := StringArg(c.Args, "thing", ""); thing != "" {
			return s.run(c, Request{Noun: s.ref.Noun, Prop: s.ref.Prop, Value: s.value(), Target: thing}, false)
		}
		// No thing named: ask for it, over the noun's own set.
		return s.run(c, Request{Noun: s.ref.Noun, Verb: "set", Args: Arguments{"props": map[string]any{s.ref.Prop: s.value()}}}, true)
	}, map[string]Parameter{"thing": {Type: "string", Named: true, Description: "The thing to associate (its key); asked for when missing"}})
	sectionVerb(b, "unassociate", "Clear the referring prop of the selected (or marked) things of the section: they become unassociated, never deleted; one undo step", func(c Ctx, s *relatedSection) error {
		if err := s.readOnly("unassociate"); err != nil {
			return err
		}
		focus := c.Runtime.relatedFocus(c)
		subjects := s.subjects(focus)
		if len(subjects) == 0 {
			return fmt.Errorf("no %s selected in %s", shortNounName(s.ref.Noun), s.ref.Label)
		}
		defer c.Runtime.inHistoryGroup(c.Runtime.newHistoryGroup("unassociate " + s.ref.ID()))()
		for _, id := range subjects {
			if _, err := c.Runtime.ExecuteAt(c.Runtime.contextOf(focus), Request{Noun: s.ref.Noun, Prop: s.ref.Prop, Value: "", Target: id}); err != nil {
				return err
			}
		}
		s.fill()
		return nil
	})
}

// registerRelatedSettings declares the verbs that write the stack's hidden
// and order settings, named by the spec.
func registerRelatedSettings(b *NounBuilder) {
	write := func(verb, desc string, name func(rel *Related) string, current func(rel *Related) []string, edit func(s *relatedSection, cur []string) ([]string, error)) {
		sectionVerb(b, verb, desc, func(c Ctx, s *relatedSection) error {
			setting := name(s.rel)
			if setting == "" {
				return fmt.Errorf("the stack names no setting for %s", verb)
			}
			next, err := edit(s, current(s.rel))
			if err != nil {
				return err
			}
			return c.Runtime.Settings().Set(setting, next)
		})
	}
	hidden := func(rel *Related) string { return rel.spec.Hidden }
	ordered := func(rel *Related) string { return rel.spec.Order }
	listed := func(rel *Related) []string { return rel.listSetting(rel.spec.Hidden) }
	ids := func(rel *Related) []string {
		var out []string
		for _, s := range rel.order() {
			out = append(out, s.ref.ID())
		}
		return out
	}
	write("hide", "Keep the section out of the stack (writes the hidden setting)", hidden, listed, func(s *relatedSection, cur []string) ([]string, error) {
		if s.rel.hiddenBy(s.ref.ID()) != "" {
			return cur, nil
		}
		return append(cur, s.ref.ID()), nil
	})
	write("show", "Bring a hidden section back (writes the hidden setting)", hidden, listed, func(s *relatedSection, cur []string) ([]string, error) {
		if by := s.rel.hiddenBy(s.ref.ID()); by != "" && by != s.ref.ID() {
			return nil, fmt.Errorf("%s is hidden by the pattern %s: change the setting", s.ref.ID(), by)
		}
		return slices.DeleteFunc(slices.Clone(cur), func(e string) bool { return strings.TrimSpace(e) == s.ref.ID() }), nil
	})
	shift := func(delta int) func(s *relatedSection, cur []string) ([]string, error) {
		return func(s *relatedSection, cur []string) ([]string, error) {
			at := slices.Index(cur, s.ref.ID())
			to := at + delta
			if at < 0 || to < 0 || to >= len(cur) {
				return cur, nil
			}
			cur = slices.Clone(cur)
			cur[at], cur[to] = cur[to], cur[at]
			return cur, nil
		}
	}
	write("move-up", "Move the section up the stack (writes the order setting)", ordered, ids, shift(-1))
	write("move-down", "Move the section down the stack (writes the order setting)", ordered, ids, shift(1))
}

// doneOr is Done(nil), or a failed result for err.
func doneOr(err error) Result {
	if err != nil {
		return Failed(err)
	}
	return Done(nil)
}

// relatedStack is the stack a verb acts on: the one named, else the one at
// the focus, else the first registered.
func relatedStack(c Ctx, id string) (*Related, error) {
	r := c.Runtime
	if id != "" {
		if rel := r.Related(id); rel != nil {
			return rel, nil
		}
		return nil, fmt.Errorf("unknown related stack %q", id)
	}
	if rel := r.relatedOf(focusOf(c.Context)); rel != nil {
		return rel, nil
	}
	var first *Related
	var seq uint64
	for _, inst := range r.Instances {
		if rel, ok := inst.Component.(*Related); ok && (first == nil || inst.seq < seq) {
			first, seq = rel, inst.seq
		}
	}
	if first == nil {
		return nil, errors.New("no related sections")
	}
	return first, nil
}

// relatedTarget is the section a verb acts on: the one named, else the one
// at the focus.
func relatedTarget(c Ctx) (*relatedSection, error) {
	rel, err := relatedStack(c, StringArg(c.Args, "related", ""))
	if err != nil {
		return nil, err
	}
	rel.sync()
	if id := StringArg(c.Args, "section", ""); id != "" {
		if s := rel.byID[id]; s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("unknown section %q", id)
	}
	if s := c.Runtime.relatedSectionOf(focusOf(c.Context)); s != nil && s.rel == rel {
		return s, nil
	}
	return nil, errors.New("no section at the focus: name one with section")
}

// readOnly refuses a write of a read-only referring prop, saying why.
func (s *relatedSection) readOnly(verb string) error {
	if s.ref.ReadOnly {
		return fmt.Errorf("cannot %s: %s.%s is read-only", verb, s.rel.r.DisplayNoun(s.ref.Noun), s.ref.Prop)
	}
	return nil
}

// run runs a request from the focus as a key would: a missing argument
// opens the args form (or, with ask, the form opens first).
func (s *relatedSection) run(c Ctx, q Request, ask bool) error {
	r := c.Runtime
	focus := r.relatedFocus(c)
	_, err := r.runOrAsk(focus, q, func(q Request) (any, error) { return r.ExecuteAt(r.contextOf(focus), q) }, ask, nil)
	if err == nil {
		s.fill()
	}
	return err
}

// subjects are the instances unassociate acts on: the section's marked
// things, else the thing at the focus.
func (s *relatedSection) subjects(focus string) []string {
	var out []string
	for _, item := range s.list.Marked() {
		if item.Noun == s.ref.Noun {
			out = append(out, item.ID)
		}
	}
	if len(out) == 0 {
		if inst := s.rel.r.Instances[focus]; inst != nil && inst.Noun == s.ref.Noun {
			out = append(out, inst.ID)
		}
	}
	return out
}

// relatedFocus is the focus a verb acts from: the one in its context, or,
// when that is a section itself (a request addressed to the noun), the live
// focus if it is inside that section.
func (r *Runtime) relatedFocus(c Ctx) string {
	focus := focusOf(c.Context)
	if inst := r.Instances[focus]; inst != nil && inst.Component != nil && r.focusInside(focus) {
		return r.Focus
	}
	return focus
}
