package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The setting verbs (lane C of the :set plan): one verb of the
// ochazuke.setting noun per thing a setting's value can do, each taking
// the setting's name (the focused Settings row when omitted), the value or
// amount it needs and the scope it writes. The :set line (settings.apply),
// the Settings page's keys, the palette, the CLI and the API run the same
// handler per verb. Which verbs a setting takes comes from the type table
// (setTable in set_line.go): a verb fits the classes some :set form maps
// to it, so the table is the one place that says what fits where.
//
// A verb computes the new value from the effective one (settingVerb.Next)
// and checks it whole (coerceSetting: type, Validate, enum) before it
// writes; the line parser only type-checks operands. Writes go where scope
// says: session (the default) by the writes-through rule of
// settings_session.go (the session layer while the runtime has one and
// settings.autosave is off, else settings.json), or file (settings.json at
// once, as the Settings page's keys do: their binding rows pass scope
// file). Every write is one undo step on the Settings page (pageChange);
// settings.apply folds a whole line into one.

// The scopes a setting verb writes (the scope argument).
const (
	ScopeSession = "session"
	ScopeFile    = "file"
)

// settingVerb is one verb of the setting noun: what it does, the argument
// that carries its operand (value or by, "" for none), the classes it
// fits, and how it computes the new value from the effective one. A verb
// with no Next writes nothing (show); Reset writes the default by removing
// the value rather than setting it.
type settingVerb struct {
	Verb        string
	Description string
	Arg         string
	ArgHelp     string
	Fits        func(Setting) bool
	Next        func(one Setting, current any, a Arguments) (any, error)
	Reset       bool
	Extra       map[string]Parameter
}

// settingVerbs are the setting noun's value verbs, in the order help lists
// them. edit, add, revert and write are registered beside them (the page's
// prompts and the session layer's verbs).
var settingVerbs = []settingVerb{
	{Verb: "show", Description: "Show the setting's value on the status line, name=value (a boolean name or noname); verbose adds where it was set", Fits: anySetting,
		Extra: map[string]Parameter{"verbose": {Type: "boolean", Description: "Add where the value was set (:verbose)"}}},
	{Verb: "set", Description: "Set the setting to a value, parsed by its type and checked against its allowed values", Arg: "value", ArgHelp: "The value: text, a number, true or false, a duration such as 400ms, or a list as a,b or a JSON array", Fits: tableFits("set"), Next: nextSet},
	{Verb: "toggle", Description: "Invert an on/off setting", Fits: tableFits("toggle"), Next: nextToggle},
	{Verb: "cycle", Description: "Step a boolean, an enum or a number to its next or previous value (an enum wraps)", Fits: settingSteps, Next: nextCycle,
		Extra: map[string]Parameter{"step": {Type: "integer", Description: "1 for the next value, -1 for the previous"}}},
	{Verb: "reset", Description: "Reset the setting to its default", Fits: anySetting, Reset: true, Next: func(one Setting, _ any, _ Arguments) (any, error) { return one.Default, nil }},
	{Verb: "increase", Description: "Add an amount to a number or duration setting", Arg: "by", ArgHelp: "The amount, of the setting's type (5, 0.5, 100ms)", Fits: tableFits("increase"), Next: arithmetic("increase")},
	{Verb: "decrease", Description: "Subtract an amount from a number or duration setting", Arg: "by", ArgHelp: "The amount, of the setting's type (5, 0.5, 100ms)", Fits: tableFits("decrease"), Next: arithmetic("decrease")},
	{Verb: "multiply", Description: "Multiply a number setting by an amount", Arg: "by", ArgHelp: "The factor, of the setting's type (2, 0.5)", Fits: tableFits("multiply"), Next: arithmetic("multiply")},
	{Verb: "append", Description: "Add text at the end of a text setting, or items at the end of a list (skipping items it holds)", Arg: "value", ArgHelp: "The text, or the items as a,b or a JSON array", Fits: tableFits("append"), Next: edits("append")},
	{Verb: "prepend", Description: "Add text at the start of a text setting, or items at the start of a list (skipping items it holds)", Arg: "value", ArgHelp: "The text, or the items as a,b or a JSON array", Fits: tableFits("prepend"), Next: edits("prepend")},
	{Verb: "remove", Description: "Remove text from a text setting (its first occurrence), or items from a list; what is not there is not an error", Arg: "value", ArgHelp: "The text, or the items as a,b or a JSON array", Fits: tableFits("remove"), Next: edits("remove")},
}

// settingVerbOf is the value verb named verb, false for any other.
func settingVerbOf(verb string) (settingVerb, bool) {
	i := slices.IndexFunc(settingVerbs, func(v settingVerb) bool { return v.Verb == verb })
	if i < 0 {
		return settingVerb{}, false
	}
	return settingVerbs[i], true
}

func anySetting(Setting) bool { return true }

// tableFits is a verb's fit read from the type table: the classes some
// :set form maps to the verb.
func tableFits(verb string) func(Setting) bool {
	return func(one Setting) bool { return slices.Contains(tableClasses(verb), setClassOf(one)) }
}

// tableClasses are the classes the type table maps some form of verb to,
// in setClassOrder.
func tableClasses(verb string) []string {
	var out []string
	for _, class := range setClassOrder {
		for _, row := range setTable {
			if cell, ok := row[class]; ok && cell.Verb == verb && !slices.Contains(out, class) {
				out = append(out, class)
			}
		}
	}
	return out
}

// fitError says a verb does not fit a setting: what the setting is and
// what the verb is for.
func fitError(v settingVerb, one Setting) error {
	info := setClasses[setClassOf(one)]
	var kinds []string
	for _, class := range setClassOrder {
		probe := Setting{Name: one.Name, Type: class}
		if class == setEnum {
			probe = Setting{Name: one.Name, Type: SettingString, Enum: []string{"x"}}
		}
		if v.Fits(probe) && !slices.Contains(kinds, setClasses[class].Kinds) {
			kinds = append(kinds, setClasses[class].Kinds)
		}
	}
	if v.Verb == "toggle" && setClassOf(one) == setEnum {
		return fmt.Errorf("%s is %s, not on/off: %s", one.Name, setClassIs(one, info), fmt.Sprintf(info.OnOff, one.Name))
	}
	return fmt.Errorf("%s is %s: setting.%s is for %s", one.Name, setClassIs(one, info), v.Verb, joinAnd(kinds))
}

// settingArg reads a verb's operand as the setting's type: a list from a
// JSON array or comma-separated items (\, inside an item), anything else
// by coerceType. It checks the type only; the verb checks the result.
func settingArg(one Setting, v any) (any, error) {
	if text, ok := v.(string); ok && one.Type == SettingList && !strings.HasPrefix(strings.TrimSpace(text), "[") {
		v = setDecode(text, true)
	}
	return coerceType(one, v)
}

// operand is a verb's operand, read as the setting's type; a missing one
// is an argument error naming it.
func operand(one Setting, a Arguments, arg string) (any, error) {
	v, ok := a[arg]
	if !ok {
		return nil, &ArgumentError{fmt.Errorf("%s is required", arg)}
	}
	got, err := settingArg(one, v)
	if err != nil {
		return nil, fmt.Errorf("%s takes %s: %v", one.Name, setClasses[setClassOf(one)].Takes, v)
	}
	return got, nil
}

// nextSet is the value as given (a list's comma form split); planSetting
// checks it whole, so its error is coerceSetting's.
func nextSet(one Setting, _ any, a Arguments) (any, error) {
	v, ok := a["value"]
	if !ok {
		return nil, &ArgumentError{errors.New("value is required")}
	}
	if text, isText := v.(string); isText && one.Type == SettingList && !strings.HasPrefix(strings.TrimSpace(text), "[") {
		return setDecode(text, true), nil
	}
	return v, nil
}

func nextToggle(_ Setting, current any, _ Arguments) (any, error) { return !current.(bool), nil }

func nextCycle(one Setting, current any, a Arguments) (any, error) {
	step := IntArg(a, "step", 1)
	switch {
	case one.Type == SettingBoolean:
		return !current.(bool), nil
	case len(one.Enum) > 0:
		i := slices.Index(one.Enum, current.(string))
		n := len(one.Enum)
		return one.Enum[((i+step)%n+n)%n], nil
	case one.Type == SettingInteger:
		return current.(int) + step, nil
	default:
		return current.(float64) + float64(step), nil
	}
}

// arithmetic is increase, decrease or multiply over integers, numbers and
// durations; an integer stays an integer.
func arithmetic(verb string) func(Setting, any, Arguments) (any, error) {
	return func(one Setting, current any, a Arguments) (any, error) {
		by, err := operand(one, a, "by")
		if err != nil {
			return nil, err
		}
		switch cur := current.(type) {
		case int:
			n := by.(int)
			return map[string]int{"increase": cur + n, "decrease": cur - n, "multiply": cur * n}[verb], nil
		case float64:
			n := by.(float64)
			return map[string]float64{"increase": cur + n, "decrease": cur - n, "multiply": cur * n}[verb], nil
		case time.Duration:
			n := by.(time.Duration)
			return map[string]time.Duration{"increase": cur + n, "decrease": cur - n}[verb], nil
		}
		return nil, fmt.Errorf("%s: not a number", one.Name)
	}
}

// edits is append, prepend or remove over text and lists. A list skips the
// items it holds when adding (and repeats in what is added); remove drops
// every copy of each item. Text removes its first occurrence; what is not
// there is not an error, as in Vim.
func edits(verb string) func(Setting, any, Arguments) (any, error) {
	return func(one Setting, current any, a Arguments) (any, error) {
		v, err := operand(one, a, "value")
		if err != nil {
			return nil, err
		}
		if text, ok := current.(string); ok {
			add := v.(string)
			switch verb {
			case "append":
				return text + add, nil
			case "prepend":
				return add + text, nil
			}
			return strings.Replace(text, add, "", 1), nil
		}
		items, given := slices.Clone(current.([]string)), v.([]string)
		if verb == "remove" {
			return slices.DeleteFunc(items, func(it string) bool { return slices.Contains(given, it) }), nil
		}
		var fresh []string
		for _, it := range given {
			if !slices.Contains(items, it) && !slices.Contains(fresh, it) {
				fresh = append(fresh, it)
			}
		}
		if verb == "prepend" {
			return append(fresh, items...), nil
		}
		return append(items, fresh...), nil
	}
}

// settingPlan is what a value verb would do to one setting: the new value,
// checked whole, or a reset to the default, and the scope it writes.
type settingPlan struct {
	one   Setting
	next  any
	reset bool
	scope string
}

// planSetting computes what verb would write over current (the setting's
// effective value, or what an earlier word of a :set line left it at).
func planSetting(v settingVerb, one Setting, current any, a Arguments) (settingPlan, error) {
	scope, err := settingScope(a)
	if err != nil {
		return settingPlan{}, err
	}
	if !v.Fits(one) {
		return settingPlan{}, fitError(v, one)
	}
	next, err := v.Next(one, current, a)
	if err != nil {
		return settingPlan{}, err
	}
	if next, err = coerceSetting(one, next); err != nil {
		return settingPlan{}, fmt.Errorf("%s: %w", one.Name, err)
	}
	return settingPlan{one: one, next: next, reset: v.Reset, scope: scope}, nil
}

// settingScope is the scope argument, session when omitted.
func settingScope(a Arguments) (string, error) {
	switch scope := StringArg(a, "scope", ScopeSession); scope {
	case ScopeSession, ScopeFile:
		return scope, nil
	default:
		return "", &ArgumentError{fmt.Errorf("scope is session or file: %s", scope)}
	}
}

// write applies the plan in its scope and returns the layer written.
func (p settingPlan) write(s *Settings) (string, error) {
	switch {
	case p.scope == ScopeFile && p.reset:
		return OriginFile, s.Reset(p.one.Name)
	case p.scope == ScopeFile:
		return OriginFile, s.Set(p.one.Name, p.next)
	case p.reset:
		return s.ResetSession(p.one.Name)
	default:
		return s.SetSession(p.one.Name, p.next)
	}
}

// change is the plan as a prompt names it: "scrolloff = 5", or "remove
// scrolloff" for a reset of a key the file holds ("" when nothing would be
// written: a reset of a key the file lacks).
func (p settingPlan) change(s *Settings) string {
	if p.reset {
		if !s.fileHolds(p.one) {
			return ""
		}
		return "remove " + p.one.Name
	}
	return p.one.Name + " = " + settingWord(p.next)
}

// SettingWritten is a value verb's answer: the setting with its value
// after the write and the layer written, session or file (a runtime with
// no session writes the file: "saved: no running session").
type SettingWritten struct {
	SettingValue
	Written string `json:"written"`
}

// SettingShown is setting.show's answer: the setting, its value and the
// line the status line shows (name=value, name or noname), with the
// origin when verbose.
type SettingShown struct {
	SettingValue
	Line   string  `json:"line"`
	Origin *Origin `json:"origin,omitempty"`
}

// showLine is a setting's value as :set shows it: name=value, a boolean
// name or noname, a list comma-separated (a comma or backslash in an item
// escaped, as the line reads it back).
func showLine(one Setting, v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return one.Name
		}
		return "no" + one.Name
	case []string:
		items := make([]string, len(t))
		for i, it := range t {
			items[i] = strings.NewReplacer(`\`, `\\`, `,`, `\,`).Replace(it)
		}
		return one.Name + "=" + strings.Join(items, ",")
	case string:
		return one.Name + "=" + t
	}
	return one.Name + "=" + fmt.Sprint(fileForm(v))
}

// settingWordArg spells a typed value as the word a verb's value or by
// argument takes (the CLI's and the palette's form): a list as a JSON
// array, so its items come back whole.
func settingWordArg(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		data, _ := json.Marshal(t)
		return string(data)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	}
	return fmt.Sprint(fileForm(v))
}

// settingWritePrompt is a value verb's question from a CLI process: the
// change it would write to settings.json.
func settingWritePrompt(v settingVerb) func(r *Runtime, a Arguments) string {
	return func(r *Runtime, a Arguments) string {
		s := r.Settings()
		s.ensure()
		one := s.find(s.canonical(StringArg(a, "name", "")))
		if one == nil {
			return ""
		}
		p, err := planSetting(v, *one, s.current(*one), a)
		if err != nil {
			return ""
		}
		if change := p.change(s); change != "" {
			return fmt.Sprintf("Write %s to %s?", change, r.Config().Path(SettingsFile))
		}
		return ""
	}
}

// settingParams are a value verb's parameters: name, its operand, scope
// and its own.
func settingParams(v settingVerb) map[string]Parameter {
	params := map[string]Parameter{
		"name": {Type: "string", Description: "Setting name (default: the focused Settings row)", Source: "ochazuke.settings.names"},
	}
	if v.Next != nil {
		params["scope"] = Parameter{Type: "string", Enum: []string{ScopeSession, ScopeFile}, Description: "Where the value is written: session (the default: settings.json when there is no running session or settings.autosave is on) or file"}
	}
	if v.Arg != "" {
		params[v.Arg] = Parameter{Type: "string", Required: true, Description: v.ArgHelp}
	}
	for name, p := range v.Extra {
		params[name] = p
	}
	return params
}

// registerSettingVerbs registers the value verbs on the setting noun.
func registerSettingVerbs(r *Runtime) {
	for _, v := range settingVerbs {
		op := Operation{Verb: v.Verb, Noun: "ochazuke.setting", Description: v.Description, Parameters: settingParams(v), Positional: []string{"name"}, Result: "setting", Run: runSettingVerb(v)}
		if v.Arg != "" {
			op.Positional = append(op.Positional, v.Arg)
		}
		if v.Next != nil {
			op.Checks = []VerbCheck{cliWriteCheck(settingWritePrompt(v))}
		}
		r.Register(op)
	}
	names := make([]string, 0, len(settingVerbs))
	for _, v := range settingVerbs {
		names = append(names, v.Verb)
	}
	r.Define("ochazuke.setting").Verbs(names...)
	registerSettingThings(r)
}

// The setting noun's things (standard verbs, lane S7): ochazuke.setting is
// a collection whose things are the declared settings, keyed by name (a
// setting's name holds dots, so settings are things, never props). Its
// store reads them only, so get, list and watch are standard; set, show,
// reset and the other value verbs above stay the named :set verbs (the
// application's verb wins over a standard one), carrying scope and the
// session layer that the standard set does not.

// settingProps are a setting thing's props, every one read-only.
var settingProps = []PropSpec{
	{Name: "name", Key: true, Parameter: Parameter{Description: "The setting's name"}},
	{Name: "type", ReadOnly: true, Parameter: Parameter{Enum: []string{SettingString, SettingInteger, SettingNumber, SettingBoolean, SettingDuration, SettingList}, Description: "The value's type"}},
	{Name: "default", ReadOnly: true, Parameter: Parameter{Description: "The default value, in its file form (a duration as 400ms)"}},
	{Name: "value", ReadOnly: true, Parameter: Parameter{Description: "The value in effect, in its file form: the session's, else settings.json's, else the default (setting.set writes it)"}},
	{Name: "enum", ReadOnly: true, Parameter: Parameter{Type: SettingList, Description: "The values allowed, when the setting limits them"}},
	{Name: "description", ReadOnly: true, Parameter: Parameter{Description: "What the setting does"}},
	{Name: "section", ReadOnly: true, Parameter: Parameter{Description: "The section of its owner it is filed under (Setting.Section, else the name before its first dot, else general; settings.sections lists them)"}},
	{Name: "owner", ReadOnly: true, Parameter: Parameter{Description: "The owner it is filed under, a tab of the Settings page: app (the application's), ochazuke (the framework's, also when the application changed its default) or one the application declares (settings.owners lists them)"}},
	{Name: "group", ReadOnly: true, Parameter: Parameter{Description: "The heading it sits under inside its section, when it has one"}},
	{Name: "origin", ReadOnly: true, Parameter: Parameter{Enum: []string{OriginDefault, OriginFile, OriginSession}, Description: "Where the value in effect comes from: the default, settings.json or this session (setting.show --verbose says more)"}},
}

// registerSettingThings declares the setting noun's props and its store,
// and tells its watches when any setting changes.
func registerSettingThings(r *Runtime) {
	r.Define("ochazuke.setting").Props(settingProps...).Store(Store{
		Kind: Collection,
		Get: func(c Ctx, name string) (Thing, error) {
			return settingThing(c.Runtime, name)
		},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			s := c.Runtime.Settings()
			out := []Thing{}
			for _, one := range s.List() {
				t, err := settingThing(c.Runtime, one.Name)
				if err != nil {
					return nil, err
				}
				out = append(out, t)
			}
			return out, nil
		},
	})
	r.Settings().Observe("", func(string, any) { r.Changed("ochazuke.setting") })
}

// settingThing is one declared setting as a thing (settingProps); an old
// name reads the setting it became.
func settingThing(r *Runtime, name string) (Thing, error) {
	s := r.Settings()
	v, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	origin, err := s.Origin(v.Name)
	if err != nil {
		return nil, err
	}
	enum := []string{}
	if len(v.Enum) > 0 {
		enum = slices.Clone(v.Enum)
	}
	return Thing{
		"name": v.Name, "type": v.Type, "default": v.Default, "value": v.Value, "enum": enum,
		"description": v.Description, "section": v.Section, "owner": v.Owner, "group": v.Group, "origin": origin.Layer,
	}, nil
}

// runSettingVerb is a value verb's handler: find the setting (its name,
// else the focused row), then show it, or plan and write the change as
// one undo step on the Settings page.
func runSettingVerb(v settingVerb) Handler {
	return func(r *Runtime, c Context, a Arguments) (any, error) {
		one, err := settingOfArgs(r, c, a)
		if err != nil {
			return nil, err
		}
		s := r.Settings()
		if v.Next == nil {
			return showSetting(r, *one, boolArg(a, "verbose"))
		}
		p, err := planSetting(v, *one, s.current(*one), a)
		if err != nil {
			return nil, err
		}
		var layer string
		if err := pageChange(r, one.Name, func() (err error) { layer, err = p.write(s); return err }); err != nil {
			return nil, err
		}
		// A reset of the focused row's list (its items gone) keeps the
		// focus on the row.
		if StringArg(a, "name", "") == "" && v.Reset {
			_ = r.SetFocus(settingRowPrefix + one.Name)
		}
		got, err := s.Get(one.Name)
		return SettingWritten{SettingValue: got, Written: layer}, err
	}
}

// showSetting answers setting.show and puts its line on the status line,
// or hands it to the :set line that runs it (settingsApply collects the
// lines of a whole line and shows them together).
func showSetting(r *Runtime, one Setting, verbose bool) (SettingShown, error) {
	s := r.Settings()
	got, err := s.Get(one.Name)
	if err != nil {
		return SettingShown{}, err
	}
	out := SettingShown{SettingValue: got, Line: showLine(one, s.current(one))}
	if verbose {
		o, _ := s.Origin(one.Name)
		out.Origin = &o
		out.Line += " — " + o.String()
	}
	if s.shown != nil {
		*s.shown = append(*s.shown, out.Line)
	} else {
		r.UI().Info(out.Line)
	}
	return out, nil
}

// SettingsApplied is settings.apply's answer for a line that is not a
// list form: what it showed and the settings it changed, with their
// values after.
type SettingsApplied struct {
	Shown   []string       `json:"shown,omitempty"`
	Changed []SettingValue `json:"changed,omitempty"`
}

// The words that make a :set line verbose (:verbose, :verb) when they lead
// it and no setting is named so.
var setVerboseWords = []string{"verbose", "verb"}

// applyLine is a settings.apply line read: its operations, whether it is
// verbose, and what each writing operation would leave its setting at.
type applyLine struct {
	ops     []setOp
	verbose bool
	plans   map[string]settingPlan // by setting, the last plan for it
	order   []string               // the settings planned, first write first
}

// readApplyLine parses a :set line and checks every operation before any
// runs: each value verb is planned over what the words before it left its
// setting at, so a line is refused whole (all or nothing).
func readApplyLine(r *Runtime, a Arguments) (applyLine, error) {
	s := r.Settings()
	s.ensure()
	line := StringArg(a, "line", "")
	out := applyLine{verbose: boolArg(a, "verbose"), plans: map[string]settingPlan{}}
	if words := setWords(line); len(words) > 0 && slices.Contains(setVerboseWords, words[0].Text) && s.find(words[0].Text) == nil {
		out.verbose = true
		line = string([]rune(line)[words[0].End:])
	}
	ops, err := parseSetLine(line, s.Declared())
	if err != nil {
		return out, err
	}
	scope, err := settingScope(a)
	if err != nil {
		return out, err
	}
	pending := map[string]any{}
	for i, op := range ops {
		v, ok := settingVerbOf(op.Verb)
		if op.Noun != setSettingNoun || !ok || v.Next == nil {
			continue
		}
		current, held := pending[op.Setting.Name]
		if !held {
			current = s.current(op.Setting)
		}
		args := opArgs(op, scope, false)
		p, err := planSetting(v, op.Setting, current, args)
		if err != nil {
			if len(ops) > 1 {
				err = fmt.Errorf("%w; nothing was changed", err)
			}
			return out, &ArgumentError{Err: &setLineError{Word: i, Err: err}}
		}
		pending[op.Setting.Name] = p.next
		if _, seen := out.plans[op.Setting.Name]; !seen {
			out.order = append(out.order, op.Setting.Name)
		}
		out.plans[op.Setting.Name] = p
	}
	out.ops = ops
	return out, nil
}

// opArgs are the arguments an operation's request carries: the line's
// value as the word the verb reads, the scope on a writing verb, verbose
// on show.
func opArgs(op setOp, scope string, verbose bool) Arguments {
	args := op.request().Args
	if args == nil {
		args = Arguments{}
	}
	if op.Arg != "" {
		args[op.Arg] = settingWordArg(op.Value)
	}
	if v, ok := settingVerbOf(op.Verb); ok && op.Noun == setSettingNoun {
		if v.Next != nil {
			args["scope"] = scope
		}
		if _, takes := v.Extra["verbose"]; takes && verbose {
			args["verbose"] = true
		}
	}
	return args
}

// applyCheck asks before settings.apply runs when one of its operations
// would ask: from a CLI process with no session, one question naming
// every change the line writes to settings.json; the other operations'
// own checks (settings.reset-all's) add theirs. A line that does not
// parse or check allows, and the verb then fails with the reason.
var applyCheck = VerbCheck{Name: "settings.apply", Check: func(r *Runtime, c Context, a Arguments) Verdict {
	line, err := readApplyLine(r, a)
	if err != nil {
		return Allow()
	}
	s := r.Settings()
	var asks []string
	if r.history.source == "cli" && !s.HasSession() && r.Config().Path(SettingsFile) != "" {
		var changes []string
		for _, name := range line.order {
			if change := line.plans[name].change(s); change != "" {
				changes = append(changes, change)
			}
		}
		if len(changes) > 0 {
			asks = append(asks, fmt.Sprintf("Write %s to %s?", strings.Join(changes, ", "), r.Config().Path(SettingsFile)))
		}
	}
	scope, _ := settingScope(a)
	for _, op := range line.ops {
		if v, ok := settingVerbOf(op.Verb); ok && op.Noun == setSettingNoun && v.Next != nil {
			continue
		}
		q := Request{Noun: op.Noun, Verb: op.Verb, Args: opArgs(op, scope, line.verbose)}
		def, ok := r.Operations[q.Key()]
		if !ok {
			continue
		}
		switch v := r.runChecks(c, q, def); {
		case v.Refuse != "":
			return v
		case v.Ask != "":
			asks = append(asks, v.Ask)
		}
	}
	if len(asks) == 0 {
		return Allow()
	}
	return AskFirst(strings.Join(asks, " "))
}}

// settingsApply runs a :set line: every operation checked first
// (readApplyLine), then each run as a request to its verb, confirmed (the
// line's check asked for all of them), as one undo step on the Settings
// page; a failure takes back what the line changed. The show lines go to
// the status line together; a list form (the empty line, all) answers
// its list, which the palette opens as results.
func settingsApply(r *Runtime, c Context, a Arguments) (any, error) {
	line, err := readApplyLine(r, a)
	if err != nil {
		return nil, err
	}
	s := r.Settings()
	scope, _ := settingScope(a)
	var shown []string
	s.shown, s.applying = &shown, r.Invoked().Name
	var listed any
	listing := false
	err = settingsGroup(r, func() error {
		for _, op := range line.ops {
			q := Request{Noun: op.Noun, Verb: op.Verb, Args: opArgs(op, scope, line.verbose), Confirmed: true}
			got, err := r.ExecuteAt(c, q)
			if err != nil {
				if len(line.ops) > 1 {
					err = fmt.Errorf("%w; nothing was changed", err)
				}
				return &setLineError{Word: op.Word, Err: err}
			}
			if op.Noun == setSettingsNoun && (op.Verb == "diff" || op.Verb == "list") {
				listed, listing = got, true
			}
		}
		return nil
	})
	s.shown, s.applying = nil, ""
	if err != nil {
		return nil, err
	}
	if len(shown) > 0 {
		r.UI().Info(strings.Join(shown, "  "))
	}
	if listing {
		return listed, nil
	}
	out := SettingsApplied{Shown: shown}
	for _, name := range line.order {
		if got, err := s.Get(name); err == nil {
			out.Changed = append(out.Changed, got)
		}
	}
	return out, nil
}

// registerSettingsApply registers settings.apply, the :set front-end.
func registerSettingsApply(r *Runtime) {
	r.Register(Operation{Verb: "apply", Noun: "ochazuke.settings", Checks: []VerbCheck{applyCheck},
		Description: "Run a :set line over the settings (opt, noopt, opt!, opt?, opt&, opt<, opt=v, opt+=v, opt-=v, opt^=v, all, all&; several words, all or nothing); a leading verbose adds where values were set",
		Parameters: map[string]Parameter{
			"line":    {Type: "string", Rest: true, Free: true, Source: "ochazuke.settings.complete", Description: "The :set line, the rest of the command: scrolloff=5 nowrap (empty: the settings that differ from their saved values)"},
			"verbose": {Type: "boolean", Description: "Add where each shown value was set (:verbose)"},
			"scope":   {Type: "string", Enum: []string{ScopeSession, ScopeFile}, Description: "Where the line's values are written: session (the default) or file"},
		},
		Positional: []string{"line"}, Result: "JSON value", Run: settingsApply})
	registerSettingsComplete(r)
	// Vim's command names for the line (plan decision 3: settings are
	// global, so the local and global forms are :set). set is also a verb
	// of mode, focus, confirm, appearance and settings, so its row shadows
	// that verb by what follows (ActionAlias.Shadows, plan decision 4):
	// set mode normal is mode set normal.
	for _, a := range []ActionAlias{
		{Alias: "set", Action: "ochazuke.settings.apply", Shadows: true, Note: "Vim's :set; set NOUN ... runs NOUN set"},
		{Alias: "se", Action: "ochazuke.settings.apply", Note: "Vim's :se"},
		{Alias: "setlocal", Action: "ochazuke.settings.apply", Note: "Vim's :setlocal; settings are global, so it is :set"},
		{Alias: "setl", Action: "ochazuke.settings.apply", Note: "Vim's :setl; settings are global, so it is :set"},
		{Alias: "setglobal", Action: "ochazuke.settings.apply", Note: "Vim's :setglobal; settings are global, so it is :set"},
		{Alias: "setg", Action: "ochazuke.settings.apply", Note: "Vim's :setg; settings are global, so it is :set"},
		{Alias: "verbose", Action: "ochazuke.settings.apply", Args: Arguments{"verbose": true}, Note: "Vim's :verbose before a set word: verbose set opt?"},
		{Alias: "verb", Action: "ochazuke.settings.apply", Args: Arguments{"verbose": true}, Note: "Vim's :verb before a set word"},
		{Alias: "options", Action: "ochazuke.settings.edit", Note: "Vim's :options: the Settings page"},
		{Alias: "opt", Action: "ochazuke.settings.edit", Note: "Vim's :opt: the Settings page"},
	} {
		if err := r.AddActionAlias(a); err != nil {
			panic(err)
		}
	}
}
