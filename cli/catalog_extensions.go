package cli

import (
	"slices"

	"ofam.dev/ochazuke"
)

// CatalogExtension preserves a mounted app's own words and the local names of
// its nouns. It is descriptive data only; it never changes agent rights.
type CatalogExtension struct {
	Name       string   `json:"name"`
	Version    string   `json:"version,omitempty"`
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
	state.Extensions = append(state.Extensions, CatalogExtension{Name: doc.Name, Version: doc.Version, About: doc.About, AgentRules: doc.AgentRules, Nouns: nouns})
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

// pageActionNouns projects only successfully mounted nouns through this page's
// exact naming map. A producer name alone does not identify a host namespace.
func pageActionNouns(r *ochazuke.Runtime, doc CatalogDoc, local func(string) string) []string {
	if doc.Name == "" {
		return nil
	}
	inst := r.Instances[extensionsID]
	if inst == nil {
		return nil
	}
	state, ok := inst.Data.(*extensionDeclarations)
	if !ok {
		return nil
	}
	var nouns []string
	for _, noun := range doc.Nouns {
		if !slices.ContainsFunc(noun.Verbs, func(v CatalogVerb) bool { return !v.Builtin }) {
			continue
		}
		name := local(noun.Name)
		for _, extension := range state.Extensions {
			if extension.Name != doc.Name {
				continue
			}
			if _, mounted := slices.BinarySearch(extension.Nouns, name); mounted {
				nouns = append(nouns, name)
				break
			}
		}
	}
	slices.Sort(nouns)
	return slices.Compact(nouns)
}
