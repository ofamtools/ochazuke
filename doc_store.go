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

// DocTable is a table of documents keyed by text, each a Thing: what a
// DocStore reads and writes. Query answers one page of a DocQuery and how
// many documents match it in all; Version answers a token that is equal
// while the table is unchanged (a write counter, sqlite's data_version).
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
// a text search and the page. Text matches a document when each word is in
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

// DocHooks are a DocStore's side effects: BeforeDelete may refuse a delete
// (or delete what goes with the thing), AfterWrite hears every create, set
// and delete (t nil for a delete), ConfirmFor is Store.ConfirmFor. Each is
// optional.
type DocHooks struct {
	BeforeDelete func(c Ctx, key string) error
	AfterWrite   func(c Ctx, key string, t Thing)
	ConfirmFor   func(c Ctx, verb string, keys []string, prompt string) string
}

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
		return nil, fmt.Errorf("no %s %q", c.Runtime.DisplayNoun(d.noun), key)
	}
	return t, nil
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
	return out, nil
}

func (d *docStore) query(c Ctx, q ListQuery) (ListPage, error) {
	dq, err := d.docQuery(c, q)
	if err != nil {
		return ListPage{}, err
	}
	return d.table.Query(dq)
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
	key := n.keyProp().Name
	t := maps.Clone(values)
	id := fmt.Sprint(t[key])
	if emptyProp(t[key]) {
		if id, err = d.nextKey(n); err != nil {
			return nil, err
		}
		t[key] = id
	} else if _, ok, err := d.table.Get(id); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("%s %q already exists", c.Runtime.DisplayNoun(n.Name), id)
	}
	if err := d.table.Put(id, t); err != nil {
		return nil, err
	}
	d.wrote(c, id, t)
	return t, nil
}

// nextKey is a new key: the noun's first letter and one past the largest
// number a key of that form holds.
func (d *docStore) nextKey(n *Noun) (string, error) {
	word := n.Name[strings.LastIndex(n.Name, ".")+1:]
	prefix := word[:1]
	page, err := d.table.Query(DocQuery{})
	if err != nil {
		return "", err
	}
	most := 0
	key := n.keyProp().Name
	for _, t := range page.Items {
		if v, ok := strings.CutPrefix(fmt.Sprint(t[key]), prefix); ok {
			if i, err := strconv.Atoi(v); err == nil {
				most = max(most, i)
			}
		}
	}
	return prefix + strconv.Itoa(most+1), nil
}

func (d *docStore) patch(c Ctx, key string, changes Thing) (Thing, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok, err := d.table.Get(key)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, fmt.Errorf("no %s %q", c.Runtime.DisplayNoun(d.noun), key)
	}
	t = maps.Clone(t)
	for k, v := range changes {
		if v == nil {
			delete(t, k)
		} else {
			t[k] = v
		}
	}
	if err := d.table.Put(key, t); err != nil {
		return nil, err
	}
	d.wrote(c, key, t)
	return t, nil
}

func (d *docStore) delete(c Ctx, key string) error {
	if _, ok, err := d.table.Get(key); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("no %s %q", c.Runtime.DisplayNoun(d.noun), key)
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
		if p.Secret || p.computed() {
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

// Put writes the document with that key (a new key goes last).
func (m *MemDocTable) Put(key string, t Thing) error {
	if key == "" {
		return errors.New("a document needs a key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[key]; !ok {
		m.order = append(m.order, key)
	}
	m.docs[key] = maps.Clone(t)
	m.ver++
	return nil
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
	page := m.r.QueryAll(m.noun, things, ListQuery{Filters: q.Filters, Sort: q.Sort, GroupBy: q.GroupBy, GroupSort: q.GroupSort, Page: q.Page, PerPage: q.PerPage, Archived: true})
	for i, t := range page.Items {
		page.Items[i] = maps.Clone(t)
	}
	m.mu.Lock()
	m.Reads += len(page.Items)
	m.Queries++
	m.mu.Unlock()
	return page, nil
}
