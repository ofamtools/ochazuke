package cli

import (
	"fmt"
	gomaps "maps"
	"strings"
	"time"

	"ofam.dev/ochazuke"
)

// DefaultProxyTimeout is how long a proxy store waits for one call to the
// extension (HostMount.CallTimeout; 0 means this).
const DefaultProxyTimeout = 30 * time.Second

// proxiedKinds are the store kinds a mounted noun gets a proxy store for: the
// things live in the extension. A noun of live instances (or any kind the
// host runtime does not know) keeps its verbs as they are mounted.
var proxiedKinds = []string{"", ochazuke.KindCollection, ochazuke.Singleton}

// proxiedVerbs are the standard verbs of a catalog noun a proxy store calls
// in the extension, each the store function it answers.
var proxiedVerbs = []string{"get", "list", "create", "set", "delete"}

// proxyStore is the Store of a mounted noun: its get, list, create, set
// (Patch) and delete are the extension's own verbs, the ones the mount
// forwards, run through the host's proxy. The standard verbs
// then run locally over it as over any store (undo, confirm, the edit
// form, history, targets by id or human key). A call the extension does
// not offer is a nil function named in Store.Missing, so its verb is
// unavailable with that reason. The store is Remote: the standard verbs run
// its calls as jobs off the loop (the host's proxy starts each call on the
// loop and its work runs off it).
//
// where and limit are not sent: the standard list applies them to what the
// extension lists, so the extension answers everything its list does.
func proxyStore(noun CatalogNoun, local, key string, h HostMount) ochazuke.Store {
	px := newProxyCalls(noun, local, key, h)
	s := ochazuke.Store{Kind: noun.Kind, Remote: true, Stamps: true, Missing: px.missing, Undo: px.has("get") && px.has("set")}
	if h.CanonicalKey != nil {
		s.CanonicalKey = func(_ ochazuke.Ctx, selector, key string) error {
			return h.CanonicalKey(noun.Name, selector, key)
		}
	}
	// A call the extension lacks is a nil function, as for any store: the
	// verb is registered but unavailable, with the reason in Missing.
	if px.has("get") {
		s.Get = px.get
	}
	if px.has("list") {
		s.List = px.list
		// The extension's store pages (CatalogStore.Query): one page of a
		// list request is asked of it, never its whole list.
		if noun.Store != nil && noun.Store.Query {
			s.Query = px.query
		}
	}
	if px.has("token") && noun.Store != nil && noun.Store.ChangeToken {
		// The extension's change token (its token verb): the host's index
		// and counts read again only when it moved. A catalog without the
		// verb (an older extension) reads as before.
		s.ChangeToken = px.token
	}
	if px.has("create") {
		s.Create = px.create
	}
	if px.has("set") {
		s.Patch = px.patch
	}
	if px.has("delete") {
		s.Delete = px.delete
	}
	if s.Kind == "" {
		s.Kind = ochazuke.KindCollection
	}
	return s
}

// newProxyCalls collects the standard calls the extension offers for noun.
func newProxyCalls(noun CatalogNoun, local, key string, h HostMount) *proxyCalls {
	px := &proxyCalls{store: local, writes: map[string]bool{}, key: key, limit: h.CallTimeout, calls: map[string]ochazuke.AppHandler{}, missing: map[string]string{}}
	if px.limit <= 0 {
		px.limit = DefaultProxyTimeout
	}
	for _, v := range noun.Verbs {
		px.add(h, noun, v)
	}
	for _, verb := range proxiedVerbs {
		if px.calls[verb] == nil {
			px.missing[verb] = px.why(noun, verb)
		}
	}
	return px
}

// read is the Read of a mounted computed prop (or a collection prop): the
// value comes from the extension's own get {prop}, never from its whole
// record, so the extension computes it only when asked. For a prop of one
// thing the thing's key goes with the call.
func (px *proxyCalls) read(prop string) func(ochazuke.Ctx, ochazuke.Thing) (any, error) {
	return func(c ochazuke.Ctx, t ochazuke.Thing) (any, error) {
		args := ochazuke.Arguments{"prop": prop}
		if px.key != "" && t != nil {
			args[px.key] = fmt.Sprint(t[px.key])
		}
		return px.call(c, "get", args)
	}
}

// proxyCalls are an extension noun's standard calls, by the verb they are.
type proxyCalls struct {
	store   string          // the noun the store belongs to, in the host
	writes  map[string]bool // the calls that change things
	key     string
	limit   time.Duration
	calls   map[string]ochazuke.AppHandler
	missing map[string]string
}

// add takes a catalog verb as a call when it is one of the proxied
// standard verbs the extension can run.
func (px *proxyCalls) add(h HostMount, noun CatalogNoun, v CatalogVerb) {
	standard := v.Standard
	if standard == "" && noun.Store != nil {
		for _, capability := range [...]struct {
			verb      string
			available bool
		}{{"get", noun.Store.Get}, {"list", noun.Store.List}, {"create", noun.Store.Create}, {"set", noun.Store.Patch}, {"delete", noun.Store.Delete}} {
			if v.Name == capability.verb && capability.available {
				standard = capability.verb
				break
			}
		}
	}
	if standard == "" || v.Unavailable != "" {
		return
	}
	px.writes[standard] = v.History == ochazuke.HistoryChange
	switch fn := h.Proxy(noun.Name, v).(type) {
	case ochazuke.AppHandler:
		px.calls[standard] = fn
	case func(ochazuke.Ctx) ochazuke.Result:
		px.calls[standard] = fn
	}
}

func (px *proxyCalls) has(verb string) bool { return px.calls[verb] != nil }

// why is the reason a call is missing: the extension offers none, or its
// own reason for the verb it cannot run.
func (px *proxyCalls) why(noun CatalogNoun, verb string) string {
	reason := fmt.Sprintf("the extension offers no %s for %s", verb, noun.Name)
	for _, v := range noun.Verbs {
		if v.Standard == verb && v.Unavailable != "" {
			reason += ": " + v.Unavailable
		}
	}
	return reason
}

// call runs one of the extension's verbs with args: the host's proxy starts
// it on the loop and its work runs off it (Ctx.Await).
func (px *proxyCalls) call(c ochazuke.Ctx, verb string, args ochazuke.Arguments) (any, error) {
	fn := px.calls[verb]
	if fn == nil {
		return nil, fmt.Errorf("%s", px.missing[verb])
	}
	c.Args = args
	id := ochazuke.StoreCall{Store: px.store, Op: verb, Key: fmt.Sprint(args), Write: px.writes[verb]}
	return c.Await(id, func() ochazuke.Result { return fn(c) }, px.limit)
}

// subject is the arguments naming one thing: its key.
func (px *proxyCalls) subject(id string) ochazuke.Arguments {
	if px.key == "" {
		return ochazuke.Arguments{}
	}
	return ochazuke.Arguments{px.key: id}
}

func (px *proxyCalls) get(c ochazuke.Ctx, id string) (ochazuke.Thing, error) {
	v, err := px.call(c, "get", px.subject(id))
	return proxyThing(v), err
}

// token asks the extension for its store's change token.
func (px *proxyCalls) token(c ochazuke.Ctx) (string, error) {
	v, err := px.call(c, "token", ochazuke.Arguments{})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(v), nil
}

func (px *proxyCalls) list(c ochazuke.Ctx, q ochazuke.ListQuery) ([]ochazuke.Thing, error) {
	v, err := px.call(c, "list", gomaps.Clone(q.Args))
	return proxyThings(v), err
}

// query asks the extension's list for one page: conditions as where pairs,
// sort and group words, the text search, the page and the archive flag.
// Conditions the existing word protocol cannot express still use the
// legacy in-memory query path.
func (px *proxyCalls) query(c ochazuke.Ctx, q ochazuke.ListQuery) (ochazuke.ListPage, error) {
	args := ochazuke.Arguments(gomaps.Clone(q.Args))
	if args == nil {
		args = ochazuke.Arguments{}
	}
	var where []string
	for _, f := range q.Filters {
		text := fmt.Sprint(f.Value)
		if f.Op != "" && f.Op != "=" || f.Value == nil || strings.ContainsAny(text, " ,=") {
			things, err := px.list(c, q)
			if err != nil {
				return ochazuke.ListPage{}, err
			}
			return c.Runtime.QueryAll(px.store, things, q), nil
		}
		where = append(where, f.Prop+"="+text)
	}
	if len(where) > 0 {
		args["where"] = strings.Join(where, ",")
	}
	if words := proxySortWords(q.Sort); words != "" {
		args["sort"] = words
	}
	if len(q.GroupBy) > 0 {
		args["group-by"] = strings.Join(q.GroupBy, ",")
	}
	if words := proxySortWords(q.GroupSort); words != "" {
		args["group-sort"] = words
	}
	if q.Text != "" {
		args["search"] = q.Text
	}
	if q.PerPage > 0 {
		args["page"], args["per-page"] = max(q.Page, 1), q.PerPage
	}
	if q.Archived {
		args["archived"] = true
	}
	v, err := px.call(c, "list", args)
	if err != nil {
		return ochazuke.ListPage{}, err
	}
	things := proxyThings(v)
	total := len(things)
	switch a := v.(type) {
	case ochazuke.ListAnswer:
		total = a.Total
	case map[string]any:
		if n, ok := a["total"].(float64); ok {
			total = int(n)
		} else if n, ok := a["total"].(int); ok {
			total = n
		}
	}
	var groups []string
	if q.Grouped() {
		groups = make([]string, len(things))
		for i, thing := range things {
			groups[i], _ = thing["group"].(string)
		}
	}
	return ochazuke.ListPage{Items: things, Total: total, Groups: groups}, nil
}

func proxySortWords(keys []ochazuke.SortKey) string {
	words := make([]string, len(keys))
	for i, key := range keys {
		words[i] = key.Field
		if key.Desc {
			words[i] += ":desc"
		}
	}
	return strings.Join(words, ",")
}

func (px *proxyCalls) create(c ochazuke.Ctx, t ochazuke.Thing) (ochazuke.Thing, error) {
	v, err := px.call(c, "create", ochazuke.Arguments(gomaps.Clone(t)))
	return proxyThing(v), err
}

func (px *proxyCalls) patch(c ochazuke.Ctx, id string, changes ochazuke.Thing) (ochazuke.Thing, error) {
	args := px.subject(id)
	args["props"] = map[string]any(changes)
	v, err := px.call(c, "set", args)
	return proxyThing(v), err
}

func (px *proxyCalls) delete(c ochazuke.Ctx, id string) error {
	_, err := px.call(c, "delete", px.subject(id))
	return err
}

// proxyThing reads an extension's answer as one thing: its props as an
// object; anything else is no thing.
func proxyThing(v any) ochazuke.Thing {
	switch m := v.(type) {
	case ochazuke.Thing:
		return m
	case map[string]any:
		return ochazuke.Thing(m)
	}
	return nil
}

// proxyThings reads an extension's list as things: an array of objects, or
// a page object holding them as items.
func proxyThings(v any) []ochazuke.Thing {
	out := []ochazuke.Thing{}
	v = ochazuke.AnswerRows(v) // a list answered as a page: {items, page, per_page, total}
	if page, ok := v.(map[string]any); ok {
		v = page["items"] // the same page, decoded from JSON
	}
	switch rows := v.(type) {
	case []ochazuke.Thing:
		return rows
	case []map[string]any:
		for _, m := range rows {
			out = append(out, ochazuke.Thing(m))
		}
	case []any:
		for _, row := range rows {
			if t := proxyThing(row); t != nil {
				out = append(out, t)
			}
		}
	}
	return out
}
