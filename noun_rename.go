package ochazuke

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

// NounRename renames a noun for one release (Keep actions). From, the old
// canonical name (ochazuke.chooser), and its framework short name
// (chooser) become deprecated noun aliases of To: requests, binding rows
// and contexts spelled by them reach To with one notice. Verbs maps each
// old verb whose name changed to its new verb on To; each becomes a
// deprecated action alias spelled under every old name, so the old name
// keeps its old meaning even where To has a verb of the old verb's name
// (chooser.open is picker.into, not picker.open). Since or RemoveAfter
// is required; Note is added to the notices.
type NounRename struct {
	From        string
	To          string
	Since       string
	RemoveAfter string
	Note        string
	Verbs       map[string]string
}

// RenameNoun registers a NounRename. The old short name is skipped when
// a noun of that name is registered (the application owns it).
func (r *Runtime) RenameNoun(n NounRename) error {
	if n.From == "" || n.To == "" {
		return errors.New("rename needs from and to")
	}
	if n.Since == "" && n.RemoveAfter == "" {
		return errors.New("rename " + n.From + ": since or removeAfter is required")
	}
	names := []string{n.From}
	if short, ok := strings.CutPrefix(n.From, FrameworkNamespace+"."); ok && r.Nouns[short] == nil {
		names = append(names, short)
	}
	for _, name := range names {
		if err := r.AddNounAlias(NounAlias{Alias: name, Noun: n.To, Since: n.Since, RemoveAfter: n.RemoveAfter, Note: n.Note}); err != nil {
			return err
		}
		for _, old := range slices.Sorted(maps.Keys(n.Verbs)) {
			if err := r.AddActionAlias(ActionAlias{Alias: name + "." + old, Action: n.To + "." + n.Verbs[old], Since: n.Since, RemoveAfter: n.RemoveAfter, Note: n.Note}); err != nil {
				return err
			}
		}
	}
	return nil
}

// spelledAlias is the action alias a request names when its noun is a
// renamed noun's old name and its noun.verb, so spelled, is an alias
// (RenameNoun's Verbs); false otherwise.
func (r *Runtime) spelledAlias(q Request) (ActionAlias, bool) {
	if q.Noun == "" || q.Verb == "" || q.IsProp() {
		return ActionAlias{}, false
	}
	if _, renamed := r.DeprecatedNounAlias(q.Noun); !renamed {
		return ActionAlias{}, false
	}
	a, ok := r.actionNames.aliases[q.Key()]
	return a, ok
}

// renamedNounSpelling is the noun an alias named name (noun.verb, its noun
// resolved) is filed under: its noun as spelled when that is a renamed
// noun's old name (ochazuke.chooser.open for ochazuke.picker.into, since
// the old verb may now name another verb of the new noun), else noun.
func (r *Runtime) renamedNounSpelling(name, noun, verb string) string {
	if noun == "" {
		return noun
	}
	if spelled := strings.TrimSuffix(name, "."+verb); spelled != noun {
		if _, renamed := r.DeprecatedNounAlias(spelled); renamed {
			return spelled
		}
	}
	return noun
}
