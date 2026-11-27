package core

import (
	"maps"
	"slices"
	"strings"
)

// Register names with a fixed meaning. Named registers are 'a'..'z'; an
// uppercase name 'A'..'Z' appends to its lowercase register.
const (
	RegUnnamed   = '"' // last yank or delete
	RegYank      = '0' // last yank
	RegSmallDel  = '-' // last delete within one line, when no register was named
	RegBlackHole = '_' // writes are discarded, reads are empty
	RegPath      = '%' // current path or ref
	RegSearch    = '/' // last search
	RegCommand   = ':' // last command line
	RegClipboard = '+' // system clipboard (the host syncs it)
	RegSelection = '*' // primary selection (the host syncs it)
	RegDot       = '.' // last inserted text
)

// RegisterValue is the content of one register. Linewise text excludes the
// final line terminator. A register may carry a plain text representation
// alongside optional structured metadata (refs, field, cut flag, source,
// extensible meta) so paste targets can consume either form.
type RegisterValue struct {
	Text     string         `json:"text"`
	Linewise bool           `json:"linewise,omitempty"`
	Kind     string         `json:"kind,omitempty"`   // e.g. "refs", "field", "text", "code", "path"
	Refs     []Ref          `json:"refs,omitempty"`   // 1 or N visually-selected records
	Field    string         `json:"field,omitempty"`  // set when user explicitly picked a field (e.g. "title", "markdown")
	Cut      bool           `json:"cut,omitempty"`    // true when cut/move, false when copy/yank
	Source   string         `json:"source,omitempty"` // origin scope/screen
	Meta     map[string]any `json:"meta,omitempty"`   // optional extensible payload
}

// HasRefs reports whether the register carries at least one structured Ref.
func (v RegisterValue) HasRefs() bool { return len(v.Refs) > 0 }

// Clone returns a copy of v with independent Refs and Meta storage.
func (v RegisterValue) Clone() RegisterValue {
	v.Refs = slices.Clone(v.Refs)
	v.Meta = cloneMeta(v.Meta)
	return v
}

func cloneMeta(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneAny(v)
	}
	return out
}

func cloneAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMeta(x)
	case []any:
		if x == nil {
			return []any(nil)
		}
		out := make([]any, len(x))
		for i, elem := range x {
			out[i] = cloneAny(elem)
		}
		return out
	case []string:
		return slices.Clone(x)
	case []Ref:
		return slices.Clone(x)
	default:
		return v
	}
}

func mergeMeta(old, add map[string]any) map[string]any {
	if len(old) == 0 && len(add) == 0 {
		return nil
	}
	out := make(map[string]any, len(old)+len(add))
	for k, v := range old {
		out[k] = cloneAny(v)
	}
	for k, v := range add {
		out[k] = cloneAny(v)
	}
	return out
}

// Registers is a Vim-style register bank. Its zero value is empty and ready
// to use. Writes are copy-on-write, so a copied Registers value is an
// independent snapshot: core.State-style code can store it by value.
type Registers struct {
	m map[rune]RegisterValue
}

// IsNamed reports whether reg is 'a'..'z' or 'A'..'Z'.
func IsNamed(reg rune) bool { return reg >= 'a' && reg <= 'z' || reg >= 'A' && reg <= 'Z' }

func isSpecialRegister(reg rune) bool {
	switch reg {
	case RegPath, RegSearch, RegCommand, RegClipboard, RegSelection, RegDot:
		return true
	}
	return false
}

// normalize maps 0 (no register given) to the unnamed register.
func normalizeRegister(reg rune) rune {
	if reg == 0 {
		return RegUnnamed
	}
	return reg
}

func lower(reg rune) rune {
	if reg >= 'A' && reg <= 'Z' {
		return reg + ('a' - 'A')
	}
	return reg
}

// Get returns the content of reg. Uppercase names read their lowercase
// register; 0 reads the unnamed register; the black hole is always empty.
func (r Registers) Get(reg rune) (RegisterValue, bool) {
	reg = lower(normalizeRegister(reg))
	if reg == RegBlackHole {
		return RegisterValue{}, false
	}
	v, ok := r.m[reg]
	if !ok {
		return RegisterValue{}, false
	}
	return v.Clone(), true
}

func (r *Registers) write(reg rune, v RegisterValue) {
	m := maps.Clone(r.m)
	if m == nil {
		m = map[rune]RegisterValue{}
	}
	m[reg] = v.Clone()
	r.m = m
}

func isRefsPair(a, b RegisterValue) bool {
	if a.Kind == "refs" && b.Kind == "refs" {
		return true
	}
	return a.Kind == "" && b.Kind == "" && a.HasRefs() && b.HasRefs()
}

func joinAppendedText(a, b string, linewise, sepNewline bool) string {
	if linewise || (sepNewline && a != "" && b != "") {
		return a + "\n" + b
	}
	return a + b
}

func commonString(a, b string) string {
	if a == b {
		return a
	}
	return ""
}

func mergeRefs(a, b []Ref) []Ref {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]Ref, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}

// appended returns the value stored after appending v to reg's content.
func (r Registers) appended(reg rune, v RegisterValue) RegisterValue {
	old, ok := r.m[reg]
	if !ok {
		return v.Clone()
	}
	linewise := old.Linewise || v.Linewise
	bothRefs := isRefsPair(old, v)
	out := RegisterValue{
		Text:     joinAppendedText(old.Text, v.Text, linewise, bothRefs),
		Linewise: linewise,
		Cut:      old.Cut && v.Cut,
	}
	if old.Kind == v.Kind {
		out.Kind = old.Kind
		out.Field = commonString(old.Field, v.Field)
		out.Source = commonString(old.Source, v.Source)
		out.Meta = mergeMeta(old.Meta, v.Meta)
	}
	if bothRefs {
		out.Refs = mergeRefs(old.Refs, v.Refs)
	}
	return out
}

// Set stores text in reg directly, without touching other registers. An
// uppercase name appends. The black hole discards the write.
func (r *Registers) Set(reg rune, text string, linewise bool) {
	r.SetValue(reg, RegisterValue{Text: text, Linewise: linewise})
}

// SetValue stores v in reg directly, without touching other registers. An
// uppercase name appends. The black hole discards the write. Refs and Meta
// are cloned on write so Registers remains copy-on-write and immutable to callers.
func (r *Registers) SetValue(reg rune, v RegisterValue) {
	reg = normalizeRegister(reg)
	if reg == RegBlackHole {
		return
	}
	v = v.Clone()
	if reg >= 'A' && reg <= 'Z' {
		reg = lower(reg)
		v = r.appended(reg, v)
	}
	r.write(reg, v)
}

// setNamed writes a named register (appending for uppercase) and returns
// the stored value.
func (r *Registers) setNamed(reg rune, v RegisterValue) RegisterValue {
	if reg >= 'A' && reg <= 'Z' {
		reg = lower(reg)
		v = r.appended(reg, v)
	}
	r.write(reg, v)
	return v
}

// Yank records a yank: the yank register "0 always receives the text; a
// named register (appending for 'A'..'Z') or a special register receives
// it when given; and the unnamed register "" mirrors the register written.
func (r *Registers) Yank(reg rune, text string, linewise bool) {
	r.YankValue(reg, RegisterValue{Text: text, Linewise: linewise})
}

// YankValue records a typed yank: the yank register "0 always receives v
// (with Cut cleared); a named register (appending for 'A'..'Z', merging Text
// and Refs when both are "refs") or a special register receives it when given;
// and the unnamed register "" mirrors the register written.
func (r *Registers) YankValue(reg rune, v RegisterValue) {
	reg = normalizeRegister(reg)
	if reg == RegBlackHole {
		return
	}
	v = v.Clone()
	v.Cut = false
	r.write(RegYank, v)
	switch {
	case IsNamed(reg):
		v = r.setNamed(reg, v)
	case isSpecialRegister(reg):
		r.write(reg, v)
	}
	r.write(RegUnnamed, v)
}

// Delete records a delete: the unnamed register "" receives the text (or
// the named register's appended result) and "0 keeps the last yank.
// Multi-line or linewise deletes shift "1.."9; small deletes into no named
// register go to "-.
func (r *Registers) Delete(reg rune, text string, linewise bool) {
	r.DeleteValue(reg, RegisterValue{Text: text, Linewise: linewise})
}

// DeleteValue records a typed delete: it marks v.Cut true, preserves "0,
// updates any named/special register and "", shifts "1.."9 for linewise,
// multi-line, or structured-ref deletes, and writes unnamed small plain-text
// deletes to "-.
func (r *Registers) DeleteValue(reg rune, v RegisterValue) {
	reg = normalizeRegister(reg)
	if reg == RegBlackHole {
		return
	}
	orig := v.Clone()
	orig.Cut = true
	stored := orig
	named := IsNamed(reg)
	if named {
		stored = r.setNamed(reg, orig)
	} else if isSpecialRegister(reg) {
		r.write(reg, orig)
	}
	if orig.Linewise || strings.Contains(orig.Text, "\n") || orig.HasRefs() {
		m := maps.Clone(r.m)
		if m == nil {
			m = map[rune]RegisterValue{}
		}
		for n := '9'; n > '1'; n-- {
			if prev, ok := m[n-1]; ok {
				m[n] = prev
			} else {
				delete(m, n)
			}
		}
		m['1'] = orig.Clone()
		r.m = m
	} else if !named {
		r.write(RegSmallDel, stored)
	}
	r.write(RegUnnamed, stored)
}

// SetSpecial stores a read-mostly register: '%' (path or ref), '/' (last
// search), ':' (last command), '+' (clipboard), '*' (selection) or '.'
// (last insert). It reports false for any other name.
func (r *Registers) SetSpecial(reg rune, text string) bool {
	return r.SetSpecialValue(reg, RegisterValue{Text: text})
}

// SetSpecialValue stores a typed value in a read-mostly special register.
// It reports false for any non-special register name.
func (r *Registers) SetSpecialValue(reg rune, v RegisterValue) bool {
	if !isSpecialRegister(reg) {
		return false
	}
	r.write(reg, v)
	return true
}

// Names returns the registers holding content, sorted.
func (r Registers) Names() []rune {
	out := slices.Collect(maps.Keys(r.m))
	slices.Sort(out)
	return out
}

// Snapshot returns a copy of every register.
func (r Registers) Snapshot() map[rune]RegisterValue {
	out := make(map[rune]RegisterValue, len(r.m))
	for k, v := range r.m {
		out[k] = v.Clone()
	}
	return out
}

// Clear empties the bank.
func (r *Registers) Clear() { r.m = nil }
