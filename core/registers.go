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
// final line terminator.
type RegisterValue struct {
	Text     string `json:"text"`
	Linewise bool   `json:"linewise,omitempty"`
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
	return v, ok
}

func (r *Registers) write(reg rune, v RegisterValue) {
	m := maps.Clone(r.m)
	if m == nil {
		m = map[rune]RegisterValue{}
	}
	m[reg] = v
	r.m = m
}

// appended returns the value stored after appending v to reg's content.
func (r Registers) appended(reg rune, v RegisterValue) RegisterValue {
	old, ok := r.m[reg]
	if !ok {
		return v
	}
	if old.Linewise || v.Linewise {
		return RegisterValue{Text: old.Text + "\n" + v.Text, Linewise: true}
	}
	return RegisterValue{Text: old.Text + v.Text}
}

// Set stores text in reg directly, without touching other registers. An
// uppercase name appends. The black hole discards the write.
func (r *Registers) Set(reg rune, text string, linewise bool) {
	reg = normalizeRegister(reg)
	if reg == RegBlackHole {
		return
	}
	v := RegisterValue{Text: text, Linewise: linewise}
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
	reg = normalizeRegister(reg)
	if reg == RegBlackHole {
		return
	}
	v := RegisterValue{Text: text, Linewise: linewise}
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
	reg = normalizeRegister(reg)
	if reg == RegBlackHole {
		return
	}
	v := RegisterValue{Text: text, Linewise: linewise}
	named := IsNamed(reg)
	if named {
		v = r.setNamed(reg, v)
	} else if isSpecialRegister(reg) {
		r.write(reg, v)
	}
	if linewise || strings.Contains(text, "\n") {
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
		m['1'] = RegisterValue{Text: text, Linewise: linewise}
		r.m = m
	} else if !named {
		r.write(RegSmallDel, v)
	}
	r.write(RegUnnamed, v)
}

// SetSpecial stores a read-mostly register: '%' (path or ref), '/' (last
// search), ':' (last command), '+' (clipboard), '*' (selection) or '.'
// (last insert). It reports false for any other name.
func (r *Registers) SetSpecial(reg rune, text string) bool {
	if !isSpecialRegister(reg) {
		return false
	}
	r.write(reg, RegisterValue{Text: text})
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
	out := maps.Clone(r.m)
	if out == nil {
		out = map[rune]RegisterValue{}
	}
	return out
}

// Clear empties the bank.
func (r *Registers) Clear() { r.m = nil }
