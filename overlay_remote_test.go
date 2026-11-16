package ochazuke

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// farStore is a remote store over n numbered things that writes none (a
// mounted tool's list with no set, create or delete, each named in
// Missing as a proxy names it): each call goes through Ctx.Await and
// answers at once, and counts what was asked of it.
type farStore struct {
	n                            int
	gets, lists, queries, tokens int
}

func (f *farStore) thing(i int) Thing {
	return Thing{"id": fmt.Sprintf("t%05d", i), "name": "Name " + strconv.Itoa(i)}
}

func (f *farStore) await(c Ctx, op, key string, count *int, answer func() (any, error)) (any, error) {
	return c.Await(StoreCall{Store: "far", Op: op, Key: key}, func() Result {
		*count++
		v, err := answer()
		if err != nil {
			return Failed(err)
		}
		return Done(v)
	}, 0)
}

func (f *farStore) store() Store {
	return Store{Kind: KindCollection, Remote: true, Stamps: true,
		Missing: map[string]string{"set": "the extension offers no set", "create": "the extension offers no create", "delete": "the extension offers no delete"},
		Get: func(c Ctx, id string) (Thing, error) {
			v, err := f.await(c, "get", id, &f.gets, func() (any, error) {
				if i, err := strconv.Atoi(strings.TrimPrefix(id, "t")); err == nil && strings.HasPrefix(id, "t") && i < f.n {
					return f.thing(i), nil
				}
				return nil, fmt.Errorf("no far %q", id)
			})
			t, _ := v.(Thing)
			return t, err
		},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			v, err := f.await(c, "list", "", &f.lists, func() (any, error) {
				out := make([]Thing, f.n)
				for i := range out {
					out[i] = f.thing(i)
				}
				return out, nil
			})
			things, _ := v.([]Thing)
			return things, err
		},
		Query: func(c Ctx, q ListQuery) (ListPage, error) {
			v, err := f.await(c, "query", fmt.Sprint(q.Page, q.PerPage, q.Filters), &f.queries, func() (any, error) {
				lo, hi := 0, f.n
				if q.PerPage > 0 {
					lo = min((max(q.Page, 1)-1)*q.PerPage, f.n)
					hi = min(lo+q.PerPage, f.n)
				}
				page := ListPage{Total: f.n}
				for i := lo; i < hi; i++ {
					page.Items = append(page.Items, f.thing(i))
				}
				return page, nil
			})
			page, _ := v.(ListPage)
			return page, err
		},
		ChangeToken: func(c Ctx) (string, error) {
			v, err := f.await(c, "token", "", &f.tokens, func() (any, error) { return "src-1", nil })
			s, _ := v.(string)
			return s, err
		},
	}
}

// farRuntime has the noun far over a farStore of n things, its overlay in
// a temporary config directory.
func farRuntime(t *testing.T, n int) (*Runtime, *farStore) {
	t.Helper()
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	f := &farStore{n: n}
	r.Define("far").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel}).Store(f.store())
	return r, f
}

// farPage is one page of far's standard list: its ids and its total.
func farPage(t *testing.T, r *Runtime, args Arguments) ([]string, int) {
	t.Helper()
	v, err := r.Execute(Request{Noun: "far", Verb: "list", Args: args})
	if err != nil {
		t.Fatalf("list %v: %v", args, err)
	}
	a := v.(ListAnswer)
	var ids []string
	for _, row := range a.Items {
		ids = append(ids, fmt.Sprint(row["id"]))
	}
	return ids, a.Total
}

// A remote store that writes nothing (a mounted tool's noun): set,
// archive, delete (it hides: get still answers the thing, marked) and
// create run through the overlay, each read back through get and a paged
// list and each undone; the verbs are marked as the overlay's for a host.
func TestOverlayFillsARemoteStore(t *testing.T) {
	r, _ := farRuntime(t, 5)
	n := r.Noun("far")
	if len(n.Store.Missing) > 0 || !slices.Equal(n.Store.Overlaid, []string{"archive", "patch", "create", "delete"}) {
		t.Fatalf("filled parts %v, missing %v", n.Store.Overlaid, n.Store.Missing)
	}
	if op, _ := r.Operation("far.set"); !r.OverlayFills(op) {
		t.Fatal("far.set is not marked as the overlay's")
	}
	if op, _ := r.Operation("far.get"); r.OverlayFills(op) {
		t.Fatal("far.get is marked as the overlay's")
	}
	page := Arguments{"page": 1, "per-page": 3}
	a := Arguments{"id": "t00001"}

	overlayStep(t, r, Request{Noun: "far", Verb: "set", Args: Arguments{"id": "t00001", "prop": "name", "value": "Mine"}})
	if got := overlayProp(t, r, "far", a, "name"); got != "Mine" {
		t.Fatalf("set: name %v", got)
	}
	v, _ := r.Execute(Request{Noun: "far", Verb: "list", Args: page})
	if got := v.(ListAnswer).Items[1]["name"]; got != "Mine" {
		t.Fatalf("set: the page reads name %v", got)
	}
	overlayUndo(t, r)
	if got := overlayProp(t, r, "far", a, "name"); got != "Name 1" {
		t.Fatalf("set undone: name %v", got)
	}

	for _, verb := range []string{"archive", "delete"} {
		overlayStep(t, r, Request{Noun: "far", Verb: verb, Args: Arguments{"id": "t00001"}, Confirmed: true})
		if ids, total := farPage(t, r, page); !slices.Equal(ids, []string{"t00000", "t00002"}) || total != 4 {
			t.Fatalf("%s: page %v total %d", verb, ids, total)
		}
		if verb == "delete" {
			if got := overlayProp(t, r, "far", a, OverlayHiddenProp); got != true {
				t.Fatalf("delete: get answers hidden %v", got)
			}
			if ids, total := farPage(t, r, Arguments{"filter": "hidden=true"}); !slices.Equal(ids, []string{"t00001"}) || total != 1 {
				t.Fatalf("hidden=true: %v %d", ids, total)
			}
		}
		overlayUndo(t, r)
		if ids, total := farPage(t, r, page); !slices.Equal(ids, []string{"t00000", "t00001", "t00002"}) || total != 5 {
			t.Fatalf("%s undone: page %v total %d", verb, ids, total)
		}
	}

	o := overlayStep(t, r, Request{Noun: "far", Verb: "create", Args: Arguments{"name": "Of mine"}})
	key := answerKey(n, o.Value)
	if !strings.HasPrefix(key, "yours-") {
		t.Fatalf("create: key %q", key)
	}
	if got := overlayProp(t, r, "far", Arguments{"id": key}, "name"); got != "Of mine" {
		t.Fatalf("create: name %v", got)
	}
	if ids, total := farPage(t, r, Arguments{"page": 2, "per-page": 3}); !slices.Equal(ids, []string{"t00003", "t00004", key}) || total != 6 {
		t.Fatalf("create: last page %v total %d", ids, total)
	}
	overlayUndo(t, r)
	if ids, total := farPage(t, r, Arguments{"page": 2, "per-page": 3}); !slices.Equal(ids, []string{"t00003", "t00004"}) || total != 5 {
		t.Fatalf("create undone: last page %v total %d", ids, total)
	}
}

// A 20,000-row remote store pages with the overlay over it: a page costs
// one source call for that page, never a full list; a thing written on the
// page shows the user's value, a hidden one leaves the page and the total,
// and a thing of the user's own is on the last page.
func TestOverlayPagesARemoteStoreWithoutAFullRead(t *testing.T) {
	r, f := farRuntime(t, 20000)
	overlayStep(t, r, Request{Noun: "far", Verb: "set", Args: Arguments{"id": "t00101", "prop": "name", "value": "Mine"}})
	overlayStep(t, r, Request{Noun: "far", Verb: "delete", Args: Arguments{"id": "t00102"}, Confirmed: true})
	made := answerKey(r.Noun("far"), overlayStep(t, r, Request{Noun: "far", Verb: "create", Args: Arguments{"name": "Of mine"}}).Value)
	lists, queries := f.lists, f.queries

	v, err := r.Execute(Request{Noun: "far", Verb: "list", Args: Arguments{"page": 3, "per-page": 50}})
	if err != nil {
		t.Fatal(err)
	}
	a := v.(ListAnswer)
	if f.lists != lists || f.queries != queries+1 {
		t.Fatalf("one page: %d full lists, %d queries (want 0 and 1)", f.lists-lists, f.queries-queries)
	}
	if len(a.Items) != 49 || a.Total != 20000 || a.Items[1]["name"] != "Mine" || a.Items[2]["id"] != "t00103" {
		t.Fatalf("page 3: %d items, total %d, %v %v", len(a.Items), a.Total, a.Items[1], a.Items[2])
	}
	ids, _ := farPage(t, r, Arguments{"page": 400, "per-page": 50})
	if f.lists != lists || ids[len(ids)-1] != made {
		t.Fatalf("last page: %d full lists, ends %v (want %s)", f.lists-lists, ids[len(ids)-1:], made)
	}
}

// The change token is the source's joined with the overlay's version: an
// overlay write moves it, and reading it asks the source for its token
// only (no list, no page).
func TestOverlayTokenMovesWithoutASourceRead(t *testing.T) {
	r, f := farRuntime(t, 3)
	n := r.Noun("far")
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), tape: &tape{}}
	before, err := n.Store.ChangeToken(c)
	if err != nil || before != "src-1" {
		t.Fatalf("token with no rows %q %v", before, err)
	}
	overlayStep(t, r, Request{Noun: "far", Verb: "set", Args: Arguments{"id": "t00001", "prop": "name", "value": "Mine"}})
	reads := f.lists + f.queries + f.gets
	c.tape = &tape{}
	after, err := n.Store.ChangeToken(c)
	if err != nil || after == before || !strings.HasPrefix(after, "src-1/") {
		t.Fatalf("token after an overlay write %q (before %q) %v", after, before, err)
	}
	if f.lists+f.queries+f.gets != reads {
		t.Fatal("the token read the source's things")
	}
}

// A ListFacts noun's rows keep their ids across runs (StableKeys): a
// value written on one survives a reload of the overlay file.
func TestListFactsOverlayRowsSurviveAReload(t *testing.T) {
	dir := t.TempDir()
	open := func() *Runtime {
		r := widgetRuntime(t, ListFacts{Noun: "widget", Sample: widgetRow{}}, widgetRows)
		r.Config().Dir = dir
		return r
	}
	overlayStep(t, open(), Request{Noun: "widget", Verb: "set", Args: Arguments{"id": "widget/a", "prop": "state", "value": "mine"}})
	if got := overlayProp(t, open(), "widget", Arguments{"id": "widget/a"}, "state"); got != "mine" {
		t.Fatalf("after a reload: state %v", got)
	}
}
