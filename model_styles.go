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
	highlights          bool
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
	k := styleKey{
		theme: a.Theme, density: a.Density, dark: a.Dark,
		schemes: a.Schemes, tones: a.AppTones, plain: os.Getenv("NO_COLOR") != "",
		modalDim:   r.dimSetting(ModalDimSetting, DefaultModalDim),
		unfocused:  r.dimSetting(DimUnfocusedSetting, DefaultDimUnfocused),
		highlights: r.Settings().String(ModalBackdropHighlightsSetting) != "plain",
	}
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
		c.base = NewStyles(*a).WithDims(k.modalDim, k.unfocused).WithBackdropHighlights(k.highlights)
		c.valid, c.key = true, k
		c.chrome = "" // force the mode-specific styles to be rebuilt too
	}
	if c.chrome != chrome || c.roles != roleKey {
		c.styled = c.base.WithChrome(chrome, roles)
		c.chrome, c.roles = chrome, roleKey
	}
	return c.styled
}
