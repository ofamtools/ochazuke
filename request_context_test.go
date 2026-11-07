package ochazuke

import "testing"

// interactionOf is c without its request context: the interaction a
// handler ran in (path, input), for comparing with r.Context().
func interactionOf(c Context) Context {
	c.Request = nil
	return c
}

// A request's handler reads its own request context on Context.Request; a
// request it runs in turn carries its own, and the outer one is the same
// afterwards. Nothing about the request stays on the runtime once it
// returns.
func TestRequestContextIsTheRequests(t *testing.T) {
	r, _, _, _ := listFixture(t)
	type seen struct {
		rc      RequestContext
		running int
		acts    string
	}
	var outer, inner, after seen
	r.Register(Operation{Key: "test.inner", Run: func(r *Runtime, c Context, _ Arguments) (any, error) {
		inner = seen{*c.Request, len(r.RunningRequests()), r.actsFor()}
		return nil, nil
	}})
	r.Register(Operation{Key: "test.outer", Run: func(r *Runtime, c Context, _ Arguments) (any, error) {
		outer = seen{*c.Request, len(r.RunningRequests()), r.actsFor()}
		if _, err := r.ExecuteAt(c, Request{Verb: "inner", Noun: "test", Target: "two"}); err != nil {
			return nil, err
		}
		after = seen{*c.Request, len(r.RunningRequests()), r.actsFor()}
		return nil, nil
	}})
	if _, err := r.Execute(Request{Verb: "outer", Noun: "test", Target: "one"}); err != nil {
		t.Fatal(err)
	}
	if outer.rc.ActsFor != "one" || outer.running != 1 || outer.acts != "one" || outer.rc.Mode.Name != r.Mode.Name {
		t.Fatalf("outer: %+v", outer)
	}
	if inner.rc.ActsFor != "two" || inner.running != 2 || inner.acts != "two" {
		t.Fatalf("inner: %+v", inner)
	}
	if after.rc.ActsFor != "one" || after.running != 1 || after.acts != "one" {
		t.Fatalf("after the inner request: %+v", after)
	}
	if n := len(r.RunningRequests()); n != 0 || r.actsFor() != "" {
		t.Fatalf("running after the request: %d %q", n, r.actsFor())
	}
}
