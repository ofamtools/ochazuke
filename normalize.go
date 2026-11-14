package ochazuke

import (
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
