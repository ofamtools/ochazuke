// Package lexiconcheck fails a test when the registered operations break the
// lexicon's naming rules (lexicon plan, Design 3): one word has one part of
// speech, noun names are singular, a namespace is not a noun or action word,
// and a key has at most three parts.
//
// The rules, each a Violation.Rule:
//
//   - noun-action: a noun's own word is also an action name (list, run,
//     copy). Names are compared whole, so copy-mode is not copy. A word may
//     be both a noun and a prop (task.project); props are not compared.
//   - pair: a noun or namespace and its plural are both present (pane and
//     panes, cmds.cmd).
//   - plural-noun: a noun whose word is a plural with no singular yet
//     (events, themes). A word the word list marks "singular" (status) is not.
//   - namespace-word: a namespace that is also a noun word or an action name,
//     unless Config.MainNouns declares it the program's main noun
//     (lane.lane).
//   - parts: an operation key of more than namespace.noun.name, or with an
//     empty part.
//   - word-part: the word list classes a word as only a noun and it is an
//     action name, or only an action and it is a noun name.
//   - unclassified: a noun word or action name with no row in the word list
//     (only when Config.Words is set).
//
// A baseline file lists today's violations. Check fails on a violation the
// baseline does not hold and on a baseline row that no longer matches, so the
// list only shrinks (the namecheck ratchet). An application or tool adopts it
// with one test over its mounted registry:
//
//	func TestLexicon(t *testing.T) {
//		rt := newRuntimeWithEverythingMounted()
//		lexiconcheck.Check(t, lexiconcheck.Config{
//			Operations: &rt.Operations,
//			Words:      "testdata/lexicon-words.tsv",
//			Baseline:   "testdata/lexiconcheck.txt",
//			MainNouns:  map[string]string{"lane": "lane"},
//		})
//	}
package lexiconcheck

import (
	"fmt"
	"sort"
	"strings"

	"ofam.dev/ochazuke"
)

// The rules a Violation can break.
const (
	RuleNounAction    = "noun-action"
	RulePair          = "pair"
	RulePluralNoun    = "plural-noun"
	RuleNamespaceWord = "namespace-word"
	RuleParts         = "parts"
	RuleWordPart      = "word-part"
	RuleUnclassified  = "unclassified"
)

// Config says what to check.
type Config struct {
	// Operations is the registry to check: Runtime.Operations, with every
	// extension mounted when the whole family catalog is checked.
	Operations *ochazuke.Operations
	// Words is the path of the word list (word, part of speech, reviewed,
	// note; tab separated). Empty skips the two rules that need it.
	Words string
	// Baseline is the path of the accepted violations.
	Baseline string
	// MainNouns maps a namespace to the noun it declares as its main noun;
	// a namespace named like that noun is then allowed (lane: lane).
	MainNouns map[string]string
}

// Violation is one broken rule. Rule and Subject identify it (the baseline
// key); Message says what to change.
type Violation struct {
	Rule    string
	Subject string
	Message string
}

// Key is the baseline key of the violation.
func (v Violation) Key() string { return v.Rule + "\t" + v.Subject }

// Scan returns the violations of the registry, sorted by rule and subject.
// words may be nil. It reads the registry through its lookups, so it never
// writes it.
func Scan(ops *ochazuke.Operations, words Words, mainNouns map[string]string) []Violation {
	var out []Violation
	add := func(rule, subject, msg string) { out = append(out, Violation{rule, subject, msg}) }

	nounWords := map[string][]string{} // noun word -> full noun names
	spaces := map[string]bool{}
	for _, n := range ops.Nouns() {
		ns, _, _ := ochazuke.Key(n.Name + ".x").Parts()
		w := ochazuke.Key(n.Name + ".x").Noun()
		nounWords[w] = append(nounWords[w], n.Name)
		spaces[ns] = true
	}
	actions := map[string]bool{}
	for _, op := range ops.List() {
		actions[op.Name()] = true
		if parts := strings.Split(string(op.Key), "."); len(parts) > 3 || hasEmpty(parts) {
			add(RuleParts, string(op.Key), "a key is namespace.noun.name: three parts, none empty")
		}
	}

	names := map[string]bool{}
	for w := range nounWords {
		names[w] = true
	}
	for ns := range spaces {
		names[ns] = true
	}
	for _, w := range sortedKeys(names) {
		s := Singular(w)
		if s == "" || words.Singular(w) {
			continue
		}
		if names[s] {
			add(RulePair, w+"/"+s, fmt.Sprintf("%s and %s both exist: keep the singular", w, s))
		} else if _, isNoun := nounWords[w]; isNoun {
			add(RulePluralNoun, w, fmt.Sprintf("noun %s is plural: name it %s", w, s))
		}
	}
	for _, w := range sortedKeys(nounWords) {
		if actions[w] {
			add(RuleNounAction, w, fmt.Sprintf("%s is a noun (%s) and an action name: a word has one part of speech", w, strings.Join(nounWords[w], ", ")))
		}
	}
	for _, ns := range sortedKeys(spaces) {
		_, isNoun := nounWords[ns]
		if !(isNoun || actions[ns]) || mainNouns[ns] == ns {
			continue
		}
		add(RuleNamespaceWord, ns, fmt.Sprintf("namespace %s is also a noun or action word: name it for the program, or declare it the main noun", ns))
	}
	if words != nil {
		for _, w := range sortedKeys(nounWords) {
			checkWord(words, w, true, actions[w], add)
		}
		for _, w := range sortedKeys(actions) {
			if _, isNoun := nounWords[w]; !isNoun {
				checkWord(words, w, false, true, add)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// checkWord applies the word-list rules to one word used as a noun
// (asNoun) or only as an action.
func checkWord(words Words, w string, asNoun, asAction bool, add func(rule, subject, msg string)) {
	row, ok := words[w]
	if !ok {
		add(RuleUnclassified, w, fmt.Sprintf("word %s has no row in the word list: classify it", w))
		return
	}
	if asNoun && row.Only("action") {
		add(RuleWordPart, w, fmt.Sprintf("%s is classed an action but is used as a noun", w))
	}
	if asAction && !asNoun && row.Only("noun") {
		add(RuleWordPart, w, fmt.Sprintf("%s is classed a noun but is used as an action name", w))
	}
}

func hasEmpty(parts []string) bool {
	for _, p := range parts {
		if p == "" {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Singular is the singular of a plural-looking word, "" when the word does
// not look plural (it ends in ss, us or is, or in no s).
func Singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ss"), strings.HasSuffix(w, "us"), strings.HasSuffix(w, "is"):
		return ""
	case strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ses"), strings.HasSuffix(w, "xes"), strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s"):
		return w[:len(w)-1]
	}
	return ""
}
