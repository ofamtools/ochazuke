package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// The generic document store (noun scaffold plan, section 3): a noun
// declared with props and a table of JSON documents needs no store code.
// DocStore builds the Store from the noun's props at run time over any
// DocTable (an application's sqlite table, MemDocTable in tests): get,
// list, create, set, delete and archive are the standard verbs over it,
// list requests run in the table (DocTable.Query: conditions, sort, text
// search, paging), and the table's Version is the store's ChangeToken. The
// side effects a hand-written store had (a task's subtasks on delete) are
// hooks (DocHooks), not a store of their own.
//
// A document's fields are the noun's props, except where a prop names the
// field that holds it (PropSpec.Field: the prop project in the field
// project_id): DocStore renames between the two on every read and write,
// and hands the table a DocQuery in field names. A derived prop
// (PropSpec.Derivation) is never in a document. Fields that are no prop
// stay in the document: a write merges into it.

// DocTable is a table of documents keyed by text, each a Thing: what a
// DocStore reads and writes. Query answers one page of a DocQuery and how
// many documents match it in all; Version answers a token that is equal
// while the table is unchanged (a write counter, sqlite's data_version).
//
// Put writes t into the document under key: a field t gives with a value
// is set to it, and a field t gives as nil is removed (a prop cleared by a
// set, an undo, a BeforeWrite hook). A table may merge t into the stored
// document, keeping every field t does not give, or store t's fields with
// a value in place of it: DocStore always hands Put the whole document (the
// fields it read, with the changes and the nils), so both store the same
// document. A new key stores t's fields that have a value.
//
// A table may also answer a create's next key itself (DocKeyTable).
type DocTable interface {
	Get(key string) (Thing, bool, error)
	Put(key string, t Thing) error
	Delete(key string) error
	Version() (string, error)
	Query(q DocQuery) (ListPage, error)
}

// DocQuery is a list request as a table runs it: every condition, the
// archive hiding (Hide names a boolean prop: a document where it is true is
// left out; one where it is false or missing is kept), the sort keys,
// GroupBy and GroupSort (the same sort-then-group order as ListQuery),
// a text search and the page. Every name in it (a condition's prop, a sort
// key, Hide, TextFields) is a document's field name (PropSpec.Field, else
// the prop's name). Text matches a document when each word is in
// one of the TextFields (case-insensitive substring); "" matches every
// document. PerPage 0 answers every match. Paging counts documents, not
// groups; ListPage.Groups labels each returned document's group.
type DocQuery struct {
	Filters    []PropFilter
	Hide       string
	Sort       []SortKey
	GroupBy    []string
	GroupSort  []SortKey
	Text       string
	TextFields []string
	Page       int
	PerPage    int
}

// DocKeyTable is a DocTable that answers a create's next key itself, with
// no read of the whole table (a max-key query): NextKey is prefix and one
// past the largest whole number N of a key prefix+N the table holds
// (prefix+"1" when it holds none). A DocStore over a table without it
// reads every document to find that number.
type DocKeyTable interface {
	NextKey(prefix string) (string, error)
}

// DocHooks are a DocStore's side effects: BeforeWrite sees every create
// and set before it is stored and may change what is written (DocWrite),
// BeforeDelete may refuse a delete (or delete what goes with the thing),
// AfterWrite hears every create, set and delete (t nil for a delete),
// ConfirmFor is Store.ConfirmFor. Each is optional.
//
// BeforeWrite is how a noun resolves or normalises values without a store
// of its own: an owner looked up (a reference that wins over another, one
// not found saved unassociated with a line saying so), a field that is no
// prop set beside a change (former_* fields when an owner goes away). It
// does not refuse: an error it answers is a failure (a disk, a lookup that
// broke), and the write is not stored.
type DocHooks struct {
	BeforeWrite  func(c Ctx, w *DocWrite) error
	BeforeDelete func(c Ctx, key string) error
	AfterWrite   func(c Ctx, key string, t Thing)
	ConfirmFor   func(c Ctx, verb string, keys []string, prompt string) string
}

// DocWrite is one create or set as DocHooks.BeforeWrite sees it. Key is
// the thing's key ("" on a create that makes one); Old is the thing as it
// is (nil on a create). Changes are the props written, by prop name: on a
// create every value given, on a set the props changed (nil clears one).
// The hook may set, add or delete entries; a name no prop declares is
// written as a document field of that name. Info lines are told as
// information (Ctx.Info), joined in one line, once the write is stored.
// Messages are told each on its own once the write is stored, by kind:
// UINotice a notice (Ctx.Notify), UIInfo the status line (Ctx.Info),
// UIError the status line in its error tone (Ctx.Error); a write that is
// not stored tells none.
type DocWrite struct {
	Key      string
	Old      Thing
	Changes  Thing
	Info     []string
	Messages []UIMessage
}

// beforeWrite runs the BeforeWrite hook over a write: the changes to
// store, and the lines and messages to tell once it is.
func (d *docStore) beforeWrite(c Ctx, key string, old, changes Thing) (Thing, []string, []UIMessage, error) {
	if d.hooks.BeforeWrite == nil {
		return changes, nil, nil, nil
	}
	w := &DocWrite{Key: key, Old: old, Changes: maps.Clone(changes)}
	if w.Changes == nil {
		w.Changes = Thing{}
	}
	if err := d.hooks.BeforeWrite(c, w); err != nil {
		return nil, nil, nil, err
	}
	return w.Changes, w.Info, w.Messages, nil
}

// tell posts a write's info lines as one line of information, then each
// message by its kind.
func tell(c Ctx, info []string, said []UIMessage) {
	if c.Runtime == nil {
		return
	}
	if len(info) > 0 {
		c.Info(strings.Join(info, "; "))
	}
	for _, m := range said {
		m.Apply(ctxSink{c})
	}
}

// ctxSink is a UISink that posts through a request's Ctx (its tape, its
// runtime's UI): what a message told by kind (UIMessage.Apply) reaches.
type ctxSink struct{ c Ctx }

func (s ctxSink) Notify(_ NoticeID, text string) { s.c.Notify(text) }
func (s ctxSink) ClearNotice(id NoticeID)        { s.c.ClearNotice(id) }
func (s ctxSink) Status(text string)             { s.c.Error(text) }
func (s ctxSink) Info(text string)               { s.c.Info(text) }

// DocStore is the Store of noun over table: a collection whose things are
// the table's documents, keyed by the noun's key prop. Create takes the
// key a value gives (AcceptsKey) or makes one (the noun's first letters
// and a number past the table's largest); every change is undoable.
func DocStore(table DocTable, noun string, hooks ...DocHooks) Store {
	var h DocHooks
	if len(hooks) > 0 {
		h = hooks[0]
	}
	d := &docStore{table: table, noun: noun, hooks: h}
	return Store{
		Kind:        KindCollection,
		Get:         d.get,
		List:        d.list,
		Query:       d.query,
		Create:      d.create,
		Patch:       d.patch,
		Delete:      d.delete,
		ChangeToken: func(Ctx) (string, error) { return table.Version() },
		ConfirmFor:  h.ConfirmFor,
		Undo:        true,
		AcceptsKey:  true,
		Stored:      d.stored,
	}
}

type docStore struct {
	table DocTable
	noun  string
	hooks DocHooks
	mu    sync.Mutex
}

func (d *docStore) nounOf(c Ctx) (*Noun, error) {
	n := c.Runtime.Nouns[c.Runtime.CanonicalNoun(d.noun)]
	if n == nil || n.keyProp() == nil {
		return nil, fmt.Errorf("doc store %s: no such noun with a key prop", d.noun)
	}
	return n, nil
}

func (d *docStore) get(c Ctx, key string) (Thing, error) {
	t, ok, err := d.table.Get(key)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, c.Runtime.ThingNotFound(c, d.noun, key)
	}
	return d.thingOf(c, t), nil
}

// thingOf is a document as the noun's thing (fields renamed to props).
func (d *docStore) thingOf(c Ctx, doc Thing) Thing {
	return c.Runtime.Nouns[c.Runtime.CanonicalNoun(d.noun)].docRead(doc)
}

// docRead is a stored document as the noun's thing (docThing) with each
// ReadDefault prop the document lacks read as its default.
func (n *Noun) docRead(doc Thing) Thing {
	t := n.docThing(doc)
	if t == nil {
		return nil
	}
	for _, p := range n.Props {
		if p.ReadDefault && t[p.Name] == nil {
			t[p.Name] = readDefault(p)
		}
	}
	return t
}

// readDefault is the value a ReadDefault prop reads when its field is
// missing: its Default, else its type's zero.
func readDefault(p PropSpec) any {
	if obj, ok := p.Default.(map[string]any); ok {
		return maps.Clone(obj) // each read its own
	}
	if p.Default != nil {
		return p.Default
	}
	if p.listed() {
		return []string{}
	}
	if zero := propZeros[p.Type]; zero != nil {
		return zero()
	}
	return ""
}

// propZeros are the zero values of the scalar and object prop types whose
// zero is not "" (a fresh object each time); a list's is an empty list.
var propZeros = map[string]func() any{
	SettingBoolean: func() any { return false },
	SettingInteger: func() any { return 0 },
	SettingNumber:  func() any { return 0.0 },
	PropObject:     func() any { return map[string]any{} },
}

// docQuery is a list request as the table runs it: the archive hiding as
// a condition, the noun's text props to search.
func (d *docStore) docQuery(c Ctx, q ListQuery) (DocQuery, error) {
	n, err := d.nounOf(c)
	if err != nil {
		return DocQuery{}, err
	}
	out := DocQuery{Filters: clonePropFilters(q.Filters), Sort: slices.Clone(q.Sort), GroupBy: slices.Clone(q.GroupBy), GroupSort: slices.Clone(q.GroupSort), Text: q.Text, Page: q.Page, PerPage: q.PerPage, TextFields: textProps(n)}
	if p := c.Runtime.archiveHiding(n, q); p != nil {
		out.Hide = p.Name
	}
	return n.fieldQuery(out), nil
}

func (d *docStore) query(c Ctx, q ListQuery) (ListPage, error) {
	dq, err := d.docQuery(c, q)
	if err != nil {
		return ListPage{}, err
	}
	page, err := d.table.Query(dq)
	for i, t := range page.Items {
		page.Items[i] = d.thingOf(c, t)
	}
	return page, err
}

func (d *docStore) list(c Ctx, q ListQuery) ([]Thing, error) {
	q.Page, q.PerPage, q.Sort = 0, 0, nil
	q.Archived = true // the standard list hides archived things itself
	page, err := d.query(c, q)
	return page.Items, err
}

func (d *docStore) create(c Ctx, values Thing) (Thing, error) {
	n, err := d.nounOf(c)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if doc := c.restoring; doc != nil {
		if t, ok, err := d.restore(c, n, values, doc); ok || err != nil {
			return t, err
		}
	}
	key := n.keyProp().Name
	given := ""
	if !emptyProp(values[key]) {
		given = fmt.Sprint(values[key])
	}
	values, info, said, err := d.beforeWrite(c, given, nil, values)
	if err != nil {
		return nil, err
	}
	// A value whose read would show another (writeReasons) is still the
	// thing's own: a create records what it is given, and says so.
	info = append(info, sortedReasons(c.Runtime.writeReasons(c, n, nil, values))...)
	t := withoutNil(n.storedDoc(values))
	id := fmt.Sprint(values[key])
	if emptyProp(values[key]) {
		if id, err = d.nextKey(n); err != nil {
			return nil, err
		}
		t[n.fieldOf(key)] = id
	} else if _, ok, err := d.table.Get(id); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("%s %q already exists", c.Runtime.DisplayNoun(n.Name), id)
	}
	if err := d.table.Put(id, t); err != nil {
		return nil, err
	}
	t = n.docRead(t)
	d.wrote(c, id, t)
	tell(c, info, said)
	return t, nil
}

// restore is an undo's create of a deleted thing (Ctx.Restoring): its
// stored document put back whole under its key, every field it held, with
// no BeforeWrite (the document is what was stored). A key the table holds
// again is not restored (false): the create runs as any other.
func (d *docStore) restore(c Ctx, n *Noun, values, doc Thing) (Thing, bool, error) {
	id := fmt.Sprint(values[n.keyProp().Name])
	if emptyProp(values[n.keyProp().Name]) {
		id = fmt.Sprint(doc[n.fieldOf(n.keyProp().Name)])
	}
	if _, ok, err := d.table.Get(id); err != nil || ok {
		return nil, false, err
	}
	doc = withoutNil(doc)
	if err := d.table.Put(id, doc); err != nil {
		return nil, false, err
	}
	t := n.docRead(doc)
	d.wrote(c, id, t)
	return t, true, nil
}

// stored is the document under key as the table keeps it (Store.Stored).
func (d *docStore) stored(c Ctx, key string) (Thing, error) {
	doc, ok, err := d.table.Get(key)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, c.Runtime.ThingNotFound(c, d.noun, key)
	}
	return doc, nil
}

// onlyStamps reports whether changes hold nothing but the times the
// framework stamps on a write (the created and updated roles).
func onlyStamps(n *Noun, changes Thing) bool {
	for name := range changes {
		if p := n.prop(name); p == nil || p.Role != RoleCreated && p.Role != RoleUpdated {
			return false
		}
	}
	return true
}

// withoutNil is t without its nil fields (a copy).
func withoutNil(t Thing) Thing {
	out := maps.Clone(t)
	maps.DeleteFunc(out, func(_ string, v any) bool { return v == nil })
	if out == nil {
		out = Thing{}
	}
	return out
}

// sortedReasons are a writeReasons map's reasons in prop order.
func sortedReasons(reasons map[string]string) []string {
	out := make([]string, 0, len(reasons))
	for _, name := range slices.Sorted(maps.Keys(reasons)) {
		out = append(out, reasons[name])
	}
	return out
}

// nextKey is a new key: the noun's first letter and one past the largest
// number a key of that form holds, from the table itself when it answers
// it (DocKeyTable), else from a read of every document.
func (d *docStore) nextKey(n *Noun) (string, error) {
	word := n.Name[strings.LastIndex(n.Name, ".")+1:]
	prefix := word[:1]
	if k, ok := d.table.(DocKeyTable); ok {
		return k.NextKey(prefix)
	}
	page, err := d.table.Query(DocQuery{})
	if err != nil {
		return "", err
	}
	key := n.fieldOf(n.keyProp().Name)
	keys := make([]string, len(page.Items))
	for i, t := range page.Items {
		keys[i] = fmt.Sprint(t[key])
	}
	return NextNumberedKey(prefix, keys), nil
}

// NextNumberedKey is prefix and one past the largest whole number N of a
// key prefix+N among keys (prefix+"1" when there is none): the key a
// DocStore's create makes, for a DocKeyTable that holds its keys in
// memory.
func NextNumberedKey(prefix string, keys []string) string {
	most := 0
	for _, k := range keys {
		if v, ok := strings.CutPrefix(k, prefix); ok {
			if i, err := strconv.Atoi(v); err == nil {
				most = max(most, i)
			}
		}
	}
	return prefix + strconv.Itoa(most+1)
}

func (d *docStore) patch(c Ctx, key string, changes Thing) (Thing, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok, err := d.table.Get(key)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, c.Runtime.ThingNotFound(c, d.noun, key)
	}
	n := c.Runtime.Nouns[c.Runtime.CanonicalNoun(d.noun)]
	old := n.docRead(t)
	changes, info, said, err := d.beforeWrite(c, key, old, changes)
	if err != nil {
		return nil, err
	}
	// A prop that keeps its own value but whose read would show another
	// (a reference's value wins: writeReasons) is not written; alone, the
	// set does nothing and says why.
	if reasons := c.Runtime.writeReasons(c, n, old, changes); len(reasons) > 0 {
		changes = maps.Clone(changes)
		for name := range reasons {
			delete(changes, name)
		}
		if onlyStamps(n, changes) {
			return nil, NothingErr(CauseKept, strings.Join(sortedReasons(reasons), "; "))
		}
		info = append(info, sortedReasons(reasons)...)
	}
	t = maps.Clone(t)
	for k, v := range changes {
		if n.derivedOnly(k) {
			continue
		}
		t[n.fieldOf(k)] = v // nil removes the field (DocTable.Put)
	}
	if err := d.table.Put(key, t); err != nil {
		return nil, err
	}
	t = n.docRead(withoutNil(t))
	d.wrote(c, key, t)
	tell(c, info, said)
	return t, nil
}

func (d *docStore) delete(c Ctx, key string) error {
	if _, ok, err := d.table.Get(key); err != nil {
		return err
	} else if !ok {
		return c.Runtime.ThingNotFound(c, d.noun, key)
	}
	if d.hooks.BeforeDelete != nil {
		if err := d.hooks.BeforeDelete(c, key); err != nil {
			return err
		}
	}
	if err := d.table.Delete(key); err != nil {
		return err
	}
	d.wrote(c, key, nil)
	return nil
}

func (d *docStore) wrote(c Ctx, key string, t Thing) {
	if d.hooks.AfterWrite != nil {
		d.hooks.AfterWrite(c, key, t)
	}
}

// textProps are the props a text search reads: the key, the label and
// every writable plain text prop (not a choice, a reference, a secret or
// a stamp such as created).
func textProps(n *Noun) []string {
	var out []string
	for _, p := range n.Props {
		if p.Secret || p.computed() || p.derivedOnly() {
			continue
		}
		if p.Key || p.Role == RoleLabel || !p.ReadOnly && (p.Type == "" || p.Type == SettingString) && len(p.Enum) == 0 && p.Noun == "" {
			out = append(out, p.Name)
		}
	}
	return out
}

// TextMatches reports whether every word of text is in one of the thing's
// fields, case-insensitive: a DocQuery's text search, as a table that holds
// its documents in memory runs it.
func TextMatches(t Thing, fields []string, text string) bool {
	for _, word := range strings.Fields(strings.ToLower(text)) {
		found := false
		for _, f := range fields {
			if v, ok := t[f]; ok && v != nil && strings.Contains(strings.ToLower(cellValueText(v)), word) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// MemDocTable is a DocTable in memory, the reference table: conditions,
// sort and paging run as the standard list runs them over the noun's
// props (Runtime.QueryAll), text through TextMatches. Its version moves on
// every write; Reads counts the documents Query answered, so a test can
// prove a page read is not a whole read.
type MemDocTable struct {
	r     *Runtime
	noun  string
	mu    sync.Mutex
	docs  map[string]Thing
	order []string
	ver   int
	// Reads is how many documents Query has answered in all; Queries how
	// many times it ran.
	Reads, Queries int
}

// NewMemDocTable is an empty MemDocTable for noun's documents.
func NewMemDocTable(r *Runtime, noun string) *MemDocTable {
	return &MemDocTable{r: r, noun: noun, docs: map[string]Thing{}}
}

// Get answers the document with that key.
func (m *MemDocTable) Get(key string) (Thing, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.docs[key]
	return maps.Clone(t), ok, nil
}

// Put merges t into the document with that key (a new key goes last): a
// field with a value is set, a field given as nil is removed, every other
// field is kept (DocTable).
func (m *MemDocTable) Put(key string, t Thing) error {
	if key == "" {
		return errors.New("a document needs a key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[key]
	if !ok {
		m.order = append(m.order, key)
		doc = Thing{}
	}
	doc = maps.Clone(doc)
	for k, v := range t {
		if v == nil {
			delete(doc, k)
		} else {
			doc[k] = v
		}
	}
	m.docs[key] = doc
	m.ver++
	return nil
}

// NextKey answers the next numbered key from the keys it holds, with no
// Query (DocKeyTable).
func (m *MemDocTable) NextKey(prefix string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return NextNumberedKey(prefix, m.order), nil
}

// Delete removes the document with that key.
func (m *MemDocTable) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[key]; !ok {
		return fmt.Errorf("no document %q", key)
	}
	delete(m.docs, key)
	m.order = slices.DeleteFunc(m.order, func(k string) bool { return k == key })
	m.ver++
	return nil
}

// Version answers the write counter.
func (m *MemDocTable) Version() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strconv.Itoa(m.ver), nil
}

// Query answers one page of the documents matching q, in insertion order
// unless q sorts.
func (m *MemDocTable) Query(q DocQuery) (ListPage, error) {
	m.mu.Lock()
	things := make([]Thing, 0, len(m.order))
	for _, k := range m.order {
		t := m.docs[k]
		if q.Hide != "" && t[q.Hide] == true {
			continue
		}
		if q.Text == "" || TextMatches(t, q.TextFields, q.Text) {
			things = append(things, t)
		}
	}
	m.mu.Unlock()
	// The documents hold field names (PropSpec.Field); the standard list
	// runs over the noun's props, so both are renamed to props for it.
	n := m.r.Nouns[m.r.CanonicalNoun(m.noun)]
	for i, t := range things {
		things[i] = n.docThing(t)
	}
	pq := n.propQuery(q)
	page := m.r.QueryAll(m.noun, things, ListQuery{Filters: pq.Filters, Sort: pq.Sort, GroupBy: pq.GroupBy, GroupSort: pq.GroupSort, Page: q.Page, PerPage: q.PerPage, Archived: true})
	for i, t := range page.Items {
		page.Items[i] = n.storedDoc(t)
	}
	m.mu.Lock()
	m.Reads += len(page.Items)
	m.Queries++
	m.mu.Unlock()
	return page, nil
}
