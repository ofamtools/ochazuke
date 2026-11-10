package ochazuke

import (
	"fmt"
	"strings"
)

// The follow verb: a noun with a reference prop (Parameter.Noun, or the
// any-noun "*") gets follow, which opens the referenced thing's index with
// its row selected (OpenIndex), one row in the verb table for every such
// noun, never by name. A verb the application registers as follow wins.

var needRefProp = storeNeed{Doc: "a reference prop (Noun)", Lacks: "it has no reference prop",
	Has: func(_ *Runtime, n *Noun) bool { return len(refProps(n)) > 0 }}

// refProps are the noun's single-thing reference props, in order.
func refProps(n *Noun) []PropSpec {
	return propsOf(n, func(p PropSpec) bool { return p.Noun != "" && p.Items == nil })
}

func refVerbs() []standardVerb {
	return []standardVerb{{
		Verb: "follow", Description: "Go to the thing a reference prop names: its noun's index, its row selected (a free verb of every noun with a reference prop)",
		Kinds: []string{KindCollection, Singleton, Instances}, Needs: []storeNeed{needGet, needRefProp},
		TakesSubject: true, HistoryKind: HistoryAction, Agent: AgentAllow,
		Params: func(n *Noun) map[string]Parameter { return propParam(refProps(n)) }, ParamNames: []string{pathPropParam},
		Run: func(r *Runtime, n *Noun, c Ctx) (any, error) {
			props := refProps(n)
			value, err := thingValue(r, n, c, props)
			if err != nil {
				return nil, err
			}
			prop := props[0]
			if name := StringArg(c.Args, pathPropParam, ""); name != "" {
				prop = *n.prop(name)
			}
			noun, key := prop.Noun, value
			if noun == AnyNoun {
				var ok bool
				if noun, key, ok = SplitAnyRef(value); !ok {
					return nil, fmt.Errorf("%s is not a noun:key reference", value)
				}
			}
			if err := r.OpenIndex(noun, strings.TrimSpace(key)); err != nil {
				return nil, err
			}
			return map[string]any{"noun": r.CanonicalNoun(noun), "key": key}, nil
		},
	}}
}
