package ochazuke

import (
	"fmt"
	"maps"
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
	// Wrap names a boolean setting: on, the focused section's selected
	// row shows its whole text, wrapped over the lines it needs, instead of
	// being cut at the stack's width (Collection.WrapSelected). "" or a
	// missing setting: rows are cut.
	Wrap string
	// Outward names a boolean setting: on, the stack also has a section
	// per reference prop of the thing's own noun (Runtime.OwnReferences),
	// listing the things the thing names, so its relations show in both
	// directions. "" or a missing setting: none (the stack lists only what
	// names the thing).
	Outward string
	// Archived names a list setting of section ids (a prefix ending in *)
	// whose lists show their archived things (each list's archived toggle,
	// set when the section is made and again when the setting changes;
	// a change reads again only the sections it changed). "" sets none.
	Archived string
	// Explain names a boolean setting: on, a referrer whose things cannot
	// be read (its noun has no store that lists, or no key prop, and no
	// Rows hook) still has its section, its heading counting – and, open,
	// one row saying why, so every relation of the noun shows. "" or a
	// missing setting: such a referrer has no section.
	Explain string
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
	tokens   map[string]string // each watched noun's store change token when last seen
	dirty    map[string]bool   // nouns that changed since the last fill
	relGen   uint64            // the relation generation the relation sections were filled at
	histSeq  uint64            // the newest history entry the history section was filled at
	read     time.Time         // when the hook sections were last read
	closed   bool
	splits   map[string]*relatedSplit // the split sections by their referrer's id (GroupBy)
	grouped  string                   // the groups setting and sections the groups were made for
	shape    int                      // counts section additions and removals: the groups follow it
	landing  *relatedLanding          // the reads off the loop whose answers land together (related_read.go)
	readSeq  uint64                   // numbers the reads, so only a read's newest answer lands
	drawn    uint64                   // the frame (watchHub.frames) the stack was last drawn in
	archived string                   // the archived setting's value the sections' lists were last set from
	query    string                   // the stack's filter query (related_filter.go)
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
	counted    bool   // count is known: false for a folded section no one-row page counts
	shown      []Thing
	filled     bool
	unreadable string            // why the things cannot be read ("" they can): the section explains (RelatedSpec.Explain)
	split      *relatedSplit     // a value section's split, nil otherwise
	group      *relatedSection   // the group heading drawing this section's rows, nil when none
	members    []*relatedSection // a group heading's sections
	grouping   bool              // a group heading (RelatedSpec.Groups)
	loading    bool              // never filled and its first read runs off the loop: it says so
	matched    int               // rows the stack's filter query matches (related_filter.go)
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
	rel := &Related{r: r, id: id, spec: spec, byID: map[string]*relatedSection{}, collapse: map[string]bool{}, seen: map[string]bool{}, tokens: map[string]string{}, dirty: map[string]bool{}, splits: map[string]*relatedSplit{}}
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
	rel.stopWatches()
}

// stopWatches stops every watch the stack holds.
func (rel *Related) stopWatches() {
	for _, w := range rel.watches {
		w.Stop()
	}
	rel.watches = nil
	clear(rel.tokens)
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
		case hideEmpty && s.counted && s.shownCount() == 0:
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
		rel.stopWatches()
		return
	}
	defer rel.syncWatches()
	noun, key := rel.target()
	if !rel.synced || noun != rel.noun {
		rel.dropSections()
		rel.noun = noun
	}
	if noun != "" {
		sections := rel.r.Settings().Bool(RelationSectionsSetting)
		var late []*relatedSection
		for _, ref := range rel.refs(noun) {
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
		rel.dropOwnSections()
		rel.ensureHistorySection()
		for _, s := range late {
			s.fill()
		}
	}
	rel.ensureLists()
	rel.syncArchived()
	look := fmt.Sprint(rel.countsShown(), rel.emptyMode(), rel.foldedCount())
	keyChanged := !rel.synced || key != rel.key
	if keyChanged {
		rel.synced, rel.key = true, key
	}
	rel.syncGroups() // a member reads as its group shows it
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
	rel.fillSections(func(ref Referrer) bool { return dirty[ref.Noun] || ref.own() && dirty[rel.noun] })
}

// flag reads a boolean setting the spec names: false for "" or a setting
// that does not exist.
func (rel *Related) flag(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := rel.r.settingValue(name); !ok {
		return false
	}
	return rel.r.Settings().Bool(name)
}

// refs are the referrers the stack has sections for: every referrer of
// noun, then, with the Outward setting on, the noun's own references.
func (rel *Related) refs(noun string) []Referrer {
	refs := rel.r.Referrers(noun)
	if rel.flag(rel.spec.Outward) {
		refs = append(refs, rel.r.OwnReferences(noun)...)
	}
	return refs
}

// dropOwnSections removes the own-reference sections while the Outward
// setting is off.
func (rel *Related) dropOwnSections() {
	if rel.flag(rel.spec.Outward) {
		return
	}
	for _, s := range slices.Clone(rel.sections) {
		if s.ref.own() {
			s.remove()
			rel.sections = slices.DeleteFunc(rel.sections, func(o *relatedSection) bool { return o == s })
			delete(rel.byID, s.ref.ID())
			rel.shape++
		}
	}
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
	if n == nil {
		return nil
	}
	stored := n.Store != nil && (n.Store.List != nil || n.Store.Query != nil)
	why := sectionUnreadable(n, stored || ref.own(), rel.spec.Rows != nil, ref)
	if why != "" && !rel.flag(rel.spec.Explain) {
		return nil
	}
	s := &relatedSection{rel: rel, ref: ref, id: rel.id + "/" + ref.ID(), n: n, view: n.relatedView, unreadable: why}
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
	s.list, list.SharedFilter, list.WrapSelected = list, rel.sharedFilter, rel.wrapsSelected // one "/" filters the stack (related_filter.go)
	list.SetMatchNoun(pluralWord(shortNounName(ref.Noun)))
	if rel.spec.Archived != "" {
		rel.setArchived(s, rel.listSetting(rel.spec.Archived)) // before its first read
	}
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
	return s
}

// sectionUnreadable says why a referrer's things cannot be read, "" when
// they can: a key prop to name its rows by, and a store that lists (read
// says so for a section that reads otherwise: the thing's own
// references), a Rows hook or a relation kind behind it.
func sectionUnreadable(n *Noun, read, rows bool, ref Referrer) string {
	switch {
	case n.keyProp() == nil:
		return "its things have no key to name them by"
	case !read && !rows && ref.Relation == "":
		return "its store cannot list its things"
	}
	return ""
}

// shortNounName is the last word of a noun's name (lane.lane is lane).
func shortNounName(noun string) string {
	if i := strings.LastIndex(noun, "."); i >= 0 {
		return noun[i+1:]
	}
	return noun
}

// syncWatches watches the nouns the open sections read and stops the
// watches no open section needs: a folded section reads nothing and
// watches nothing until it is opened (its count, when shown, is a one-row
// page read with the key). A split's noun is watched while it has its
// split: its things are read on every key change.
func (rel *Related) syncWatches() {
	want := map[string]bool{}
	for _, s := range rel.sections {
		for _, noun := range s.watchedNouns() {
			want[noun] = true
		}
	}
	for _, sp := range rel.splits {
		if sp.watched(rel.r) {
			want[sp.ref.Noun] = true
		}
	}
	kept := rel.watches[:0]
	for _, w := range rel.watches {
		if want[w.noun] {
			kept = append(kept, w)
			continue
		}
		w.Stop()
		delete(rel.tokens, w.noun)
	}
	clear(rel.watches[len(kept):])
	rel.watches = kept
	for _, noun := range slices.Sorted(maps.Keys(want)) {
		rel.watch(noun)
	}
}

// watchedNouns are the nouns an open section reads from a store, so a
// change there fills it again: its own noun's (a store that lists) and,
// for the thing's own references, the thing's noun's (its prop changes
// what it names). None for a folded section, a group heading, the history
// and relation sections, and one that cannot be read.
func (s *relatedSection) watchedNouns() []string {
	if s.grouping || s.list == nil || s.folded() || s.unreadable != "" || s.ref.History || s.ref.Relation != "" {
		return nil
	}
	var out []string
	if st := s.n.Store; st != nil && (st.List != nil || st.Query != nil) {
		out = append(out, s.ref.Noun)
	}
	if s.ref.own() && s.rel.noun != "" {
		out = append(out, s.rel.noun)
	}
	return out
}

// watch listens to a noun's store: any change fills the sections that read
// it again, once per batch. The watch reads nothing itself (a notify watch,
// as a windowed list's): it marks the noun dirty unless the store's change
// token says nothing changed since it last did, and the sections read
// their own things.
func (rel *Related) watch(noun string) {
	for _, w := range rel.watches {
		if w.noun == noun {
			return
		}
	}
	r := rel.r
	c := r.backgroundCtx()
	token := func() string {
		if n := r.Nouns[noun]; n != nil && n.Store != nil {
			return storeToken(n.Store, c)
		}
		return ""
	}
	rel.tokens[noun] = token()
	w, err := r.watchAs(c, noun, func(WatchEvent) {
		if t := token(); t != "" {
			if t == rel.tokens[noun] {
				return // the store says nothing changed since the last fill
			}
			rel.tokens[noun] = t
		}
		rel.dirty[noun] = true
	}, true)
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

// folded reports whether the section's things are out of sight: it is
// collapsed, or a member of a collapsed group.
func (s *relatedSection) folded() bool {
	if s.rel.filtering() {
		return false // a stack filter reads and shows every section's matches
	}
	if s.group != nil {
		return s.group.collapsed()
	}
	return s.collapsed()
}

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

// readRefPage reads the things of ref owned by the key as a page of q (a
// folded section's count is a one-row page's total).
func (rel *Related) readRefPage(c Ctx, ref Referrer, q ListQuery) (ListPage, error) {
	r := rel.r
	if ref.List {
		return rel.readList(c, ref, q)
	}
	if rel.key == "" {
		return ListPage{}, nil
	}
	if ref.Relation != "" {
		return r.readRelatedRelations(c, rel.noun, rel.key, ref, q)
	}
	if ref.History {
		things, err := factThings(r.thingHistory(rel.noun, rel.key), nil)
		return ListPage{Items: things, Total: len(things)}, err
	}
	return r.readRelatedThings(c, rel.spec, rel.noun, rel.key, ref, q)
}

// The values of RelatedFoldedCountSetting: what a folded section reads.
const (
	// RelatedFoldedCountPage counts a folded section with a one-row page
	// where that costs no full read of a store (a store that answers a
	// query, the relations, the history, the thing's own references); any
	// other folded section shows no count until it is opened. The default.
	RelatedFoldedCountPage = "page"
	// RelatedFoldedCountFull reads a folded section's things to count them,
	// as before v0.143.1 (still without a watch while folded).
	RelatedFoldedCountFull = "full"
	// RelatedFoldedCountNone reads nothing for a folded section: no count
	// until it is opened.
	RelatedFoldedCountNone = "none"
)

// RelatedFoldedCountSetting says what a folded related section reads.
const RelatedFoldedCountSetting = "detail.related-folded-count"

// foldedCount is the folded-count setting's value, the page mode when the
// setting is missing.
func (rel *Related) foldedCount() string {
	if _, ok := rel.r.settingValue(RelatedFoldedCountSetting); !ok {
		return RelatedFoldedCountPage
	}
	return rel.r.Settings().String(RelatedFoldedCountSetting)
}

// countable reports whether a one-row page counts the section without a
// full read of a store (the Nouns page's rule, countNoun): the history and
// the relations are read from memory and indexes, the thing's own
// references by key, a store with Query pages itself, and facts and live
// instances are the registry's memory. A store that only lists (a remote
// one too), and the Rows hook, cannot.
func (s *relatedSection) countable() bool {
	if s.ref.History || s.ref.Relation != "" || s.ref.own() {
		return true
	}
	return countsCheaply(s.n.Store)
}

// countsCheaply reports whether a one-row page counts a store's things
// without reading them all: the store pages itself (Query), or its things
// are the registry's memory (facts, live instances). A store that only
// lists, remote or local, does not. The Nouns page's counts and a folded
// related section's count share it.
func countsCheaply(st *Store) bool {
	return st != nil && (st.Query != nil || st.Facts || st.Kind == Instances)
}

// fill reads the things and shows them: the heading, then the things unless
// the section is collapsed (a placed view draws its own). A value section
// reads its split, a group heading its members. A folded section reads by
// the folded-count setting: a one-row page's total, or nothing. The read
// itself is rel.readSection's: on the loop, or as a job off it (related_read.go).
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
	if s.unreadable != "" {
		s.show(nil)
		return
	}
	q, land := ListQuery{Archived: true}, func(page ListPage) { s.show(page.Items) }
	if s.folded() {
		switch mode := s.rel.foldedCount(); {
		case mode == RelatedFoldedCountFull:
		case mode == RelatedFoldedCountNone || !s.countable():
			s.counted, s.count, s.shown, s.filled, s.loading = false, 0, nil, false, false
			s.render()
			return
		default:
			q = ListQuery{Archived: r.archiveHiding(s.n, s.list.vs) == nil, Page: 1, PerPage: 1}
			land = func(page ListPage) {
				s.counted, s.count, s.shown, s.filled, s.loading = true, page.Total, nil, true, false
				s.render()
			}
		}
	}
	s.rel.readSection(s.ref.ID(), s, s.ref, q, land)
}

// RelatedRemoteSetting turns the reads of remote stores in related
// sections on (default) or off: on, a section whose things live in a
// remote store (a mounted extension's) is read as a job off the loop and
// shows its things when it lands; off, it reads as empty.
const RelatedRemoteSetting = "detail.related-remote"

// remoteRef reports whether reading ref's things needs a remote store:
// its noun's, or, for the thing's own references, the thing's noun's.
func (rel *Related) remoteRef(ref Referrer) bool {
	if ref.own() {
		if own := rel.r.Nouns[rel.noun]; own != nil && own.Store != nil && own.Store.Remote {
			return true
		}
	}
	n := rel.r.Nouns[ref.Noun]
	return n != nil && n.Store != nil && n.Store.Remote
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
	s.count, s.counted, s.shown, s.filled, s.loading = len(shown), true, shown, true, false
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
		s.count, s.matched, s.counted = 0, 0, true
		for _, m := range s.members {
			if rel.hiddenBy(m.ref.ID()) == "" {
				s.count += m.count
				s.matched += m.matched
				s.counted = s.counted && m.counted
			}
		}
		if err := s.list.SetItems([]ListItem{s.heading()}); err != nil {
			r.UI().Error(fmt.Sprintf("%s section: %v", s.ref.Label, err))
		}
		return
	}
	shown := s.shown
	c := r.backgroundCtx()
	rows := s.rowItems(c, shown)
	if rel.filtering() {
		rows = s.matching(rows) // the stack's one filter (related_filter.go)
	}
	s.matched = len(rows)
	items := []ListItem{s.heading()}
	if s.group != nil {
		items = nil
		if !s.group.collapsed() || rel.filtering() {
			items = rows
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
	} else if !s.collapsed() || rel.filtering() {
		switch {
		case rel.filtering():
		case s.unreadable != "":
			items = append(items, s.noteRow(s.unreadable))
		case empty && s.loading:
			items = append(items, s.noteRow(LoadingText(pluralWord(shortNounName(s.ref.Noun)))))
		case empty && rel.emptyMode() == relatedEmptyAtOpen:
			items = append(items, s.noteRow(EmptyText(pluralWord(shortNounName(s.ref.Noun)))))
		}
		items = append(items, rows...)
	}
	if err := s.list.SetItems(items); err != nil {
		r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(s.ref.Noun), err))
	}
}

// heading is the section's first row: a fold marker, the label and the
// count; a focus stop whose verbs are the section's.
func (s *relatedSection) heading() ListItem {
	mark := "▾"
	if !s.rel.filtering() && s.collapsed() || s.shownCount() == 0 && s.unreadable == "" {
		mark = "▸"
	}
	label := mark + " " + s.ref.Label
	switch {
	case !s.rel.countsShown():
	case s.unreadable != "":
		label += " (–)"
	case !s.counted:
		// folded and not counted (RelatedFoldedCountSetting): no count
		// until it is opened
	default:
		label += fmt.Sprintf(" (%d)", s.shownCount())
	}
	item := ListItem{ID: s.headID(), Label: label, Noun: RelatedNoun, Data: s.thing(), Tones: []ToneSpan{{Cell: 0, Tone: ToneHeader}}}
	if n := len(s.list.columns); n > 0 {
		item.Cells = make([]string, n)
		item.Cells[0] = label
	}
	return item
}

// noteRow is the one row an open section draws in place of things: why
// they cannot be read, that its first read runs (the one loading text), or
// that it has none (the open empty mode).
func (s *relatedSection) noteRow(text string) ListItem {
	item := ListItem{ID: s.emptyID(), Label: "  " + text, Noun: RelatedNoun, Data: s.thing()}
	if n := len(s.list.columns); n > 0 {
		item.Cells = make([]string, n)
		item.Cells[0] = item.Label
	}
	return item
}

// thing is the section as the noun's thing: what list and get answer.
func (s *relatedSection) thing() Thing {
	return Thing{"id": s.ref.ID(), "noun": s.ref.Noun, "prop": s.ref.Prop, "label": s.ref.Label, "count": s.count, "counted": s.counted,
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
	defer rel.syncWatches() // an open section is watched, a folded one is not
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
