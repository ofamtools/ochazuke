package ochazuke

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// The Normalize kinds (Parameter.Normalize).
const (
	// NormalizeTrim drops leading and trailing blanks.
	NormalizeTrim = "trim"
	// NormalizeSlug lowers the text and joins its words with dashes
	// ("My Ext" is my-ext).
	NormalizeSlug = "slug"
	// NormalizeEscape writes line ends and tabs as \n, \r and \t, so a
	// pasted value with them fits one line.
	NormalizeEscape = "escape"
	// NormalizeCoerce reads a word as the parameter's type ("3" for an
	// integer is 3, "yes" for a boolean is true).
	NormalizeCoerce = "coerce"
)

// normalizers are the Normalize kinds, each a function of the value and
// the parameter's type.
var normalizers = map[string]func(v any, typ string) any{
	NormalizeTrim:   onText(strings.TrimSpace),
	NormalizeSlug:   onText(slug),
	NormalizeEscape: onText(strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`).Replace),
	NormalizeCoerce: coerceWord,
}

func onText(fn func(string) string) func(any, string) any {
	return func(v any, _ string) any {
		if s, ok := v.(string); ok {
			return fn(s)
		}
		return v
	}
}

// slug is text as a slug: lower case, runs of anything but letters and
// digits a single dash, no dash at either end.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// coerceWord reads a text value as typ, with the parser the palette and
// the CLI read words with (kindOf); a word it cannot read stays text.
func coerceWord(v any, typ string) any {
	s, ok := v.(string)
	if k := kindOf(typ); ok && k.parse != nil {
		if typed, ok := k.parse(strings.TrimSpace(s)); ok {
			return typed
		}
	}
	return v
}

// normalizeArguments applies each argument's parameter Normalize kinds in
// order; an unknown kind is skipped (a declaration error the reference
// shows, never a refusal of the request).
func normalizeArguments(op Operation, args Arguments) {
	for name, v := range args {
		p, ok := op.Parameters[name]
		if !ok || len(p.Normalize) == 0 {
			continue
		}
		for _, kind := range p.Normalize {
			if fn := normalizers[kind]; fn != nil {
				v = fn(v, p.Type)
			}
		}
		args[name] = v
	}
}

// OutOfRangeSetting says what a number outside its parameter's Min and Max
// does (no value is refused for its size): OutOfRangeClamp uses the
// nearest allowed value and says so, OutOfRangeKeep stores it as typed,
// OutOfRangeReopen asks for the value again, naming the range.
const OutOfRangeSetting = "values.out-of-range"

// The values of OutOfRangeSetting.
const (
	OutOfRangeClamp  = "clamp"
	OutOfRangeKeep   = "keep"
	OutOfRangeReopen = "reopen"
)

// rangeArguments applies each number argument's Min and Max under
// values.out-of-range: clamp swaps the value in args for the nearest
// allowed one with a notice (fallback.notice), keep leaves it, reopen drops
// it and answers the MissingParameterError that asks for it, naming the
// range (needs / value). A value inside the range, or a parameter with no
// range, is untouched.
func (r *Runtime) rangeArguments(op Operation, args Arguments) error {
	mode := OutOfRangeClamp
	if s := r.Settings(); s != nil && s.Has(OutOfRangeSetting) {
		mode = s.String(OutOfRangeSetting)
	}
	for name, v := range args {
		p, ok := op.Parameters[name]
		if !ok || p.Min == nil && p.Max == nil {
			continue
		}
		n, ok := numberOf(v)
		if !ok {
			continue
		}
		bound, round := n, math.Ceil
		switch {
		case p.Min != nil && n < *p.Min:
			bound = *p.Min
		case p.Max != nil && n > *p.Max:
			bound, round = *p.Max, math.Floor
		default:
			continue
		}
		switch mode {
		case OutOfRangeKeep:
		case OutOfRangeReopen:
			delete(args, name)
			return &MissingParameterError{Param: name, Reason: fmt.Sprintf("%v is not %s", v, p.rangeText())}
		default:
			if p.Type == "integer" {
				args[name] = int(round(bound))
			} else {
				args[name] = bound
			}
			r.notice("%s: used %v for %v (%s)", name, args[name], v, p.rangeText())
		}
	}
	return nil
}

// rangeText says the range a parameter's Min and Max allow.
func (p Parameter) rangeText() string {
	switch {
	case p.Min != nil && p.Max != nil:
		return fmt.Sprintf("between %v and %v", *p.Min, *p.Max)
	case p.Min != nil:
		return fmt.Sprintf("at least %v", *p.Min)
	}
	return fmt.Sprintf("at most %v", *p.Max)
}

// numberOf is a number value (an int or a float64) as a float64.
func numberOf(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
