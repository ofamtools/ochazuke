package ochazuke

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// The reader: one way to read the leading words of a line as an operation,
// for the palette, the CLI's verb-first words, the CLI's noun default
// action and completions (lexicon plan §4). Each of the first two words is
// looked up as nouns and as actions, and the pairs are both orders:
//
//	w1.nouns × w2.actions  ∪  w2.nouns × w1.actions
//
// A missing word is an omitted part: it matches every noun or every action
// (so one word, two words and a half-typed word take the same path), and
// the words after the pair are the rest of the line. Punctuation narrows
// the orders: a.b is noun.action (a.b.c namespace.noun.action) and a, b is
// action, noun, with spaces allowed around either mark.
//
// The candidates are then chosen in two steps, each a ranking over keys
// that are data (best):
//
//  1. what the typed words name: how they matched (exact over alias over
//     tail over prefix over omitted), an action over a prop, a whole action
//     word over a whole noun word (an ex command's verb first), then how far
//     the rest of the line binds, counted only for a pair both of whose
//     words were typed (arguments settle which pair two words name, never
//     what one word alone names);
//  2. the part a word left out: the noun's default action, how far the rest
//     binds, then the scope's preference (the focus path's nouns, the
//     scope's main noun).
//
// The answer is the top candidate, or the ties; whether that is one or
// several is the only size question, and it is asked by the caller at the
// very end.

// Via is how a word matched what it names.
type Via string

// The ways a word matches, worst first (ViaOrder).
const (
	ViaOmitted Via = "omitted" // no word: every name matches
	ViaPrefix  Via = "prefix"
	ViaTail    Via = "tail"  // a namespaced noun's last part (doc for json.doc)
	ViaAlias   Via = "alias" // another name, typed whole
	ViaExact   Via = "exact"
)

// ViaOrder ranks the ways a word matches, worst first.
var ViaOrder = []Via{ViaOmitted, ViaPrefix, ViaTail, ViaAlias, ViaExact}

// Rank is the way's place in ViaOrder.
func (v Via) Rank() int { return slices.Index(ViaOrder, v) }

// Order is which word of a pair named the noun.
type Order string

// The two orders of a pair.
const (
	NounFirst   Order = "noun-first"   // noun action (row transform)
	ActionFirst Order = "action-first" // action noun (transform row)
)

// viaOf is how word matches name: "" matches as omitted, the whole name
// (any case) exactly, its start as a prefix; ok is false otherwise.
func viaOf(name, word string) (Via, bool) {
	lower, low := strings.ToLower(name), strings.ToLower(word)
	switch {
	case word == "":
		return ViaOmitted, true
	case lower == low:
		return ViaExact, true
	case strings.HasPrefix(lower, low):
		return ViaPrefix, true
	}
	return "", false
}

// wholeVia is viaOf for a name that matches only typed whole (an alias, a
// tail): via when word is name, else no match.
func wholeVia(name, word string, via Via) (Via, bool) {
	if word != "" && strings.EqualFold(name, word) {
		return via, true
	}
	return "", false
}

// Lexicon is what the reader matches words against. Nouns is every noun a
// word names (all of them, omitted, for ""); Actions is everything a word
// names on a noun: its actions, its action aliases and its props, each a
// Candidate with Action and Operation (and Prop) set.
type Lexicon struct {
	Nouns   func(word string) []Match
	Actions func(n *Noun, word string) []Candidate
}

// Lexicon is the registry's own words: each noun by its registered name
// and its own word, each action by its name.
func (ops *Operations) Lexicon() Lexicon {
	return Lexicon{Nouns: ops.nounWords, Actions: ops.actionWords}
}

func (ops *Operations) nounWords(word string) []Match {
	var out []Match
	for _, n := range ops.Nouns() {
		via, ok := bestVia(word, n.Name, nounWord(n.Name), n.FullName())
		if ok {
			out = append(out, Match{OriginalValue: word, PartOfSpeech: NounPart, Noun: n, Via: via, Exact: via == ViaExact})
		}
	}
	return out
}

func (ops *Operations) actionWords(n *Noun, word string) []Candidate {
	var out []Candidate
	for _, a := range ops.ActionsOnNoun(n) {
		if via, ok := viaOf(a.Name, word); ok {
			out = append(out, Candidate{Operation: ops.OperationFor(n, a), Action: Match{OriginalValue: word, PartOfSpeech: ActionPart, Action: a, Via: via, Exact: via == ViaExact}})
		}
	}
	return out
}

// FullName is the noun's name with its namespace (mtm.pane for pane, in
// DefaultNamespace): the spelling of a three-part key.
func (n *Noun) FullName() string { return n.Namespace() + "." + nounWord(n.Name) }

// bestVia is the best way word matches any of names.
func bestVia(word string, names ...string) (best Via, ok bool) {
	for _, name := range names {
		if via, hit := viaOf(name, word); hit && (!ok || via.Rank() > best.Rank()) {
			best, ok = via, true
		}
	}
	return best, ok
}

// Reading is how one caller reads: the lexicon, the operations in scope,
// how far the rest of the line binds on a candidate and what the scope
// prefers. Every function may be nil: the registry's lexicon, every
// operation, Operation.BindWords, no preference.
type Reading struct {
	Lexicon Lexicon
	// Within keeps the candidates the scope may name (the palette's
	// offered operations, a namespace group's nouns).
	Within func(Candidate) bool
	// Bind is how far the rest of the line binds on a candidate: higher
	// binds further (the palette's parse, the CLI's flag reading).
	Bind func(Candidate) int
	// Prefer are the scope's preference keys for a candidate, compared in
	// order, higher first: the focus path's nouns, the main noun.
	Prefer func(Candidate) []int
	// Punctuation reads a.b and a, b as order hints (lexicon.punctuation).
	Punctuation bool
	// WordsOnly keeps the rest of the line from settling what the typed
	// words name (palette.pair-words off): only how they matched does.
	WordsOnly bool
}

// Lookup reads a line against the registry's own words: the best
// candidates (one, or the ties).
func (ops *Operations) Lookup(line string) []Candidate {
	return Reading{Lexicon: ops.Lexicon(), Punctuation: true}.Read(strings.Fields(line))
}

// SplitLookup reads two words as a pair, both orders (word2 "" leaves a
// part out): the best candidates (one, or the ties).
func (ops *Operations) SplitLookup(word1, word2 string) []Candidate {
	rd := Reading{Lexicon: ops.Lexicon()}
	return rd.Choose(rd.pairs(split{First: word1, Second: word2, Words: 2}, nil))
}

// Read is the best candidates for the leading words (one, or the ties).
func (rd Reading) Read(words []string) []Candidate { return rd.Choose(rd.Candidates(words)) }

// Candidates is every reading of the leading words, unranked.
func (rd Reading) Candidates(words []string) []Candidate {
	if rd.Lexicon.Nouns == nil {
		rd.Lexicon = Lexicon{Nouns: func(string) []Match { return nil }, Actions: func(*Noun, string) []Candidate { return nil }}
	}
	var out []Candidate
	for _, s := range splits(words, rd.Punctuation) {
		out = append(out, rd.pairs(s, words[s.Words:])...)
	}
	return out
}

// split is one way to take the leading words as a pair: First and Second
// in Order ("" both orders), using Words words of the line.
type split struct {
	First, Second string
	Order         Order
	Words         int
}

// splits are the pairs the leading words may be: the first two words,
// the first word alone, and with punctuation the first word read as
// noun.action or a joined action, noun.
func splits(words []string, punctuation bool) []split {
	var out []split
	if punctuation {
		out = append(out, punctuated(words)...)
	}
	first, second := wordAt(words, 0), wordAt(words, 1)
	if flagWord(second) {
		second = ""
	}
	if second != "" {
		out = append(out, split{First: first, Second: second, Words: 2})
	}
	return append(out, split{First: first, Words: min(1, len(words))})
}

func wordAt(words []string, i int) string {
	if i < len(words) {
		return words[i]
	}
	return ""
}

// flagWord is a word starting with -: a flag, never a noun or an action.
func flagWord(word string) bool { return strings.HasPrefix(word, "-") }

// punctuated reads the head of the line for the marks: words joined
// around a lone or a leading or trailing "," or "." (a , b and a. b are
// a,b and a.b), then a,b as action, noun and a.b.c as the noun a.b and the
// action c. A head with no mark gives none.
func punctuated(words []string) []split {
	head, n := joinedHead(words)
	if before, after, ok := strings.Cut(head, ","); ok {
		return []split{{First: before, Second: after, Order: ActionFirst, Words: n}}
	}
	if i := strings.LastIndex(head, "."); i > 0 && i < len(head)-1 {
		return []split{{First: head[:i], Second: head[i+1:], Order: NounFirst, Words: n}}
	}
	return nil
}

// joinedHead is the first word with the words a mark joins to it, and how
// many words it took.
func joinedHead(words []string) (string, int) {
	head, n := wordAt(words, 0), min(1, len(words))
	for n < len(words) && (endsMark(head) || startsMark(words[n])) {
		head += words[n]
		n++
	}
	return head, n
}

func endsMark(word string) bool { return strings.HasSuffix(word, ",") || strings.HasSuffix(word, ".") }
func startsMark(word string) bool {
	return strings.HasPrefix(word, ",") || strings.HasPrefix(word, ".")
}

// pairs reads one split in its orders: First as the noun and Second the
// action (noun first), and the other way round.
func (rd Reading) pairs(s split, rest []string) []Candidate {
	var out []Candidate
	if s.Order != ActionFirst {
		out = append(out, rd.pairsOf(s.First, s.Second, NounFirst, s.Words, rest)...)
	}
	if s.Order != NounFirst {
		out = append(out, rd.pairsOf(s.Second, s.First, ActionFirst, s.Words, rest)...)
	}
	return out
}

func (rd Reading) pairsOf(nounWord, actionWord string, order Order, words int, rest []string) []Candidate {
	var out []Candidate
	for _, nm := range rd.Lexicon.Nouns(nounWord) {
		for _, c := range rd.Lexicon.Actions(nm.Noun, actionWord) {
			c.Noun, c.Order, c.Words, c.Rest = nm, order, words, rest
			if c.Operation != nil && c.propFollowsNoun() && (rd.Within == nil || rd.Within(c)) {
				out = append(out, c)
			}
		}
	}
	return out
}

// propFollowsNoun holds for every candidate but a prop reading whose noun
// was not typed before it: a prop is read only as NOUN PROP.
func (c Candidate) propFollowsNoun() bool {
	return c.Prop == "" || c.Order == NounFirst && c.Noun.Via != ViaOmitted
}

// Typed reports whether both parts of the candidate were typed.
func (c Candidate) Typed() bool { return c.Noun.Via != ViaOmitted && c.Action.Via != ViaOmitted }

// ActionWord is the word the candidate's action part reads as: the prop,
// the alias's word or the action's name.
func (c Candidate) ActionWord() string {
	switch {
	case c.Prop != "":
		return c.Prop
	case c.Action.Action != nil:
		return c.Action.Action.Name
	}
	return c.Operation.Name()
}

// Label is the noun action pair the candidate stands for, the noun spelled
// by display.
func (c Candidate) Label(display func(string) string) string {
	return display(c.Noun.Noun.Name) + " " + c.ActionWord()
}

// TypedReading is what the typed words of the candidate name, in line
// order, one key per word: the same for every candidate that reads them
// the same way.
func (c Candidate) TypedReading() []string {
	var out []string
	for _, m := range c.typedParts() {
		out = append(out, c.partKey(m))
	}
	return out
}

// typedParts are the candidate's typed matches in line order.
func (c Candidate) typedParts() []Match {
	parts := []Match{c.Noun, c.Action}
	if c.Order == ActionFirst {
		slices.Reverse(parts)
	}
	return slices.DeleteFunc(parts, func(m Match) bool { return m.Via == ViaOmitted })
}

// partKey is one part's key: a noun's registered name, else the action
// word, apart by a leading byte.
func (c Candidate) partKey(m Match) string {
	if m.Noun != nil {
		return "\x01" + m.Noun.Name
	}
	return "\x02" + c.ActionWord()
}

// partName is one part's name: the noun spelled by display, else the
// action word.
func (c Candidate) partName(m Match, display func(string) string) string {
	if m.Noun != nil {
		return display(m.Noun.Name)
	}
	return c.ActionWord()
}

// Choose ranks the candidates in the reader's two steps and returns the
// best (one, or the ties), with Rank filled: what the typed words name,
// then, for each reading of them that ties, the part they left out.
func (rd Reading) Choose(cs []Candidate) []Candidate {
	var out []Candidate
	for _, group := range byReading(best(cs, rd.typedRank)) {
		out = append(out, best(group, rd.omittedRank)...)
	}
	return uniqueCandidates(out)
}

// byReading groups candidates by what their typed words name, in first
// appearance order.
func byReading(cs []Candidate) [][]Candidate {
	var keys []string
	groups := map[string][]Candidate{}
	for _, c := range cs {
		key := strings.Join(c.TypedReading(), "|")
		if _, seen := groups[key]; !seen {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], c)
	}
	out := make([][]Candidate, len(keys))
	for i, key := range keys {
		out[i] = groups[key]
	}
	return out
}

// typedRank is the first step's keys: how the typed words matched, an
// action over a prop, a whole action word over a whole noun word, and how
// far the rest binds on a pair both of whose words were typed.
func (rd Reading) typedRank(c Candidate) []int {
	keys := []int{c.Noun.Via.Rank() + c.Action.Via.Rank(), boolKey(c.Prop == ""), boolKey(c.Action.Via == ViaExact)}
	if c.Typed() && !rd.WordsOnly {
		return append(keys, rd.bind(c))
	}
	return append(keys, 0)
}

// omittedRank is the second step's keys: the noun's default action, how
// far the rest binds, the scope's preference.
func (rd Reading) omittedRank(c Candidate) []int {
	keys := []int{boolKey(c.Action.Via == ViaOmitted && c.Noun.Noun.DefaultAction != "" && c.Noun.Noun.DefaultAction == c.ActionWord()), rd.bind(c)}
	if rd.Prefer != nil {
		keys = append(keys, rd.Prefer(c)...)
	}
	return keys
}

func (rd Reading) bind(c Candidate) int {
	if rd.Bind != nil {
		return rd.Bind(c)
	}
	return c.Operation.BindWords(c.Rest)
}

func boolKey(b bool) int {
	if b {
		return 1
	}
	return 0
}

// best keeps the candidates whose score is highest, comparing the keys in
// order, each candidate's Rank set to its score.
func best(cs []Candidate, score func(Candidate) []int) []Candidate {
	var top []int
	var out []Candidate
	for _, c := range cs {
		c.Rank = score(c)
		switch d := slices.Compare(c.Rank, top); {
		case d > 0 || out == nil:
			top, out = c.Rank, []Candidate{c}
		case d == 0:
			out = append(out, c)
		}
	}
	return out
}

// uniqueCandidates keeps the first candidate of each operation and prop
// read the same way.
func uniqueCandidates(cs []Candidate) []Candidate {
	var out []Candidate
	for _, c := range cs {
		if !slices.ContainsFunc(out, func(o Candidate) bool {
			return o.Operation == c.Operation && o.Prop == c.Prop && slices.Equal(o.TypedReading(), c.TypedReading())
		}) {
			out = append(out, c)
		}
	}
	return out
}

// Ambiguity is the error for tied candidates: the words whose readings
// differ, quoted, and the readings, as noun action pairs when both words
// differ.
func Ambiguity(cs []Candidate, display func(string) string) error {
	words, labels := ambiguousWords(cs, display)
	return fmt.Errorf("%q is ambiguous: %s", strings.Join(words, " "), strings.Join(labels, ", "))
}

// ambiguousWords are the typed words the tied candidates read differently
// and each candidate's reading of them (the noun's before the action's),
// readings starting with an action first, each set in name order, once.
func ambiguousWords(cs []Candidate, display func(string) string) (words, labels []string) {
	differ, omitted := differingWords(cs)
	type reading struct {
		action bool
		label  string
	}
	var rs []reading
	for _, c := range cs {
		parts := nounsFirst(c.typedParts(), differ)
		r := reading{action: len(parts) > 0 && parts[0].Noun == nil}
		var names []string
		for _, m := range parts {
			names = append(names, c.partName(m, display))
		}
		r.label = strings.Join(names, " ")
		if omitted {
			r = reading{label: c.Label(display)}
		}
		rs = append(rs, r)
	}
	slices.SortStableFunc(rs, func(a, b reading) int {
		return cmp.Or(cmp.Compare(boolKey(b.action), boolKey(a.action)), cmp.Compare(a.label, b.label))
	})
	for _, r := range rs {
		if !slices.Contains(labels, r.label) {
			labels = append(labels, r.label)
		}
	}
	typed := typedWords(cs[0])
	for _, i := range differ {
		words = append(words, typed[i])
	}
	return words, labels
}

// nounsFirst are the parts at the indexes, the noun's before the action's.
func nounsFirst(parts []Match, at []int) []Match {
	var out []Match
	for _, i := range at {
		if i < len(parts) {
			out = append(out, parts[i])
		}
	}
	slices.SortStableFunc(out, func(a, b Match) int { return cmp.Compare(boolKey(a.Noun == nil), boolKey(b.Noun == nil)) })
	return out
}

// differingWords are the indexes of the typed words from the first the
// candidates read differently to the last: a pair whose first word differs
// is named whole, a second word that differs alone. When none differs the
// tie is over an omitted part (omitted): every typed word.
func differingWords(cs []Candidate) (at []int, omitted bool) {
	first := cs[0].TypedReading()
	from := slices.IndexFunc(first, func(part string) bool {
		i := slices.Index(first, part)
		return slices.ContainsFunc(cs, func(c Candidate) bool { r := c.TypedReading(); return i >= len(r) || r[i] != part })
	})
	for i := max(from, 0); i < len(first); i++ {
		at = append(at, i)
	}
	return at, from < 0
}

// typedWords are the words the candidate's typed parts were read from, in
// line order.
func typedWords(c Candidate) []string {
	var out []string
	for _, m := range c.typedParts() {
		out = append(out, m.OriginalValue)
	}
	return out
}

// BindWords is how far words bind as the operation's arguments: one for
// each word read before the first that does not (a --NAME naming a
// parameter, with its value; a bare word filling the next positional
// parameter), and one more when all of them bind.
func (o *Operation) BindWords(words []string) int {
	open := slices.DeleteFunc(o.ParameterOrder(), func(n string) bool { return o.Parameters[n].Named })
	for i := 0; i < len(words); i++ {
		name, ok := strings.CutPrefix(words[i], "--")
		switch {
		case ok:
			name, _, inline := strings.Cut(name, "=")
			p, known := o.Parameters[name]
			if !known {
				return i
			}
			if !inline && p.Type != "boolean" {
				i++
			}
			open = slices.DeleteFunc(open, func(n string) bool { return n == name })
		case len(open) > 0:
			open = open[1:]
		default:
			return i
		}
	}
	return len(words) + 1
}

// Complete is what the last of words may be (completions): the name it
// reads as (an action, a prop, a noun spelled by display; an alias with its
// action's name too) in every reading
// of the line that takes it as a part, or, when it is "", the names of the
// part the words before it leave out. Sorted, each once. The palette and
// the CLI's complete both list these, so they narrow alike.
func (rd Reading) Complete(words []string, display func(string) string) []string {
	if len(words) == 0 {
		words = []string{""}
	}
	before, last := words[:len(words)-1], words[len(words)-1]
	read := words
	if last == "" {
		read = before
	}
	var out []string
	for _, c := range rd.Candidates(read) {
		if c.Words == len(read) {
			out = append(out, partNames(c, last, display)...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// partNames are the names of the candidate's parts the word stands for:
// the typed part read from word, or every omitted part when word is "".
func partNames(c Candidate, word string, display func(string) string) []string {
	var out []string
	if (c.Noun.Via == ViaOmitted) == (word == "") && (word == "" || c.Noun.OriginalValue == word) {
		out = append(out, display(c.Noun.Noun.Name))
	}
	if (c.Action.Via == ViaOmitted) == (word == "") && (word == "" || c.Action.OriginalValue == word) {
		out = append(out, c.ActionWord())
		if c.Action.Alias != "" {
			out = append(out, c.Operation.Name()) // an alias completes with its action's name
		}
	}
	return out
}
