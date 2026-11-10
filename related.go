package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// RelatedNoun is the noun of a related section: the section instance, and
// each section's heading row (and, in the open empty mode, its empty row).
// Its verbs (related.go's registerRelated) act on the nearest section on
// the focus path, so they answer on a heading row and on any row of the
// section's own thing alike.
const RelatedNoun = "ochazuke.related-section"

// RelatedStackNoun is the noun of the stack of sections AddRelated makes.
const RelatedStackNoun = "ochazuke.related"

// The values of the empty setting (RelatedSpec.Empty): what a section with
// no things draws.
const (
	// RelatedEmptyHeader draws the heading with (0) and nothing under it:
	// you see what can be associated and press n or a there. The default.
	RelatedEmptyHeader = "header"
	// RelatedEmptyHide leaves the section out until it has a thing.
	RelatedEmptyHide = "hide"
	// RelatedEmptyOpen draws the heading and an empty row under it.
	RelatedEmptyOpen = "open"
)

// RelatedSpec describes one stack of related sections: for the things of
// Noun (a project), one section per Referrer, the referring noun's things
// whose referring prop holds the key. Everything about a noun comes from
// its declared props (Runtime.Referrers); the spec holds only what the
// application places: which key, which settings, and where rows come from
// for a noun without a store.
//
// The setting names are the application's own (it declares them, so its
// Settings page and settings.json own them); "" means no setting and the
// built-in default. Order lists section ids (NOUN.PROP), the ones it names
// first in that order, the others after in registration order. Hidden
// lists ids kept out (an entry ending in * is a prefix). Collapsed lists
// ids that start collapsed; the state afterwards is the stack's view state.
// Counts is a boolean (default on): the heading shows (N). Empty is
// RelatedEmptyHeader (default), RelatedEmptyHide or RelatedEmptyOpen.
type RelatedSpec struct {
	// Noun is the noun the sections hang off.
	Noun string
	// Key reads the key of the Noun thing the sections show, on every
	// draw and verb: the project list's selection. "" shows no rows.
	Key func() string
	// Target, when set, replaces Noun and Key: it names the thing the
	// sections show now, by noun and key, on every draw and verb (the
	// selected row of a list: FollowSelection). The sections are those of
	// the noun's referrers, and are made again when the noun changes with
	// the selection, so one stack serves a list of mixed nouns. An empty
	// noun or key shows no sections. Noun may then be empty.
	Target func() (noun, key string)
	// Enabled names a boolean setting (default on when unset or missing)
	// that turns the whole stack off: nothing is read and nothing drawn
	// while it is false. "" is always on.
	Enabled string
	// Order, Hidden, Collapsed, Counts and Empty name the settings above.
	Order, Hidden, Collapsed, Counts, Empty string
	// Rows lists the things of a noun without a store (a mounted noun)
	// whose prop holds key (for an any-noun reference, AnyNoun, "noun:key"); a noun with a store lists through the store's
	// standard list with the filter PROP = key. Called when the key changes,
	// a section unfolds, refresh runs or Interval passes, never on a draw.
	Rows func(c Ctx, ref Referrer, key string) ([]Thing, error)
	// Interval names a duration setting: the hook sections are read again
	// that often (0 or "" never). They are otherwise read when the key
	// changes, a section unfolds, the refresh verb runs or Refill is
	// called, never on a draw.
	Interval string
	// Now is the clock the interval reads (default time.Now).
	Now func() time.Time
	// Scope narrows a placed view (NounBuilder.RelatedView) to the things
	// owned by key: called when the key changes, and when the sections are
	// refilled. Nil: the view is drawn as it stands.
	Scope func(c Ctx, ref Referrer, view, key string) error
}

// Related is a stack of related sections drawn one under the other in a
// scrolling region. Each section is an instance of RelatedNoun holding a
// list whose first row is the section's heading; the j and k motions
// spill from one section to the next as they do between lists, and the
// sections' verbs (toggle, open, close, next, new, associate, ...) are the
// noun ochazuke.related-section's.
type Related struct {
	r    *Runtime
	id   string
	spec RelatedSpec

	look     string // the counts and empty settings the headings were made with
	noun     string // the noun the sections hang off now
	key      string // the key the sections show now
	synced   bool
	sections []*relatedSection // every section, in referrer order
	byID     map[string]*relatedSection
	collapse map[string]bool // collapsed sections (view state)
	seen     map[string]bool // sections whose collapse started from the setting
	top      int             // the first line the stack shows
	lastLine int             // the focused line of the last draw in the stack
	watches  []*Watch
	dirty    map[string]bool // nouns that changed since the last fill
	relGen   uint64          // the relation generation the relation sections were filled at
	read     time.Time       // when the hook sections were last read
	closed   bool
}

// relatedSection is one section: a referrer's things for the key.
type relatedSection struct {
	rel   *Related
	ref   Referrer
	id    string // the section instance
	list  *Collection
	n     *Noun
	view  string // the placed view standing for the things, "" the list
	count int    // things shown, archived ones left out
}

// AddRelated registers a stack of related sections under Parent (default
// app). Draw it from a page or a pane with View; it keeps itself in step
// with the key, the settings and the stores of the referring nouns.
func (r *Runtime) AddRelated(id string, spec RelatedSpec, opts ...InstanceOption) (*Related, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("related ID %q must be nonempty and unused", id)
	}
	if spec.Target == nil {
		if spec.Noun == "" || r.Noun(spec.Noun) == nil {
			return nil, fmt.Errorf("related %q: no such noun %q", id, spec.Noun)
		}
		spec.Noun = r.CanonicalNoun(spec.Noun)
	}
	is := instanceSpec{parent: "app"}
	for _, opt := range opts {
		opt(&is)
	}
	if r.Instances[is.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", is.parent)
	}
	rel := &Related{r: r, id: id, spec: spec, byID: map[string]*relatedSection{}, collapse: map[string]bool{}, seen: map[string]bool{}, dirty: map[string]bool{}}
	r.Instance(id, Parent(is.parent), Kind(RelatedStackNoun), FocusOnClose(is.closeFocus))
	r.Instances[id].Component = rel
	rel.sync()
	return rel, nil
}

// Related is the stack with this id, or nil.
func (r *Runtime) Related(id string) *Related { return componentAt[*Related](r, id) }

// relatedOf is the stack on a focus's ancestry, or nil.
func (r *Runtime) relatedOf(focus string) *Related { return nearestComponent[*Related](r, focus) }

// relatedSectionOf is the nearest section on a focus's ancestry, or nil.
func (r *Runtime) relatedSectionOf(focus string) *relatedSection {
	return nearestComponent[*relatedSection](r, focus)
}

// ID is the stack's instance id.
func (rel *Related) ID() string { return rel.id }

// Key is the key the sections show.
func (rel *Related) Key() string { rel.sync(); return rel.key }

// List is the list of the section with this id (NOUN.PROP), nil when there
// is no such section.
func (rel *Related) List(section string) *Collection {
	if s := rel.byID[section]; s != nil {
		return s.list
	}
	return nil
}

// SectionID is the id of the section on the focus path, "" when none.
func (rel *Related) SectionID(focus string) string {
	if s := rel.r.relatedSectionOf(focus); s != nil && s.rel == rel {
		return s.ref.ID()
	}
	return ""
}

// HandleMessage: the stack does no off-loop work.
func (rel *Related) HandleMessage(tea.Msg) bool { return false }

// PendingCommand: nothing is ever scheduled.
func (rel *Related) PendingCommand() tea.Cmd { return nil }

// Close stops the watches of the referring nouns.
func (rel *Related) Close() {
	rel.closed = true
	for _, w := range rel.watches {
		w.Stop()
	}
	rel.watches = nil
}

// holdsList declares a section's list its child table (listHolder).
func (s *relatedSection) holdsList(l *Collection) bool { return s.list == l }

// Refill reads every section's things again.
func (rel *Related) Refill() {
	rel.sync()
	rel.fillAll()
}

// settings --------------------------------------------------------------

func (rel *Related) listSetting(name string) []string {
	if name == "" {
		return nil
	}
	return rel.r.Settings().Strings(name)
}

func (rel *Related) countsShown() bool {
	if rel.spec.Counts == "" {
		return true
	}
	if _, ok := rel.r.settingValue(rel.spec.Counts); !ok {
		return true
	}
	return rel.r.Settings().Bool(rel.spec.Counts)
}

// The empty modes by place in relatedEmptyModes.
const (
	relatedEmptyAtHeader = iota
	relatedEmptyAtHide
	relatedEmptyAtOpen
)

var relatedEmptyModes = []string{RelatedEmptyHeader, RelatedEmptyHide, RelatedEmptyOpen}

// emptyMode is the place of the empty setting's value in relatedEmptyModes;
// the header mode for no setting or a value that is none of them.
func (rel *Related) emptyMode() int {
	if rel.spec.Empty == "" {
		return relatedEmptyAtHeader
	}
	return max(relatedEmptyAtHeader, slices.Index(relatedEmptyModes, rel.r.Settings().String(rel.spec.Empty)))
}

// hiddenBy is the hidden entry that names the section, "" when none does.
func (rel *Related) hiddenBy(id string) string {
	for _, entry := range rel.listSetting(rel.spec.Hidden) {
		if noSpillMatches(strings.TrimSpace(entry), id) {
			return entry
		}
	}
	return ""
}

// order is every section in display order: the ones the order setting
// names first, then the others in referrer order.
func (rel *Related) order() []*relatedSection {
	out := make([]*relatedSection, 0, len(rel.sections))
	for _, id := range rel.listSetting(rel.spec.Order) {
		if s := rel.byID[strings.TrimSpace(id)]; s != nil && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, s := range rel.sections {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// visible is the sections drawn: not hidden by the setting and, in the
// omit empty mode, not empty.
func (rel *Related) visible() []*relatedSection {
	if !rel.enabled() {
		return nil
	}
	hideEmpty := rel.emptyMode() == relatedEmptyAtHide
	var out []*relatedSection
	for _, s := range rel.order() {
		if rel.hiddenBy(s.ref.ID()) != "" || hideEmpty && s.count == 0 {
			continue
		}
		out = append(out, s)
	}
	return out
}

// sync brings the stack in step with the referrers and the key: sections
// for new referrers, a fill when the key changed, a poll of the row hooks.
func (rel *Related) sync() {
	if rel.closed {
		return
	}
	if !rel.enabled() {
		rel.synced = false // read again once it is turned on
		return
	}
	noun, key := rel.target()
	if !rel.synced || noun != rel.noun {
		rel.dropSections()
		rel.noun = noun
	}
	if noun != "" {
		sections := rel.r.Settings().Bool(RelationSectionsSetting)
		var late []*relatedSection
		for _, ref := range rel.r.Referrers(noun) {
			if ref.Relation != "" && !sections {
				continue
			}
			if rel.byID[ref.ID()] == nil {
				rel.addSection(ref)
				if added := rel.byID[ref.ID()]; added != nil && rel.synced && ref.Relation != "" {
					late = append(late, added) // switched on after the first read: read it now
				}
			}
		}
		rel.dropRelationSections(sections)
		for _, s := range late {
			s.fill()
		}
	}
	look := fmt.Sprint(rel.countsShown(), rel.emptyMode())
	keyChanged := !rel.synced || key != rel.key
	if keyChanged {
		rel.synced, rel.key = true, key
	}
	if keyChanged || look != rel.look {
		rel.look = look
		rel.fillAll()
	}
	rel.syncRelations(keyChanged)
	if keyChanged {
		for _, s := range rel.sections {
			if s.list != nil && s.list.Len() > 0 {
				_ = s.list.Select(s.headID())
			}
		}
		return
	}
	for noun := range rel.dirty {
		delete(rel.dirty, noun)
		for _, s := range rel.sections {
			if s.ref.Noun == noun {
				s.fill()
			}
		}
	}
	rel.refreshDue()
}

// target is the noun and key the sections show now: the Target func's
// answer, else Noun and Key. An empty noun or key shows none.
func (rel *Related) target() (noun, key string) {
	switch {
	case rel.spec.Target != nil:
		noun, key = rel.spec.Target()
		if noun != "" && noun != AnyNoun {
			noun = rel.r.CanonicalNoun(noun)
		}
	default:
		noun = rel.spec.Noun
		if rel.spec.Key != nil {
			key = rel.spec.Key()
		}
	}
	if rel.r.Noun(noun) == nil {
		return "", ""
	}
	return noun, key
}

// enabled reports whether the stack is on (Enabled; a setting that does
// not exist counts as on).
func (rel *Related) enabled() bool {
	if rel.spec.Enabled == "" {
		return true
	}
	if _, ok := rel.r.settingValue(rel.spec.Enabled); !ok {
		return true
	}
	return rel.r.Settings().Bool(rel.spec.Enabled)
}

// dropSections removes every section: the selection moved to a thing of
// another noun, so its referrers are others. Collapse state stays, by id.
func (rel *Related) dropSections() {
	for _, s := range rel.sections {
		_ = rel.r.RemoveInstance(s.id)
	}
	rel.sections, rel.byID = nil, map[string]*relatedSection{}
	rel.lastLine, rel.top = 0, 0
	rel.look = ""
}

// value is what the section's referring prop holds to name the stack's
// thing: its key, or "noun:key" for an any-noun reference.
func (s *relatedSection) value() string { return s.ref.Value(s.rel.noun, s.rel.key) }

// refreshDue refills the hook sections when the spec's interval setting
// names a duration that has passed since they were last read. It reads the
// clock, never the hook, on a draw that is not due.
func (rel *Related) refreshDue() {
	if rel.spec.Interval == "" {
		return
	}
	every := rel.r.Settings().Duration(rel.spec.Interval)
	if every <= 0 {
		return
	}
	now := rel.now()
	if rel.read.IsZero() {
		rel.read = now
		return
	}
	if now.Sub(rel.read) >= every {
		rel.refreshHooks()
	}
}

func (rel *Related) now() time.Time {
	if rel.spec.Now != nil {
		return rel.spec.Now()
	}
	return time.Now()
}

// refreshHooks reads the rows of every hook section again.
func (rel *Related) refreshHooks() {
	rel.read = rel.now()
	for _, s := range rel.sections {
		if s.hooked() {
			s.fill()
		}
	}
}

func (rel *Related) fillAll() {
	for _, s := range rel.sections {
		s.fill()
	}
}

// addSection makes the section of ref when its things can be listed: the
// noun has a store with a list and a key prop, or the spec has Rows.
func (rel *Related) addSection(ref Referrer) {
	r := rel.r
	n := r.Nouns[ref.Noun]
	if n == nil || n.keyProp() == nil {
		return
	}
	stored := n.Store != nil && (n.Store.List != nil || n.Store.Query != nil)
	if !stored && rel.spec.Rows == nil && ref.Relation == "" {
		return
	}
	s := &relatedSection{rel: rel, ref: ref, id: rel.id + "/" + ref.ID(), n: n, view: n.relatedView}
	r.Instance(s.id, Parent(rel.id), Kind(RelatedNoun))
	r.Instances[s.id].Component = s
	r.Instances[s.id].Label = ref.Label
	opts := []InstanceOption{Parent(s.id)}
	if len(n.Columns) > 0 && s.view == "" {
		// The heading shares the first column with its rows' cells, so the
		// column is as wide as the heading needs: no fixed width or cap.
		cols := slices.Clone(n.Columns)
		cols[0].Width, cols[0].Max = 0, 0
		opts = append(opts, Columns(cols...), CellSelect())
	}
	list, err := r.AddList(s.id+"/list", nil, opts...)
	if err != nil {
		_ = r.RemoveInstance(s.id)
		return
	}
	s.list = list
	list.SetMatchNoun(pluralWord(shortNounName(ref.Noun)))
	if s.view != "" {
		// The view stands for the things: it sits under the section, so a
		// z verb or a motion inside it reaches the section.
		if v := r.Instances[s.view]; v != nil {
			v.Parent = s.id
			r.renderCache.instances.generation++
		}
	}
	rel.sections = append(rel.sections, s)
	rel.byID[ref.ID()] = s
	if stored && ref.Relation == "" {
		rel.watch(ref.Noun)
	}
}

// shortNounName is the last word of a noun's name (lane.lane is lane).
func shortNounName(noun string) string {
	if i := strings.LastIndex(noun, "."); i >= 0 {
		return noun[i+1:]
	}
	return noun
}

// watch listens to a referring noun's store: any change fills its sections
// again, once per batch.
func (rel *Related) watch(noun string) {
	for _, w := range rel.watches {
		if w.noun == noun {
			return
		}
	}
	w, err := rel.r.Watch(noun, nil, func(WatchEvent) { rel.dirty[noun] = true })
	if err != nil {
		return
	}
	w.batch = func() {
		for n := range rel.dirty {
			delete(rel.dirty, n)
			for _, s := range rel.sections {
				if s.ref.Noun == n {
					s.fill()
				}
			}
		}
	}
	rel.watches = append(rel.watches, w)
}

// section ----------------------------------------------------------------

func (s *relatedSection) headID() string  { return s.list.id + "/@head" }
func (s *relatedSection) emptyID() string { return s.list.id + "/@empty" }

func (s *relatedSection) hooked() bool {
	return s.ref.Relation == "" && s.rel.spec.Rows != nil && !(s.n.Store != nil && (s.n.Store.List != nil || s.n.Store.Query != nil))
}

func (s *relatedSection) collapsed() bool { return s.rel.isCollapsed(s.ref.ID()) }

// isCollapsed is the section's collapse state, starting from the setting.
func (rel *Related) isCollapsed(id string) bool {
	if !rel.seen[id] {
		rel.seen[id] = true
		for _, entry := range rel.listSetting(rel.spec.Collapsed) {
			if noSpillMatches(strings.TrimSpace(entry), id) {
				rel.collapse[id] = true
			}
		}
	}
	return rel.collapse[id]
}

// read lists the things owned by the key.
func (s *relatedSection) read() ([]Thing, error) {
	rel := s.rel
	if rel.key == "" {
		return nil, nil
	}
	r := rel.r
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context()}
	if s.ref.Relation != "" {
		return s.readRelations(c)
	}
	if s.hooked() {
		return rel.spec.Rows(c, s.ref, s.value())
	}
	if s.n.Store.Remote {
		return nil, nil // a remote store is filled by its own job; not read on the loop
	}
	page, err := r.queryThings(c, s.n, ListQuery{Archived: true, Filters: []PropFilter{{Prop: s.ref.Prop, Op: "=", Value: s.value()}}})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// fill reads the things and shows them: the heading, then the things unless
// the section is collapsed (a placed view draws its own).
func (s *relatedSection) fill() {
	if s.list == nil {
		return
	}
	rel := s.rel
	r := rel.r
	things, err := s.read()
	if err != nil {
		r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(s.ref.Noun), err))
		things = nil
	}
	hiding := r.archiveHiding(s.n, s.list.vs)
	shown := make([]Thing, 0, len(things))
	for _, t := range things {
		if hiding == nil || !archivedThing(hiding, t) {
			shown = append(shown, t)
		}
	}
	s.count = len(shown)
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context()}
	items := []ListItem{s.heading()}
	empty := len(shown) == 0
	if s.view != "" {
		if rel.spec.Scope != nil && rel.key != "" {
			if err := rel.spec.Scope(c, s.ref, s.view, s.value()); err != nil {
				r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(s.ref.Noun), err))
			}
		}
	} else if !s.collapsed() {
		if empty && rel.emptyMode() == relatedEmptyAtOpen {
			items = append(items, s.emptyRow())
		}
		items = append(items, r.thingItems(c, s.n, s.list.id, shown, s.list.columns)...)
	}
	if err := s.list.SetItems(items); err != nil {
		r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(s.ref.Noun), err))
	}
}

// heading is the section's first row: a fold marker, the label and the
// count; a focus stop whose verbs are the section's.
func (s *relatedSection) heading() ListItem {
	mark := "▾"
	if s.collapsed() || s.count == 0 {
		mark = "▸"
	}
	label := mark + " " + s.ref.Label
	if s.rel.countsShown() {
		label += fmt.Sprintf(" (%d)", s.count)
	}
	item := ListItem{ID: s.headID(), Label: label, Noun: RelatedNoun, Data: s.thing(), Tones: []ToneSpan{{Cell: 0, Tone: ToneHeader}}}
	if n := len(s.list.columns); n > 0 {
		item.Cells = make([]string, n)
		item.Cells[0] = label
	}
	return item
}

// emptyRow is the row of an open empty section.
func (s *relatedSection) emptyRow() ListItem {
	text := EmptyText(pluralWord(shortNounName(s.ref.Noun)))
	item := ListItem{ID: s.emptyID(), Label: "  " + text, Noun: RelatedNoun, Data: s.thing()}
	if n := len(s.list.columns); n > 0 {
		item.Cells = make([]string, n)
		item.Cells[0] = item.Label
	}
	return item
}

// thing is the section as the noun's thing: what list and get answer.
func (s *relatedSection) thing() Thing {
	return Thing{"id": s.ref.ID(), "noun": s.ref.Noun, "prop": s.ref.Prop, "label": s.ref.Label, "count": s.count,
		"collapsed": s.collapsed(), "hidden": s.rel.hiddenBy(s.ref.ID()) != "", "position": slices.Index(s.rel.order(), s) + 1}
}

// setCollapsed folds or unfolds the section; the focus stays on its
// heading when a fold takes the row it was on.
func (s *relatedSection) setCollapsed(collapsed bool) {
	rel := s.rel
	rel.isCollapsed(s.ref.ID()) // the setting's start first
	if rel.collapse[s.ref.ID()] == collapsed {
		return
	}
	rel.collapse[s.ref.ID()] = collapsed
	if collapsed && rel.r.focusInside(s.id) {
		_ = s.list.Select(s.headID())
		_ = rel.r.SetFocus(s.list.FocusTarget())
	}
	s.fill()
}

// focusInside reports whether the focus is on or inside the instance id.
func (r *Runtime) focusInside(id string) bool { return slices.Contains(r.Ancestry(r.Focus), id) }

// The settings of a related stack placed as a detail (AddRelatedDetail).
const (
	// RelatedDetailSetting turns the detail stacks on (default) or off.
	RelatedDetailSetting = "detail.related"
	// RelatedDetailHiddenSetting lists the section ids (NOUN.PROP, or a
	// prefix ending in *) kept out of detail stacks.
	RelatedDetailHiddenSetting = "detail.related-hidden"
)

// SelectionTarget is a RelatedSpec.Target that follows the selected row of
// a list: the row's noun and the key of the thing it stands for, so a stack
// shows what names that thing, whatever its noun. list names the list
// (a *Collection) on every call, so a page that switches
// between lists answers for the one now followed. No selection or a row
// without a noun names nothing.
func (r *Runtime) SelectionTarget(list func() *Collection) func() (noun, key string) {
	return func() (string, string) {
		l := list()
		if l == nil {
			return "", ""
		}
		item, ok := l.Selected()
		if !ok {
			return "", ""
		}
		inst := r.Instances[item.ID]
		noun := item.Noun
		if noun == "" && inst != nil {
			noun = inst.Noun
		}
		n := r.Nouns[r.CanonicalNoun(noun)]
		if inst == nil || n == nil {
			return "", ""
		}
		if n.keyProp() == nil {
			// A noun with no key prop (a mounted catalog's row): the row's
			// Data is the id its verbs take.
			key, _ := inst.Data.(string)
			return n.Name, key
		}
		key, err := r.rowKey(n, inst)
		if err != nil {
			return "", ""
		}
		return n.Name, key
	}
}

// AddRelatedDetail registers a related stack that is the detail of a
// list's selected row: its sections are the referrers of the row's noun
// (notes about it, tasks that belong to it), read for the row's key, made
// again when the selection moves to a row of another noun and filled again
// when it moves to another row. It reads on that change, on a store's
// change, on unfold and refresh, never on a draw. list names the followed
// list on every call. The settings are RelatedDetailSetting
// (off: nothing drawn or read) and RelatedDetailHiddenSetting. Draw it
// with View, or Text inside a scrolling detail.
func (r *Runtime) AddRelatedDetail(id string, list func() *Collection, opts ...InstanceOption) (*Related, error) {
	return r.AddRelated(id, RelatedSpec{Target: r.SelectionTarget(list), Enabled: RelatedDetailSetting, Hidden: RelatedDetailHiddenSetting}, opts...)
}

// Noun is the noun the sections hang off now ("" when none).
func (rel *Related) Noun() string { rel.sync(); return rel.noun }

// Text draws the sections at their natural height, one under the other,
// width cells wide: a detail's text to scroll as a whole, with the sections'
// lists still the focus stops they are. "" when nothing is shown.
func (rel *Related) Text(width int) string {
	rel.sync()
	if width <= 0 {
		return ""
	}
	var lines []string
	for _, s := range rel.visible() {
		text, _ := s.draw(width)
		lines = append(lines, strings.Split(text, "\n")...)
	}
	return strings.Join(lines, "\n")
}
