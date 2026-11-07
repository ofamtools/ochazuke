package ochazuke

import (
	"encoding/json"
	"strconv"
	"strings"
)

// paramKind is everything the routes know about one parameter type: how
// each reads text as its value, which Go values the runtime accepts, and
// an example. Routes keep their own error wording and call these.
type paramKind struct {
	// form: a form field can hold it (never an array or an object).
	form bool
	// action: an action in actions.json may declare it.
	action bool
	// word: the palette reads a word as its value.
	word bool
	// trimmed: a form and the record editor read the text without its
	// surrounding spaces.
	trimmed bool
	// must finishes "NAME must be " when the form or palette cannot read
	// the text.
	must string
	// phrase is the record editor's reason for text parse refuses.
	phrase string
	// parse is the strict reading of text; nil keeps the text as it is.
	parse func(text string) (any, bool)
	// fromForm overrides parse for the args form (a boolean's form reads
	// anything but "true" as false); nil uses parse.
	fromForm func(text string) (any, bool)
	// flag is the CLI's reading of a flag value; nil keeps the text.
	flag func(text string) (any, error)
	// accepts is the runtime's check of a Go value (checkArguments); nil
	// accepts nothing.
	accepts func(v any) bool
	// sample is an example's value in agent metadata; nil is "<NAME>".
	sample any
}

func parseInteger(text string) (any, bool) {
	n, err := strconv.Atoi(text)
	return n, err == nil
}

func parseNumber(text string) (any, bool) {
	n, err := strconv.ParseFloat(text, 64)
	return n, err == nil
}

func parseBoolean(text string) (any, bool) {
	return text == "true", text == "true" || text == "false"
}

func isString(v any) bool { _, ok := v.(string); return ok }

// paramKinds is the table, by Parameter.Type. A type it lacks (duration,
// whatever an app invents) is read by no route and accepts nothing.
var paramKinds = map[string]paramKind{
	"":                {form: true, word: true, accepts: nil},
	"string":          {form: true, action: true, word: true, accepts: isString},
	PathParameterType: {form: true, action: true, word: true, accepts: isString},
	AnyParameterType:  {form: true, word: true, accepts: func(any) bool { return true }},
	"integer": {
		form: true, action: true, word: true, trimmed: true, must: "integer", phrase: "a whole number",
		parse: parseInteger,
		flag:  func(text string) (any, error) { return strconv.Atoi(text) },
		accepts: func(v any) bool {
			switch n := v.(type) {
			case int:
				return true
			case float64:
				return n == float64(int(n))
			}
			return false
		},
		sample: 1,
	},
	"number": {
		form: true, word: true, trimmed: true, must: "a number", phrase: "a number",
		parse: parseNumber,
		flag:  func(text string) (any, error) { return strconv.ParseFloat(text, 64) },
		accepts: func(v any) bool {
			switch v.(type) {
			case int, float64:
				return true
			}
			return false
		},
		sample: 1,
	},
	"boolean": {
		form: true, action: true, word: true, must: "boolean (true or false)", phrase: "true or false",
		parse:    parseBoolean,
		fromForm: func(text string) (any, bool) { return text == "true", true },
		flag:     func(text string) (any, error) { return strconv.ParseBool(text) },
		accepts:  func(v any) bool { _, ok := v.(bool); return ok },
		sample:   true,
	},
	"array": {
		flag: jsonFlag,
		accepts: func(v any) bool {
			switch v.(type) {
			case []any, []Predicate:
				return true
			}
			return false
		},
	},
	"object": {
		flag: jsonFlag,
		accepts: func(v any) bool {
			switch v.(type) {
			case map[string]any, Arguments, Request, *Request:
				return true
			}
			return false
		},
	},
	"duration": {sample: "1s"},
}

func jsonFlag(text string) (any, error) {
	var out any
	err := json.Unmarshal([]byte(text), &out)
	return out, err
}

// kindOf is the parameter type's row; a type with none reads as the zero
// row.
func kindOf(typ string) paramKind { return paramKinds[typ] }

// ParseFlagValue is a CLI flag's text as its parameter type's value:
// strconv's reading for numbers and booleans, JSON for an array or an
// object, the text itself for any other type.
func ParseFlagValue(typ, text string) (any, error) {
	if k := kindOf(typ); k.flag != nil {
		return k.flag(text)
	}
	return text, nil
}

// readForm is a form field's text as the type's value; ok is false when it
// is not of the type. A type with no parse keeps the text.
func (k paramKind) readForm(text string) (any, bool) {
	if k.trimmed {
		text = strings.TrimSpace(text)
	}
	switch {
	case k.fromForm != nil:
		return k.fromForm(text)
	case k.parse != nil:
		return k.parse(text)
	}
	return text, true
}
