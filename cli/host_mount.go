package cli

import (
	"fmt"
	gomaps "maps"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

// HostMount says how a host (mtm, nabe) mounts an extension's catalog
// nouns into its own runtime with MountNouns: the host's name for each
// catalog noun and the handler that forwards a verb to the extension.
// Everything else comes from the catalog as data, so a noun with props
// and generated standard verbs mounts with no host code per noun.
type HostMount struct {
	// Local is the host's name for a catalog noun (cli.Namespace(ext)
	// and the noun, say), "" to leave that noun out.
	Local func(noun string) string
	// Proxy is the handler of one mounted verb: any handler
	// ochazuke.NounBuilder.Verb takes (it runs the verb in the extension).
	Proxy func(noun string, v CatalogVerb) any
	// Param, when set, rewrites each parameter of a mounted verb and
	// each prop's parameter after the default rewrite (MountParameter),
	// for a host that mounts its own choice sources.
	Param func(p ochazuke.Parameter) ochazuke.Parameter
}

// MountedNouns is what MountNouns mounted: each catalog noun's host
// name (only the nouns mounted) and the warnings for what it skipped.
type MountedNouns struct {
	Nouns    map[string]string
	Warnings []string
}

// MountNouns registers a catalog's nouns (CatalogDoc.AppNouns, usually) in
// the host's runtime r, as data:
//
//   - each noun under its host name (HostMount.Local), its columns and its
//     props (CatalogNoun.Props: kind, roles, key, fallback and the rest),
//     a reference prop pointing at the host name of the noun it names;
//   - each verb as a proxy (HostMount.Proxy) with the catalog verb's
//     description, confirm, parameters, positional order, quiet, many,
//     repeatable, rows, history kind and standard mark
//     (ochazuke.Operation.Standard), so the host's palette shows a
//     generated verb as it shows its own standard verbs (setting
//     palette.standard-verbs) and prop-shaped requests (NOUN PROP VALUE)
//     run its forwarded get and set;
//   - each verb's aliases (CatalogVerb.Aliases: patch, preset aliases
//     like done = set {prop: status, value: done}, deprecated names) under
//     the host names. A bare alias (w!) becomes NOUN.ALIAS: a host never
//     takes a global name for an extension's shortcut.
//
// A prop's availability guard (When) names a guard only the extension can
// run, so the host treats the prop as available and the extension refuses
// it, as it does for verbs. A noun whose host name is taken, and a prop,
// verb or alias the host's runtime refuses (a clash, a bad name), is
// skipped with a warning; mounting never panics. A framework short-name
// alias does not own the name: the mounted noun claims its exact host name.
func MountNouns(r *ochazuke.Runtime, nouns []CatalogNoun, h HostMount) MountedNouns {
	out := MountedNouns{Nouns: map[string]string{}}
	warn := func(format string, a ...any) { out.Warnings = append(out.Warnings, fmt.Sprintf(format, a...)) }
	// Every host name first, so a reference or an alias may name a noun
	// mounted after its own.
	var mounting []CatalogNoun
	for _, n := range nouns {
		local := h.Local(n.Name)
		switch {
		case local == "":
			continue
		case r.Nouns[local] != nil:
			warn("noun %s skipped: the host already has a noun %s", n.Name, local)
			continue
		}
		out.Nouns[n.Name] = local
		mounting = append(mounting, n)
	}
	param := func(p ochazuke.Parameter) ochazuke.Parameter {
		p = MountParameter(p, out.Nouns)
		if h.Param != nil {
			p = h.Param(p)
		}
		return p
	}
	for _, n := range mounting {
		local := out.Nouns[n.Name]
		var b *ochazuke.NounBuilder
		if err := try(func() { b = r.DefineOwn(local) }); err != nil {
			warn("noun %s skipped: %v", n.Name, err)
			delete(out.Nouns, n.Name)
			continue
		}
		b.About(n.About)
		if len(n.Columns) > 0 {
			b.Columns(n.Columns...)
		}
		for _, p := range n.Props {
			p.Parameter = param(p.Parameter)
			p.When = ""
			if err := try(func() { b.Props(p) }); err != nil {
				warn("%s prop %s skipped: %v", n.Name, p.Name, err)
			}
		}
		if len(n.PropsOff) > 0 {
			if err := try(func() { b.PropsOff(n.PropsOff...) }); err != nil {
				warn("%s propsOff skipped: %v", n.Name, err)
			}
		}
		for _, f := range n.Filters {
			f.Noun = local
			if err := try(func() { b.Filters(f) }); err != nil {
				warn("%s filter %s skipped: %v", n.Name, f.Name, err)
			}
		}
		for _, v := range n.Verbs {
			if err := mountVerb(r, b, local, n.Name, v, h, out.Nouns, param, func(format string, a ...any) {
				warn("%s %s: %s", n.Name, v.Name, fmt.Sprintf(format, a...))
			}); err != nil {
				warn("%s %s skipped: %v", n.Name, v.Name, err)
			}
		}
	}
	for _, n := range mounting {
		local, ok := out.Nouns[n.Name]
		if !ok {
			continue
		}
		for _, v := range n.Verbs {
			action := local + "." + v.Name
			if _, ok := r.Operations[action]; !ok {
				continue // the verb was skipped
			}
			for _, a := range v.Aliases {
				if err := mountAlias(r, a, action, out.Nouns); err != nil {
					warn("%s %s alias %s skipped: %v", n.Name, v.Name, a.Name(), err)
				}
			}
		}
	}
	return out
}

// mountVerb registers one catalog verb of a mounted noun as a proxy; an
// error skips it, note reports what it mounted differently.
func mountVerb(r *ochazuke.Runtime, b *ochazuke.NounBuilder, local, noun string, v CatalogVerb, h HostMount, nouns map[string]string, param func(ochazuke.Parameter) ochazuke.Parameter, note func(format string, a ...any)) error {
	if err := try(func() { b.Verb(v.Name, h.Proxy(noun, v), v.Description) }); err != nil {
		return err
	}
	key := r.CanonicalKey(local + "." + v.Name)
	op, ok := r.Operations[key]
	if !ok {
		return fmt.Errorf("the host did not register %s", key)
	}
	if v.Confirm != "" {
		op.Confirm = v.Confirm
	}
	if len(v.Params) > 0 {
		op.Parameters = make(map[string]ochazuke.Parameter, len(v.Params))
		for name, p := range v.Params {
			op.Parameters[name] = param(p)
		}
	}
	for _, name := range v.Positional {
		if _, ok := op.Parameters[name]; !ok {
			note("positional %s names no parameter; mounted without a positional order", name)
			v.Positional = nil
			break
		}
	}
	op.CatalogProxy = true
	op.CatalogGuards = slices.Clone(v.Guards)
	op.Examples = slices.Clone(v.Examples)
	for i := range op.Examples {
		op.Examples[i].Args = gomaps.Clone(op.Examples[i].Args)
	}
	op.Destructive, op.Agent = v.Destructive, v.AgentDeclared
	if op.Agent == "" && v.Agent != "" && v.Agent != (ochazuke.Operation{Destructive: v.Destructive, Confirm: v.Confirm}).AgentRightOf(false) {
		op.Agent = v.Agent
	}
	switch op.Agent {
	case "", ochazuke.AgentAllow, ochazuke.AgentConfirm, ochazuke.AgentNever:
	default:
		note("invalid agent right %q; using the default", op.Agent)
		op.Agent = ""
	}
	op.Positional = slices.Clone(v.Positional)
	op.Quiet, op.Many, op.Repeatable, op.Standard = v.Quiet, v.Many, v.Repeatable, v.Standard
	if v.Result != "" {
		op.Result = v.Result
	}
	if v.Rows != "" {
		op.Rows = nouns[v.Rows]
	}
	switch v.History {
	case "", ochazuke.HistoryAction, ochazuke.HistoryChange, ochazuke.HistoryFetch:
		op.HistoryKind = v.History
	default:
		note("declares history %q, not action, change or fetch; recording it as the default", v.History)
	}
	r.Operations[key] = op
	return nil
}

// mountAlias registers one catalog alias of a mounted verb under the host
// names: its noun (the verb's own, or another mounted noun's) by its host
// name; a bare alias under its noun.
func mountAlias(r *ochazuke.Runtime, a ochazuke.ActionAlias, action string, nouns map[string]string) error {
	noun, ok := nouns[a.Noun]
	if !ok {
		return fmt.Errorf("its noun %s is not mounted", a.Noun)
	}
	alias := ochazuke.ActionAlias{
		Alias: noun + "." + a.Verb, Action: action,
		Since: a.Since, RemoveAfter: a.RemoveAfter, Note: a.Note, Source: a.Source,
		Palette: a.Palette, ParamAliases: slices.Clone(a.ParamAliases),
	}
	if len(a.Args) > 0 {
		alias.Args = make(ochazuke.Arguments, len(a.Args))
		for k, v := range a.Args {
			alias.Args[k] = v
		}
	}
	if alias.Source == "" || alias.Source == ochazuke.AliasFramework {
		alias.Source = ochazuke.AliasApp
	}
	return r.AddActionAlias(alias)
}

// MountParameter is a mounted parameter (or prop parameter) as the host
// sees it, given the catalog nouns mounted (catalog name -> host name): a
// noun reference names the host's noun ("" when that noun is not
// mounted, so it never reaches a host noun of the same name), and a
// choice source (a NOUN.VERB of the extension) is the host's NOUN.VERB of
// a mounted noun, else dropped with its values accepted (Free), as mtm
// and nabe did by hand.
func MountParameter(p ochazuke.Parameter, nouns map[string]string) ochazuke.Parameter {
	if p.Noun != "" {
		p.Noun = nouns[p.Noun]
	}
	if p.Source != "" {
		source := ""
		best := ""
		for from, to := range nouns {
			if rest, ok := strings.CutPrefix(p.Source, from+"."); ok && len(from) > len(best) && rest != "" {
				best, source = from, to+"."+rest
			}
		}
		if source == "" {
			p.Free = true
		}
		p.Source = source
	}
	p.Enum = slices.Clone(p.Enum)
	return p
}

// try runs fn and returns its panic as an error: the runtime's builders
// panic on a bad declaration, which a host mounting data reports instead.
func try(fn func()) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("%v", v)
		}
	}()
	fn()
	return nil
}
