package cli

// CatalogOption declares app text shared by Catalog and New's shell catalog.
type CatalogOption func(*CatalogDoc)

// About declares the app's purpose in one short paragraph.
func About(text string) CatalogOption { return func(d *CatalogDoc) { d.About = text } }

// AgentRules declares the app's guidance to agents. Text does not grant rights.
func AgentRules(text string) CatalogOption { return func(d *CatalogDoc) { d.AgentRules = text } }
