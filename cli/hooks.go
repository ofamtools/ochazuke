package cli

import (
	"github.com/unixsuperhero/ochazuke"
)

// catalogHooks are the runtime's default hook rows with each verb in its
// canonical spelling, and the events it declares that are not the
// framework's.
func catalogHooks(r *ochazuke.Runtime) ([]ochazuke.HookRow, []ochazuke.EventSpec) {
	var rows []ochazuke.HookRow
	for _, row := range r.DefaultHooks() {
		row.Do = r.CanonicalKey(row.Do)
		row.Source = ""
		rows = append(rows, row)
	}
	var events []ochazuke.EventSpec
	for _, e := range r.Events() {
		if e.Source == ochazuke.FrameworkNamespace {
			continue
		}
		e.Source = ""
		events = append(events, e)
	}
	return rows, events
}

// CatalogHooks are doc's default hook rows as a host adds them with
// Runtime.AddDefaultHooks: each verb's noun mapped to the host's (noun, as
// CatalogSegments maps it; nil keeps it) and each row's source set to
// source (the extension's name). They run from the host's events, and the
// host's user drops or replaces them in the host's hooks.json.
func CatalogHooks(doc CatalogDoc, source string, noun func(string) string) []ochazuke.HookRow {
	var out []ochazuke.HookRow
	for _, row := range doc.Hooks {
		if noun != nil {
			n, v := ochazuke.SplitKey(row.Do)
			row.Do = noun(n) + "." + v
		}
		row.Source = source
		out = append(out, row)
	}
	return out
}

// CatalogEvents are doc's declared events as a host declares them with
// Runtime.DeclareEvent, each with source set to source.
func CatalogEvents(doc CatalogDoc, source string) []ochazuke.EventSpec {
	var out []ochazuke.EventSpec
	for _, e := range doc.Events {
		e.Source = source
		out = append(out, e)
	}
	return out
}

// MountCatalogHooks declares doc's events and adds its default hook rows to
// r (CatalogEvents, CatalogHooks): the one call a host makes per extension.
func MountCatalogHooks(r *ochazuke.Runtime, doc CatalogDoc, source string, noun func(string) string) error {
	for _, e := range CatalogEvents(doc, source) {
		if err := r.DeclareEvent(e); err != nil {
			return err
		}
	}
	return r.AddDefaultHooks(CatalogHooks(doc, source, noun)...)
}
