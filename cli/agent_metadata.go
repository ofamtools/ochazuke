package cli

import "ofam.dev/ochazuke"

// CatalogOption declares app text shared by Catalog and New's shell catalog.
type CatalogOption func(*CatalogDoc)

// About declares the app's purpose in one short paragraph.
func About(text string) CatalogOption { return func(d *CatalogDoc) { d.About = text } }

// AgentRules declares the app's guidance to agents. Text does not grant rights.
func AgentRules(text string) CatalogOption { return func(d *CatalogDoc) { d.AgentRules = text } }

// PeerBindings declares host-only rows in bindings.json spelling. The producer
// does not validate or install these rows; an older host ignores the field.
func PeerBindings(rows ...ochazuke.Binding) CatalogOption {
	return func(d *CatalogDoc) { d.PeerBindings = append(d.PeerBindings, rows...) }
}
