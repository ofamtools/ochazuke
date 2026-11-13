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
	// History adds the history section last: the thing's history entries
	// (the history verb's rows), read again when the history grows.
	History bool
	// GroupBy splits a section by a prop of its things: section id
	// (NOUN.PROP) to the prop. Each value the things hold is a section of
	// its own, id NOUN.SPLITPROP=VALUE, its heading the value's plural (a
	// context item's kind: Links, Files, Dirs), in the order the values
	// first appear; the things are read once for all of them, and a value
	// section's new fills the value.
	GroupBy map[string]string
	// Groups names a list setting whose entries "ID = MEMBER, MEMBER" draw
	// several sections under one heading ID (needs-you = action-item,
	// question.decision): a member is a section id (a prefix ending in *)
	// or a noun, standing for every section of that noun. The heading
	// counts the members' things and folds them together; each row keeps
	// its own section's verbs. The heading reads the id with its dashes as
	// spaces. "" or an empty setting groups nothing.
	Groups string
	// Lists are sections that list a noun's saved filter, whatever thing
	// the stack shows (the day's focus in a pane's rail). They read
	// through the noun's standard list, refill on its watch and the
	// refresh verb, never on a draw or a change of the stack's thing, and
	// take the order, hidden, collapsed, counts and empty settings by
	// their id.
	Lists []RelatedList
	// Ends names a string setting: "stay" keeps j and k inside the stack
	// at its first and last drawn rows (UX-RULES R3.6, as NoSpill), any
	// other value or "" lets them spill to the neighbouring stops.
	Ends string
	// Plain draws every section as a plain list, without its noun's
	// columns: a narrow region (the rail).
	Plain bool
}

// RelatedList is one list section (RelatedSpec.Lists): the things of Noun
// its saved filter Filter keeps ("" every thing), under Label (default the
// noun's plural); New names the verb (noun.verb) the section's new runs,
// "" the noun's create.
type RelatedList struct {
	ID, Noun, Filter, Label, New string
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
	histSeq  uint64          // the newest history entry the history section was filled at
	read     time.Time       // when the hook sections were last read
	closed   bool
	splits   map[string]*relatedSplit // the split sections by their referrer's id (GroupBy)
	grouped  string                   // the groups setting and sections the groups were made for
	shape    int                      // counts section additions and removals: the groups follow it
}

// relatedSplit is a section split by a prop (RelatedSpec.GroupBy): its
// referrer's things are read once and shown in one value section each.
type relatedSplit struct {
	ref  Referrer
	prop string
	at   int // where its referrer came among the sections: its values go there
}

// relatedSection is one section: a referrer's things for the key.
type relatedSection struct {
	rel        *Related
	ref        Referrer
	id         string // the section instance
	list       *Collection
	n          *Noun
	view       string // the placed view standing for the things, "" the list
	viewParent string // the borrowed view's owner before section placement
	count      int    // things shown, archived ones left out
	shown      []Thing
	filled     bool
	split      *relatedSplit     // a value section's split, nil otherwise
	group      *relatedSection   // the group heading drawing this section's rows, nil when none
	members    []*relatedSection // a group heading's sections
	grouping   bool              // a group heading (RelatedSpec.Groups)
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
	rel := &Related{r: r, id: id, spec: spec, byID: map[string]*relatedSection{}, collapse: map[string]bool{}, seen: map[string]bool{}, dirty: map[string]bool{}, splits: map[string]*relatedSplit{}}
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
	return namedBy(rel.listSetting(rel.spec.Hidden), id)
}

// namedBy is the entry of a hidden or collapsed setting that names the
// section id: an id, or a prefix ending in *; a later entry "!PATTERN"
// takes the ids it names back out ("*", "!needs-you": every section but
// one). "" when none names it.
func namedBy(entries []string, id string) string {
	by := ""
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if not, ok := strings.CutPrefix(entry, "!"); ok {
			if noSpillMatches(strings.TrimSpace(not), id) {
				by = ""
			}
			continue
		}
		if noSpillMatches(entry, id) {
			by = entry
		}
	}
	return by
}

// order is every section in display order: the ones the order setting
// names first, then the others in referrer order, with history last. A
// group heading stands where the first of its members would, or where
// the order setting names it, its members after it.
func (rel *Related) order() []*relatedSection {
	base := make([]*relatedSection, 0, len(rel.sections))
	var add func(s *relatedSection)
	add = func(s *relatedSection) {
		if s.grouping {
			for _, m := range s.members {
				add(m)
			}
			return
		}
		if !slices.Contains(base, s) {
			base = append(base, s)
		}
	}
	for _, id := range rel.listSetting(rel.spec.Order) {
		if s := rel.byID[strings.TrimSpace(id)]; s != nil && !s.ref.History {
			add(s)
		}
	}
	for _, s := range rel.sections {
		if !s.ref.History && !s.grouping {
			add(s)
		}
	}
	for _, s := range rel.sections {
		if s.ref.History {
			add(s)
		}
	}
	out := make([]*relatedSection, 0, len(rel.sections))
	for _, s := range base {
		switch {
		case s.group == nil:
			out = append(out, s)
		case !slices.Contains(out, s.group):
			out = append(out, s.group)
			for _, m := range base {
				if m.group == s.group {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// visible is the sections drawn: not hidden by the setting and, in the
// omit empty mode, not empty. A group's member is drawn under its heading
// while it has rows to show.
func (rel *Related) visible() []*relatedSection {
	if !rel.enabled() {
		return nil
	}
	hideEmpty := rel.emptyMode() == relatedEmptyAtHide
	var out []*relatedSection
	for _, s := range rel.order() {
		switch {
		case rel.hiddenBy(s.ref.ID()) != "":
		case s.group != nil:
			if slices.Contains(out, s.group) && s.list.Len() > 0 {
				out = append(out, s)
			}
		case hideEmpty && s.count == 0:
		default:
			out = append(out, s)
		}
	}
	return out
}

// headed is the drawn sections that have a heading (the members of a
// group draw under their group's).
func (rel *Related) headed() []*relatedSection {
	return slices.DeleteFunc(rel.visible(), func(s *relatedSection) bool { return s.group != nil })
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
			if prop := rel.spec.GroupBy[ref.ID()]; prop != "" {
				rel.addSplit(ref, prop)
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
		rel.ensureHistorySection()
		for _, s := range late {
			s.fill()
		}
	}
	rel.ensureLists()
	look := fmt.Sprint(rel.countsShown(), rel.emptyMode())
	keyChanged := !rel.synced || key != rel.key
	if keyChanged {
		rel.synced, rel.key = true, key
	}
	switch {
	case look != rel.look:
		rel.look = look
		rel.fillAll()
	case keyChanged:
		// A list section lists the same things whatever the key: it is
		// read once, then on its noun's watch and refresh.
		rel.fillSections(func(ref Referrer) bool { return !ref.List || !rel.filledList(ref) })
	}
	rel.syncRelations(keyChanged)
	rel.syncHistory(keyChanged)
	rel.syncGroups()
	if keyChanged {
		for _, s := range rel.sections {
			if s.list != nil && s.list.Len() > 0 && s.group == nil {
				_ = s.list.Select(s.headID())
			}
		}
		return
	}
	rel.fillDirty()
	rel.refreshDue()
}

// fillDirty fills the sections of the nouns that changed since the last
// fill again.
func (rel *Related) fillDirty() {
	if len(rel.dirty) == 0 || !rel.enabled() {
		return // off reads nothing; turned on, the stack is read whole
	}
	dirty := rel.dirty
	rel.dirty = map[string]bool{}
	rel.fillSections(func(ref Referrer) bool { return dirty[ref.Noun] })
}

// filledList reports whether the list section of ref has been read.
func (rel *Related) filledList(ref Referrer) bool {
	s := rel.byID[ref.ID()]
	return s != nil && s.filled
}

// fillSections reads the sections whose referrer want keeps again: a
// split's things once for all its values. A group heading follows its
// members.
func (rel *Related) fillSections(want func(Referrer) bool) {
	for _, sp := range rel.sortedSplits() {
		if want(sp.ref) {
			rel.fillSplit(sp)
		}
	}
	for _, s := range slices.Clone(rel.sections) {
		if !s.grouping && s.split == nil && want(s.ref) {
			s.fill()
		}
	}
	rel.syncGroups()
}

// sortedSplits are the splits in their referrers' order.
func (rel *Related) sortedSplits() []*relatedSplit {
	out := make([]*relatedSplit, 0, len(rel.splits))
	for _, sp := range rel.splits {
		out = append(out, sp)
	}
	slices.SortFunc(out, func(a, b *relatedSplit) int { return strings.Compare(a.ref.ID(), b.ref.ID()) })
	return out
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

// dropSections removes every section but the list sections: the
// selection moved to a thing of another noun, so its referrers are
// others. Collapse state stays, by id.
func (rel *Related) dropSections() {
	var kept []*relatedSection
	for _, s := range rel.sections {
		if s.ref.List {
			s.group = nil
			kept = append(kept, s)
			continue
		}
		s.remove()
	}
	rel.sections, rel.byID = kept, map[string]*relatedSection{}
	for _, s := range kept {
		rel.byID[s.ref.ID()] = s
	}
	rel.splits = map[string]*relatedSplit{}
	rel.dirty = map[string]bool{}
	rel.grouped = ""
	rel.shape++
	rel.lastLine, rel.top = 0, 0
	rel.look = ""
}

// remove returns a borrowed view to its owner before removing the section.
// Its rows survive section rebuilds, including enabled toggles.
func (s *relatedSection) remove() {
	r := s.rel.r
	if view := r.Instances[s.view]; view != nil && view.Parent == s.id {
		view.Parent = s.viewParent
		r.renderCache.instances.generation++
	}
	_ = r.RemoveInstance(s.id)
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
	rel.fillSections(rel.hookedRef)
}

func (rel *Related) fillAll() {
	rel.fillSections(func(Referrer) bool { return true })
}

// addSection makes the section of ref when its things can be listed: the
// noun has a store with a list and a key prop, or the spec has Rows.
func (rel *Related) addSection(ref Referrer) *relatedSection {
	r := rel.r
	n := r.Nouns[ref.Noun]
	if n == nil || n.keyProp() == nil {
		return nil
	}
	stored := n.Store != nil && (n.Store.List != nil || n.Store.Query != nil)
	if !stored && rel.spec.Rows == nil && ref.Relation == "" {
		return nil
	}
	s := &relatedSection{rel: rel, ref: ref, id: rel.id + "/" + ref.ID(), n: n, view: n.relatedView}
	if ref.List || ref.Section != "" {
		s.view = "" // a placed view stands for one referrer's things only
	}
	r.Instance(s.id, Parent(rel.id), Kind(RelatedNoun))
	r.Instances[s.id].Component = s
	r.Instances[s.id].Label = ref.Label
	opts := []InstanceOption{Parent(s.id)}
	if len(n.Columns) > 0 && s.view == "" && !rel.spec.Plain {
		// The heading shares the first column with its rows' cells, so the
		// column is as wide as the heading needs: no fixed width or cap.
		cols := slices.Clone(n.Columns)
		cols[0].Width, cols[0].Max = 0, 0
		opts = append(opts, Columns(cols...), CellSelect())
	}
	list, err := r.AddList(s.id+"/list", nil, opts...)
	if err != nil {
		_ = r.RemoveInstance(s.id)
		return nil
	}
	s.list = list
	list.SetMatchNoun(pluralWord(shortNounName(ref.Noun)))
	if s.view != "" {
		// The view stands for the things: it sits under the section, so a
		// z verb or a motion inside it reaches the section.
		if v := r.Instances[s.view]; v != nil {
			s.viewParent = v.Parent
			if owner := componentAt[*relatedSection](r, v.Parent); owner != nil {
				s.viewParent = owner.viewParent
			}
			v.Parent = s.id
			r.renderCache.instances.generation++
		}
	}
	rel.sections = append(rel.sections, s)
	rel.byID[ref.ID()] = s
	rel.shape++
	if stored && ref.Relation == "" && !ref.History {
		rel.watch(ref.Noun)
	}
	return s
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
	w.batch = rel.fillDirty
	rel.watches = append(rel.watches, w)
}

// section ----------------------------------------------------------------

func (s *relatedSection) headID() string  { return s.list.id + "/@head" }
func (s *relatedSection) emptyID() string { return s.list.id + "/@empty" }

func (s *relatedSection) hooked() bool { return !s.grouping && s.rel.hookedRef(s.ref) }

// hookedRef reports whether ref's things come from the Rows hook: a noun
// without a store that lists.
func (rel *Related) hookedRef(ref Referrer) bool {
	n := rel.r.Nouns[ref.Noun]
	return n != nil && ref.Relation == "" && rel.spec.Rows != nil && !(n.Store != nil && (n.Store.List != nil || n.Store.Query != nil))
}

func (s *relatedSection) collapsed() bool { return s.rel.isCollapsed(s.ref.ID()) }

// isCollapsed is the section's collapse state, starting from the setting.
func (rel *Related) isCollapsed(id string) bool {
	if !rel.seen[id] {
		rel.seen[id] = true
		if namedBy(rel.listSetting(rel.spec.Collapsed), id) != "" {
			rel.collapse[id] = true
		}
	}
	return rel.collapse[id]
}

// read lists the things owned by the key.
func (s *relatedSection) read() ([]Thing, error) { return s.rel.readRef(s.ref) }

// readRef lists the things of ref owned by the key: a list section's
// filter, whatever the key.
func (rel *Related) readRef(ref Referrer) ([]Thing, error) {
	r := rel.r
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context()}
	if ref.List {
		return rel.readList(c, ref)
	}
	if rel.key == "" {
		return nil, nil
	}
	if ref.Relation != "" {
		page, err := r.readRelatedRelations(c, rel.noun, rel.key, ref, ListQuery{Archived: true})
		return page.Items, err
	}
	if ref.History {
		return factThings(r.thingHistory(rel.noun, rel.key), nil)
	}
	page, err := r.readRelatedThings(c, rel.spec, rel.noun, rel.key, ref, ListQuery{Archived: true})
	return page.Items, err
}

// fill reads the things and shows them: the heading, then the things unless
// the section is collapsed (a placed view draws its own). A value section
// reads its split, a group heading its members.
func (s *relatedSection) fill() {
	switch {
	case s.split != nil:
		s.rel.fillSplit(s.split)
		return
	case s.grouping:
		for _, m := range s.members {
			m.fill()
		}
		return
	case s.list == nil:
		return
	}
	r := s.rel.r
	things, err := s.read()
	if err != nil {
		r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(s.ref.Noun), err))
		things = nil
	}
	s.show(things)
}

// show keeps the things the archive setting does not hide and draws them.
func (s *relatedSection) show(things []Thing) {
	r := s.rel.r
	hiding := r.archiveHiding(s.n, s.list.vs)
	shown := make([]Thing, 0, len(things))
	for _, t := range things {
		if hiding == nil || !archivedThing(hiding, t) {
			shown = append(shown, t)
		}
	}
	s.count, s.shown, s.filled = len(shown), shown, true
	s.render()
}

// render sets the section list's rows from the things it holds: the
// heading, then the things unless the section (or its group) is folded.
// A group's member draws no heading of its own; a group heading draws its
// heading alone, counting its members' things.
func (s *relatedSection) render() {
	rel := s.rel
	r := rel.r
	if s.grouping {
		s.count = 0
		for _, m := range s.members {
			if rel.hiddenBy(m.ref.ID()) == "" {
				s.count += m.count
			}
		}
		if err := s.list.SetItems([]ListItem{s.heading()}); err != nil {
			r.UI().Error(fmt.Sprintf("%s section: %v", s.ref.Label, err))
		}
		return
	}
	shown := s.shown
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context()}
	items := []ListItem{s.heading()}
	if s.group != nil {
		items = nil
		if !s.group.collapsed() {
			items = s.rowItems(c, shown)
		}
		if err := s.list.SetItems(items); err != nil {
			r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(s.ref.Noun), err))
		}
		s.group.render()
		return
	}
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
		items = append(items, s.rowItems(c, shown)...)
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
	if s.group != nil {
		s.group.setCollapsed(collapsed) // a member's rows fold with its group
		return
	}
	rel := s.rel
	rel.isCollapsed(s.ref.ID()) // the setting's start first
	if rel.collapse[s.ref.ID()] == collapsed {
		return
	}
	rel.collapse[s.ref.ID()] = collapsed
	if collapsed && s.grouping {
		for _, m := range s.members {
			if rel.r.focusInside(m.id) {
				_ = s.list.Select(s.headID())
				_ = rel.r.SetFocus(s.list.FocusTarget())
			}
		}
	}
	if collapsed && rel.r.focusInside(s.id) {
		_ = s.list.Select(s.headID())
		_ = rel.r.SetFocus(s.list.FocusTarget())
	}
	if collapsed {
		// Folding shows less of what is held; unfolding reads again.
		for _, m := range s.members {
			m.render()
		}
		s.render()
		return
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
