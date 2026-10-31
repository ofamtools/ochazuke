package cli

import (
	"slices"

	"github.com/unixsuperhero/ochazuke"
)

// CatalogExtension preserves a mounted app's own words and the local names of
// its nouns. It is descriptive data only; it never changes agent rights.
type CatalogExtension struct {
	Name       string   `json:"name"`
	About      string   `json:"about,omitempty"`
	AgentRules string   `json:"agentRules,omitempty"`
	Nouns      []string `json:"nouns"`
}

const extensionsID = "__catalog-extensions"

type extensionDeclarations struct{ Extensions []CatalogExtension }

// MountCatalog is MountNouns over doc.AppNouns, preserving the producing app's
// about and agentRules for catalog discovery and MCP startup guidance.
func MountCatalog(r *ochazuke.Runtime, doc CatalogDoc, h HostMount) MountedNouns {
	out := MountNouns(r, doc.AppNouns(), h)
	if len(out.Nouns) == 0 {
		return out
	}
	var state *extensionDeclarations
	if inst := r.Instances[extensionsID]; inst != nil {
		state, _ = inst.Data.(*extensionDeclarations)
		if state == nil {
			out.Warnings = append(out.Warnings, "extension metadata instance is already in use")
			return out
		}
	} else {
		state = &extensionDeclarations{}
		r.Instance(extensionsID, ochazuke.Kind(ochazuke.AppNoun)).Data = state
	}
	nouns := []string{}
	for _, name := range out.Nouns {
		nouns = append(nouns, name)
	}
	slices.Sort(nouns)
	state.Extensions = append(state.Extensions, CatalogExtension{Name: doc.Name, About: doc.About, AgentRules: doc.AgentRules, Nouns: nouns})
	return out
}

func declaredExtensions(r *ochazuke.Runtime) []CatalogExtension {
	if inst := r.Instances[extensionsID]; inst != nil {
		if state, ok := inst.Data.(*extensionDeclarations); ok {
			out := slices.Clone(state.Extensions)
			for i := range out {
				out[i].Nouns = slices.Clone(out[i].Nouns)
			}
			return out
		}
	}
	return nil
}
