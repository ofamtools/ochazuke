package ochazuke

import (
	"fmt"
	"slices"
)

// Subjects: a request acts on one instance (its target, else the focus)
// or on several (Request.Subjects: the rows of a visual selection). A verb
// that takes many (Operation.Many, NounBuilder.Many) runs once and reads
// them all from Ctx.Subjects, so it asks one question and makes one
// change; any other verb runs once per subject, in display order
// (eachSubject). Which one a verb is comes from its operation, never from
// its name: the runtime has no list of verbs that fan out.

// subjectsOf is the instances q acts on: its Subjects resolved (focus, the
// instance execute resolved the target to, among them), else focus alone.
// Every subject must be an instance of the focus's noun.
func (r *Runtime) subjectsOf(q Request, focus string) ([]string, error) {
	if len(q.Subjects) == 0 {
		return []string{focus}, nil
	}
	noun := r.Instances[focus].Noun
	out := make([]string, 0, len(q.Subjects)+1)
	for _, s := range q.Subjects {
		id, err := r.TargetInstance(s)
		if err != nil {
			return nil, err
		}
		if inst := r.Instances[id]; inst == nil || inst.Noun != noun {
			return nil, fmt.Errorf("subject %q is not a %s", s, r.DisplayNoun(noun))
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if !slices.Contains(out, focus) {
		out = append([]string{focus}, out...)
	}
	return out, nil
}

// takeSubjects hands the application handler about to run on inst the
// subjects execute resolved for it, and forgets them, so a request the
// handler runs in turn starts from its own. inst alone when there are none.
func (r *Runtime) takeSubjects(inst *Instance) []*Instance {
	ids := r.subjects
	r.subjects = nil
	if inst == nil || !slices.Contains(ids, inst.ID) {
		if inst == nil {
			return nil
		}
		return []*Instance{inst}
	}
	out := make([]*Instance, 0, len(ids))
	for _, id := range ids {
		if s := r.Instances[id]; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// SubjectIDs are the ids of ctx's subjects, for a handler that hands them
// on (Request.Subjects of the request it runs next: a confirm, a chooser's
// choice).
func (ctx Ctx) SubjectIDs() []string {
	out := make([]string, 0, len(ctx.Subjects))
	for _, s := range ctx.Subjects {
		out = append(out, s.ID)
	}
	return out
}

// eachSubject runs q once per subject, in order, each a request of its own
// with that subject as target (already confirmed when q was). One notice
// reports the outcome: an info when every subject took it, an error naming
// how many were done and how many failed otherwise; only when none was
// done does the error return as well. A run that opens a modal (a chooser,
// a confirm the handler opened itself) stops the loop: the question is
// about that subject, so the rest are not run and the notice says so (the
// verb wants Many). The result is {verb, applied, skipped}, list.apply's.
func (r *Runtime) eachSubject(q Request, subjects []string) (any, error) {
	modal := r.TopModal()
	defer r.undoGroup(subjects)()
	applied, skipped := []string{}, []string{}
	var first error
	stopped := 0
	for i, id := range subjects {
		one := q
		one.Target, one.Subjects, one.Args = id, nil, cloneArgs(q.Args)
		if _, err := r.Execute(one); err != nil {
			skipped = append(skipped, id+": "+err.Error())
			if first == nil {
				first = err
			}
		} else {
			applied = append(applied, id)
		}
		if top := r.TopModal(); top != modal && top != "" && i < len(subjects)-1 {
			stopped = len(subjects) - 1 - i
			for _, rest := range subjects[i+1:] {
				skipped = append(skipped, rest+": not run: "+q.Key()+" asked about "+id)
			}
			break
		}
	}
	result := map[string]any{"verb": q.Verb, "applied": applied, "skipped": skipped}
	name := r.DisplayKey(q.Key())
	if stopped > 0 {
		r.UI().Error(fmt.Sprintf("%s asks about one row at a time: ran on %d, left %d as they were", name, len(applied), stopped))
		return result, nil
	}
	return result, r.subjectsNotice(name, applied, skipped, first)
}

// subjectsNotice is the one notice a run over several subjects posts: an
// info when every subject took it; an error naming how many were done and
// how many failed (and the first failure) when some failed; and when none
// was done, an error returned instead, as one subject's failure would be.
func (r *Runtime) subjectsNotice(name string, applied, skipped []string, first error) error {
	switch {
	case first != nil && len(applied) == 0:
		return fmt.Errorf("%s failed on all %d rows: %w", name, len(skipped), first)
	case first != nil:
		r.UI().Error(fmt.Sprintf("%s: %d done, %d failed (%s)", name, len(applied), len(skipped), skipped[0]))
	default:
		r.UI().Info(fmt.Sprintf("%s: done on %d rows", name, len(applied)))
	}
	return nil
}

// Each is how a verb that takes many subjects (Operation.Many) makes its
// change once it knows what to do (an argument given, a chooser
// answered): one runs for every subject in order, each with a Ctx narrowed
// to that subject (Instance, Context and Subjects), so the one-subject
// body stays as it was. A run over several posts the notice a per-row run
// would (subjectsNotice): one info, or one error counting the done and the
// failed. A body must answer synchronously (Done or Failed); pending work
// counts as a failure. On one subject Each is that subject's Result.
func (ctx Ctx) Each(one AppHandler) Result {
	if len(ctx.Subjects) <= 1 {
		return one(ctx)
	}
	r := ctx.Runtime
	defer r.undoGroup(ctx.SubjectIDs())()
	applied, skipped := []string{}, []string{}
	var first error
	for _, s := range ctx.Subjects {
		if r.Instances[s.ID] == nil {
			continue // an earlier subject's change removed it
		}
		sub := ctx
		sub.Instance, sub.Context, sub.Subjects = s, r.contextOf(s.ID), []*Instance{s}
		res := one(sub)
		err := res.err
		if err == nil && res.work != nil {
			err = fmt.Errorf("%s: pending work is not run for several subjects", s.ID)
		}
		if err != nil {
			skipped = append(skipped, s.ID+": "+err.Error())
			if first == nil {
				first = err
			}
			continue
		}
		applied = append(applied, s.ID)
	}
	name := r.DisplayKey(ctx.Invoked.Noun + "." + ctx.Invoked.Verb)
	if err := r.subjectsNotice(name, applied, skipped, first); err != nil {
		return Failed(err)
	}
	return Done(map[string]any{"verb": ctx.Invoked.Verb, "applied": applied, "skipped": skipped})
}

// undoGroup makes a run over several subjects one undo step where their
// rows' list keeps an undo stack (a tree: List.undoGroup); the returned end
// seals it. A no-op elsewhere.
func (r *Runtime) undoGroup(subjects []string) (end func()) {
	if len(subjects) > 1 {
		if l := r.List(r.rows[subjects[0]]); l != nil && l.undoGroup != nil {
			return l.undoGroup()
		}
	}
	return func() {}
}

// cloneArgs is a shallow copy of args, nil for none.
func cloneArgs(args Arguments) Arguments {
	if args == nil {
		return nil
	}
	out := make(Arguments, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

// subjectsLabel is what a confirm's {{label}} reads for several subjects:
// the first one's label and how many more.
func (r *Runtime) subjectsLabel(ids []string) string {
	label := ids[0]
	if inst := r.Instances[ids[0]]; inst != nil {
		if l := r.thingLabel(inst); l != "" {
			label = l // the thing's label prop, else its Fallback
		} else if inst.Label != "" {
			label = inst.Label
		}
	}
	if len(ids) == 1 {
		return label
	}
	return fmt.Sprintf("%s and %d more", label, len(ids)-1)
}
