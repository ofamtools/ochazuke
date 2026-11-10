package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PaletteInstance is a labeled object whose ID becomes the default action argument.
type PaletteInstance struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Word is the word the palette offers and takes for it ("" the id):
	// a page's title (Choice.Word).
	Word string `json:"word,omitempty"`
}

// PaletteSource reads current instances in the context captured when the palette opened.
type PaletteSource func(*Runtime, Context) []PaletteInstance

// PaletteNoun explicitly chooses the action and string argument used for an instance.
type PaletteNoun struct {
	Source      PaletteSource
	DefaultVerb string
	Argument    string
}

// PaletteEntry is one completion: Value is the word it appends, Kind is
// verb, noun or choice, Label what the list shows.
type PaletteEntry struct {
	Value string `json:"value"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	ID    string `json:"id,omitempty"`
	Verb  string `json:"verb,omitempty"`
	// Aliases are the other verb words that name this entry's action
	// (ActionAlias): typing one narrows to this entry, which is listed
	// once under its canonical name.
	Aliases []string `json:"aliases,omitempty"`
	// Splice marks a rest parameter's completion (Parameter.Rest): taking
	// it puts Value in place of the line from From (a rune offset) to the
	// cursor, adding no space, instead of replacing the word being typed.
	Splice bool `json:"splice,omitempty"`
	From   int  `json:"from,omitempty"`
	// Reason is why the entry's action cannot run whatever is chosen
	// (unavailableReason: disabled, a setting off, a store that cannot do
	// it); the entry is still listed, with the reason beside it.
	Reason string `json:"reason,omitempty"`
	// Verbs are a noun entry's verbs and Nouns a verb entry's nouns, as
	// the first word lists them: the board counts them on the cell and
	// names them in its help line, and tells the two kinds apart by them.
	Verbs []string `json:"-"`
	Nouns []string `json:"-"`
	// Match holds the drawn label's matching rune offsets. Presentation
	// only: CLI completion and state JSON keep their existing shape.
	Match []int `json:"-"`
	// Tones colour runs of the drawn Label (a choice's dim id,
	// Choice.Entry). Presentation only.
	Tones []ToneSpan `json:"-"`
}

// RegisterNoun adds or replaces a live instance source without changing operation forms.
func (p *Palette) RegisterNoun(noun string, config PaletteNoun) error {
	noun = p.r.CanonicalNoun(noun)
	known := false
	for _, op := range p.r.Catalog() {
		if op.Noun() == noun {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("palette: unknown noun %q", noun)
	}
	if config.Source == nil {
		return fmt.Errorf("palette noun %q: source is required", noun)
	}
	op, ok := p.r.Operations.Find(Key(Request{Verb: config.DefaultVerb, Noun: noun}.Key()))
	if !ok {
		return fmt.Errorf("palette noun %q: unknown default action %q", noun, noun+"."+config.DefaultVerb)
	}
	param, ok := op.Parameters[config.Argument]
	if !ok || param.Type != "string" {
		return fmt.Errorf("palette noun %q: default action %s requires a string instance argument; got %q", noun, op.Key.Short(), config.Argument)
	}
	if guard := p.r.VerbGuard(op.Key.Short()); guard != "" {
		return fmt.Errorf("palette noun %q: default action %s is guarded by %q; a default verb is never guarded", noun, op.Key.Short(), guard)
	}
	for name, param := range op.Parameters {
		if name != config.Argument && param.Required {
			return fmt.Errorf("palette noun %q: default action %s also requires argument %q", noun, op.Key.Short(), name)
		}
	}
	p.nouns[noun] = config
	return nil
}

// PagesPaletteSource reads the runtime registry on every listing, not at startup.
func PagesPaletteSource(r *Runtime, _ Context) []PaletteInstance {
	var instances []PaletteInstance
	for _, page := range r.PageList() {
		instances = append(instances, PaletteInstance{ID: page.ID, Label: page.Title, Word: page.Title})
	}
	return instances
}

// PaletteActivePageSetting prefers the active page's actions within the
// palette's existing completion groups.
const PaletteActivePageSetting = "palette.active-page"

// completions is the list under the line: the words the next position
// accepts, grouped verbs, nouns, then parameter choices, narrowed by the
// word being typed (a prefix, as abbreviation resolves) and by the filter
// code, if one was typed for this position.
func (p *Palette) completions() []PaletteEntry {
	if entries, ok := p.restCompletions(); ok {
		if p.filter != "" && p.filter != "p" {
			return nil
		}
		return entries
	}
	tokens, trailing := tokenize(p.Text())
	partial := ""
	partialStart := len([]rune(p.Text()))
	if !trailing {
		partial = tokens[len(tokens)-1].Text
		partialStart = tokens[len(tokens)-1].Start
		tokens = tokens[:len(tokens)-1]
	}
	ix := p.index()
	tokens = ix.nounRuns(p.r, tokens)
	view := p.r.StandardVerbsView()
	nounPosition := len(tokens) == 0
	var out []PaletteEntry
	verbs := func(names []string, noun string) {
		for _, verb := range ix.orderVerbs(names, noun, view) {
			label := verb
			var aliases []string
			if noun != "" {
				op := ix.ops[noun+"."+verb]
				label = verbLabel(op)
				aliases = ix.aliasWords(p.r, op, noun)
			} else {
				for _, n := range ix.verbs[verb] {
					aliases = append(aliases, ix.aliasWords(p.r, ix.ops[n+"."+verb], "")...)
				}
				slices.Sort(aliases)
				aliases = slices.Compact(aliases)
			}
			if view.Tag && ix.standardOnly(verb, noun) {
				label += " · std"
			}
			if len(aliases) > 0 {
				label += " · also " + strings.Join(aliases, ", ")
			}
			reason := ""
			if noun != "" {
				reason = p.r.unavailableReason(ix.ops[noun+"."+verb])
			}
			out = append(out, PaletteEntry{Value: verb, Kind: "verb", Label: label, Verb: verb, Aliases: aliases, Reason: reason, Nouns: ix.verbs[verb]})
		}
	}
	props := func(noun string) {
		for _, prop := range p.propWords(ix, noun) {
			out = append(out, PaletteEntry{Value: prop.Name, Kind: "prop", Label: propLabel(prop)})
		}
	}
	nouns := func(names []string) {
		for _, noun := range names {
			out = append(out, PaletteEntry{Value: noun, Kind: "noun", Label: noun, Verbs: ix.nouns[noun]})
		}
	}
	p.loadingNote = ""
	choices := func(op Operation, name string, args Arguments) {
		list := p.slotChoices(op, name, partial, args)
		pairs := op.Parameters[name].Pairs
		for _, choice := range list {
			entry := p.choiceEntry(choice)
			if pairs && strings.HasSuffix(choice.ID, "=") {
				// NAME= of a pairs parameter: taking it leaves the cursor
				// after the =, for the value.
				entry.Splice, entry.From = true, partialStart
			}
			out = append(out, entry)
		}
	}
	boardWord := p.styleBoard() && len(tokens) < 2 // the board narrows by prefix itself (boardFilter)
	if len(tokens) == 0 {
		reader := partial
		if boardWord {
			reader = ""
		}
		own, named := p.firstWords(ix, reader)
		verbs(own, "")
		nouns(named)
		if partial != "" {
			out = append(out, ix.spaceEntries("")...)
		}
	} else {
		c := p.parseTokens(ix, tokens)
		if entries, ok := ix.namespaceAt(p.r, tokens); ok {
			// After a namespace's words (json ), its nouns' next words.
			out = append(out, entries...)
		}
		switch {
		case boardWord && len(tokens) == 1 && p.boardWords(ix, tokens[0].Text, verbs, props, nouns):
			// The board committed this word as a noun or as a verb (tag is
			// both): the next word is the other kind, whatever the reader
			// would read the word as.
		case c.Err != nil:
			pairFits(ix, c, len(tokens), nouns, verbs)
		case len(tokens) == 1 && c.Words[0].Kind == "verb":
			nounPosition = true
			names := ix.verbs[c.Words[0].Value]
			if a, bare := p.r.bareAlias(tokens[0].Text); bare && a.ShadowedVerb() != "" {
				// set NOUN reads only one-value nouns (owner decision 17).
				names = p.r.shadowWords(names, a.ShadowedVerb())
				here := p.r.lineIndex(p.offeredHere, p.nounBefore)
				names = slices.DeleteFunc(slices.Clone(names), func(n string) bool { return !slices.Contains(here.verbs[a.ShadowedVerb()], n) })
			}
			nouns(names)
		case len(tokens) == 1 && c.Words[0].Kind == "noun":
			noun := c.Words[0].Value
			verbs(ix.nouns[noun], noun)
			props(noun)
		}
		out = dedupEntries(out)
		if slot, ok := p.slot(ix, c, tokens); ok {
			if param := slot.Op.Parameters[slot.Name]; param.Focus && len(slot.Next) > 0 && len(slot.Op.Parameters[slot.Next[0]].Enum) > 0 {
				// The optional key word: the next parameter's words (a
				// prop name) first, then the things.
				choices(slot.Op, slot.Next[0], slot.Args)
			}
			choices(slot.Op, slot.Name, slot.Args)
		}
	}
	return p.narrow(out, ix, view, partial, boardWord, nounPosition)
}

// narrow keeps the entries the word being typed (partial) and the filter
// code leave: the board's cells by prefix (boardCells), else the entries
// with the word, an alias or the id as a prefix, the active page's first
// at a noun position.
func (p *Palette) narrow(out []PaletteEntry, ix lineIndex, view StandardVerbsView, partial string, boardWord, nounPosition bool) []PaletteEntry {
	kind := map[string]string{"v": "verb", "n": "noun", "p": "choice"}[p.filter]
	if boardWord {
		return p.boardCells(out, kind, partial)
	}
	filtered := out[:0]
	for _, entry := range out {
		if (kind == "" || entry.Kind == kind) && (hasPrefixFold(entry.Value, partial) || partial != "" && (slices.ContainsFunc(entry.Aliases, func(a string) bool { return hasPrefixFold(a, partial) }) || entry.ID != "" && entry.ID != entry.Value && hasPrefixFold(entry.ID, partial))) {
			filtered = append(filtered, entry)
		}
	}
	if nounPosition {
		p.preferActivePage(filtered, ix, partial, view)
	}
	return filtered
}

// firstWords are the verbs (an alias's word aside) and the nouns the
// first word may be, in the palette's order: those the reader reads the
// typed prefix as (Reading.Complete, as the CLI's complete lists them);
// every one before a word is typed.
func (p *Palette) firstWords(ix lineIndex, partial string) (verbs, nouns []string) {
	reads := func(string) bool { return true }
	if partial != "" {
		names := p.r.CompleteWords(p.reading(ix, nil), []string{partial})
		reads = func(name string) bool { _, ok := slices.BinarySearch(names, name); return ok }
	}
	for _, o := range ix.byVerbs {
		if !o.alias && reads(o.Value) {
			verbs = append(verbs, o.Value)
		}
	}
	return verbs, slices.DeleteFunc(ix.nounWords(), func(n string) bool { return !reads(n) })
}

// preferActivePage changes display order only. Exact words keep their old
// order, as do focus-path nouns, standard-verb groups and parameter choices.
func (p *Palette) preferActivePage(entries []PaletteEntry, ix lineIndex, partial string, view StandardVerbsView) {
	page := p.r.PageOf(p.r.Page)
	if page == nil || len(page.ActionNouns) == 0 || !p.r.Settings().Bool(PaletteActivePageSetting) {
		return
	}
	if partial != "" {
		exact := func(word string) bool { return strings.EqualFold(word, partial) }
		for _, entry := range entries {
			if exact(entry.Value) || entry.ID != "" && exact(entry.ID) || slices.ContainsFunc(entry.Aliases, exact) {
				return
			}
		}
		word := strings.ToLower(partial)
		if ix.names[word] != "" || len(ix.tails[word]) > 0 {
			return
		}
	}
	path := p.r.Ancestry(focusOf(p.origin))
	bucket := func(entry PaletteEntry) int {
		switch entry.Kind {
		case "verb":
			if view.Order == StandardVerbsLast && ix.standardOnly(entry.Value, "") {
				return 1
			}
			return 0
		case "noun":
			if _, noun := ix.nouns[entry.Value]; !noun {
				return -1 // namespace words are not canonical noun entries
			}
			for _, id := range path {
				if inst := p.r.Instances[id]; inst != nil && p.r.DisplayNoun(inst.Noun) == entry.Value {
					return -1
				}
			}
			return 2
		default:
			return -1
		}
	}
	preferred := func(entry PaletteEntry) bool {
		if entry.Kind == "noun" {
			return slices.Contains(page.ActionNouns, p.r.CanonicalNoun(entry.Value))
		}
		for _, noun := range ix.verbs[entry.Value] {
			if slices.Contains(page.ActionNouns, ix.ops[noun+"."+entry.Value].Noun()) {
				return true
			}
		}
		return false
	}
	for start := 0; start < len(entries); {
		group := bucket(entries[start])
		end := start + 1
		if group >= 0 {
			for end < len(entries) && bucket(entries[end]) == group {
				end++
			}
			slices.SortStableFunc(entries[start:end], func(a, b PaletteEntry) int {
				ap, bp := preferred(a), preferred(b)
				if ap == bp {
					return 0
				}
				if ap {
					return -1
				}
				return 1
			})
		}
		start = end
	}
}

// nounWords are the nouns' own words (byNouns without the other names
// and tails), in list order.
func (ix lineIndex) nounWords() []string {
	var out []string
	for _, o := range ix.byNouns {
		if o.to == "" {
			out = append(out, o.Value)
		}
	}
	return out
}

// spaceEntries are the completions under namespace prefix ("" the top
// level: the namespaces themselves, json · namespace): a word that
// completes a noun shows the noun's word, a deeper namespace says so.
func (ix lineIndex) spaceEntries(prefix string) []PaletteEntry {
	var out []PaletteEntry
	if prefix == "" {
		for _, name := range slices.Sorted(maps.Keys(ix.spaces)) {
			if !strings.Contains(name, ".") && !ix.isNounName(name) {
				out = append(out, PaletteEntry{Value: name, Kind: "noun", Label: name + " · namespace"})
			}
		}
		return out
	}
	next := ix.spaces[prefix]
	for _, word := range slices.Sorted(maps.Keys(next)) {
		label := word + " · namespace"
		if next[word] != "" {
			label = word + " · " + prefix + "." + word
		}
		out = append(out, PaletteEntry{Value: word, Kind: "noun", Label: label})
	}
	return out
}

// namespaceAt is the completion after namespace words at a noun position
// (the line's words so far, or those after a first verb, joined with dots,
// are a namespace): that namespace's next words. palette.namespace-words.
func (ix lineIndex) namespaceAt(r *Runtime, tokens []lineToken) ([]PaletteEntry, bool) {
	if !r.Settings().Bool(PaletteNamespaceWordsSetting) {
		return nil, false
	}
	for _, at := range []int{0, 1} {
		if at >= len(tokens) || at == 1 && ix.isNounName(tokens[0].Text) {
			continue
		}
		words := make([]string, 0, len(tokens)-at)
		for _, t := range tokens[at:] {
			words = append(words, strings.ToLower(t.Text))
		}
		if prefix := strings.Join(words, "."); ix.spaces[prefix] != nil {
			return ix.spaceEntries(prefix), true
		}
	}
	return nil, false
}

// aliasWords are the words that name op as a verb in the palette besides
// its own: its aliases' verbs on noun word (any noun when word is ""), and
// another noun's alias as word.verb.
func (ix lineIndex) aliasWords(r *Runtime, op Operation, word string) []string {
	if op.Name() == "" {
		return nil
	}
	var out []string
	for _, a := range r.AliasesOf(op.Key.Short()) {
		if _, offered := ix.aliasOps[r.DisplayNoun(a.Noun)+"."+a.Verb]; !offered {
			continue
		}
		if word == "" || r.DisplayNoun(a.Noun) == word {
			out = append(out, a.Verb)
		} else {
			out = append(out, r.DisplayKey(a.Alias))
		}
	}
	return out
}

// offered reports whether the palette lists an operation: a verb whose
// guard (Verb(...).When) is false at the focus and mode the palette opened
// over is left out; a palette noun's default verb always stays.
func (p *Palette) offered(op Operation) bool {
	if p.offeredHere(op) {
		return true
	}
	// palette.reach: a guard that is false here is no reason to leave the
	// verb out; choosing it asks for what it lacks and runs it at a thing
	// of its noun (reach).
	return p.r.Settings().Bool(PaletteReachSetting)
}

// offeredHere is offered under the focus and mode alone (palette.reach
// off): a palette noun's default verb, or a verb whose guard holds. A bare
// alias that shadows a verb (set) reads the nouns that have the verb here,
// so reaching a guarded noun.set never changes how set NAME reads.
func (p *Palette) offeredHere(op Operation) bool {
	if config, ok := p.nouns[op.Noun()]; ok && config.DefaultVerb == op.Name() {
		return true
	}
	return p.r.VerbShown(op.Key.Short(), focusOf(p.origin), p.originMode)
}

// verbLabel names a verb in the palette with its kind when it is an
// application verb and marks a verb that asks for confirmation.
func verbLabel(op Operation) string {
	label := op.Name()
	if op.Kind == KindApplication {
		label += " · application"
	}
	if op.Confirm != "" {
		label += " · confirm"
	}
	return label
}

// restAt finds the rest parameter (Parameter.Rest) the palette's cursor is
// in: the line's operation, the parameter, where its text starts (a rune
// offset into the line) and the cursor in that text. A line whose rest
// has not started yet (set and a space) is in it at the end of the line.
func (p *Palette) restAt(ix lineIndex) (op Operation, name string, start, cursor int, ok bool) {
	text := p.Text()
	n := len([]rune(text))
	at := n
	if p.line != nil {
		at = max(0, min(p.line.Cursor, n))
	}
	tokens, trailing := tokenize(text)
	if len(tokens) == 0 {
		return Operation{}, "", 0, 0, false
	}
	tokens = ix.nounRuns(p.r, tokens)
	c := p.parseTokens(ix, tokens)
	if c.Err == nil && c.Rest != nil && at >= c.Rest.Start {
		return c.Op, c.Rest.Param, c.Rest.Start, at - c.Rest.Start, true
	}
	if !trailing || c.Rest != nil {
		return Operation{}, "", 0, 0, false
	}
	if slot, ok := p.slot(ix, c, tokens); ok && slot.Op.Parameters[slot.Name].Rest {
		return slot.Op, slot.Name, n, at - n, at == n
	}
	return Operation{}, "", 0, 0, false
}

// restCompletions are the rest parameter's choices at the cursor, each
// spliced over the rest's text up to the cursor; false when the cursor is
// in no rest parameter. Cached per text and cursor until the line changes.
func (p *Palette) restCompletions() ([]PaletteEntry, bool) {
	ix := p.index()
	op, name, start, cursor, ok := p.restAt(ix)
	if !ok {
		return nil, false
	}
	rest := string([]rune(p.Text())[start:])
	key := fmt.Sprintf("%s/%s\x00rest\x00%d\x00%s", op.Key.Short(), name, cursor, p.Text())
	cached, hit := p.choiceCache[key]
	if !hit {
		cached.choices, cached.err = p.r.restChoices(p.origin.Clone(), op, rest, cursor, p.parse(p.Text()).Args)
		if p.choiceCache == nil {
			p.choiceCache = map[string]choiceList{}
		}
		p.choiceCache[key] = cached
	}
	out := make([]PaletteEntry, 0, len(cached.choices))
	for _, choice := range cached.choices {
		label := choice.Label
		if label == "" {
			label = choice.ID
		}
		out = append(out, PaletteEntry{Value: choice.ID, Kind: "choice", Label: label, ID: choice.ID, Splice: true, From: start})
	}
	return out, true
}

// orderVerbs is a verb column's verbs as palette.standard-verbs orders
// them: last puts the noun's named verbs before its standard ones, hidden
// leaves the standard ones out, mixed keeps one alphabetical list.
func (ix lineIndex) orderVerbs(names []string, noun string, view StandardVerbsView) []string {
	if view.Order == StandardVerbsMixed {
		return names
	}
	var named, standard []string
	for _, verb := range names {
		if ix.standardOnly(verb, noun) {
			standard = append(standard, verb)
		} else {
			named = append(named, verb)
		}
	}
	if view.Order == StandardVerbsHidden {
		return named
	}
	return append(named, standard...)
}

// propWords are the props the palette offers after a noun (NOUN PROP
// [VALUE]): every prop shown where the palette opened, when the noun reads
// or writes its things here.
func (p *Palette) propWords(ix lineIndex, noun string) []PropSpec {
	get, _ := ix.op(noun, "get")
	set, _ := ix.op(noun, "set")
	n := p.r.Noun(noun)
	if n == nil || get.Name() == "" && set.Name() == "" {
		return nil
	}
	var out []PropSpec
	for _, prop := range n.Props {
		if p.r.propShown(prop, focusOf(p.origin), p.originMode, nil) || p.r.Settings().Bool(PaletteReachSetting) {
			out = append(out, prop)
		}
	}
	return out
}

// propLabel names a prop in the completion list: the name, prop, and
// read-only when nothing writes it.
func propLabel(prop PropSpec) string {
	label := prop.Name + " · prop"
	if !prop.writable() {
		label += " · read-only"
	}
	return label
}

// pairFits adds the words that complete an ambiguous first word (the only
// word typed so far): the nouns of each verb it could be and the verbs of
// each noun, as the pair reader keeps the readings the second word fits.
func pairFits(ix lineIndex, c commandLine, typed int, nouns func([]string), verbs func([]string, string)) {
	if typed != 1 {
		return
	}
	for _, cand := range c.Words[0].Candidates {
		nouns(ix.verbs[cand])
		verbs(ix.nouns[cand], cand)
	}
}

// dedupEntries drops later entries repeating an earlier one's kind, value,
// id and label.
func dedupEntries(entries []PaletteEntry) []PaletteEntry {
	seen := map[[4]string]bool{}
	out := entries[:0]
	for _, e := range entries {
		key := [4]string{e.Kind, e.Value, e.ID, e.Label}
		if !seen[key] {
			seen[key] = true
			out = append(out, e)
		}
	}
	return out
}
