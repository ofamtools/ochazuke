package inspect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Catalog is the JSON an ochazuke tool prints for `BIN catalog` (the
// cli.CatalogDoc shape), decoded loosely so a catalog written by an older
// ochazuke (bare framework nouns, no builtin flag) reads too.
type Catalog struct {
	Schema   int               `json:"schema"`
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	State    string            `json:"state"`
	Aliases  map[string]string `json:"aliases"`
	Nouns    []CatNoun         `json:"nouns"`
	Bindings []Binding         `json:"bindings"`
	Pages    []Page            `json:"pages"`
}

type Page struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type CatNoun struct {
	Name  string    `json:"name"`
	Pin   string    `json:"pin,omitempty"`
	Verbs []CatVerb `json:"verbs"`
}

type CatVerb struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"`
	History     string   `json:"history"`
	Params      Params   `json:"params"`
	Positional  []string `json:"positional"`
	Result      string   `json:"result"`
	Confirm     string   `json:"confirm"`
	Rows        string   `json:"rows"`
	Builtin     bool     `json:"builtin"`
	Quiet       bool     `json:"quiet"`
	Repeatable  bool     `json:"repeatable"`
}

type Param struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Source      string   `json:"source,omitempty"`
	Noun        string   `json:"noun,omitempty"`
	Free        bool     `json:"free,omitempty"`
	Positional  int      `json:"positional"` // index in positional, -1 when named only
}

// Params keeps the catalog's parameter order, which a Go map would lose.
type Params []Param

func (p *Params) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("params: want object, got %v", tok)
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var one Param
		if err := dec.Decode(&one); err != nil {
			return err
		}
		one.Name = kt.(string)
		*p = append(*p, one)
	}
	_, err = dec.Token()
	return err
}

type Request struct {
	Verb string         `json:"verb"`
	Noun string         `json:"noun"`
	Args map[string]any `json:"args,omitempty"`
}

type Binding struct {
	Noun     string         `json:"noun"`
	Instance string         `json:"instance,omitempty"`
	Mode     string         `json:"mode,omitempty"`
	Keys     string         `json:"keys,omitempty"`
	Pattern  string         `json:"pattern,omitempty"`
	When     string         `json:"when,omitempty"`
	Hint     string         `json:"hint,omitempty"`
	Help     string         `json:"help,omitempty"`
	Hidden   bool           `json:"hidden,omitempty"`
	Capture  bool           `json:"capture,omitempty"`
	Replay   bool           `json:"replay,omitempty"`
	From     map[string]any `json:"from,omitempty"`
	Request  *Request       `json:"request"`
}

// UserAction is one entry of the user's actions.json (verbs that run commands).
type UserAction struct {
	Noun        string   `json:"noun"`
	Verb        string   `json:"verb"`
	Description string   `json:"description"`
	Command     []string `json:"command,omitempty"`
	Base        []string `json:"base,omitempty"`
	Call        string   `json:"call,omitempty"`
	Confirm     string   `json:"confirm,omitempty"`
	History     string   `json:"history,omitempty"`
	Parameters  Params   `json:"parameters,omitempty"`
	Positional  []string `json:"positional,omitempty"`
}

// Source is one tool's catalog plus how it was found. Bindings, when set,
// are the tool's effective key rows (what config export writes, the live
// bindings.json included) and replace the catalog's default rows.
type Source struct {
	Tool          ToolInfo
	Catalog       *Catalog
	Bindings      []Binding // effective rows; nil means the catalog's own
	BindingSource string
}

type ToolInfo struct {
	Name          string `json:"name"`
	Short         string `json:"short"`
	Bin           string `json:"bin"`
	Version       string `json:"version"`
	Ochazuke      string `json:"ochazuke,omitempty"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	BindingSource string `json:"bindingSource,omitempty"`
	Nouns         int    `json:"nouns"`
	Verbs         int    `json:"verbs"`
	Bindings      int    `json:"bindings"`
	Pages         []Page `json:"pages,omitempty"`
}

// Output model, inlined in the HTML.

type Noun struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Owner    string   `json:"owner"`
	NS       string   `json:"ns"`
	Tools    []string `json:"tools"`
	Aliases  []string `json:"aliases,omitempty"`
	MirrorOf string   `json:"mirrorOf,omitempty"`
	Pin      string   `json:"pin,omitempty"`
	UserOnly bool     `json:"userOnly,omitempty"`
}

type Verb struct {
	ID          string   `json:"id"`
	Noun        string   `json:"noun"` // noun id
	Name        string   `json:"name"`
	Owner       string   `json:"owner"`
	Tools       []string `json:"tools"`
	Description string   `json:"desc,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	History     string   `json:"history,omitempty"`
	Params      []Param  `json:"params,omitempty"`
	Result      string   `json:"result,omitempty"`
	Confirm     string   `json:"confirm,omitempty"`
	Rows        string   `json:"rows,omitempty"`
	Builtin     bool     `json:"builtin,omitempty"`
	Quiet       bool     `json:"quiet,omitempty"`
	Repeatable  bool     `json:"repeatable,omitempty"`
	User        bool     `json:"user,omitempty"`
	Command     string   `json:"command,omitempty"`
}

type KeyRow struct {
	Tool     string         `json:"tool"`
	Scope    string         `json:"scope"` // noun id of the keymap the row sits on
	Instance string         `json:"instance,omitempty"`
	Mode     string         `json:"mode,omitempty"`
	Keys     string         `json:"keys,omitempty"`
	Pattern  string         `json:"pattern,omitempty"`
	When     string         `json:"when,omitempty"`
	Hint     string         `json:"hint,omitempty"`
	Help     string         `json:"help,omitempty"`
	Hidden   bool           `json:"hidden,omitempty"`
	Capture  bool           `json:"capture,omitempty"`
	Replay   bool           `json:"replay,omitempty"`
	ReqNoun  string         `json:"reqNoun,omitempty"` // noun id
	ReqVerb  string         `json:"reqVerb,omitempty"`
	Args     map[string]any `json:"args,omitempty"`
	VerbID   string         `json:"verb,omitempty"` // empty when the request names no known verb
	Unbind   bool           `json:"unbind,omitempty"`
}

// Data is the merged model the page draws, inlined in it as JSON. Host is
// the tool the page was generated by (its tool row is selected first and its
// actions.json verbs are its own).
type Data struct {
	Generated string     `json:"generated"`
	Host      string     `json:"host,omitempty"`
	Tools     []ToolInfo `json:"tools"`
	Nouns     []*Noun    `json:"nouns"`
	Verbs     []*Verb    `json:"verbs"`
	Keys      []KeyRow   `json:"keys"`
	Actions   []string   `json:"actions"` // verb ids that came from actions.json
	ActionsAt string     `json:"actionsPath,omitempty"`
	// Annotations are the notes the Gaps view draws (suggested verbs per
	// noun); nil hides the view.
	Annotations *Annotations `json:"annotations,omitempty"`
	Notes       []string     `json:"notes,omitempty"`
}

const fw = "ochazuke"
const fwPrefix = "ochazuke."

func shortName(tool string) string { return strings.TrimPrefix(tool, "o-") }

// semverKey orders "v0.20.0" above "v0.9.0"; unknown versions sort last.
func semverKey(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var k [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{-1, -1, -1}
		}
		k[i] = n
	}
	return k
}

func namespaced(c *Catalog) bool {
	for _, n := range c.Nouns {
		if strings.HasPrefix(n.Name, fwPrefix) {
			return true
		}
	}
	return false
}

func allBuiltin(n CatNoun) bool {
	if len(n.Verbs) == 0 {
		return false
	}
	for _, v := range n.Verbs {
		if !v.Builtin {
			return false
		}
	}
	return true
}

func anyBuiltin(n CatNoun) bool {
	for _, v := range n.Verbs {
		if v.Builtin {
			return true
		}
	}
	return false
}

// frameworkNames is the set of ochazuke noun names (without the prefix):
// every ochazuke.X in a namespaced catalog, plus every bare noun (not app) of
// an older, un-namespaced catalog whose verbs are all builtin.
func frameworkNames(srcs []Source) map[string]bool {
	set := map[string]bool{}
	for _, s := range srcs {
		if s.Catalog == nil {
			continue
		}
		ns := namespaced(s.Catalog)
		for _, n := range s.Catalog.Nouns {
			switch {
			case strings.HasPrefix(n.Name, fwPrefix):
				set[strings.TrimPrefix(n.Name, fwPrefix)] = true
			case !ns && n.Name != "app" && allBuiltin(n):
				set[n.Name] = true
			}
		}
	}
	return set
}

// resolver maps one catalog's noun names to canonical noun ids.
type resolver struct {
	tool   string
	ns     bool
	fwSet  map[string]bool
	appSet map[string]bool // bare nouns of an old catalog that stay the app's
}

func newResolver(tool string, c *Catalog, fwSet map[string]bool) resolver {
	r := resolver{tool: tool, ns: namespaced(c), fwSet: fwSet, appSet: map[string]bool{}}
	if !r.ns {
		for _, n := range c.Nouns {
			if fwSet[n.Name] && !anyBuiltin(n) {
				r.appSet[n.Name] = true // e.g. o-ai reuses "settings" for its own kind
			}
		}
	}
	return r
}

// canon returns the canonical noun name (ochazuke.X for framework nouns).
func (r resolver) canon(name string) string {
	if name == "" || strings.HasPrefix(name, fwPrefix) || name == "app" {
		return name
	}
	if !r.ns && r.fwSet[name] && !r.appSet[name] {
		return fwPrefix + name
	}
	return name
}

func (r resolver) nounID(name string) string {
	c := r.canon(name)
	if strings.HasPrefix(c, fwPrefix) {
		return fw + ":" + c
	}
	return r.tool + ":" + c
}

func verbID(nounID, verb string) string { return nounID + "#" + verb }

// namespaceOf groups a tool's own noun: app, or ext:<prefix> for a hosted
// extension's mirror nouns (mtm's ai-*, nabe's mtm-*; cli.Namespace's
// ai.agent too). rest is the noun's name without the prefix.
func namespaceOf(name string, extPrefixes []string) (ns, prefix, rest string) {
	if strings.HasPrefix(name, fwPrefix) {
		return fwPrefix, "", ""
	}
	best := ""
	for _, p := range extPrefixes {
		if (strings.HasPrefix(name, p+"-") || strings.HasPrefix(name, p+".")) && len(p) > len(best) {
			best = p
		}
	}
	if best != "" {
		return "ext:" + best, best, name[len(best)+1:]
	}
	return "app", "", ""
}

func addUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func toVerb(id, nounID, owner, tool string, v CatVerb) *Verb {
	pos := map[string]int{}
	for i, p := range v.Positional {
		pos[p] = i
	}
	params := make([]Param, 0, len(v.Params))
	for _, p := range v.Params {
		p.Positional = -1
		if i, ok := pos[p.Name]; ok {
			p.Positional = i
		}
		params = append(params, p)
	}
	return &Verb{
		ID: id, Noun: nounID, Name: v.Name, Owner: owner, Tools: []string{tool},
		Description: v.Description, Kind: v.Kind, History: v.History, Params: params,
		Result: v.Result, Confirm: v.Confirm, Rows: v.Rows, Builtin: v.Builtin,
		Quiet: v.Quiet, Repeatable: v.Repeatable,
	}
}

// Merge folds every tool's catalog into one model. Framework (ochazuke)
// nouns and builtin verbs are kept once, whichever catalogs carry them; the
// newest ochazuke's definition wins. extPrefixes maps a tool to the noun
// prefixes it mounts other tools under.
func Merge(srcs []Source, extPrefixes map[string][]string, actions []UserAction, actionsHost string) *Data {
	fwSet := frameworkNames(srcs)

	// Newest ochazuke first so its framework definitions win; namespaced
	// catalogs are newer than bare ones whatever their build info says.
	order := make([]int, len(srcs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		sa, sb := srcs[order[a]], srcs[order[b]]
		if sa.Catalog == nil || sb.Catalog == nil {
			return sa.Catalog != nil
		}
		na, nb := namespaced(sa.Catalog), namespaced(sb.Catalog)
		if na != nb {
			return na
		}
		ka, kb := semverKey(sa.Tool.Ochazuke), semverKey(sb.Tool.Ochazuke)
		return ka[0] > kb[0] || ka[0] == kb[0] && (ka[1] > kb[1] || ka[1] == kb[1] && ka[2] > kb[2])
	})

	d := &Data{Host: actionsHost}
	nouns := map[string]*Noun{}
	verbs := map[string]*Verb{}
	var nounOrder, verbOrder []string
	ensureNoun := func(id, name, owner, ns, tool string) *Noun {
		n, ok := nouns[id]
		if !ok {
			n = &Noun{ID: id, Name: name, Owner: owner, NS: ns}
			nouns[id] = n
			nounOrder = append(nounOrder, id)
		}
		if tool != "" {
			n.Tools = addUnique(n.Tools, tool)
		}
		return n
	}
	resolvers := map[string]resolver{}

	for _, i := range order {
		s := srcs[i]
		if s.Catalog == nil {
			continue
		}
		tool := s.Tool.Name
		r := newResolver(tool, s.Catalog, fwSet)
		resolvers[tool] = r
		for _, cn := range s.Catalog.Nouns {
			canonName := r.canon(cn.Name)
			for _, cv := range cn.Verbs {
				// An old catalog's mixed noun: builtin verbs belong to the
				// framework noun, the app's extras ride on it.
				nid := r.nounID(cn.Name)
				name := canonName
				owner := tool
				if cv.Builtin && cn.Name != "app" {
					owner = fw
				}
				var nounOwner, ns, prefix, rest string
				if strings.HasPrefix(name, fwPrefix) {
					nounOwner, ns = fw, fwPrefix
				} else {
					nounOwner = tool
					ns, prefix, rest = namespaceOf(name, extPrefixes[tool])
				}
				n := ensureNoun(nid, name, nounOwner, ns, tool)
				if cn.Pin != "" {
					n.Pin = cn.Pin
				}
				if prefix != "" && n.MirrorOf == "" {
					n.MirrorOf = prefix + "\x00" + rest
				}
				vid := verbID(nid, cv.Name)
				if v, ok := verbs[vid]; ok {
					v.Tools = addUnique(v.Tools, tool)
					continue
				}
				verbs[vid] = toVerb(vid, nid, owner, tool, cv)
				verbOrder = append(verbOrder, vid)
			}
			if len(cn.Verbs) == 0 {
				ns, _, _ := namespaceOf(canonName, extPrefixes[tool])
				owner := tool
				if strings.HasPrefix(canonName, fwPrefix) {
					owner = fw
				}
				ensureNoun(r.nounID(cn.Name), canonName, owner, ns, tool)
			}
		}
		for short, full := range s.Catalog.Aliases {
			if n, ok := nouns[r.nounID(full)]; ok {
				n.Aliases = addUnique(n.Aliases, short)
			}
		}
	}

	// Resolve mirrors: nabe's mtm-pane is mtm's pane; mtm's ai-agent is o-ai's agent.
	byShort := map[string]string{}
	for _, s := range srcs {
		byShort[shortName(s.Tool.Name)] = s.Tool.Name
		byShort[s.Tool.Name] = s.Tool.Name
	}
	for _, id := range nounOrder {
		n := nouns[id]
		if n.MirrorOf == "" {
			continue
		}
		parts := strings.SplitN(n.MirrorOf, "\x00", 2)
		n.MirrorOf = ""
		if t, ok := byShort[parts[0]]; ok {
			if _, ok := nouns[t+":"+parts[1]]; ok {
				n.MirrorOf = t + ":" + parts[1]
			}
		}
	}

	// User actions (actions.json) are verbs of the host that reads them.
	if len(actions) > 0 {
		r, ok := resolvers[actionsHost]
		if !ok {
			r = resolver{tool: actionsHost, fwSet: fwSet, appSet: map[string]bool{}, ns: true}
		}
		for _, a := range actions {
			nid := r.nounID(a.Noun)
			name := r.canon(a.Noun)
			owner, ns := actionsHost, "app"
			if strings.HasPrefix(name, fwPrefix) {
				owner, ns = fw, fwPrefix
			} else {
				ns, _, _ = namespaceOf(name, extPrefixes[actionsHost])
			}
			_, existed := nouns[nid]
			n := ensureNoun(nid, name, owner, ns, actionsHost)
			if !existed {
				n.UserOnly = true
			}
			vid := verbID(nid, a.Verb)
			command := ""
			switch {
			case len(a.Command) > 0:
				command = strings.Join(a.Command, " ")
			case len(a.Base) > 0 || a.Call != "":
				command = strings.TrimSpace(strings.Join(a.Base, " ") + " " + a.Call)
			}
			// A host that installed its actions lists them in its own
			// catalog: that verb is the action, marked, not a second one.
			if v, ok := verbs[vid]; ok && !v.Builtin && !v.User {
				v.User, v.Owner, v.Command = true, "user", command
				d.Actions = append(d.Actions, vid)
				continue
			}
			cv := CatVerb{Name: a.Verb, Description: a.Description, Kind: "action", History: a.History,
				Params: a.Parameters, Positional: a.Positional, Confirm: a.Confirm}
			v := toVerb(vid, nid, "user", actionsHost, cv)
			v.User, v.Command = true, command
			if _, dup := verbs[vid]; dup {
				vid += "@user"
				v.ID = vid
			}
			verbs[vid] = v
			verbOrder = append(verbOrder, vid)
			d.Actions = append(d.Actions, vid)
		}
	}

	// Key rows, per tool (each tool's effective rows are its own).
	for _, s := range srcs {
		if s.Catalog == nil {
			continue
		}
		r := resolvers[s.Tool.Name]
		rows := s.Bindings
		if rows == nil {
			rows = s.Catalog.Bindings
		}
		for _, b := range rows {
			k := KeyRow{Tool: s.Tool.Name, Scope: r.nounID(b.Noun), Instance: b.Instance, Mode: b.Mode,
				Keys: b.Keys, Pattern: b.Pattern, When: b.When, Hint: b.Hint, Help: b.Help,
				Hidden: b.Hidden, Capture: b.Capture, Replay: b.Replay}
			if b.Noun == "" && b.Instance != "" {
				k.Scope = ""
			}
			if b.Request == nil {
				k.Unbind = true
			} else {
				k.ReqNoun = r.nounID(b.Request.Noun)
				k.ReqVerb = b.Request.Verb
				k.Args = b.Request.Args
				if _, ok := verbs[verbID(k.ReqNoun, k.ReqVerb)]; ok {
					k.VerbID = verbID(k.ReqNoun, k.ReqVerb)
				}
			}
			d.Keys = append(d.Keys, k)
		}
	}

	for _, id := range nounOrder {
		n := nouns[id]
		sort.Strings(n.Aliases)
		d.Nouns = append(d.Nouns, n)
	}
	sort.SliceStable(d.Nouns, func(a, b int) bool {
		if d.Nouns[a].Owner != d.Nouns[b].Owner {
			return d.Nouns[a].Owner < d.Nouns[b].Owner
		}
		return d.Nouns[a].Name < d.Nouns[b].Name
	})
	for _, id := range verbOrder {
		d.Verbs = append(d.Verbs, verbs[id])
	}
	sort.SliceStable(d.Verbs, func(a, b int) bool {
		if d.Verbs[a].Noun != d.Verbs[b].Noun {
			return d.Verbs[a].Noun < d.Verbs[b].Noun
		}
		return d.Verbs[a].Name < d.Verbs[b].Name
	})

	// Tool rows, with per-tool counts and the framework pseudo-tool.
	best := ""
	for _, s := range srcs {
		t := s.Tool
		t.Short = shortName(t.Name)
		if s.Catalog != nil {
			t.Pages = s.Catalog.Pages
			for _, n := range d.Nouns {
				if contains(n.Tools, t.Name) {
					t.Nouns++
				}
			}
			for _, v := range d.Verbs {
				if contains(v.Tools, t.Name) {
					t.Verbs++
				}
			}
			for _, k := range d.Keys {
				if k.Tool == t.Name {
					t.Bindings++
				}
			}
			if t.BindingSource == "" {
				t.BindingSource = s.BindingSource
			}
			if t.BindingSource == "" {
				t.BindingSource = "catalog"
			}
			if t.Ochazuke != "" && (best == "" || less(semverKey(best), semverKey(t.Ochazuke))) {
				best = t.Ochazuke
			}
		}
		d.Tools = append(d.Tools, t)
	}
	fwTool := ToolInfo{Name: fw, Short: fw, Version: best, Ochazuke: best, Role: "framework", Status: "merged",
		BindingSource: "per tool"}
	for _, n := range d.Nouns {
		if n.Owner == fw {
			fwTool.Nouns++
		}
	}
	for _, v := range d.Verbs {
		if v.Owner == fw {
			fwTool.Verbs++
		}
	}
	d.Tools = append(d.Tools, fwTool)
	return d
}

func less(a, b [3]int) bool {
	return a[0] < b[0] || a[0] == b[0] && (a[1] < b[1] || a[1] == b[1] && a[2] < b[2])
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
