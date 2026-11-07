package ochazuke

import (
	"os"
	"strings"
)

// Appearance has public fields, so the key reads values rather than relying
// on setters. RGBA also handles mutable or non-comparable color.Color values.
type styleKey struct {
	theme, density      string
	dark, accent, plain bool
	r, g, b, a          uint32
	schemes             *SchemeSet
	tones               *ToneRows
	toneGeneration      uint64
	modalDim, unfocused int
	selectionDim        int
	highlights          bool
	unfocusedSelection  bool
	borderSet, glyphSet string
	glyphRows           *GlyphRows
	glyphGeneration     uint64
}

// apply sets the dim and highlight settings the key read on s.
func (k styleKey) apply(s Styles) Styles {
	s = s.WithDims(k.modalDim, k.unfocused).WithSelectionDim(k.selectionDim).WithBackdropHighlights(k.highlights).WithUnfocusedSelection(k.unfocusedSelection).WithBorder(k.borderSet)
	s.glyphSet, s.glyphRows = k.glyphSet, k.glyphRows
	mark := s.Glyph("tab.active")
	s.ActiveTab = s.ActiveTab.Transform(func(text string) string { return mark + " " + text })
	return s
}

// settingsKey reads the dim and highlight settings into a key.
func (r *Runtime) settingsKey() styleKey {
	return styleKey{
		modalDim:           r.dimSetting(ModalDimSetting, DefaultModalDim),
		unfocused:          r.dimSetting(DimUnfocusedSetting, DefaultDimUnfocused),
		selectionDim:       r.dimSetting(DimUnfocusedSelectionSetting, DefaultDimUnfocusedSelection),
		highlights:         r.Settings().String(ModalBackdropHighlightsSetting) != "plain",
		unfocusedSelection: r.Settings().String(UnfocusedSelectionSetting) != "hide",
		borderSet:          r.Settings().String(ChromeBorderSetting),
	}
}

// StylesFor is NewStyles(a) with the runtime's dim and highlight settings
// (modal-dim, dim-unfocused, dim-unfocused-selection, modal-highlights,
// unfocused-selection), for a component drawn in an appearance of its
// own: its Selection follows the user's settings as the Model's does.
func (r *Runtime) StylesFor(a Appearance) Styles {
	k := r.settingsKey()
	k.glyphSet = a.resolvedGlyphs(r.Settings().String(ChromeGlyphsSetting))
	k.glyphRows = a.Glyphs
	return k.apply(NewStyles(a))
}

type modelStyles struct {
	valid         bool
	key           styleKey
	base          Styles
	chrome, roles string
	styled        Styles
	// Drawn memoizes the styles of a surface drawn in a mode that is not
	// the live one (StylesAt: the page beneath a modal), so a frame that
	// draws the page in the mode the modal opened over and the chrome in
	// the live mode rebuilds neither.
	Drawn struct {
		Chrome, Roles string
		Styled        Styles
	}
}

// baseOf is the Model's styles before the mode chrome: the appearance
// with the dim, highlight and glyph settings applied.
func (c *modelStyles) baseOf(m *Model) Styles {
	a, r := m.Appearance, m.Runtime
	k := r.settingsKey()
	k.theme, k.density, k.dark = a.Theme, a.Density, a.Dark
	k.schemes, k.tones, k.plain = a.Schemes, a.AppTones, os.Getenv("NO_COLOR") != ""
	k.glyphSet = a.resolvedGlyphs(r.Settings().String(ChromeGlyphsSetting))
	k.glyphRows = a.Glyphs
	if a.Glyphs != nil {
		k.glyphGeneration = a.Glyphs.generation
	}
	if a.Accent != nil {
		k.accent = true
		k.r, k.g, k.b, k.a = a.Accent.RGBA()
	}
	if a.AppTones != nil {
		k.toneGeneration = a.AppTones.generation
	}
	if !c.valid || c.key != k {
		c.base = k.apply(NewStyles(*a))
		c.valid, c.key = true, k
		c.chrome = "" // force the mode-specific styles to be rebuilt too
		c.Drawn.Chrome = ""
	}
	return c.base
}

// get is the Model's styles in the live mode.
func (c *modelStyles) get(m *Model) Styles {
	base := c.baseOf(m)
	roles := m.Runtime.modeRolesOf(m.Runtime.Mode)
	roleKey, chrome := strings.Join(roles, "\x00"), m.Runtime.ChromeLevel()
	if c.chrome != chrome || c.roles != roleKey {
		c.styled = base.WithChrome(chrome, roles)
		c.chrome, c.roles = chrome, roleKey
	}
	return c.styled
}

// at is the Model's styles in mode, the mode a surface is drawn in: the
// live styles when it is the live mode's, else the Drawn memo.
func (c *modelStyles) at(m *Model, mode ModeState) Styles {
	live := c.get(m)
	roles := m.Runtime.modeRolesOf(mode)
	roleKey, chrome := strings.Join(roles, "\x00"), m.Runtime.ChromeLevel()
	if roleKey == c.roles {
		return live
	}
	if c.Drawn.Chrome != chrome || c.Drawn.Roles != roleKey {
		c.Drawn.Styled = c.base.WithChrome(chrome, roles)
		c.Drawn.Chrome, c.Drawn.Roles = chrome, roleKey
	}
	return c.Drawn.Styled
}
