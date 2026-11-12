package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// KeysGraphDepthSetting is how many hops of relations key dispatch follows
// from each level of the focus path to find a subject: 0 reads the focus
// path alone, 1 the things a level names directly, and so on.
const KeysGraphDepthSetting = "keys.graph-depth"

// KeysGraphFanOutSetting bounds the neighbours read for one thing in that walk.
const KeysGraphFanOutSetting = "keys.graph-fan-out"

// reached is a thing the graph walk found from a focus-path level: its
// distance in hops, the props and relation kinds that led to it (Via), and
// the live instance standing for it (inst, "" when none does).
type reached struct {
	Noun, Key, Label string
	Dist             int
	Via              []string
	inst             string
}

func (a reached) ref() string { return AnyRef(a.Noun, a.Key) }

// reachWalk is the cached result of one walk and what it depends on.
type reachWalk struct {
	nodes      []reached
	uncachable bool
	instGen    uint64
	relGen     uint64
	live       map[string]string // live instance read → its association fingerprint
	tokens     map[string]string // local store read → its change token
}

type reachKey struct {
	from       string
	depth, fan int
}

// graphDepth is the hops key dispatch follows. subject.from-association off
// keeps the focus path alone.
func (r *Runtime) graphDepth() int {
	if !r.Settings().Bool(SubjectFromAssociationSetting) {
		return 0
	}
	return max(0, r.Settings().Int(KeysGraphDepthSetting))
}

// liveRefs are the things inst names through its association props, in prop
// order: the edges a live instance holds without any store being read.
func (r *Runtime) liveRefs(inst *Instance) []outwardRef {
	if inst == nil || r.Nouns[inst.Noun] == nil {
		return nil
	}
	var out []outwardRef
	for _, p := range r.Nouns[inst.Noun].Props {
		if !p.Association {
			continue
		}
		var values []any
		switch v := dataField(inst.Data, p.Name).(type) {
		case []any:
			values = v
		case []string:
			for _, key := range v {
				values = append(values, key)
			}
		default:
			values = []any{v}
		}
		for _, value := range values {
			if !emptyProp(value) {
				out = append(out, outwardRef{Prop: p, Noun: r.CanonicalNoun(p.Noun), Key: fmt.Sprint(value)})
			}
		}
	}
	return out
}

func (r *Runtime) liveFingerprint(inst *Instance) string {
	var b strings.Builder
	for _, e := range r.liveRefs(inst) {
		fmt.Fprintf(&b, "%s=%s:%s;", e.Prop.Name, e.Noun, e.Key)
	}
	return b.String()
}

// reachFrom is the things reachable from the focus-path level id, nearest
// first (distance, then discovery order: association props, reference props,
// relation rows), up to keys.graph-depth hops and keys.graph-fan-out
// neighbours per thing. The result is cached on the level, the settings, the
// instance set, the relation generation, each live instance read and the
// change token of each store read; a store with no token is never cached.
func (r *Runtime) reachFrom(id string) []reached {
	depth := r.graphDepth()
	if depth == 0 || r.Instances[id] == nil || id == "app" {
		return nil
	}
	key := reachKey{id, depth, max(1, r.Settings().Int(KeysGraphFanOutSetting))}
	if w := r.reachCache[key]; w != nil && r.reachValid(w) {
		return w.nodes
	}
	// A store read can itself run dispatch (a facts store lists the bindings);
	// a walk begun inside a walk finds nothing rather than recursing.
	if r.reaching {
		return nil
	}
	r.reaching = true
	w := r.walkReach(id, key)
	r.reaching = false
	if r.reachCache == nil {
		r.reachCache = map[reachKey]*reachWalk{}
	}
	if w.uncachable {
		delete(r.reachCache, key)
	} else {
		r.reachCache[key] = w
	}
	return w.nodes
}

func (r *Runtime) reachValid(w *reachWalk) bool {
	if w.instGen != r.renderCache.instances.generation || w.relGen != r.relationGen() {
		return false
	}
	for id, fp := range w.live {
		if r.Instances[id] == nil || r.liveFingerprint(r.Instances[id]) != fp {
			return false
		}
	}
	for noun, token := range w.tokens {
		if n := r.Noun(noun); n == nil || n.Store == nil || storeToken(n.Store, r.reachCtx(noun)) != token {
			return false
		}
	}
	return true
}

func (r *Runtime) relationGen() uint64 {
	if r.relations == nil {
		return 0
	}
	return r.relations.gen
}

func (r *Runtime) reachCtx(noun string) Ctx {
	return Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), Noun: noun}
}

// walkReach is a breadth-first walk by graph distance. A thing is entered
// once, at its shortest distance, and a thing already on the walk is never
// entered again, so a cycle or a self reference ends. Only identical things
// (the same noun and key) collapse: two neighbours that each name a
// different project both leave a project in the result. A remote store is
// never called here (a process call must not happen inside key dispatch):
// such a thing is listed by its key and has no edges of its own.
func (r *Runtime) walkReach(from string, key reachKey) *reachWalk {
	w := &reachWalk{instGen: r.renderCache.instances.generation, relGen: r.relationGen(), live: map[string]string{}, tokens: map[string]string{}}
	start := r.Instances[from]
	startThing := r.instanceThing(start)
	seen := map[string]bool{AnyRef(start.Noun, startThing.Key): true, "instance:" + from: true}
	type item struct {
		noun, key string
		thing     Thing
		inst      string
		dist      int
		via       []string
	}
	queue := []item{{noun: start.Noun, key: startThing.Key, inst: from}}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if at.dist >= key.depth {
			continue
		}
		type edge struct{ noun, key, via string }
		var edges []edge
		if inst := r.Instances[at.inst]; inst != nil {
			w.live[inst.ID] = r.liveFingerprint(inst)
			for _, e := range r.liveRefs(inst) {
				edges = append(edges, edge{e.Noun, e.Key, e.Prop.Name})
			}
		}
		if n := r.Noun(at.noun); n != nil {
			thing := at.thing
			if thing == nil && at.dist == 0 {
				thing = r.reachGet(w, n, at.key)
			}
			for _, e := range r.outwardRefs(n, thing) {
				edges = append(edges, edge{e.Noun, e.Key, e.Prop.Name})
			}
		}
		if s := r.RelationStore(); s != nil {
			this := AnyRef(at.noun, at.key)
			if rels, err := r.RelationsOf(RelationQuery{Thing: this}); err == nil {
				for _, x := range rels {
					if noun, k, ok := SplitAnyRef(x.Other(this)); ok && r.Noun(noun) != nil {
						edges = append(edges, edge{r.CanonicalNoun(noun), k, x.Kind})
					}
				}
			}
		}
		if len(edges) > key.fan {
			edges = edges[:key.fan]
		}
		for _, e := range edges {
			ref := AnyRef(e.noun, e.key)
			if seen[ref] {
				continue
			}
			seen[ref] = true
			n := r.Noun(e.noun)
			next := item{noun: e.noun, key: e.key, dist: at.dist + 1, via: append(slices.Clone(at.via), e.via)}
			next.inst = r.associationInstance(e.noun, e.key)
			if next.inst != "" {
				if seen["instance:"+next.inst] {
					continue // the same live thing under another key
				}
				seen["instance:"+next.inst] = true
			}
			label := e.key
			if next.inst != "" {
				label = r.instanceThing(r.Instances[next.inst]).Label
			}
			if n != nil {
				if next.thing = r.reachGet(w, n, e.key); next.thing != nil {
					label = n.displayLabel(func(name string) any { return next.thing[name] })
					if label == "" {
						label = e.key
					}
				}
			}
			queue = append(queue, next)
			w.nodes = append(w.nodes, reached{Noun: e.noun, Key: e.key, Label: label, Dist: next.dist, Via: next.via, inst: next.inst})
		}
	}
	return w
}

// reachGet reads a thing from a local store with a token, noting the token.
// A store that calls out of the process (Remote), a store without Get, and a
// failed read give nil; a store without a change token makes the walk
// uncachable.
func (r *Runtime) reachGet(w *reachWalk, n *Noun, key string) Thing {
	if n.Store == nil || n.Store.Get == nil || n.Store.Remote || n.keyProp() == nil {
		return nil
	}
	c := r.reachCtx(n.Name)
	t, err := n.Store.Get(c, key)
	if err != nil {
		return nil
	}
	if n.Store.ChangeToken == nil {
		w.uncachable = true
	} else if _, ok := w.tokens[n.Name]; !ok {
		w.tokens[n.Name] = storeToken(n.Store, c)
	}
	return t
}

// nearestReached is the nearest thing of noun from the level id and past it:
// the level itself, then what the walk finds, nearest distance only.
func (r *Runtime) nearestReached(noun, id string) []reached {
	var best []reached
	for _, a := range r.reachFrom(id) {
		if a.Noun != noun || len(best) > 0 && a.Dist > best[0].Dist {
			continue
		}
		best = append(best, a)
	}
	return best
}

// reachDependency is what a cached binding scan read through nearestOf: the
// scan is valid while asking again gives the same answer.
type reachDependency struct {
	noun, focus string
	distant     bool
	answer      string
}
