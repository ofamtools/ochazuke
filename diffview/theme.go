// Copyright (c) 2025 Gaurav Gosain. MIT License (see LICENSE).
// Adapted from tuios/internal/diffview. No Crush code is copied.
package diffview

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// renderTheme resolves semantic ochazuke tones each frame. Named diff.*
// overrides are optional; the existing success/error and text/surface tones
// supply defaults. No fixed RGB or ANSI slots belong to this component.
type renderTheme struct {
	base   [numKinds]lipgloss.Style
	gutter [numKinds]lipgloss.Style
	word   [numKinds]lipgloss.Style
	syntax [NumClasses]lipgloss.Style
}

func hasColor(c color.Color) bool { _, none := c.(lipgloss.NoColor); return c != nil && !none }
func rgb(c color.Color) bool {
	if !hasColor(c) {
		return false
	}
	switch c.(type) {
	case ansi.BasicColor, ansi.IndexedColor:
		return false
	}
	return true
}
func newTheme(s ochazuke.Styles) renderTheme {
	var t renderTheme
	text := s.Tone("diff.text")
	ground := text.GetBackground()
	if !hasColor(ground) {
		ground = s.Tone(ochazuke.ToneChipFocused).GetForeground()
	}
	fg := text.GetForeground()
	if !hasColor(fg) {
		fg = s.Tone(ochazuke.ToneHeader).GetForeground()
	}
	add := s.Tone("diff.add")
	if !hasColor(add.GetForeground()) {
		add = s.Tone(ochazuke.ToneDone)
	}
	del := s.Tone("diff.remove")
	if !hasColor(del.GetForeground()) {
		del = s.Error
	}
	for k := range numKinds {
		base := lipgloss.NewStyle()
		if hasColor(fg) {
			base = base.Foreground(fg)
		}
		gutter := s.Muted
		tint := color.Color(nil)
		switch Kind(k) {
		case Add:
			tint = add.GetForeground()
			gutter = add
		case Delete:
			tint = del.GetForeground()
			gutter = del
		}
		word := base.Bold(true).Underline(true)
		// Small indexed palettes use signs and attributes instead of inventing
		// RGB grounds that the active palette cannot represent reliably.
		if rgb(ground) && rgb(tint) {
			ramp := lipgloss.Blend1D(101, ground, tint)
			base = base.Background(ramp[16])
			word = word.Background(ramp[36])
		} else if hasColor(ground) && rgb(ground) {
			base = base.Background(ground)
			word = word.Background(ground)
		}
		t.base[k], t.gutter[k], t.word[k] = base, gutter, word
	}
	for class := range NumClasses {
		tone := ""
		switch Class(class) {
		case Keyword, Type, Tag:
			tone = ochazuke.ToneWorking
		case Func, Builtin, Heading:
			tone = ochazuke.ToneQueued
		case String:
			tone = ochazuke.ToneDone
		case Number, Preproc, Attr:
			tone = ochazuke.ToneStar
		case Comment, Meta:
			tone = ochazuke.ToneExited
		}
		st := s.Tone(tone)
		custom := s.Tone("diff.syntax." + ClassNames[class])
		if hasColor(custom.GetForeground()) {
			st = custom
		}
		t.syntax[class] = st
	}
	return t
}
