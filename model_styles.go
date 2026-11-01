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
}

// apply sets the dim and highlight settings the key read on s.
func (k styleKey) apply(s Styles) Styles {
	return s.WithDims(k.modalDim, k.unfocused).WithSelectionDim(k.selectionDim).WithBackdropHighlights(k.highlights).WithUnfocusedSelection(k.unfocusedSelection)
}

// settingsKey reads the dim and highlight settings into a key.
func (r *Runtime) settingsKey() styleKey {
	return styleKey{
		modalDim:           r.dimSetting(ModalDimSetting, DefaultModalDim),
		unfocused:          r.dimSetting(DimUnfocusedSetting, DefaultDimUnfocused),
		selectionDim:       r.dimSetting(DimUnfocusedSelectionSetting, DefaultDimUnfocusedSelection),
		highlights:         r.Settings().String(ModalBackdropHighlightsSetting) != "plain",
		unfocusedSelection: r.Settings().String(UnfocusedSelectionSetting) != "hide",
	}
}

// StylesFor is NewStyles(a) with the runtime's dim and highlight settings
// (modal-dim, dim-unfocused, dim-unfocused-selection, modal-highlights,
// unfocused-selection), for a component drawn in an appearance of its
// own: its Selection follows the user's settings as the Model's does.
func (r *Runtime) StylesFor(a Appearance) Styles {
	return r.settingsKey().apply(NewStyles(a))
}

type modelStyles struct {
	valid         bool
	key           styleKey
	base          Styles
	chrome, roles string
	styled        Styles
}

func (c *modelStyles) get(m *Model) Styles {
	a, r := m.Appearance, m.Runtime
	k := r.settingsKey()
	k.theme, k.density, k.dark = a.Theme, a.Density, a.Dark
	k.schemes, k.tones, k.plain = a.Schemes, a.AppTones, os.Getenv("NO_COLOR") != ""
	if a.Accent != nil {
		k.accent = true
		k.r, k.g, k.b, k.a = a.Accent.RGBA()
	}
	if a.AppTones != nil {
		k.toneGeneration = a.AppTones.generation
	}
	roles := r.modeRoles()
	roleKey, chrome := strings.Join(roles, "\x00"), r.ChromeLevel()
	if !c.valid || c.key != k {
		c.base = k.apply(NewStyles(*a))
		c.valid, c.key = true, k
		c.chrome = "" // force the mode-specific styles to be rebuilt too
	}
	if c.chrome != chrome || c.roles != roleKey {
		c.styled = c.base.WithChrome(chrome, roles)
		c.chrome, c.roles = chrome, roleKey
	}
	return c.styled
}
