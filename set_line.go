package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// The :set line grammar (Vim's :set, ochazuke's settings). A line is the
// text after the command word: words separated by spaces, each one
// option form (the setting's name, or a unique prefix of it, then an
// operator):
//
//	(empty)        the settings that differ from their default
//	all            every setting
//	all&           every setting to its default
//	opt            a boolean on; any other type shows its value
//	noopt          a boolean off
//	invopt, opt!   a boolean inverted
//	opt?           show the value
//	opt&, opt&vim  back to the default (opt&vi is refused)
//	opt<           drop the session value
//	opt=v, opt:v   set the value
//	opt+=v         add, append
//	opt-=v         subtract, remove
//	opt^=v         multiply, prepend
//
// The operator and the setting's declared type pick the verb from one
// table (setTable), never a branch on a setting's name. A value may hold
// spaces escaped with a backslash (\ ) or inside double quotes; a
// backslash takes the next character as it is (\\ is one backslash). A
// list value is comma-separated (\, is a comma inside an item) or a JSON
// array. parseSetLine checks every word before answering: the operations
// for the whole line, or one error and none (all or nothing).

// The nouns a :set line's operations run on.
const (
	setSettingNoun  = "ochazuke.setting"
	setSettingsNoun = "ochazuke.settings"
)

// setOp is one operation of a :set line: the verb Noun.Verb runs with the
// setting's name and Value (as Arg). A line-level form (empty, all, all&)
// has no Setting and Word -1 for the empty line.
type setOp struct {
	Word    int     // index into the line's setWords, -1 for the empty line
	Form    string  // the form: "", no, inv, !, ?, &, <, =, +=, -=, ^=, all, all&, or "line" for the empty line
	Setting Setting // the setting, under its current name; zero for a line form
	Noun    string
	Verb    string
	Arg     string // the argument Value fills: value, by, or "" for none
	Value   any    // the parsed value, of the setting's type (a list as []string)
}

// request is the request the operation runs; the caller adds the scope.
func (o setOp) request() Request {
	args := Arguments{}
	if o.Setting.Name != "" {
		args["name"] = o.Setting.Name
	}
	if o.Arg != "" {
		args[o.Arg] = o.Value
	}
	if len(args) == 0 {
		args = nil
	}
	return Request{Noun: o.Noun, Verb: o.Verb, Args: args}
}

// setWord is one word of a :set line and where its parts are. Offsets
// count runes. The name runs from Start to NameEnd; the operator from
// NameEnd to OpEnd ("" when there is none); Rest is the text after the
// operator to End, as typed: a value after =, :, +=, -= and ^=, or text
// that should not be there after !, ?, & and <.
type setWord struct {
	Start, End int
	Text       string // the whole word as typed
	Name       string // the name as typed, with any no or inv in front
	NameEnd    int
	Op         string
	OpEnd      int
	Rest       string
}

// takesValue reports whether the word's operator is followed by a value.
func (w setWord) takesValue() bool { return setValueOps[w.Op] }

// setValueOps are the operators a value follows.
var setValueOps = map[string]bool{"=": true, ":": true, "+=": true, "-=": true, "^=": true}

// setWords splits a :set line into its words and their parts. It reads
// any text; what the words mean is parseSetLine's.
func setWords(line string) []setWord {
	rs := []rune(line)
	var words []setWord
	i := 0
	for {
		for i < len(rs) && unicode.IsSpace(rs[i]) {
			i++
		}
		if i >= len(rs) {
			return words
		}
		w := setWord{Start: i}
		for i < len(rs) && !unicode.IsSpace(rs[i]) && !setOpStart(rs, i) {
			i++
		}
		w.NameEnd = i
		w.Name = string(rs[w.Start:i])
		if i < len(rs) && setOpStart(rs, i) {
			n := 1
			if rs[i] == '+' || rs[i] == '-' || rs[i] == '^' {
				n = 2
			}
			w.Op = string(rs[i : i+n])
			i += n
		}
		w.OpEnd = i
		if w.takesValue() {
			i = setValueEnd(rs, i)
		} else {
			for i < len(rs) && !unicode.IsSpace(rs[i]) {
				i++
			}
		}
		w.End = i
		w.Rest = string(rs[w.OpEnd:i])
		w.Text = string(rs[w.Start:i])
		words = append(words, w)
	}
}

// setOpStart reports whether an operator starts at rs[i]: = : ! ? & <,
// or + - ^ followed by = (a name may hold a dash).
func setOpStart(rs []rune, i int) bool {
	switch rs[i] {
	case '=', ':', '!', '?', '&', '<':
		return true
	case '+', '-', '^':
		return i+1 < len(rs) && rs[i+1] == '='
	}
	return false
}

// setValueEnd is where a value starting at i ends: at a space that is
// neither escaped nor inside double quotes. A value that starts with [
// and has a matching ] reads to it whatever it holds (a JSON array with
// spaces), then on as any value.
func setValueEnd(rs []rune, i int) int {
	if i < len(rs) && rs[i] == '[' {
		if end, ok := jsonArrayEnd(rs, i); ok {
			i = end
		}
	}
	quoted := false
	for i < len(rs) {
		switch r := rs[i]; {
		case r == '\\' && i+1 < len(rs):
			i += 2
			continue
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			return i
		}
		i++
	}
	return i
}

// jsonArrayEnd finds the ] that closes the [ at rs[i], skipping JSON
// strings, and answers the offset after it.
func jsonArrayEnd(rs []rune, i int) (int, bool) {
	depth, inString := 0, false
	for ; i < len(rs); i++ {
		switch r := rs[i]; {
		case inString && r == '\\':
			i++
		case inString:
			inString = r != '"'
		case r == '"':
			inString = true
		case r == '[':
			depth++
		case r == ']':
			if depth--; depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// setDecode reads a value as typed into its text: a backslash takes the
// next character as it is, double quotes group and are dropped. With
// split, an unescaped comma outside quotes separates items; an empty value
// is no items.
func setDecode(raw string, split bool) []string {
	if split && raw == "" {
		return []string{}
	}
	var items []string
	var b strings.Builder
	rs := []rune(raw)
	quoted := false
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '\\' && i+1 < len(rs):
			i++
			b.WriteRune(rs[i])
		case r == '"':
			quoted = !quoted
		case r == ',' && split && !quoted:
			items = append(items, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(items, b.String())
}

// setCursor is what the cursor of a :set line is on, for completion: the
// word (Word indexes Words; len(Words) when the cursor is between words
// and a new one starts there), the part of it (name, operator or value),
// where that part starts (the text completion replaces runs from Start to
// the cursor) and the text typed there so far.
type setCursor struct {
	Words []setWord
	Word  int
	Part  string // name, operator, value
	Start int
	Typed string
}

// The parts of a word the cursor can be on.
const (
	setPartName     = "name"
	setPartOperator = "operator"
	setPartValue    = "value"
)

// setCursorAt answers what the cursor (a rune offset into line, clamped
// to it) is on. A cursor at the end of a word is in that word.
func setCursorAt(line string, cursor int) setCursor {
	rs := []rune(line)
	cursor = max(0, min(cursor, len(rs)))
	c := setCursor{Words: setWords(line)}
	c.Word = len(c.Words)
	for i, w := range c.Words {
		if cursor >= w.Start && cursor <= w.End {
			c.Word = i
			break
		}
		if w.Start > cursor {
			c.Word = i
			c.Words = slices.Insert(c.Words, i, setWord{Start: cursor, End: cursor, NameEnd: cursor, OpEnd: cursor})
			break
		}
	}
	if c.Word == len(c.Words) {
		c.Words = append(c.Words, setWord{Start: cursor, End: cursor, NameEnd: cursor, OpEnd: cursor})
	}
	w := c.Words[c.Word]
	switch {
	case cursor <= w.NameEnd:
		c.Part, c.Start = setPartName, w.Start
	case cursor < w.OpEnd || !w.takesValue():
		c.Part, c.Start = setPartOperator, w.NameEnd
	default:
		c.Part, c.Start = setPartValue, w.OpEnd
	}
	c.Typed = string(rs[c.Start:cursor])
	return c
}

// A setting's class is its type, with a string that has an Enum apart.
const setEnum = "enum"

// setClass is what the table and the error texts say about one class.
type setClass struct {
	Is      string // what a setting of the class is: "an integer"
	Takes   string // what a value must be: "a whole number"
	Example string // the value in a hint: "N"
	Kinds   string // the class among others: "numbers"
	OnOff   string // the hint after "not on/off": "use NAME=N"
}

// setClassOrder is the order classes are named in.
var setClassOrder = []string{SettingBoolean, SettingInteger, SettingNumber, SettingDuration, SettingString, setEnum, SettingList}

var setClasses = map[string]setClass{
	SettingBoolean:  {Is: "on/off", Takes: "true or false", Example: "true", Kinds: "on/off settings"},
	SettingInteger:  {Is: "an integer", Takes: "a whole number", Example: "N", Kinds: "numbers"},
	SettingNumber:   {Is: "a number", Takes: "a number", Example: "N", Kinds: "numbers"},
	SettingDuration: {Is: "a duration", Takes: "a duration such as 400ms or 2s", Example: "400ms", Kinds: "durations"},
	SettingString:   {Is: "text", Takes: "text", Example: "TEXT", Kinds: "text"},
	setEnum:         {Is: "one of %s", Takes: "one of %s", Example: "VALUE", Kinds: "fixed values", OnOff: "use setting.cycle to step through them, or %s=VALUE"},
	SettingList:     {Is: "a list", Takes: `a list such as a,b or ["a","b"]`, Example: "a,b", Kinds: "lists"},
}

// setCell is one cell of the type table: the verb of the setting noun,
// the argument the value fills, and a fixed value (opt and noopt on a
// boolean).
type setCell struct {
	Verb  string
	Arg   string
	Fixed any
}

// setValueForms are the value operators in the order errors name them.
var setValueForms = []string{"=", "+=", "-=", "^="}

// setTable is the type table: the form and the setting's class pick the
// verb. A class missing from a form's row is an error naming what the
// class takes.
var setTable = func() map[string]map[string]setCell {
	all := func(c setCell) map[string]setCell {
		row := map[string]setCell{}
		for _, class := range setClassOrder {
			row[class] = c
		}
		return row
	}
	show, set := setCell{Verb: "show"}, setCell{Verb: "set", Arg: "value"}
	bare := all(show)
	bare[SettingBoolean] = setCell{Verb: "set", Arg: "value", Fixed: true}
	toggle := map[string]setCell{SettingBoolean: {Verb: "toggle"}}
	return map[string]map[string]setCell{
		"":    bare,
		"no":  {SettingBoolean: {Verb: "set", Arg: "value", Fixed: false}},
		"inv": toggle,
		"!":   toggle,
		"?":   all(show),
		"&":   all(setCell{Verb: "reset"}),
		"<":   all(setCell{Verb: "revert"}),
		"=":   all(set),
		"+=": {
			SettingInteger: {Verb: "increase", Arg: "by"}, SettingNumber: {Verb: "increase", Arg: "by"},
			SettingDuration: {Verb: "increase", Arg: "by"},
			SettingString:   {Verb: "append", Arg: "value"}, SettingList: {Verb: "append", Arg: "value"},
		},
		"-=": {
			SettingInteger: {Verb: "decrease", Arg: "by"}, SettingNumber: {Verb: "decrease", Arg: "by"},
			SettingDuration: {Verb: "decrease", Arg: "by"},
			SettingString:   {Verb: "remove", Arg: "value"}, SettingList: {Verb: "remove", Arg: "value"},
		},
		"^=": {
			SettingInteger: {Verb: "multiply", Arg: "by"}, SettingNumber: {Verb: "multiply", Arg: "by"},
			SettingString: {Verb: "prepend", Arg: "value"}, SettingList: {Verb: "prepend", Arg: "value"},
		},
	}
}()

// setLineForms are the words that are line forms rather than settings,
// typed whole.
var setLineForms = map[string]setCell{
	"all":  {Verb: "list"},
	"all&": {Verb: "reset-all"},
}

// setClassOf is a setting's class.
func setClassOf(s Setting) string {
	if s.Type == SettingString && len(s.Enum) > 0 {
		return setEnum
	}
	return s.Type
}

// setLineError is a :set line refused at one word (Word indexes the
// line's setWords); it comes wrapped in an ArgumentError.
type setLineError struct {
	Word int
	Err  error
}

func (e *setLineError) Error() string { return e.Err.Error() }
func (e *setLineError) Unwrap() error { return e.Err }

// setAmbiguousShown is how many candidates an ambiguous name lists.
const setAmbiguousShown = 5

// parseSetLine reads a :set line (the text after the command word) over
// the declared settings: one operation per word, or one error for the
// first word that fails and no operations. An empty line is one
// settings.diff.
func parseSetLine(line string, settings []Setting) ([]setOp, error) {
	words := setWords(line)
	if len(words) == 0 {
		return []setOp{{Word: -1, Form: "line", Noun: setSettingsNoun, Verb: "diff"}}, nil
	}
	ops := make([]setOp, 0, len(words))
	for i, w := range words {
		op, err := parseSetWord(w, settings)
		if err != nil {
			if len(words) > 1 {
				err = fmt.Errorf("%w; nothing was changed", err)
			}
			return nil, &ArgumentError{Err: &setLineError{Word: i, Err: err}}
		}
		op.Word = i
		ops = append(ops, op)
	}
	return ops, nil
}

// parseSetWord reads one word.
func parseSetWord(w setWord, settings []Setting) (setOp, error) {
	if cell, ok := setLineForms[w.Text]; ok {
		return setOp{Form: w.Text, Noun: setSettingsNoun, Verb: cell.Verb}, nil
	}
	one, prefix, err := resolveSetName(w.Name, settings)
	if err != nil {
		return setOp{}, err
	}
	form := w.Op
	switch {
	case prefix != "" && form != "":
		return setOp{}, fmt.Errorf("unexpected text after %s%s: %s%s", prefix, one.Name, w.Op, w.Rest)
	case prefix != "":
		form = prefix
	case form == ":":
		form = "="
	case form == "&" && w.Rest == "vim":
	case form == "&" && w.Rest == "vi":
		return setOp{}, fmt.Errorf("settings have one default: use %s&", one.Name)
	case !w.takesValue() && w.Rest != "":
		return setOp{}, fmt.Errorf("unexpected text after %s%s: %s", one.Name, w.Op, w.Rest)
	}
	class := setClassOf(one)
	info, known := setClasses[class]
	if !known {
		return setOp{}, fmt.Errorf("%s has a type :set does not read: %s", one.Name, one.Type)
	}
	cell, ok := setTable[form][class]
	if !ok {
		return setOp{}, setFormError(one, class, info, form)
	}
	op := setOp{Form: form, Setting: one, Noun: setSettingNoun, Verb: cell.Verb, Arg: cell.Arg, Value: cell.Fixed}
	if setValueOps[form] {
		op.Value, err = setValue(one, class, info, cell, w.Rest)
	}
	return op, err
}

// setFormError says why a form does not fit the setting's class: an
// on/off form on a setting that is not a boolean, or a value operator
// the class has no verb for.
func setFormError(one Setting, class string, info setClass, form string) error {
	if !setValueOps[form] {
		hint := fmt.Sprintf("use %s=%s", one.Name, info.Example)
		if info.OnOff != "" {
			hint = fmt.Sprintf(info.OnOff, one.Name)
		}
		return fmt.Errorf("%s is %s, not on/off: %s", one.Name, setClassIs(one, info), hint)
	}
	var takes, kinds []string
	for _, f := range setValueForms {
		if _, ok := setTable[f][class]; ok {
			takes = append(takes, f)
		}
	}
	for _, c := range setClassOrder {
		if _, ok := setTable[form][c]; ok && !slices.Contains(kinds, setClasses[c].Kinds) {
			kinds = append(kinds, setClasses[c].Kinds)
		}
	}
	allowed := joinAnd(takes)
	if len(takes) == 1 {
		allowed += " only"
	}
	return fmt.Errorf("%s takes %s: %s is for %s", one.Name, allowed, form, joinAnd(kinds))
}

// setClassIs is what the setting is, with an enum's values.
func setClassIs(one Setting, info setClass) string {
	if strings.Contains(info.Is, "%s") {
		return fmt.Sprintf(info.Is, strings.Join(one.Enum, ", "))
	}
	return info.Is
}

// joinAnd joins words as a sentence does: "a, b and c".
func joinAnd(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// setValue parses the value after a value operator by the setting's
// type. A value that sets the setting is checked whole (coerceSetting:
// type, Validate, enum); an amount or items to add or remove are checked
// for their type only, since the verb checks the result.
func setValue(one Setting, class string, info setClass, cell setCell, raw string) (any, error) {
	shown := raw
	if shown == "" {
		shown = `""`
	}
	var word any
	switch class {
	case SettingList:
		if strings.HasPrefix(strings.TrimSpace(raw), "[") {
			word = raw
		} else {
			word = setDecode(raw, true)
		}
	case setEnum:
		text := setDecode(raw, false)[0]
		hit, candidates, _ := matchWord(text, setEnumOptions(one))
		switch {
		case hit.Value != "" && text != "":
			word = hit.Value
		case len(candidates) > 1 && text != "":
			return nil, fmt.Errorf("%s: %s is ambiguous: %s", one.Name, shown, strings.Join(optionValues(candidates), ", "))
		default:
			return nil, fmt.Errorf("%s has no value %s: %s", one.Name, shown, strings.Join(one.Enum, ", "))
		}
	default:
		word = setDecode(raw, false)[0]
	}
	v, err := coerceType(one, word)
	if err != nil {
		return nil, fmt.Errorf("%s takes %s: %s", one.Name, info.Takes, shown)
	}
	if cell.Verb != "set" {
		return v, nil
	}
	if v, err = coerceSetting(one, v); err != nil {
		return nil, fmt.Errorf("%s: %w", one.Name, err)
	}
	return v, nil
}

func setEnumOptions(one Setting) []option {
	out := make([]option, len(one.Enum))
	for i, v := range one.Enum {
		out[i] = option{Kind: "argument", Value: v}
	}
	return out
}

// The words in front of a boolean's name.
var setNamePrefixes = []string{"no", "inv"}

// resolveSetName finds the setting a word names: its name, or no or inv
// and its name, each exact or a unique prefix; an exact name wins over a
// prefix, and a setting's own name over a no or inv spelling. An older
// name (Deprecated) reads as the setting when typed whole.
func resolveSetName(word string, settings []Setting) (Setting, string, error) {
	var options []option
	type spelled struct {
		setting Setting
		prefix  string
	}
	spellings := map[string]spelled{}
	add := func(s Setting, prefix, name string, whole bool) {
		key := strings.ToLower(prefix + name)
		if _, taken := spellings[key]; taken {
			return
		}
		spellings[key] = spelled{s, prefix}
		options = append(options, option{Kind: prefix, Value: prefix + name, alias: whole})
	}
	for _, prefix := range append([]string{""}, setNamePrefixes...) {
		for _, s := range settings {
			add(s, prefix, s.Name, false)
		}
		for _, s := range settings {
			for _, old := range s.Deprecated {
				add(s, prefix, old, true)
			}
		}
	}
	hit, candidates, _ := matchWord(word, options)
	if hit.Value == "" && word != "" && len(candidates) > 1 {
		return Setting{}, "", fmt.Errorf("%s is ambiguous: %s", word, setCandidates(candidates))
	}
	if hit.Value == "" {
		return Setting{}, "", setUnknown(word, settings)
	}
	one := spellings[strings.ToLower(hit.Value)]
	return one.setting, one.prefix, nil
}

// setCandidates lists an ambiguous word's candidates, the first few.
func setCandidates(candidates []option) string {
	names := optionValues(candidates)
	if len(names) > setAmbiguousShown {
		return strings.Join(names[:setAmbiguousShown], ", ") + ", …"
	}
	return strings.Join(names, ", ")
}

// setUnknown is the error for a word that names no setting, with the
// three closest names.
func setUnknown(word string, settings []Setting) error {
	if word == "" {
		return errors.New("a setting name comes before the operator")
	}
	if len(settings) == 0 {
		return fmt.Errorf("unknown setting %s: this application declares none", word)
	}
	bare := strings.ToLower(word)
	stripped := bare
	for _, prefix := range setNamePrefixes {
		if rest, ok := strings.CutPrefix(bare, prefix); ok {
			stripped = rest
		}
	}
	// A name is near by its distance from the word over the word's length
	// (a mistyped prefix, as a unique prefix is typed), then over all of it.
	type near struct {
		name          string
		prefix, whole int
	}
	head := func(name, word string) string {
		rs := []rune(name)
		return string(rs[:min(len(rs), len([]rune(word)))])
	}
	nears := make([]near, len(settings))
	for i, s := range settings {
		name := strings.ToLower(s.Name)
		nears[i] = near{s.Name,
			min(editDistance(bare, head(name, bare)), editDistance(stripped, head(name, stripped))),
			min(editDistance(bare, name), editDistance(stripped, name))}
	}
	slices.SortStableFunc(nears, func(a, b near) int {
		if a.prefix != b.prefix {
			return a.prefix - b.prefix
		}
		return a.whole - b.whole
	})
	names := make([]string, 0, 3)
	for _, n := range nears[:min(3, len(nears))] {
		names = append(names, n.name)
	}
	return fmt.Errorf("unknown setting %s: did you mean %s?", word, strings.Join(names, ", "))
}

// editDistance is the Levenshtein distance between two words, in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
