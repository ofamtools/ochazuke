package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// The index tree (spine S7 rule 2, run plan 5.6): the index of a noun that
// declares a parent (Noun.Parent, a reference prop) drawn as a read-only
// tree: the parent noun's things are the parent rows, each followed by the
// noun's things that name it, and a thing whose parent is empty or not
// listed sits at the top level (S7 rule 5). Every row is a row of its own
// noun (a parent row is a thing of the parent noun, with that noun's keys
// and verbs), its cells the index columns of the listed noun (cell 0 the
// label). It fills itself from both stores and again after any change of
// either or of a noun a reference column reads labels from (a notify-only
// watch each, so a change reads each store once).
// A noun without a parent is drawn as a flat tree of its things.
//
// It is what a catalog page's index list with "tree": true mounts
// (cli.ListKindIndex); its row ids are LIST/KEY for the noun's things and
// LIST/PARENT-NOUN:KEY for the parent rows.

// indexTree is an index tree's state: its tree, the nouns it reads, and
// whether a change was heard since the last fill.
type indexTree struct {
	r       *Runtime
	tree    *Tree
	noun    string
	parent  string // the reference prop naming a thing's parent, "" none
	pnoun   string // the noun that prop names
	columns []Column
	dirty   bool
	watches []*Watch
	fills   int
	round   uint64 // the refresh pass it last read in (watchHub.round)
}

// AddIndexTree adds the index of noun drawn as a tree by its declared
// parent (see above). Options are AddTree's (Parent, Guides, NoSpill...);
// a Columns option replaces the noun's index columns.
func (r *Runtime) AddIndexTree(id, noun string, opts ...InstanceOption) (*Tree, error) {
	n, err := r.listSource(noun)
	if err != nil {
		return nil, err
	}
	r.declareIndexColumns(n.Name)
	it := &indexTree{r: r, noun: n.Name, columns: r.IndexColumns(n.Name)}
	if p := n.prop(n.Parent); p != nil {
		if refs := declaredRef(*p).refNouns(); len(refs) > 0 {
			if pn := r.Nouns[r.CanonicalNoun(refs[0])]; pn != nil && pn.Store != nil && (pn.Store.List != nil || pn.Store.Query != nil) && pn.keyProp() != nil {
				it.parent, it.pnoun = p.Name, pn.Name
			}
		}
	}
	tree, err := r.AddTree(id, nil, append([]InstanceOption{Columns(it.columns...)}, opts...)...)
	if err != nil {
		return nil, err
	}
	it.tree = tree
	it.columns = tree.List().Columns() // a caller's Columns option wins over the index columns
	r.Instances[id].Data = it
	list := tree.List()
	p := list.Placeholder()
	p.Plural = pluralWord(r.DisplayNoun(n.Name))
	list.SetPlaceholder(p)
	// A store that does not answer yet (a daemon not running) leaves the
	// tree empty, saying why; the next open or change reads it again.
	_ = it.refresh()
	for _, name := range it.watched() {
		w, err := r.watchAs(r.backgroundCtx(), name, func(WatchEvent) { it.dirty = true }, true)
		if err != nil {
			continue // not watchable: it fills on open (IndexTreeRefill) and after this noun's changes
		}
		w.batch = it.flush
		it.watches = append(it.watches, w)
	}
	return tree, nil
}

// watched are the nouns whose changes the tree shows: its noun, the parent
// noun, and every noun a reference column of either reads labels from
// (refCellNouns), so renaming a referenced thing redraws its cells even
// when the listed stores' change tokens held.
func (it *indexTree) watched() []string {
	out := []string{it.noun}
	add := func(name string) {
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	add(it.pnoun)
	for _, name := range []string{it.noun, it.pnoun} {
		n := it.r.Nouns[name]
		if name == "" || n == nil {
			continue
		}
		for _, col := range it.columns {
			if p := n.prop(columnField(col.Title)); p != nil && p.Cell == nil {
				for _, ref := range it.r.refCellNouns(*p) {
					add(ref)
				}
			}
		}
	}
	return out
}

// IndexTreeRefill fills the index tree id again (a page opening it); a
// tree that is not an index tree is left alone. A store that does not
// answer is said on the tree's empty line and returned.
func (r *Runtime) IndexTreeRefill(id string) error {
	inst := r.Instances[id]
	if inst == nil {
		return nil
	}
	it, ok := inst.Data.(*indexTree)
	if !ok {
		return nil
	}
	it.dirty = false
	return it.refresh()
}

// refresh fills the tree and says on its empty line why it could not:
// the failure is the tree's, never its page's.
func (it *indexTree) refresh() error {
	err := it.fill()
	list := it.tree.List()
	p := list.Placeholder()
	p.Empty = ""
	if err != nil {
		p.Empty = fmt.Sprintf("%s did not answer: %v", pluralWord(it.r.DisplayNoun(it.noun)), err)
		for _, w := range it.watches {
			w.forgetToken() // read again on the next refresh, though no token moved
		}
	}
	list.SetPlaceholder(p)
	return err
}

// flush fills the tree again when a change was heard, and stops the
// watches once the tree is gone.
func (it *indexTree) flush() {
	if inst := it.r.Instances[it.tree.id]; inst == nil || inst.Data != it {
		for _, w := range it.watches {
			w.Stop()
		}
		return
	}
	if !it.dirty || !it.shown() {
		return // read when its page opens (IndexTreeRefill)
	}
	it.dirty = false
	// A pass that told both nouns' watches reads the stores once: the
	// first watch's fill already read what the second's would.
	if h := it.r.watchHub(); h.refreshing {
		if it.round == h.round {
			return
		}
		it.round = h.round
	}
	// A failure is the tree's empty line, never an error on screen: the
	// stores answer again on the next change or open.
	_ = it.refresh()
}

// shown reports whether the tree's page is the current page (a tree on no
// page is always shown): a change heard while it is not is read on open.
func (it *indexTree) shown() bool {
	for _, step := range it.r.Ancestry(it.tree.id) {
		if it.r.PageOf(step) != nil {
			return step == it.r.Page
		}
	}
	return true
}

// fill reads both stores and sets the tree's nodes: each parent thing,
// then the things naming it; then the things with no listed parent.
func (it *indexTree) fill() error {
	r := it.r
	c := r.backgroundCtx()
	// One read of each store per fill: the parents are read first and kept
	// as the batch's other things (Ctx.refs), so the listed noun's derived
	// props that read its parent (an inherited owner) and the cells that
	// show a reference's label find them there instead of reading them
	// again, one by one.
	c.refs = map[string]Thing{}
	n := r.Nouns[it.noun]
	id := it.tree.id
	var pn *Noun
	var parents ListPage
	if it.pnoun != "" {
		pn = r.Nouns[it.pnoun]
		var err error
		if parents, err = r.queryThings(c, pn, ListQuery{}); err != nil {
			return err
		}
		if kp := pn.keyProp(); kp != nil {
			for _, t := range parents.Items {
				if !emptyProp(t[kp.Name]) {
					c.refs[pn.Name+"\x00"+fmt.Sprint(t[kp.Name])] = t
				}
			}
		}
	}
	page, err := r.queryThings(c, n, ListQuery{})
	if err != nil {
		return err
	}
	it.fills++
	items := r.thingItems(c, n, id, page.Items, it.columns)
	if pn == nil {
		return it.tree.SetNodes(it.nodes(items, nil))
	}
	pitems := r.thingItems(c, pn, id, parents.Items, it.columns)
	known := map[string]bool{}
	for i := range pitems { // LIST/NOUN:KEY: never an id of the listed noun
		key := strings.TrimPrefix(pitems[i].ID, id+"/")
		pitems[i].ID = id + "/" + pn.Name + ":" + key
		known[key] = true
	}
	under := map[string][]ListItem{}
	var top []ListItem
	for _, item := range items {
		t, _ := item.Data.(Thing)
		ref := ""
		if t != nil && !emptyProp(t[it.parent]) {
			ref = fmt.Sprint(t[it.parent])
		}
		if known[ref] {
			under[ref] = append(under[ref], item)
		} else {
			top = append(top, item)
		}
	}
	var nodes []TreeNode
	for _, row := range pitems {
		key := strings.TrimPrefix(row.ID, id+"/"+pn.Name+":")
		nodes = append(nodes, it.nodes([]ListItem{row}, nil)...)
		nodes = append(nodes, it.nodes(under[key], &row)...)
	}
	nodes = append(nodes, it.nodes(top, nil)...)
	return it.tree.SetNodes(nodes)
}

// nodes are rows as tree nodes under parent (nil: the top level); cell 0
// is the label when the columns name no prop for it.
func (it *indexTree) nodes(items []ListItem, parent *ListItem) []TreeNode {
	out := make([]TreeNode, 0, len(items))
	for _, item := range items {
		node := TreeNode{ID: item.ID, Label: item.Label, Noun: item.Noun, Data: item.Data, Cells: item.Cells, Tones: item.Tones, Search: item.Search, Classes: item.Classes}
		if t, ok := item.Data.(Thing); ok {
			node.Props = t
		}
		if len(node.Cells) > 0 && node.Cells[0] == "" {
			node.Cells[0] = item.Label
		}
		if parent != nil {
			node.Parent = parent.ID
		}
		out = append(out, node)
	}
	return out
}
